package position

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// key is one source position on one instance.
var key = Key{Instance: "home", Source: "brave:Profile 3"}

func tempStore(t *testing.T) *Store {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "nested", "ambient", DefaultFileName))
}

func TestDefaultPath_EnvOverride(t *testing.T) {
	t.Setenv(EnvFile, "/tmp/x/custom.state")
	p, err := DefaultPath()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/x/custom.state", p)
}

func TestDefaultPath_XDGState(t *testing.T) {
	t.Setenv(EnvFile, "")
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	p, err := DefaultPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "ctxt", "ambient", "browserhistory.state"), p)
}

func TestMissingFileIsEmpty(t *testing.T) {
	s := tempStore(t)
	got, ok, err := s.Get(key)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.True(t, got.IsZero())

	all, err := s.All()
	require.NoError(t, err)
	assert.Empty(t, all)

	_, statErr := os.Stat(s.Path())
	assert.ErrorIs(t, statErr, os.ErrNotExist, "reads never create the file")
}

func TestAdvanceAndGet(t *testing.T) {
	s := tempStore(t)
	ts := time.Date(2026, 9, 10, 12, 0, 0, 123456000, time.UTC)

	got, err := s.Advance(key, ts)
	require.NoError(t, err)
	assert.True(t, got.Equal(ts))

	read, ok, err := s.Get(key)
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, read.Equal(ts))
	assert.Equal(t, time.UTC, read.Location())

	other, ok, err := s.Get(Key{Instance: "home", Source: "chrome:Default"})
	require.NoError(t, err)
	assert.False(t, ok, "keys are independent")
	assert.True(t, other.IsZero())
}

func TestAdvanceNeverMovesBackwards(t *testing.T) {
	s := tempStore(t)
	later := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	earlier := later.Add(-time.Hour)

	_, err := s.Advance(key, later)
	require.NoError(t, err)

	got, err := s.Advance(key, earlier)
	require.NoError(t, err)
	assert.True(t, got.Equal(later), "Advance returns the retained position")

	read, _, err := s.Get(key)
	require.NoError(t, err)
	assert.True(t, read.Equal(later))

	got, err = s.Advance(key, later)
	require.NoError(t, err)
	assert.True(t, got.Equal(later), "equal time is a no-op")

	newer := later.Add(time.Microsecond)
	got, err = s.Advance(key, newer)
	require.NoError(t, err)
	assert.True(t, got.Equal(newer))
}

func TestAdvanceRejectsBadInput(t *testing.T) {
	s := tempStore(t)
	for _, k := range []Key{{}, {Instance: "home"}, {Source: "brave:Profile 3"}} {
		_, err := s.Advance(k, time.Now())
		require.ErrorIs(t, err, ErrEmptyKey, "%+v", k)
		_, _, err = s.Get(k)
		require.ErrorIs(t, err, ErrEmptyKey, "%+v", k)
		require.ErrorIs(t, s.Reset(k), ErrEmptyKey, "%+v", k)
	}
	_, err := s.Advance(key, time.Time{})
	require.ErrorIs(t, err, ErrZeroTime)
}

