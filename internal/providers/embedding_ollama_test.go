package providers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
)

// Cassettes under testdata/cassettes/ollama-embed were recorded against a
// real local Ollama with snowflake-arctic-embed2 pulled. Re-record:
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run TestOllamaEmbedding ./internal/providers/
const ollamaCassettes = "testdata/cassettes/ollama-embed"

const (
	ollamaBase       = "http://127.0.0.1:11434"
	snowflake        = "snowflake-arctic-embed2"
	snowflakeContext = 8192 // bert.context_length in /api/show
)

type sentEmbed struct {
	Model    string `json:"model"`
	Input    string `json:"input"`
	Truncate *bool  `json:"truncate"`
	Options  struct {
		NumCtx   int `json:"num_ctx"`
		NumBatch int `json:"num_batch"`
	} `json:"options"`
}

// lastEmbed decodes the body of the last /api/embed call.
func lastEmbed(t *testing.T, calls *providertest.OllamaCalls) sentEmbed {
	t.Helper()
	urls, bodies := calls.URLs(), calls.Bodies()
	for i := len(urls) - 1; i >= 0; i-- {
		if strings.HasSuffix(urls[i], "/api/embed") {
			var s sentEmbed
			if err := json.Unmarshal([]byte(bodies[i]), &s); err != nil {
				t.Fatalf("decode embed body %q: %v", bodies[i], err)
			}
			return s
		}
	}
	t.Fatalf("no /api/embed call in %v", urls)
	return sentEmbed{}
}

func TestOllamaEmbedding_EmbedRecorded(t *testing.T) {
	client, calls := providertest.OllamaClient(t, ollamaCassettes)
	p := providers.NewOllamaEmbeddingProvider(ollamaBase, snowflake,
		providers.WithOllamaEmbeddingHTTPClient(client))

	vec, err := p.Embed(context.Background(), "ctxt embedding provider resolution")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 1024 {
		t.Fatalf("snowflake-arctic-embed2 dimension = %d, want 1024", len(vec))
	}
	want := []string{ollamaBase + "/api/show", ollamaBase + "/api/embed"}
	if got := calls.URLs(); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("request URLs = %v, want %v", got, want)
	}
	sent := lastEmbed(t, calls)
	if sent.Model != snowflake || sent.Input != "ctxt embedding provider resolution" {
		t.Errorf("embed body model/input = %q/%q", sent.Model, sent.Input)
	}
	if sent.Truncate == nil || *sent.Truncate {
		t.Errorf("truncate = %v, want an explicit false so overflow fails loudly", sent.Truncate)
	}
	if sent.Options.NumCtx != snowflakeContext || sent.Options.NumBatch != snowflakeContext {
		t.Errorf("options = %+v, want num_ctx and num_batch at the model context %d", sent.Options, snowflakeContext)
	}
}

// The context length is read once per provider, not once per input.
func TestOllamaEmbedding_ContextLengthReadOnce(t *testing.T) {
	client, calls := providertest.OllamaClient(t, ollamaCassettes)
	p := providers.NewOllamaEmbeddingProvider(ollamaBase, snowflake,
		providers.WithOllamaEmbeddingHTTPClient(client))

	for range 2 {
		if _, err := p.Embed(context.Background(), "ctxt embedding provider resolution"); err != nil {
			t.Fatalf("Embed: %v", err)
		}
	}
	if got := calls.URLs(); len(got) != 3 || len(calls.EmbedURLs()) != 2 {
		t.Fatalf("request URLs = %v, want one /api/show then two /api/embed", got)
	}
}

func TestOllamaEmbedding_ModelNotPulledRecorded(t *testing.T) {
	client, _ := providertest.OllamaClient(t, ollamaCassettes)
	p := providers.NewOllamaEmbeddingProvider(ollamaBase, "ctxt-missing-embed-model",
		providers.WithOllamaEmbeddingHTTPClient(client))

	_, err := p.Embed(context.Background(), "ctxt embedding provider resolution")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Embed with an unpulled model: err = %v, want Ollama's not-found error", err)
	}
}

func TestOllamaEmbedding_TrailingSlashEndpoint(t *testing.T) {
	client, calls := providertest.OllamaClient(t, ollamaCassettes)
	p := providers.NewOllamaEmbeddingProvider(ollamaBase+"/", snowflake,
		providers.WithOllamaEmbeddingHTTPClient(client))

	if _, err := p.Embed(context.Background(), "ctxt embedding provider resolution"); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if got := calls.EmbedURLs(); len(got) != 1 || got[0] != ollamaBase+"/api/embed" {
		t.Fatalf("embed URLs = %v, want a single slash before api/embed", got)
	}
}

