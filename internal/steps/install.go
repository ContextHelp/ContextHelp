package steps

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Install limits for a step package.
const (
	// MaxArchiveBytes caps a step archive, compressed.
	MaxArchiveBytes = 64 << 20
	// maxUnpackedBytes caps the files an archive unpacks to.
	maxUnpackedBytes = 256 << 20
	maxArchiveFiles  = 4096
)

// Install and uninstall outcomes callers map to responses.
var (
	ErrStepExists        = errors.New("step already installed")
	ErrStepNotFound      = errors.New("step not installed")
	ErrUntrustedRegistry = errors.New("registry is untrusted")
	ErrDigestMismatch    = errors.New("archive digest mismatch")
	ErrNotInRegistry     = errors.New("step not in registry")
)

var stepNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidateStepName accepts a lowercase name of letters, digits, '-' and
// '_' (it names the step's directory).
func ValidateStepName(name string) error {
	if !stepNameRE.MatchString(name) {
		return invalid("step name %q: want 1-64 lowercase letters, digits, '-' or '_', starting with a letter or digit", name)
	}
	return nil
}

// Installer installs steps into Dir and records them in the steps table.
type Installer struct {
	// Dir is the steps directory (steps.path); each step gets Dir/<name>.
	Dir   string
	Store storage.StorageDriver
	// Discovery fetches registry manifests and archives.
	Discovery *StepDiscovery
	// Reserved reports names a step may not take (built-in steps).
	Reserved func(string) bool
	// Now stamps install times; nil means time.Now.
	Now func() time.Time
}

// InstallArchive installs the step packaged in archive (a gzipped tar
// of the step directory, SKILL.md at its root) under name. source is
// recorded as the step's source; trust is the trust_level of the
// registry it came from, empty for a local package.
func (in *Installer) InstallArchive(ctx context.Context, name string, archive []byte, source string, trust config.RegistryTrustLevel) (*storage.RegisteredStep, error) {
	if err := ValidateStepName(name); err != nil {
		return nil, err
	}
	if in.Reserved != nil && in.Reserved(name) {
		return nil, invalid("step name %q is a built-in step", name)
	}
	if in.Dir == "" {
		return nil, errors.New("steps directory not configured (steps.path)")
	}
	if _, err := in.Store.Steps().Get(ctx, name); err == nil {
		return nil, fmt.Errorf("%w: %q (dpkms pipeline step uninstall %s first)", ErrStepExists, name, name)
	} else if !errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("look up step: %w", err)
	}
	if err := os.MkdirAll(in.Dir, 0o750); err != nil {
		return nil, fmt.Errorf("steps directory: %w", err)
	}
	final := filepath.Join(in.Dir, name)
	if _, err := os.Lstat(final); err == nil {
		return nil, fmt.Errorf("%w: %s exists but no step %q is recorded; move it aside", ErrStepExists, final, name)
	}

	stage, err := os.MkdirTemp(in.Dir, ".install-"+name+"-")
	if err != nil {
		return nil, fmt.Errorf("stage step: %w", err)
	}
	defer os.RemoveAll(stage)

	meta, err := checkPackage(archive, stage, name, trust)
	if err != nil {
		return nil, err
	}

	// Readable by the unprivileged user container isolation runs as.
	if err := os.Chmod(stage, 0o755); err != nil { // #nosec G302 -- step files are not secret; containers read them as another uid
		return nil, fmt.Errorf("stage step: %w", err)
	}
	if err := os.Rename(stage, final); err != nil {
		return nil, fmt.Errorf("install step: %w", err)
	}
	now := time.Now
	if in.Now != nil {
		now = in.Now
	}
	t := now().UTC().Truncate(time.Second)
	rs := &storage.RegisteredStep{Name: name, Source: source, Path: final, Metadata: meta, InstalledAt: t, UpdatedAt: t}
	if err := in.Store.Steps().Create(ctx, rs); err != nil {
		_ = os.RemoveAll(final)
		return nil, fmt.Errorf("record step: %w", err)
	}
	return rs, nil
}

// checkPackage unpacks archive into stage and checks it is the step
// name: SKILL.md at its root naming it, a parseable execution block, an
// executable regular file, an image when it must run in a container.
// It records the executable's digest and trust in the metadata.
func checkPackage(archive []byte, stage, name string, trust config.RegistryTrustLevel) (*storage.StepMetadata, error) {
	if err := unpack(archive, stage); err != nil {
		return nil, err
	}
	skill, err := os.ReadFile(filepath.Join(stage, "SKILL.md")) // #nosec G304 -- staged under the steps directory
	if err != nil {
		return nil, invalid("package has no SKILL.md at its root")
	}
	meta, err := ParseSkill(skill)
	if err != nil {
		return nil, err
	}
	if meta.Name != name {
		return nil, invalid("SKILL.md names the step %q, not %q", meta.Name, name)
	}
	exePath := filepath.Join(stage, filepath.FromSlash(meta.Execution.Executable))
	fi, err := os.Lstat(exePath)
	if err != nil {
		return nil, invalid("executable %s not in the package", meta.Execution.Executable)
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return nil, invalid("executable %s is not an executable regular file (chmod +x it)", meta.Execution.Executable)
	}
	digest, err := fileSHA256(exePath)
	if err != nil {
		return nil, fmt.Errorf("hash executable: %w", err)
	}
	meta.Execution.ExecutableSHA256 = digest
	meta.Execution.TrustLevel = string(trust)
	if EffectiveIsolation(meta.Execution, "") == storage.StepIsolationContainer && meta.Execution.ContainerImage == "" {
		return nil, invalid("step must run under container isolation (sandboxed registry or minimum_isolation) but SKILL.md names no execution.container_image")
	}
	return meta, nil
}

