package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/keys"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestEncryptionConfig_ValidationAndModes(t *testing.T) {
	p, err := keys.NewMemory("k1")
	require.NoError(t, err)

	require.ErrorIs(t, (EncryptionConfig{ColumnEncryption: config.ColumnEncryptionSensitive}).Validate(), keys.ErrUnavailable)
	require.ErrorIs(t, (EncryptionConfig{
		ColumnEncryption: config.ColumnEncryptionAll,
		FTSOnEncrypted:   config.FTSOnEncryptedRequireTDE,
		KeyProvider:      p,
	}).Validate(), ErrTDERequired)
	require.NoError(t, (EncryptionConfig{
		RequireTDE:       true,
		ColumnEncryption: config.ColumnEncryptionAll,
		FTSOnEncrypted:   config.FTSOnEncryptedRequireTDE,
		KeyProvider:      p,
	}).Validate())

	sensCfg := EncryptionConfig{ColumnEncryption: config.ColumnEncryptionSensitive, KeyProvider: p}
	require.True(t, sensCfg.ShouldEncryptObject(&storage.KnowledgeObject{ID: "1", Classes: []string{"personal"}}))
	require.True(t, sensCfg.ShouldEncryptObject(&storage.KnowledgeObject{ID: "2", Metadata: map[string]any{"bos_sensitivity": "restricted"}}))
	require.False(t, sensCfg.ShouldEncryptObject(&storage.KnowledgeObject{ID: "3", Classes: []string{"public"}}))
}

func makeSamplePGObject(t *testing.T, sentinel string) (*storage.KnowledgeObject, objectFields, []byte) {
	t.Helper()
	obj := &storage.KnowledgeObject{
		ID:          "pg-obj-1",
		Type:        "note",
		RawContent:  sentinel,
		TextContent: sentinel,
		Summaries:   []string{"summary: " + sentinel},
		Metadata:    map[string]any{"secret_field": sentinel},
		Graph: &storage.ObjectGraph{
			Nodes: []storage.GraphNode{{ID: "n1", NodeType: "claim", Content: sentinel}},
		},
	}
	f, err := marshalObjectFields(obj)
	require.NoError(t, err)
	graphJSON, err := marshalGraph(obj.Graph)
	require.NoError(t, err)
	return obj, f, graphJSON
}

func TestPostgresColumnEncryption_ZeroPlaintextAndFTSPolicy(t *testing.T) {
	ctx := context.Background()
	p, err := keys.NewMemory("k1")
	require.NoError(t, err)
	const sentinel = "PG-COLUMN-ENCRYPT-SENTINEL-3319"
	obj, f, graphJSON := makeSamplePGObject(t, sentinel)

	cfgOmit := EncryptionConfig{ColumnEncryption: config.ColumnEncryptionAll, FTSOnEncrypted: config.FTSOnEncryptedOmit, KeyProvider: p}
	w, err := cfgOmit.prepareObjectWrite(ctx, obj, &f, graphJSON, "fts "+sentinel, "masked "+sentinel)
	require.NoError(t, err)
	for _, val := range []string{w.rawContent, w.textContent, w.metadata, w.summaries, string(w.graphJSON), w.ftsBody, w.maskedFTS} {
		require.NotContains(t, val, sentinel)
	}
	require.Empty(t, w.ftsBody)
	require.Empty(t, w.maskedFTS)
	require.False(t, w.ftsIndexed)
	for _, col := range []string{w.metadata, w.summaries, string(w.graphJSON)} {
		var tmp map[string]any
		require.NoError(t, json.Unmarshal([]byte(col), &tmp))
	}

	cfgTDE := EncryptionConfig{RequireTDE: true, ColumnEncryption: config.ColumnEncryptionAll, FTSOnEncrypted: config.FTSOnEncryptedRequireTDE, KeyProvider: p}
	wTDE, err := cfgTDE.prepareObjectWrite(ctx, obj, &f, graphJSON, "fts "+sentinel, "masked "+sentinel)
	require.NoError(t, err)
	require.NotEmpty(t, wTDE.ftsBody)
	require.True(t, wTDE.ftsIndexed)
}

func TestPostgresColumnEncryption_DecryptAADWrongKeyAndRewrap(t *testing.T) {
	ctx := context.Background()
	p, err := keys.NewMemory("k1")
	require.NoError(t, err)
	const sentinel = "PG-COLUMN-ENCRYPT-SENTINEL-9921"
	obj, f, graphJSON := makeSamplePGObject(t, sentinel)

	cfg := EncryptionConfig{ColumnEncryption: config.ColumnEncryptionAll, FTSOnEncrypted: config.FTSOnEncryptedOmit, KeyProvider: p}
	w, err := cfg.prepareObjectWrite(ctx, obj, &f, graphJSON, "", "")
	require.NoError(t, err)

	raw, text := w.rawContent, w.textContent
	metaB, sumB, graphB := []byte(w.metadata), []byte(w.summaries), w.graphJSON
	require.NoError(t, cfg.decryptRawColumns(ctx, obj.ID, &raw, &text, &metaB, &sumB, &graphB))
	require.Equal(t, sentinel, raw)
	require.Equal(t, sentinel, text)
	require.True(t, strings.Contains(string(metaB), sentinel) && strings.Contains(string(sumB), sentinel) && strings.Contains(string(graphB), sentinel))

	// Cross-row swap fails AAD check.
	rawS, textS, metaS, sumS, graphS := w.rawContent, w.textContent, []byte(w.metadata), []byte(w.summaries), w.graphJSON
	require.True(t, errors.Is(cfg.decryptRawColumns(ctx, "pg-obj-OTHER", &rawS, &textS, &metaS, &sumS, &graphS), keys.ErrUnwrap))

	// Wrong key fails closed.
	wrongP, _ := keys.NewMemory("k1")
	wrongCfg := EncryptionConfig{ColumnEncryption: config.ColumnEncryptionAll, KeyProvider: wrongP}
	rawW, textW, metaW, sumW, graphW := w.rawContent, w.textContent, []byte(w.metadata), []byte(w.summaries), w.graphJSON
	require.ErrorIs(t, wrongCfg.decryptRawColumns(ctx, obj.ID, &rawW, &textW, &metaW, &sumW, &graphW), keys.ErrUnwrap)

	// Rotate KEK + Rewrap + Retire k1 -> still decrypts.
	_, err = p.Rotate("k2")
	require.NoError(t, err)
	rwRaw, changed, err := rewrapTextColumn(ctx, p, "objects", obj.ID, "raw_content", w.rawContent, "k2")
	require.NoError(t, err)
	require.True(t, changed)
	rwMeta, changed, err := rewrapObjectJSONColumn(ctx, p, obj.ID, "metadata", []byte(w.metadata), "k2")
	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, p.Retire("k1"))

	gotRaw, err := openTextColumn(ctx, p, "objects", obj.ID, "raw_content", rwRaw)
	require.NoError(t, err)
	require.Equal(t, sentinel, gotRaw)
	gotMeta, err := openObjectJSONColumn(ctx, p, obj.ID, "metadata", rwMeta)
	require.NoError(t, err)
	require.Contains(t, string(gotMeta), sentinel)
}
