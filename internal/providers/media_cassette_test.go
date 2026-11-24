package providers_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"hop.top/xrr"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
)

// Exec cassettes under testdata/cassettes/<tool> were recorded by running
// the real tools on the media fixtures. Replay starts no process. Re-record:
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run 'Cassette' ./internal/providers/
//
// See docs/testing-media-cassettes.md.

func execCtx(t *testing.T, dir string) (context.Context, *providertest.ExecRunner) {
	t.Helper()
	r := providertest.ExecCassette(t, dir)
	ctx := providers.WithCommandRunner(context.Background(), r.Run)
	return providers.WithToolLookup(ctx, r.Lookup), r
}

// wantCalls fails unless exactly n tool calls went through the cassette
// runner. It is what proves a test replayed: with the seam bypassed, the
// real tool runs and the runner sees no calls.
func wantCalls(t *testing.T, r *providertest.ExecRunner, n int) {
	t.Helper()
	if got := len(r.Calls()); got != n {
		t.Fatalf("cassette runner saw %d calls, want %d: %v", got, n, r.Calls())
	}
}

func TestTesseractCassette_ReadsFixtureText(t *testing.T) {
	ctx, r := execCtx(t, "testdata/cassettes/tesseract")
	img := providertest.MediaBytes(t, providertest.MediaImage)

	res, err := providers.NewTesseractOCRProvider("eng").Extract(ctx, img, "image/png")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, line := range providertest.ImageLines {
		if !strings.Contains(res.Text, line) {
			t.Errorf("OCR text %q lacks %q", res.Text, line)
		}
	}
	if res.Confidence <= 0.90 {
		t.Errorf("confidence = %v, want > 0.90", res.Confidence)
	}
	wantCalls(t, r, 2) // plain text, then tsv for confidence
}

// The tsv cassette must stay byte-faithful: tesseract prints structural rows
// as 12 tab-separated fields ending in an empty text field ("...\t-1\t"). A
// whitespace fixer that strips the trailing tab would change what the
// confidence parser sees.
func TestTesseractCassette_TsvRowsKeepTrailingTab(t *testing.T) {
	r := providertest.ExecCassette(t, "testdata/cassettes/tesseract")
	var tsv string
	capture := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		out, errOut, code, err := r.Run(ctx, name, args...)
		if slices.Contains(args, "tsv") {
			tsv = out
		}
		return out, errOut, code, err
	}
	ctx := providers.WithCommandRunner(context.Background(), capture)
	img := providertest.MediaBytes(t, providertest.MediaImage)
	if _, err := providers.NewTesseractOCRProvider("eng").Extract(ctx, img, "image/png"); err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimRight(tsv, "\n"), "\n")
	if len(rows) < 3 {
		t.Fatalf("tsv = %q", tsv)
	}
	for _, row := range rows {
		if n := len(strings.Split(row, "\t")); n != 12 {
			t.Errorf("row %q has %d fields, want 12", row, n)
		}
	}
	if !strings.Contains(tsv, "\t-1\t\n") {
		t.Errorf("no structural row ends in an empty text field; cassette lost trailing tabs")
	}
}

func TestFFmpegCassette_ProbeFixtureVideo(t *testing.T) {
	ctx, r := execCtx(t, "testdata/cassettes/ffmpeg")
	path := providertest.MediaFile(t, providertest.MediaVideo)

	meta, err := providers.NewFFmpegVideoProvider().ProbeMetadata(ctx, path)
	if err != nil {
		t.Fatalf("ProbeMetadata: %v", err)
	}
	if meta.Width != 320 || meta.Height != 180 || meta.Codec != "h264" || meta.AudioCodec != "aac" {
		t.Errorf("meta = %+v, want 320x180 h264 + aac", meta)
	}
	last := providertest.SpeechTurns[len(providertest.SpeechTurns)-1].End
	if d := meta.Duration - last; d < -200*time.Millisecond || d > 200*time.Millisecond {
		t.Errorf("duration = %v, want about %v", meta.Duration, last)
	}
	wantCalls(t, r, 1)
}

