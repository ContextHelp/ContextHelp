package cursor

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tempCursorPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "cursors.yaml")
}

func TestValidateName(t *testing.T) {
	good := []string{"sales-acme", "abc", "x", "a1", "feed-2026", strings.Repeat("a", 64)}
	for _, n := range good {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q): unexpected error: %v", n, err)
		}
	}
	bad := []string{"", "Sales", "sales_acme", "-leading", "-", "with space", strings.Repeat("a", 65), "über"}
	for _, n := range bad {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q): expected error", n)
		}
	}
}

func TestParseTimestamp_RFC3339(t *testing.T) {
	got, err := ParseTimestamp("2026-04-28T14:22:11Z")
	require.NoError(t, err)
	want := time.Date(2026, 4, 28, 14, 22, 11, 0, time.UTC)
	assert.Equal(t, want, got)
}

func TestParseTimestamp_DateOnly(t *testing.T) {
	got, err := ParseTimestamp("2026-04-01")
	require.NoError(t, err)
	assert.Equal(t, 2026, got.Year())
	assert.Equal(t, time.April, got.Month())
}

func TestParseTimestamp_Relative(t *testing.T) {
	now := time.Now().UTC()
	got, err := ParseTimestamp("-7d")
	require.NoError(t, err)
	delta := now.Sub(got)
	assert.InDelta(t, (7 * 24 * time.Hour).Seconds(), delta.Seconds(), 5)

	got, err = ParseTimestamp("+1h")
	require.NoError(t, err)
	assert.True(t, got.After(now))
}

func TestParseTimestamp_Bad(t *testing.T) {
	for _, s := range []string{"", "garbage", "-", "-7x", "7d"} {
		if _, err := ParseTimestamp(s); err == nil {
			t.Errorf("ParseTimestamp(%q): expected error", s)
		}
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	p := tempCursorPath(t)
	now := time.Date(2026, 4, 28, 14, 22, 11, 0, time.UTC)

	in := &File{
		SchemaVersion: SchemaVersion,
		Cursors: map[string]*Cursor{
			"sales-acme": {
				Name:         "sales-acme",
				LastSeenAt:   now,
				LastObjectID: "ko_01H",
				Query:        QuerySnapshot{Mention: []string{"@client.acme"}},
				CreatedAt:    now,
				UpdatedAt:    now,
			},
		},
	}
	require.NoError(t, Save(p, in))

	// Permission lockdown.
	st, err := os.Stat(p)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())

	out, err := Load(p)
	require.NoError(t, err)
	require.Contains(t, out.Cursors, "sales-acme")
	c := out.Cursors["sales-acme"]
	assert.Equal(t, "sales-acme", c.Name)
	assert.True(t, c.LastSeenAt.Equal(now))
	assert.Equal(t, "ko_01H", c.LastObjectID)
	assert.Equal(t, []string{"@client.acme"}, c.Query.Mention)
}

func TestLoad_MissingReturnsEmpty(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	require.NoError(t, err)
	assert.Equal(t, SchemaVersion, f.SchemaVersion)
	assert.Empty(t, f.Cursors)
}

func TestPath_HonorsEnv(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom.yaml")
	t.Setenv(EnvFile, custom)
	got, err := Path()
	require.NoError(t, err)
	assert.Equal(t, custom, got)
}

func TestManager_AdvanceCreatesCursor(t *testing.T) {
	p := tempCursorPath(t)
	m := NewWithPath(p)

	now := time.Now().UTC().Truncate(time.Second)
	c, err := m.Advance("feed", now, "ko_1", QuerySnapshot{Mention: []string{"@x"}})
	require.NoError(t, err)
	assert.True(t, c.LastSeenAt.Equal(now))
	assert.Equal(t, "ko_1", c.LastObjectID)

	// Second advance moves forward.
	later := now.Add(time.Hour)
	c2, err := m.Advance("feed", later, "ko_2", QuerySnapshot{Mention: []string{"@x"}})
	require.NoError(t, err)
	assert.True(t, c2.LastSeenAt.Equal(later))
	assert.Equal(t, "ko_2", c2.LastObjectID)
}