// InstallFromRegistry installs name from the registry at reg.URL. The
// registry's trust_level gates it: untrusted (the default) refuses,
// sandboxed installs the step pinned to container isolation, trusted
// installs it as is. The manifest must declare the archive's sha256;
// a download that does not match it is refused before anything is
// unpacked.
func (in *Installer) InstallFromRegistry(ctx context.Context, name string, reg config.RegistryConfig) (*storage.RegisteredStep, error) {
	if err := ValidateStepName(name); err != nil {
		return nil, err
	}
	trust := reg.EffectiveTrustLevel()
	if trust == config.RegistryTrustLevelUntrusted {
		return nil, fmt.Errorf("%w: %s: steps run code, so an untrusted registry cannot install them; "+
			"set trust_level: trusted (runs as installed) or sandboxed (container isolation only) on its registries entry in dpkms.yaml",
			ErrUntrustedRegistry, reg.URL)
	}
	manifest, err := in.Discovery.FetchRegistryManifest(ctx, reg.URL)
	if err != nil {
		return nil, err
	}
	var ms *storage.ManifestStep
	for i := range manifest.Steps {
		if manifest.Steps[i].Name == name {
			ms = &manifest.Steps[i]
			break
		}
	}
	if ms == nil {
		return nil, fmt.Errorf("%w: %q at %s", ErrNotInRegistry, name, reg.URL)
	}
	want := strings.ToLower(ms.SHA256)
	if len(want) != 64 || strings.Trim(want, "0123456789abcdef") != "" {
		return nil, invalid("registry manifest gives step %q no sha256 (64 hex digits) for its archive", name)
	}
	archive, err := in.Discovery.DownloadArchive(ctx, reg.URL, ms.Archive)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("%w: step %q archive sha256 %s, manifest declares %s", ErrDigestMismatch, name, got, want)
	}
	return in.InstallArchive(ctx, name, archive, "registry:"+reg.URL, trust)
}

// Uninstall removes the step's directory and its steps-table row. Files
// are removed only from the step's own directory under Dir.
func (in *Installer) Uninstall(ctx context.Context, name string) error {
	rs, err := in.Store.Steps().Get(ctx, name)
	if errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("%w: %q", ErrStepNotFound, name)
	}
	if err != nil {
		return fmt.Errorf("look up step: %w", err)
	}
	if in.Dir != "" && rs.Path != "" && ValidateStepName(name) == nil {
		own := filepath.Join(in.Dir, name)
		if filepath.Clean(rs.Path) == own {
			if err := os.RemoveAll(own); err != nil {
				return fmt.Errorf("remove step files: %w", err)
			}
		}
	}
	return in.Store.Steps().Unregister(ctx, name)
}

// unpack extracts a gzipped tar into dir: regular files and directories
// only, every path inside dir, within the size and count caps.
func unpack(archive []byte, dir string) error {
	if len(archive) > MaxArchiveBytes {
		return invalid("archive is %d bytes; the limit is %d", len(archive), MaxArchiveBytes)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return invalid("archive is not gzip: %v", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	var total int64
	files := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return invalid("archive: %v", err)
		}
		files++
		if files > maxArchiveFiles {
			return invalid("archive has more than %d entries", maxArchiveFiles)
		}
		name := strings.TrimPrefix(h.Name, "./")
		if name == "" || name == "." {
			continue
		}
		rel, err := cleanRelPath(strings.TrimSuffix(name, "/"))
		if err != nil {
			return invalid("archive entry: %v", err)
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return err
			}
		case tar.TypeReg:
			if h.Size < 0 || total+h.Size > maxUnpackedBytes {
				return invalid("archive unpacks to more than %d bytes", maxUnpackedBytes)
			}
			total += h.Size
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return err
			}
			mode := fs.FileMode(0o644)
			if h.FileInfo().Mode().Perm()&0o111 != 0 {
				mode = 0o755
			}
			if err := writeEntry(target, tr, h.Size, mode); err != nil {
				return err
			}
		default:
			return invalid("archive entry %s: only regular files and directories are allowed (no links or devices)", path.Clean(name))
		}
	}
}

func writeEntry(target string, r io.Reader, size int64, mode fs.FileMode) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode) // #nosec G304 -- target checked to stay inside the staging dir
	if err != nil {
		return invalid("archive entry %s: %v", filepath.Base(target), err)
	}
	if _, err := io.CopyN(f, r, size); err != nil {
		f.Close()
		return invalid("archive entry %s: %v", filepath.Base(target), err)
	}
	return f.Close()
}

// PackDir packs a step directory into the archive InstallArchive takes:
// regular files and directories only; a symlink or other special file
// is an error. It is what `dpkms pipeline step install --path` sends.
func PackDir(dir string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	var total int64
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		switch {
		case info.IsDir():
			return tw.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0o755})
		case info.Mode().IsRegular():
			total += info.Size()
			if total > maxUnpackedBytes {
				return fmt.Errorf("%s holds more than %d bytes", dir, maxUnpackedBytes)
			}
			mode := int64(0o644)
			if info.Mode().Perm()&0o111 != 0 {
				mode = 0o755
			}
			if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: mode, Size: info.Size()}); err != nil {
				return err
			}
			// Through the root: a path swapped for a symlink cannot escape dir.
			f, err := root.Open(rel)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(tw, f)
			return err
		default:
			return fmt.Errorf("%s: only regular files and directories can be installed (no symlinks)", name)
		}
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	if buf.Len() > MaxArchiveBytes {
		return nil, fmt.Errorf("%s packs to %d bytes; the limit is %d", dir, buf.Len(), MaxArchiveBytes)
	}
	return buf.Bytes(), nil
}