// The chained flow: extract audio, then probe the extracted file. The probe
// reads what the extraction wrote, which replay must have recreated.
func TestFFmpegCassette_ChainedExtractProbe(t *testing.T) {
	ctx, r := execCtx(t, "testdata/cassettes/ffmpeg-chain")
	path := providertest.MediaFile(t, providertest.MediaVideo)
	p := providers.NewFFmpegVideoProvider()

	audio, err := p.ExtractAudio(ctx, path, t.TempDir())
	if err != nil {
		t.Fatalf("ExtractAudio: %v", err)
	}
	if st, err := os.Stat(audio.Path); err != nil || st.Size() < 100_000 {
		t.Fatalf("extracted audio %s: size/err = %v / %v", audio.Path, st, err)
	}
	ameta, err := p.ProbeMetadata(ctx, audio.Path)
	if err != nil {
		t.Fatalf("probe extracted audio: %v", err)
	}
	if ameta.AudioCodec != "pcm_s16le" {
		t.Errorf("extracted audio codec = %q, want pcm_s16le", ameta.AudioCodec)
	}
	if _, err := p.ProbeMetadata(ctx, path); err != nil {
		t.Fatalf("probe video: %v", err)
	}

	// 1 extract + 1 probe of the audio + 1 probe of the video.
	wantCalls(t, r, 3)
	calls := r.Calls()
	if !strings.HasPrefix(calls[1][len(calls[1])-1], "<file:") {
		t.Errorf("probe of extracted audio did not see the recreated file: %v", calls[1])
	}
}

func TestExecCassette_MissIsLoud(t *testing.T) {
	if os.Getenv(xrr.EnvMode) == string(xrr.ModeRecord) {
		t.Skip("a miss only exists in replay mode")
	}
	ctx, r := execCtx(t, t.TempDir())
	_, err := providers.RunCommand(ctx, "tesseract", "never-recorded.png", "stdout")
	if !errors.Is(err, xrr.ErrCassetteMiss) || !providers.IsCommandNotRecorded(err) {
		t.Fatalf("err = %v, want a not-recorded error matching xrr.ErrCassetteMiss", err)
	}
	wantCalls(t, r, 1)
}

func TestOllamaLLMCassette_Generate(t *testing.T) {
	client, calls := providertest.OllamaClient(t, "testdata/cassettes/ollama-chat")
	p := providers.NewOllamaLLMProvider("http://127.0.0.1:11434", "qwen3:4b-instruct-2507-q4_K_M").WithHTTPClient(client)

	out, err := p.Generate(context.Background(), "Reply with the single word: pong")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), "pong") {
		t.Errorf("reply = %q, want it to contain pong", out)
	}
	if len(calls.URLs()) != 1 {
		t.Errorf("calls = %v, want one", calls.URLs())
	}
}

// The factory hands its HTTP client to the Ollama LLM provider.
func TestFactoryOllamaLLM_UsesInjectedClient(t *testing.T) {
	client, calls := providertest.OllamaClient(t, "testdata/cassettes/ollama-chat")
	cfg := config.ProvidersConfig{}
	cfg.LLM.Backend = "ollama"
	cfg.LLM.Endpoint = "http://127.0.0.1:11434"
	cfg.LLM.Model = "qwen3:4b-instruct-2507-q4_K_M"

	llm := providers.NewFactory(cfg, nil).WithHTTPClient(client).LLM()
	out, err := llm.Generate(context.Background(), "Reply with the single word: pong")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), "pong") || len(calls.URLs()) != 1 {
		t.Errorf("reply %q, calls %v", out, calls.URLs())
	}
}

// Factory probes run under the factory's context, so the seam reaches them.
func TestFactoryProbesUseContextSeams(t *testing.T) {
	var ran [][]string
	run := func(_ context.Context, name string, args ...string) (string, string, int, error) {
		ran = append(ran, append([]string{name}, args...))
		return "ok\n", "", 0, nil
	}
	look := func(name string) (string, error) {
		if name == "python3" {
			return name, nil
		}
		return "", errors.New("absent")
	}
	ctx := providers.WithToolLookup(providers.WithCommandRunner(context.Background(), run), look)
	f := providers.NewFactory(config.ProvidersConfig{}, nil).WithContext(ctx)

	d := f.Diarization()
	if d == nil || d.Name() != "pyannote" {
		t.Fatalf("Diarization() = %v, want pyannote via the python module", d)
	}
	if len(ran) != 1 || !slices.Contains(ran[0], "import pyannote.audio; print('ok')") {
		t.Errorf("probe commands = %v, want the python import check through the seam", ran)
	}
}
