package viewer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"slices"
	"strings"
	"time"
)

// ObjectAction is what the page offers for a clicked object node.
type ObjectAction string

const (
	// ObjectCopyCLI shows the object id with a copyable `ctxt show <id>`.
	ObjectCopyCLI ObjectAction = "copy-cli"
	// ObjectLink shows a link built from Config.ObjectHref.
	ObjectLink ObjectAction = "link"
)

// IDPlaceholder is replaced, in Config.ObjectHref, by the URL-encoded
// object id.
const IDPlaceholder = "{id}"

// Config is how a host tells the page where its graph document lives and
// what a click on an object offers. The page reads it from a JSON element
// the host injects (see AssetsWith); the page's own URL never chooses
// either target, so a crafted link cannot point the viewer elsewhere.
// Both targets must resolve on the page's own origin.
type Config struct {
	// DataURL is the graph document the page fetches: a path, absolute or
	// relative to the page, on the page's origin. Default DataFile.
	DataURL string `json:"dataUrl"`
	// ForwardQuery appends the page's query parameters to DataURL, except
	// those DataURL already sets. A page at /ui/searchgraph/?q=x with
	// DataURL /api/v1/search/graph fetches /api/v1/search/graph?q=x.
	ForwardQuery bool `json:"forwardQuery"`
	// ObjectAction is ObjectCopyCLI (default) or ObjectLink.
	ObjectAction ObjectAction `json:"objectAction"`
	// ObjectHref is the link for ObjectLink: a path on the page's origin
	// containing IDPlaceholder, e.g. "/ui/objects/{id}".
	ObjectHref string `json:"objectHref,omitempty"`
}

// DefaultConfig is what the page does with no injected config: fetch
// DataFile next to the page and offer `ctxt show <id>` for objects.
func DefaultConfig() Config {
	return Config{DataURL: DataFile, ObjectAction: ObjectCopyCLI}
}

// ErrInvalidConfig reports a Config the page would refuse.
var ErrInvalidConfig = errors.New("invalid search graph viewer config")

// Validate reports whether the page accepts c. The page applies the same
// same-origin checks when it resolves each URL; this catches a bad host
// config at startup instead of on first load.
func (c Config) Validate() error {
	if err := checkSameOrigin(c.DataURL); err != nil {
		return fmt.Errorf("%w: dataUrl %q: %w", ErrInvalidConfig, c.DataURL, err)
	}
	switch c.ObjectAction {
	case ObjectCopyCLI:
	case ObjectLink:
		if !strings.Contains(c.ObjectHref, IDPlaceholder) {
			return fmt.Errorf("%w: objectHref %q lacks %s", ErrInvalidConfig, c.ObjectHref, IDPlaceholder)
		}
		if err := checkSameOrigin(strings.ReplaceAll(c.ObjectHref, IDPlaceholder, "id")); err != nil {
			return fmt.Errorf("%w: objectHref %q: %w", ErrInvalidConfig, c.ObjectHref, err)
		}
	default:
		return fmt.Errorf("%w: unknown objectAction %q", ErrInvalidConfig, c.ObjectAction)
	}
	return nil
}

// checkSameOrigin accepts a non-empty relative reference that a browser
// resolves on the page's origin: no scheme, no authority, and none of the
// characters browsers rewrite before parsing (a backslash reads as a
// slash, so "/\host" is protocol-relative; tabs and newlines are
// stripped, so "/\n/host" is too).
func checkSameOrigin(ref string) error {
	if ref == "" {
		return errors.New("empty")
	}
	for _, r := range ref {
		if r == '\\' || r <= ' ' || r == 0x7f {
			return fmt.Errorf("contains %q", r)
		}
	}
	if strings.HasPrefix(ref, "//") {
		return errors.New("protocol-relative URL")
	}
	u, err := url.Parse(ref)
	if err != nil {
		return err
	}
	if u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" {
		return errors.New("not a same-origin path")
	}
	return nil
}