func TestTimestampsUTCMicrosecondRoundTrip(t *testing.T) {
	s := tempStore(t)
	toronto, err := time.LoadLocation("America/Toronto")
	require.NoError(t, err)
	ts := time.Date(2026, 11, 1, 1, 30, 0, 987654321, toronto)
	want := ts.UTC().Truncate(time.Microsecond)

	got, err := s.Advance(key, ts)
	require.NoError(t, err)
	assert.True(t, got.Equal(want))

	raw, err := os.ReadFile(s.Path())
	require.NoError(t, err)
	var f struct {
		Version   int                          `json:"version"`
		Instances map[string]map[string]string `json:"instances"`
	}
	require.NoError(t, json.Unmarshal(raw, &f))
	assert.Equal(t, 2, f.Version)
	assert.Equal(t, want.Format("2006-01-02T15:04:05.000000Z"), f.Instances[key.Instance][key.Source])

	// A fresh Store reading the file sees the exact instant.
	read, _, err := New(s.Path()).Get(key)
	require.NoError(t, err)
	assert.True(t, read.Equal(want), "got %s want %s", read, want)
	assert.Equal(t, want, read, "round trip is exact, including location")

	// Sub-microsecond growth that truncates to the stored value is a no-op.
	got, err = s.Advance(key, ts.Add(10*time.Nanosecond))
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestReset(t *testing.T) {
	s := tempStore(t)
	ts := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	_, err := s.Advance(key, ts)
	require.NoError(t, err)
	_, err = s.Advance(Key{Instance: "home", Source: "chrome:Default"}, ts)
	require.NoError(t, err)

	require.NoError(t, s.Reset(key))
	_, ok, err := s.Get(key)
	require.NoError(t, err)
	assert.False(t, ok)
	_, ok, err = s.Get(Key{Instance: "home", Source: "chrome:Default"})
	require.NoError(t, err)
	assert.True(t, ok, "Reset touches only its key")

	// After Reset the position may move to any time, including earlier.
	earlier := ts.Add(-24 * time.Hour)
	got, err := s.Advance(key, earlier)
	require.NoError(t, err)
	assert.True(t, got.Equal(earlier))

	require.NoError(t, s.Reset(Key{Instance: "home", Source: "never-set"}), "resetting an absent key is a no-op")
}

func TestPermissions(t *testing.T) {
	s := tempStore(t)
	_, err := s.Advance(key, time.Now())
	require.NoError(t, err)

	fi, err := os.Stat(s.Path())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())

	di, err := os.Stat(filepath.Dir(s.Path()))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), di.Mode().Perm())

	entries, err := os.ReadDir(filepath.Dir(s.Path()))
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "no temp files left behind")
	}
}

