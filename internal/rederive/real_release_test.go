package rederive

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// These cases use the step versions the registry ships, with no test bump:
// an object stored with its pipeline's baseline signature (every step at
// version 1, what an object stored before signatures carries) is stale once
// a shipped step has a higher version.

// stampBaseline gives the object the baseline signature of its pipeline.
func (e *realEnv) stampBaseline(id, name string) string {
	e.t.Helper()
	p, err := e.reg.Get(name)
	require.NoError(e.t, err)
	require.NotEqual(e.t, p.Signature, p.BaselineSignature, "%s has no shipped step bump", name)
	_, err = sqliteDB(e.t, e.driver).Exec(`UPDATE objects SET derivation_sig = ? WHERE id = ?`, p.BaselineSignature, id)
	require.NoError(e.t, err)
	require.Equal(e.t, p.BaselineSignature, e.sigOf(id))
	return p.Signature
}

func TestReal_ShippedCleanerVersionRefreshesAPage(t *testing.T) {
	e := newRealEnv(t)
	obj := e.ingest("url.generic", e.srv.URL+"/p", "")
	cur := e.stampBaseline(obj.ID, "url.generic")
	e.record()
	e.setPage(samePage("second page text added"))
	before := e.hits.Load()

	res, err := e.run()
	require.NoError(t, err)
	require.Equal(t, 1, res.Processed, "%+v", res)
	require.Equal(t, before+1, e.hits.Load(), "the page is fetched once")
	require.Equal(t, cur, e.sigOf(obj.ID))
	require.Contains(t, e.get(obj.ID).TextContent, "second page text")
}

func TestReal_ShippedMapperVersionDistillsTheREADME(t *testing.T) {
	const raw = "# Hello World\n\n[![Build](https://img.shields.io/badge/build-ok.svg)](https://ci.example/run)\n\n" +
		"This project does one thing and documents it in prose. This project does one thing and documents it in prose. " +
		"This project does one thing and documents it in prose. This project does one thing and documents it in prose.\n\n" +
		"## License\n\nMIT licensed.\n"
	e := newRealEnv(t)
	e.gh.set(raw)
	obj := e.ingest("url.github.repo", repoURL, "")

	// What a repository stored before the distilling holds: the whole README.
	old := e.get(obj.ID)
	old.RawContent = raw
	require.NoError(t, e.driver.Objects().Update(ctxSystem(), old))
	cur := e.stampBaseline(obj.ID, "url.github.repo")
	e.record()
	require.Contains(t, e.get(obj.ID).RawContent, "img.shields.io")

	res, err := e.run()
	require.NoError(t, err)
	require.Equal(t, 1, res.Processed, "%+v", res)
	require.Equal(t, cur, e.sigOf(obj.ID))
	got := e.get(obj.ID).RawContent
	require.Contains(t, got, "documents it in prose")
	require.NotContains(t, got, "img.shields.io", "the badge is distilled away")
	require.NotContains(t, got, "MIT licensed", "the license section is distilled away")
}