func TestManager_AdvanceNeverRegresses(t *testing.T) {
	p := tempCursorPath(t)
	m := NewWithPath(p)

	t1 := time.Now().UTC().Truncate(time.Second)
	t0 := t1.Add(-time.Hour)

	_, err := m.Advance("feed", t1, "ko_1", QuerySnapshot{})
	require.NoError(t, err)
	c, err := m.Advance("feed", t0, "ko_old", QuerySnapshot{})
	require.NoError(t, err)
	assert.True(t, c.LastSeenAt.Equal(t1), "advance must not regress")
	assert.Equal(t, "ko_1", c.LastObjectID)
}

func TestManager_ResetSetDelete(t *testing.T) {
	p := tempCursorPath(t)
	m := NewWithPath(p)

	now := time.Now().UTC().Truncate(time.Second)
	_, err := m.Advance("feed", now, "ko_1", QuerySnapshot{})
	require.NoError(t, err)

	c, err := m.Reset("feed")
	require.NoError(t, err)
	assert.True(t, c.LastSeenAt.IsZero())
	assert.Empty(t, c.LastObjectID)

	target := now.Add(-24 * time.Hour)
	c, err = m.SetTo("feed", target)
	require.NoError(t, err)
	assert.True(t, c.LastSeenAt.Equal(target))

	require.NoError(t, m.Delete("feed"))
	_, err = m.Get("feed")
	assert.ErrorIs(t, err, ErrUnknownCursor)
}

func TestManager_DeleteUnknown(t *testing.T) {
	p := tempCursorPath(t)
	m := NewWithPath(p)
	err := m.Delete("nope")
	assert.ErrorIs(t, err, ErrUnknownCursor)
}

func TestManager_GetOrInit(t *testing.T) {
	p := tempCursorPath(t)
	m := NewWithPath(p)

	c, fresh, err := m.GetOrInit("new")
	require.NoError(t, err)
	assert.True(t, fresh)
	assert.True(t, c.LastSeenAt.IsZero(), "new cursor must start at epoch 0")
	assert.False(t, c.CreatedAt.IsZero())

	// Persist via advance.
	_, err = m.Advance("new", time.Now().UTC(), "ko_1", QuerySnapshot{})
	require.NoError(t, err)
	c2, fresh, err := m.GetOrInit("new")
	require.NoError(t, err)
	assert.False(t, fresh)
	assert.False(t, c2.LastSeenAt.IsZero())
}

func TestManager_List_Sorted(t *testing.T) {
	p := tempCursorPath(t)
	m := NewWithPath(p)
	for _, n := range []string{"zeta", "alpha", "mid"} {
		_, err := m.Advance(n, time.Now().UTC(), "ko_"+n, QuerySnapshot{})
		require.NoError(t, err)
	}
	got, err := m.List()
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []string{"alpha", "mid", "zeta"},
		[]string{got[0].Name, got[1].Name, got[2].Name})
}

func TestSchemaVersionPersisted(t *testing.T) {
	p := tempCursorPath(t)
	m := NewWithPath(p)
	_, err := m.Advance("x", time.Now().UTC(), "ko_1", QuerySnapshot{})
	require.NoError(t, err)
	data, err := os.ReadFile(p) // #nosec G304
	require.NoError(t, err)
	assert.Contains(t, string(data), "schema_version: 1")
}

func TestConcurrentAdvance(t *testing.T) {
	p := tempCursorPath(t)
	m := NewWithPath(p)

	var wg sync.WaitGroup
	const n = 30
	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ts := base.Add(time.Duration(i) * time.Second)
			_, err := m.Advance("feed", ts, "ko", QuerySnapshot{})
			if err != nil {
				t.Errorf("advance %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	c, err := m.Get("feed")
	require.NoError(t, err)
	want := base.Add(time.Duration(n-1) * time.Second)
	assert.True(t, c.LastSeenAt.Equal(want),
		"final last_seen_at: got %v, want %v", c.LastSeenAt, want)
}

func TestQuerySnapshot_Equal(t *testing.T) {
	a := QuerySnapshot{Mention: []string{"@x"}, Tag: []string{"t1"}}
	b := QuerySnapshot{Mention: []string{"@x"}, Tag: []string{"t1"}}
	c := QuerySnapshot{Mention: []string{"@y"}, Tag: []string{"t1"}}
	assert.True(t, a.Equal(b))
	assert.False(t, a.Equal(c))
}