// configMarker is where Index inserts the config element: just before the
// viewer script, which reads it on start. Tests pin it to one match.
const configMarker = scriptSrcTag

// Index returns IndexFile carrying cfg. The config is embedded in a
// <script type="application/json" id="viewer-config"> element, escaped
// the way Standalone escapes the graph document, so no value can close
// the element; the page's Content Security Policy is untouched, since a
// JSON element never executes.
func Index(cfg Config) ([]byte, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("viewer: encode config: %w", err)
	}
	var data bytes.Buffer
	json.HTMLEscape(&data, raw)

	page, err := fs.ReadFile(Assets(), IndexFile)
	if err != nil {
		return nil, fmt.Errorf("viewer: read embedded %s: %w", IndexFile, err)
	}
	if n := bytes.Count(page, []byte(configMarker)); n != 1 {
		return nil, fmt.Errorf("viewer: %s: expected one script placeholder, found %d", IndexFile, n)
	}
	elem := concat(`<script type="application/json" id="viewer-config">`, data.Bytes(), "</script>\n"+configMarker)
	return bytes.Replace(page, []byte(configMarker), elem, 1), nil
}

// AssetsWith returns the files of Assets() with IndexFile replaced by
// Index(cfg), for a host that serves the viewer under its own routes,
// e.g. with http.FileServerFS. Hosts that serve Assets() unchanged, like
// the ctxt CLI, get DefaultConfig.
func AssetsWith(cfg Config) (fs.FS, error) {
	index, err := Index(cfg)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	err = fs.WalkDir(Assets(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(Assets(), name)
		files[name] = b
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("viewer: read embedded assets: %w", err)
	}
	files[IndexFile] = index
	return memFS(files), nil
}

// memFS is a flat, read-only in-memory file system.
type memFS map[string][]byte

func (m memFS) Open(name string) (fs.File, error) {
	if name == "." {
		return &memDir{fsys: m}, nil
	}
	b, ok := m[name]
	if !ok || !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &memFile{info: memInfo{name: name, size: int64(len(b))}, r: bytes.NewReader(b)}, nil
}

func (m memFS) ReadFile(name string) ([]byte, error) {
	b, ok := m[name]
	if !ok || !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	return bytes.Clone(b), nil
}

func (m memFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return m.entries(), nil
}

func (m memFS) entries() []fs.DirEntry {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	slices.Sort(names)
	out := make([]fs.DirEntry, len(names))
	for i, n := range names {
		out[i] = fs.FileInfoToDirEntry(memInfo{name: n, size: int64(len(m[n]))})
	}
	return out
}

type memInfo struct {
	name string
	size int64
	dir  bool
}

func (i memInfo) Name() string       { return i.name }
func (i memInfo) Size() int64        { return i.size }
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return i.dir }
func (i memInfo) Sys() any           { return nil }
func (i memInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

type memFile struct {
	info memInfo
	r    *bytes.Reader
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *memFile) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *memFile) Close() error               { return nil }

// Seek lets http.FileServerFS serve ranges and sniff content.
func (f *memFile) Seek(off int64, whence int) (int64, error) { return f.r.Seek(off, whence) }

// ReadAt matches embed's files, which also implement io.ReaderAt.
func (f *memFile) ReadAt(p []byte, off int64) (int, error) { return f.r.ReadAt(p, off) }

type memDir struct {
	fsys memFS
	rest []fs.DirEntry
	read bool
}

func (d *memDir) Stat() (fs.FileInfo, error) { return memInfo{name: ".", dir: true}, nil }
func (d *memDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: ".", Err: fs.ErrInvalid}
}
func (d *memDir) Close() error { return nil }

func (d *memDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if !d.read {
		d.rest, d.read = d.fsys.entries(), true
	}
	if n <= 0 {
		out := d.rest
		d.rest = nil
		return out, nil
	}
	if len(d.rest) == 0 {
		return nil, io.EOF
	}
	n = min(n, len(d.rest))
	out := d.rest[:n]
	d.rest = d.rest[n:]
	return out, nil
}
