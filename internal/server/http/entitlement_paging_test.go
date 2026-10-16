package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// seedPagingEntities adds five ungranted entities that sort ahead of
// every granted one, and five more granted ones, to the gated fixture
// (which already holds ai.bert in ai.models and med.claims in
// med.records). "ops" is entitled to ai.bert plus ai.m1..ai.m5. They
// are registry-served like the fixture's, so the share grant shows
// them without a mention (ADR-080 §6) and only the namespace
// entitlement decides.
func seedPagingEntities(t *testing.T, driver storage.StorageDriver) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	for i := 1; i <= 5; i++ {
		for _, e := range []*storage.Entity{
			{Slug: fmt.Sprintf("aa.x%d", i), Namespace: "aa.restricted"},
			{Slug: fmt.Sprintf("ai.m%d", i), Namespace: "ai.models"},
		} {
			e.Title, e.RegistryURL, e.CreatedAt, e.UpdatedAt = e.Slug, "https://registry.test", now, now
			require.NoError(t, driver.Entities().Upsert(ownerCtx(), e))
		}
	}
}

// walkEntityPages lists /entities page by page with the given token
// until a short page, returning every page's slugs.
func walkEntityPages(t *testing.T, token, base string, limit int) [][]string {
	t.Helper()
	var pages [][]string
	for offset := 0; ; offset += limit {
		require.Less(t, offset, 100, "paging never ended")
		resp, body := gatedDoAs(t, token, http.MethodGet,
			fmt.Sprintf("%s/api/v1/entities?limit=%d&offset=%d", base, limit, offset))
		require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
		var out struct {
			Data []storage.Entity `json:"data"`
		}
		require.NoError(t, json.Unmarshal(body, &out))
		page := make([]string, 0, len(out.Data))
		for _, e := range out.Data {
			page = append(page, e.Slug)
		}
		pages = append(pages, page)
		if len(page) < limit {
			return pages
		}
	}
}

// A gated principal's page holds limit entitled entities whenever that
// many exist past the offset: the entitlement filter applies before
// Limit and Offset, so ungranted rows sorting first never shrink or
// empty a page, and walking the offsets visits every entitled entity
// exactly once.
func TestEntityGateListingPagesOverEntitledSet(t *testing.T) {
	ts, _, _, driver := newGatedServerWithDriver(t, storageutil.NewTestDriver(t))
	seedPagingEntities(t, driver)

	pages := walkEntityPages(t, "tok-valid-fixture-0123456789abcdef0123456789", ts.URL, 2)
	assert.Equal(t, [][]string{
		{"ai.bert", "ai.m1"},
		{"ai.m2", "ai.m3"},
		{"ai.m4", "ai.m5"},
		{},
	}, pages)
}

// The admin owner bypasses the gate: the same walk visits every entity.
func TestEntityGateListingAdminPagesOverEverything(t *testing.T) {
	ts, _, _, driver := newGatedServerWithDriver(t, storageutil.NewTestDriver(t))
	seedPagingEntities(t, driver)

	pages := walkEntityPages(t, "tok-admin-fixture-0123456789abcdef0123456789", ts.URL, 5)
	assert.Equal(t, [][]string{
		{"aa.x1", "aa.x2", "aa.x3", "aa.x4", "aa.x5"},
		{"ai.bert", "ai.m1", "ai.m2", "ai.m3", "ai.m4"},
		{"ai.m5", "med.claims"},
	}, pages)
}

// A search narrows the entitled set before Limit too: the first entitled
// match fills the page even though ungranted matches sort ahead of it.
func TestEntityGateSearchLimitAppliesAfterEntitlement(t *testing.T) {
	ts, _, _, driver := newGatedServerWithDriver(t, storageutil.NewTestDriver(t))
	seedPagingEntities(t, driver)

	resp, body := gatedDo(t, http.MethodGet, ts.URL+"/api/v1/entities?q=x&limit=1")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var out struct {
		Data []storage.Entity `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	assert.Empty(t, out.Data, "no entitled entity matches x")

	resp, body = gatedDo(t, http.MethodGet, ts.URL+"/api/v1/entities?q=a&limit=1")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Data, 1)
	assert.Equal(t, "ai.bert", out.Data[0].Slug)
}