// frenchProse is ~8 KB of accented French prose: about 2,300 tokens for
// snowflake-arctic-embed2, over Ollama's default embed batch (2,048).
func frenchProse() string {
	var b strings.Builder
	for i := 0; ; i++ {
		s := fmt.Sprintf("Le château d'été n°%d, où l'on déjeunait près de la forêt, était très célèbre à Genève ; ça n'étonnait personne. ", i)
		if b.Len()+len(s) > 8000 {
			return b.String()
		}
		b.WriteString(s)
	}
}

// frenchCitations is ~8 KB of dense French legal citations: about 4,200
// tokens, over Ollama's default embed context (4,096).
func frenchCitations() string {
	var b strings.Builder
	for i := 0; ; i++ {
		s := fmt.Sprintf("«cf. art. L.%d-%d, al. %d ; §%d (%c)» ; ",
			i%97+1, i%13+1, i%7+1, i%11+1, []rune("abcdéèàç")[i%8])
		if b.Len()+len(s) > 8000 {
			return b.String()
		}
		b.WriteString(s)
	}
}

// Inputs within the byte budget but past Ollama's default batch and
// context embed whole: the request runs the model at its full context.
func TestOllamaEmbedding_LongFrenchTextEmbeds(t *testing.T) {
	for name, text := range map[string]string{"prose": frenchProse(), "citations": frenchCitations()} {
		t.Run(name, func(t *testing.T) {
			if len(text) >= 8192-8 {
				t.Fatalf("fixture is %d bytes; it must fit the byte budget uncut", len(text))
			}
			client, calls := providertest.OllamaClient(t, ollamaCassettes)
			p := providers.NewOllamaEmbeddingProvider(ollamaBase, snowflake,
				providers.WithOllamaEmbeddingHTTPClient(client))

			vec, err := p.Embed(context.Background(), text)
			if err != nil {
				t.Fatalf("Embed(%d bytes): %v", len(text), err)
			}
			if len(vec) != 1024 {
				t.Fatalf("dimension = %d, want 1024", len(vec))
			}
			if sent := lastEmbed(t, calls); sent.Input != text {
				t.Errorf("input sent was cut to %d of %d bytes", len(sent.Input), len(text))
			}
		})
	}
}

// A configured context length wins over /api/show, which is then never
// read; it sizes num_ctx, num_batch and the cut, which never splits a rune.
func TestOllamaEmbedding_ContextLengthOverride(t *testing.T) {
	client, calls := providertest.OllamaClient(t, ollamaCassettes)
	p := providers.NewOllamaEmbeddingProvider(ollamaBase, snowflake,
		providers.WithOllamaEmbeddingHTTPClient(client),
		providers.WithOllamaEmbeddingContextLength(64))

	// 64-token context, 8 reserved: a 56-byte budget. Every rune of the
	// input is 2 bytes after the first, so byte 56 falls mid-rune.
	text := "x" + strings.Repeat("é", 60)
	vec, err := p.Embed(context.Background(), text)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 1024 {
		t.Fatalf("dimension = %d, want 1024", len(vec))
	}
	if got := calls.URLs(); len(got) != 1 || got[0] != ollamaBase+"/api/embed" {
		t.Fatalf("request URLs = %v, want the embed call only (no /api/show)", got)
	}
	sent := lastEmbed(t, calls)
	if sent.Options.NumCtx != 64 || sent.Options.NumBatch != 64 {
		t.Errorf("options = %+v, want num_ctx and num_batch 64", sent.Options)
	}
	want := "x" + strings.Repeat("é", 27) // 55 bytes: the rune ending at 57 is dropped
	if sent.Input != want || !utf8.ValidString(sent.Input) {
		t.Errorf("input sent = %q (%d bytes), want %q (%d bytes)", sent.Input, len(sent.Input), want, len(want))
	}
}

func TestOllamaEmbedding_Dimensions(t *testing.T) {
	if got := providers.NewOllamaEmbeddingProvider("http://x", "m").Dimensions(); got != 0 {
		t.Errorf("Dimensions() without a configured dimension = %d, want 0 (unknown)", got)
	}
	p := providers.NewOllamaEmbeddingProvider("http://x", "m", providers.WithOllamaEmbeddingDimension(1024))
	if got := p.Dimensions(); got != 1024 {
		t.Errorf("Dimensions() = %d, want 1024", got)
	}
}