func TestCorruptFileIsAnError(t *testing.T) {
	cases := map[string]string{
		"garbage":         "not json",
		"empty":           "",
		"truncated":       `{"version":2,"instances":{"i":{"a":"2026-09-10T12:00:00.000000Z"`,
		"bad timestamp":   `{"version":2,"instances":{"i":{"a":"yesterday"}}}`,
		"unknown version": `{"version":99,"instances":{}}`,
		"version 1":       `{"version":1,"positions":{"a":"2026-09-10T12:00:00.000000Z"}}`,
		"missing version": `{"instances":{}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			s := tempStore(t)
			require.NoError(t, os.MkdirAll(filepath.Dir(s.Path()), 0o700))
			require.NoError(t, os.WriteFile(s.Path(), []byte(body), 0o600))

			k := Key{Instance: "i", Source: "a"}
			_, _, err := s.Get(k)
			require.ErrorIs(t, err, ErrCorrupt)
			assert.Contains(t, err.Error(), s.Path(), "error names the file")

			_, err = s.Advance(k, time.Now())
			require.ErrorIs(t, err, ErrCorrupt)
			require.ErrorIs(t, s.Reset(k), ErrCorrupt)
			_, err = s.All()
			require.ErrorIs(t, err, ErrCorrupt)

			after, err := os.ReadFile(s.Path())
			require.NoError(t, err)
			assert.Equal(t, body, string(after), "a corrupt file is never rewritten")
		})
	}
}

func TestConcurrentAdvanceInProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultFileName)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	const writers, steps = 8, 40
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := New(path) // separate Store, separate lock fd
			for i := range steps {
				// Writers interleave, so each key sees out-of-order proposals.
				ts := base.Add(time.Duration(i*writers+w) * time.Second)
				_, err := s.Advance(Key{Instance: "home", Source: fmt.Sprintf("k%d", i%3)}, ts)
				assert.NoError(t, err)
			}
		}()
	}
	wg.Wait()

	all, err := New(path).All()
	require.NoError(t, err)
	require.Len(t, all, 3)
	for k, got := range all {
		require.Equal(t, "home", k.Instance)
		var want time.Time
		for w := range writers {
			for i := range steps {
				if fmt.Sprintf("k%d", i%3) != k.Source {
					continue
				}
				ts := base.Add(time.Duration(i*writers+w) * time.Second)
				if ts.After(want) {
					want = ts
				}
			}
		}
		assert.True(t, got.Equal(want), "%s: got %s want %s (lost update)", k, got, want)
	}
}

// TestConcurrentAdvanceAcrossProcesses re-executes the test binary as
// several writer processes hammering one file, then checks the file
// parses and holds the maximum each writer proposed.
func TestConcurrentAdvanceAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultFileName)
	const procs, steps = 4, 60

	cmds := make([]*exec.Cmd, 0, procs)
	for p := range procs {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperWriter$") // #nosec G204 -- test binary
		cmd.Env = append(
			os.Environ(),
			"POSITION_TEST_HELPER=1",
			"POSITION_TEST_PATH="+path,
			"POSITION_TEST_WRITER="+strconv.Itoa(p),
			"POSITION_TEST_STEPS="+strconv.Itoa(steps),
		)
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Start())
		cmds = append(cmds, cmd)
	}
	for _, cmd := range cmds {
		require.NoError(t, cmd.Wait())
	}

	all, err := New(path).All()
	require.NoError(t, err, "file stays valid under concurrent writers")
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for p := range procs {
		k := Key{Instance: "home", Source: "proc:" + strconv.Itoa(p)}
		assert.True(t, all[k].Equal(base.Add(time.Duration(steps-1)*time.Second)), "%s: %s", k, all[k])
	}
	// Every writer also advances one shared key; the highest wins.
	shared := Key{Instance: "home", Source: "shared"}
	assert.True(t, all[shared].Equal(base.Add(time.Duration(procs*steps-1)*time.Second)),
		"shared: %s", all[shared])
}

func TestHelperWriter(t *testing.T) {
	if os.Getenv("POSITION_TEST_HELPER") != "1" {
		t.Skip("helper process for TestConcurrentAdvanceAcrossProcesses")
	}
	w, _ := strconv.Atoi(os.Getenv("POSITION_TEST_WRITER"))
	steps, _ := strconv.Atoi(os.Getenv("POSITION_TEST_STEPS"))
	s := New(os.Getenv("POSITION_TEST_PATH"))
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := range steps {
		own := Key{Instance: "home", Source: "proc:" + strconv.Itoa(w)}
		if _, err := s.Advance(own, base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("advance own key: %v", err)
		}
		// Writers interleave on the shared key: w, w+4, ... up to
		// 4*steps-1 overall (4 = procs in the parent test).
		shared := base.Add(time.Duration(i*4+w) * time.Second)
		if _, err := s.Advance(Key{Instance: "home", Source: "shared"}, shared); err != nil {
			t.Fatalf("advance shared key: %v", err)
		}
	}
}

// TestPositionsArePerInstance: the same source key on two instances
// holds two independent positions, so history one instance received is
// never skipped for the other.
func TestPositionsArePerInstance(t *testing.T) {
	s := tempStore(t)
	a := Key{Instance: "home", Source: "brave:Profile 3"}
	b := Key{Instance: "http://127.0.0.1:7700", Source: "brave:Profile 3"}
	ts := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	_, err := s.Advance(a, ts)
	require.NoError(t, err)

	_, ok, err := s.Get(b)
	require.NoError(t, err)
	assert.False(t, ok, "advancing one instance leaves the other unset")

	_, err = s.Advance(b, ts.Add(-time.Hour))
	require.NoError(t, err)
	gotA, _, err := s.Get(a)
	require.NoError(t, err)
	gotB, _, err := s.Get(b)
	require.NoError(t, err)
	assert.True(t, gotA.Equal(ts))
	assert.True(t, gotB.Equal(ts.Add(-time.Hour)), "an earlier position on another instance is its own")

	require.NoError(t, s.Reset(a))
	_, ok, err = s.Get(b)
	require.NoError(t, err)
	assert.True(t, ok, "Reset touches only its instance")

	all, err := s.All()
	require.NoError(t, err)
	assert.Equal(t, map[Key]time.Time{b: ts.Add(-time.Hour)}, all)
}

// TestVersionOneFileNeedsReset: a file from before per-instance
// positions is refused as an unsupported version, with nothing
// rewritten, and the error says how to start over.
func TestVersionOneFileNeedsReset(t *testing.T) {
	s := tempStore(t)
	body := `{"version":1,"positions":{"brave:Profile 3":"2026-09-10T12:00:00.000000Z"}}`
	require.NoError(t, os.MkdirAll(filepath.Dir(s.Path()), 0o700))
	require.NoError(t, os.WriteFile(s.Path(), []byte(body), 0o600))

	_, _, err := s.Get(key)
	require.ErrorIs(t, err, ErrUnsupportedVersion)
	require.ErrorIs(t, err, ErrCorrupt)
	assert.Contains(t, err.Error(), "version 1")

	_, err = s.Advance(key, time.Now())
	require.ErrorIs(t, err, ErrUnsupportedVersion)
	after, err := os.ReadFile(s.Path())
	require.NoError(t, err)
	assert.Equal(t, body, string(after), "an old file is never migrated or rewritten")
}
