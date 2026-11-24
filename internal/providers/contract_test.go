package providers_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/providers/contracttest"
	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
)

// This file is the backend table of every role: each role's contract
// suite (contracttest) runs against each backend listed for it. A new
// backend is added to its role's table and must pass the suite unchanged.
//
// Hosted backends answer from an in-memory transport carrying canned
// responses; CLI backends run fixture scripts placed first on PATH. No
// test here opens a socket or starts a real tool.

// tinyPNG is a 1x1 PNG.
var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0xf8, 0xff, 0xff, 0x3f,
	0x00, 0x05, 0xfe, 0x02, 0xfe, 0xa7, 0x35, 0x81, 0x84, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
	0x44, 0xae, 0x42, 0x60, 0x82,
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// cannedClient answers every request with a 200 and body, unless the
// request's context is already done.
func cannedClient(body string) *http.Client {
	return &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}
}

// fixtureTools writes executable sh scripts named after tools into a temp
// dir and puts it first on PATH for the test. Scripts may call cat, which
// every platform that runs them has.
func fixtureTools(t testing.TB, scripts map[string]string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture tools are sh scripts")
	}
	dir := t.TempDir()
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil { // #nosec G306 -- test fixture must be executable
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// fixtureFile writes content to a temp file and returns its path.
func fixtureFile(t testing.TB, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const (
	openAIBody    = `{"choices":[{"message":{"content":"A red square. Labels: square, red"}}]}`
	anthropicBody = `{"content":[{"type":"text","text":"A red square. Labels: square, red"}]}`
)

func TestContractFrames(t *testing.T) {
	cases := []contracttest.FramesCase{
		{Name: "stub", Stub: true, New: func(testing.TB) (providers.FrameProvider, contracttest.FramesFixture) {
			return providers.NewStubFrameProvider(), contracttest.FramesFixture{
				Source: providers.FileSource("/nonexistent/v.mp4"), DurationMS: 150_000,
			}
		}},
		{Name: "ffmpeg", New: func(t testing.TB) (providers.FrameProvider, contracttest.FramesFixture) {
			installFFmpegFixture(t, "")
			return providers.NewFFmpegFrameProvider(), contracttest.FramesFixture{
				Source:     providers.FileSource(fixtureFile(t, "v.mp4", []byte("video"))),
				DurationMS: 60_000,
				// the script also reports 12.9s, too close to 12.5s
				SceneCutsMS: []int64{12_500, 40_000},
			}
		}},
	}
	contracttest.Frames(t, cases)
}

// installFFmpegFixture puts fixture ffprobe and ffmpeg first on PATH. The
// ffprobe reports a 60s video stream. The ffmpeg reports scene cuts at
// 12.5s, 12.9s and 40s when asked for scene detection, and otherwise
// writes a PNG. It exits 2 when the arguments are not the expected ones:
// a file: input, the default 0.3 scene threshold, an -ss timestamp. When
// argLog is set, every call's arguments are appended to that file.
func installFFmpegFixture(t testing.TB, argLog string) {
	png := fixtureFile(t, "frame.png", tinyPNG)
	logCmd := ":"
	if argLog != "" {
		logCmd = `echo "$*" >> ` + argLog
	}
	fixtureTools(t, map[string]string{
		"ffprobe": `printf '{"streams":[{"duration":"60.0"}],"format":{"duration":"60.5"}}'`,
		"ffmpeg": logCmd + `
case "$*" in *' -i file:/'*) ;; *) echo "input is not a file: path: $*" >&2; exit 2;; esac
for a in "$@"; do case "$a" in select=*)
if [ -n "$FIXTURE_THRESHOLD" ]; then case "$a" in *"gt(scene,$FIXTURE_THRESHOLD)"*) ;; *) echo "bad threshold: $a" >&2; exit 2;; esac; fi
printf 'n:0 pts:1 pts_time:12.5 pos:1\nn:5 pts:3 pts_time:12.9 pos:3\nn:1 pts:2 pts_time:40 pos:2\n' >&2; exit 0;; esac; done
case "$*" in *' -ss 00:'*) ;; *) echo "no -ss timestamp: $*" >&2; exit 2;; esac
cat ` + png,
	})
}

func TestFFmpegFramesArgs(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "args.log")
	installFFmpegFixture(t, logFile)
	t.Setenv("FIXTURE_THRESHOLD", "0.5")
	src := providers.FileSource(fixtureFile(t, "v.mp4", []byte("video")))
	opts := providers.FrameOptions{MaxFPS: 1, SceneDetection: true, SceneThreshold: 0.5, MaxDimension: 640}
	frames, err := providers.CollectFrames(context.Background(), providers.NewFFmpegFrameProvider(), src, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3 (0, 12.5s, 40s)", len(frames))
	}
	raw, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	log := string(raw)
	for _, want := range []string{
		"gt(scene,0.5)",
		"-ss 00:00:12.500",
		"-ss 00:00:40.000",
		`scale=w=min(iw\,640):h=min(ih\,640)`,
		"-frames:v 1",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("no ffmpeg call with %q in:\n%s", want, log)
		}
	}
	if strings.Contains(log, "force_divisible_by") {
		t.Error("the scale filter must not force even sides: a 1px source would collapse")
	}
	if strings.Contains(log, "-ss 00:00:12.900") {
		t.Error("the 12.9s cut is closer than 1s to 12.5s and must not be extracted")
	}
}

func TestFFmpegFramesRejectsRelativePath(t *testing.T) {
	installFFmpegFixture(t, "")
	_, err := providers.CollectFrames(context.Background(), providers.NewFFmpegFrameProvider(), providers.FileSource("rel:ative.mp4"), providers.FrameOptions{MaxFPS: 1})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("err = %v, want an absolute-path error", err)
	}
}

func TestFFmpegFramesNoVideoStream(t *testing.T) {
	fixtureTools(t, map[string]string{
		"ffprobe": `printf '{"streams":[],"format":{"duration":"60.5"}}'`,
		"ffmpeg":  `exit 2`,
	})
	src := providers.FileSource(fixtureFile(t, "a.mp4", []byte("audio only")))
	frames, err := providers.CollectFrames(context.Background(), providers.NewFFmpegFrameProvider(), src, providers.FrameOptions{MaxFPS: 1})
	if err != nil || len(frames) != 0 {
		t.Fatalf("got %d frames, %v; want none and no error", len(frames), err)
	}
}

func TestFFmpegFramesSkipsCoverArt(t *testing.T) {
	// The first video stream is attached art with a bogus duration; the
	// real stream is second.
	png := fixtureFile(t, "frame.png", tinyPNG)
	fixtureTools(t, map[string]string{
		"ffprobe": `printf '{"streams":[{"duration":"5.0","disposition":{"attached_pic":1}},{"duration":"30.0","disposition":{"attached_pic":0}}],"format":{}}'`,
		"ffmpeg":  "cat " + png,
	})
	src := providers.FileSource(fixtureFile(t, "v.mp4", []byte("video")))
	frames, err := providers.CollectFrames(context.Background(), providers.NewFFmpegFrameProvider(), src, providers.FrameOptions{MaxFPS: 0.1})
	if err != nil || len(frames) != 3 { // 0, 10s, 20s of a 30s video
		t.Fatalf("got %d frames, %v; want 3", len(frames), err)
	}
	// Only cover art: no video to sample.
	fixtureTools(t, map[string]string{
		"ffprobe": `printf '{"streams":[{"duration":"5.0","disposition":{"attached_pic":1}}],"format":{"duration":"5.0"}}'`,
		"ffmpeg":  "cat " + png,
	})
	frames, err = providers.CollectFrames(context.Background(), providers.NewFFmpegFrameProvider(), src, providers.FrameOptions{MaxFPS: 1})
	if err != nil || len(frames) != 0 {
		t.Fatalf("cover art only: got %d frames, %v; want none", len(frames), err)
	}
}

func TestFFmpegFramesLongRecordingIsSampledInFull(t *testing.T) {
	// 30h: beyond the old 24h cap. Frames must span it.
	png := fixtureFile(t, "frame.png", tinyPNG)
	fixtureTools(t, map[string]string{
		"ffprobe": `printf '{"streams":[{"duration":"108000.0"}],"format":{}}'`,
		"ffmpeg":  "cat " + png,
	})
	src := providers.FileSource(fixtureFile(t, "v.mp4", []byte("video")))
	frames, err := providers.CollectFrames(context.Background(), providers.NewFFmpegFrameProvider(), src, providers.FrameOptions{MinFPS: 0.001, MaxFrames: 50})
	if err != nil || len(frames) == 0 || frames[len(frames)-1].TimestampMS < 90_000_000 {
		t.Fatalf("30h recording: %d frames, err %v", len(frames), err)
	}
}

func TestFFmpegFramesUnusableDurationIsPermanent(t *testing.T) {
	fixtureTools(t, map[string]string{
		"ffprobe": `printf '{"streams":[{"duration":"N/A"}],"format":{"duration":"N/A"}}'`,
		"ffmpeg":  `exit 2`,
	})
	src := providers.FileSource(fixtureFile(t, "v.mp4", []byte("video")))
	_, err := providers.CollectFrames(context.Background(), providers.NewFFmpegFrameProvider(), src, providers.FrameOptions{MaxFPS: 1})
	if !errors.Is(err, providers.ErrUnusableMedia) {
		t.Fatalf("err = %v, want ErrUnusableMedia", err)
	}
}

func TestFFmpegFramesStreamDurationWins(t *testing.T) {
	// Container says 1e12 s; the video stream says 30s.
	png := fixtureFile(t, "frame.png", tinyPNG)
	fixtureTools(t, map[string]string{
		"ffprobe": `printf '{"streams":[{"duration":"30.0"}],"format":{"duration":"1000000000000"}}'`,
		"ffmpeg":  "cat " + png,
	})
	src := providers.FileSource(fixtureFile(t, "v.mp4", []byte("video")))
	frames, err := providers.CollectFrames(context.Background(), providers.NewFFmpegFrameProvider(), src, providers.FrameOptions{MaxFPS: 0.1})
	if err != nil || len(frames) != 3 { // 0, 10s, 20s
		t.Fatalf("got %d frames, %v; want 3", len(frames), err)
	}
}

func TestFFmpegFramesRejectsAbsurdDuration(t *testing.T) {
	// ffmpeg would happily extract frames, so only the duration check
	// can produce the error.
	png := fixtureFile(t, "frame.png", tinyPNG)
	fixtureTools(t, map[string]string{
		"ffprobe": `printf '{"streams":[{"duration":"1000000000000"}],"format":{}}'`,
		"ffmpeg":  "cat " + png,
	})
	src := providers.FileSource(fixtureFile(t, "v.mp4", []byte("video")))
	if _, err := providers.CollectFrames(context.Background(), providers.NewFFmpegFrameProvider(), src, providers.FrameOptions{MaxFPS: 1}); err == nil {
		t.Fatal("want an error for an implausible duration")
	}
}

func TestFFmpegFramesDropsTrailingEmptyFrames(t *testing.T) {
	// The ffmpeg fixture emits an image only for seeks before 20s.
	png := fixtureFile(t, "frame.png", tinyPNG)
	fixtureTools(t, map[string]string{
		"ffprobe": `printf '{"streams":[{"duration":"60.0"}],"format":{}}'`,
		"ffmpeg":  `case "$*" in *' -ss 00:00:0'*|*' -ss 00:00:1'*) cat ` + png + `;; esac`,
	})
	src := providers.FileSource(fixtureFile(t, "v.mp4", []byte("video")))
	frames, err := providers.CollectFrames(context.Background(), providers.NewFFmpegFrameProvider(), src, providers.FrameOptions{MaxFPS: 0.1})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || frames[1].Index != 1 { // 0s and 10s kept, 20s..50s dropped
		t.Fatalf("got %d frames, want 2 with contiguous indexes", len(frames))
	}
}

func TestContractSummarization(t *testing.T) {
	prompt, err := providers.NewSummaryPrompt("")
	if err != nil {
		t.Fatal(err)
	}
	cases := []contracttest.Case[providers.SummarizationProvider]{
		{Name: "stub", Stub: true, New: func(testing.TB) providers.SummarizationProvider { return providers.NewStubSummarizationProvider() }},
		{Name: "ollama", New: func(testing.TB) providers.SummarizationProvider {
			return providers.NewOllamaSummarizer("http://ollama.invalid", "chat-model", prompt).
				WithHTTPClient(cannedClient(`{"message":{"role":"assistant","content":"Ship on Friday."}}`))
		}},
		{Name: "openai", New: func(testing.TB) providers.SummarizationProvider {
			llm := providers.NewOpenAILLMProvider("key", "m").WithHTTPClient(cannedClient(`{"choices":[{"message":{"content":"Ship on Friday."}}]}`))
			return providers.NewLLMSummarizer(llm, prompt)
		}},
		{Name: "anthropic", New: func(testing.TB) providers.SummarizationProvider {
			llm := providers.NewAnthropicLLMProvider("key", "m").WithHTTPClient(cannedClient(`{"content":[{"type":"text","text":"Ship on Friday."}]}`))
			return providers.NewLLMSummarizer(llm, prompt)
		}},
	}
	contracttest.Summarization(t, cases)
}

func TestContractLLM(t *testing.T) {
	cases := []contracttest.Case[providers.LLMProvider]{
		{Name: "stub", Stub: true, New: func(testing.TB) providers.LLMProvider { return providers.NewStubLLMProvider() }},
		{Name: "ollama", New: func(testing.TB) providers.LLMProvider {
			return providers.NewOllamaLLMProvider("http://ollama.invalid", "m").WithHTTPClient(cannedClient(`{"response":"hello"}`))
		}},
		{Name: "openai", New: func(testing.TB) providers.LLMProvider {
			return providers.NewOpenAILLMProvider("key", "m").WithHTTPClient(cannedClient(`{"choices":[{"message":{"content":"hello"}}]}`))
		}},
		{Name: "anthropic", New: func(testing.TB) providers.LLMProvider {
			return providers.NewAnthropicLLMProvider("key", "m").WithHTTPClient(cannedClient(`{"content":[{"type":"text","text":"hello"}]}`))
		}},
	}
	contracttest.LLM(t, cases)
}

func TestContractVision(t *testing.T) {
	cases := []contracttest.Case[vision.Provider]{
		{Name: "stub", Stub: true, New: func(testing.TB) vision.Provider { return vision.NewStubProvider() }},
		{Name: "openai", New: func(testing.TB) vision.Provider {
			return vision.NewOpenAIProvider("key", "m").WithHTTPClient(cannedClient(openAIBody))
		}},
		{Name: "anthropic", New: func(testing.TB) vision.Provider {
			return vision.NewAnthropicProvider("key", "m").WithHTTPClient(cannedClient(anthropicBody))
		}},
		{Name: "gemini", New: func(testing.TB) vision.Provider {
			return vision.NewGeminiProvider("key", "m").WithHTTPClient(cannedClient(`{"candidates":[{"content":{"parts":[{"text":"A red square. Labels: square, red"}]}}]}`))
		}},
		{Name: "openrouter", New: func(testing.TB) vision.Provider {
			return vision.NewOpenRouterProvider("key", "m").WithHTTPClient(cannedClient(openAIBody))
		}},
	}
	contracttest.Vision(t, cases, tinyPNG)
}

func TestContractOCR(t *testing.T) {
	cases := []contracttest.Case[providers.OCRProvider]{
		{Name: "stub", Stub: true, New: func(testing.TB) providers.OCRProvider { return providers.NewStubOCRProvider() }},
		{Name: "tesseract", New: func(t testing.TB) providers.OCRProvider {
			fixtureTools(t, map[string]string{
				"tesseract": `for a in "$@"; do [ "$a" = tsv ] && { printf 'level\tconf\ttext\n5\t90\tHello\n'; exit 0; }; done
printf 'Hello world\n'`,
			})
			return providers.NewTesseractOCRProvider("eng")
		}},
	}
	contracttest.OCR(t, cases, tinyPNG)
}

func TestContractTranscription(t *testing.T) {
	cases := []contracttest.Case[providers.TranscriptionProvider]{
		{Name: "stub", Stub: true, New: func(testing.TB) providers.TranscriptionProvider { return providers.NewStubTranscriptionProvider() }},
		{Name: "whisper", New: func(t testing.TB) providers.TranscriptionProvider {
			fixtureTools(t, map[string]string{
				"whisper-fixture": `printf '{"text":"hello there","language":"en","segments":[{"start":0,"end":1.5,"text":"hello"},{"start":1.5,"end":3,"text":"there"}]}'`,
			})
			return providers.NewWhisperTranscriptionProvider("whisper-fixture")
		}},
	}
	contracttest.Transcription(t, cases, "/nonexistent/a.wav")
}

func TestContractDiarization(t *testing.T) {
	cases := []contracttest.Case[providers.DiarizationProvider]{
		{Name: "stub", Stub: true, New: func(testing.TB) providers.DiarizationProvider { return providers.NewStubDiarizationProvider() }},
		{Name: "pyannote", New: func(t testing.TB) providers.DiarizationProvider {
			fixtureTools(t, map[string]string{
				"pyannote-fixture": `printf 'SPEAKER a 1 0.0 4.0 <NA> <NA> S1 <NA> <NA>\nSPEAKER a 1 4.0 5.0 <NA> <NA> S2 <NA> <NA>\n'`,
			})
			return providers.NewPyannoteDiarizationProvider("pyannote-fixture")
		}},
	}
	contracttest.Diarization(t, cases, "/nonexistent/a.wav")
}

func TestContractVideo(t *testing.T) {
	cases := []contracttest.Case[providers.VideoProvider]{
		{Name: "stub", Stub: true, New: func(testing.TB) providers.VideoProvider { return providers.NewStubVideoProvider() }},
		{Name: "ffmpeg", New: func(t testing.TB) providers.VideoProvider {
			fixtureTools(t, map[string]string{
				"ffprobe": `printf '{"streams":[{"codec_type":"video","codec_name":"h264","width":640,"height":360,"r_frame_rate":"30/1"},{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"60.0","size":"1000"}}'`,
				"ffmpeg":  `for a; do :; done; : > "$a"`,
			})
			return providers.NewFFmpegVideoProvider()
		}},
	}
	contracttest.Video(t, cases, filepath.Join(t.TempDir(), "v.mp4"))
}

func TestContractDocument(t *testing.T) {
	cases := []contracttest.Case[providers.DocumentProvider]{
		{Name: "stub", Stub: true, New: func(testing.TB) providers.DocumentProvider { return providers.NewStubDocumentProvider() }},
		{Name: "golib", New: func(testing.TB) providers.DocumentProvider { return providers.NewGolibDocumentProvider() }},
	}
	contracttest.Document(t, cases, filepath.Join("testdata", "survey.docx"))
}

func TestFFmpegVideoRejectsRelativePaths(t *testing.T) {
	p := providers.NewFFmpegVideoProvider()
	if _, err := p.ProbeMetadata(context.Background(), "rel:ative.mp4"); !errors.Is(err, providers.ErrUnusableMedia) {
		t.Errorf("ProbeMetadata: err = %v", err)
	}
	if _, err := p.ExtractAudio(context.Background(), "rel:ative.mp4", t.TempDir()); !errors.Is(err, providers.ErrUnusableMedia) {
		t.Errorf("ExtractAudio: err = %v", err)
	}
}

func TestFFmpegVideoInputsUseTheFilePrefix(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "args.log")
	fixtureTools(t, map[string]string{
		"ffprobe": `echo "$*" >> ` + logFile + `; printf '{"format":{"duration":"1"}}'`,
		"ffmpeg":  `echo "$*" >> ` + logFile + `; for a; do :; done; : > "$a"`,
	})
	p := providers.NewFFmpegVideoProvider()
	video := filepath.Join(t.TempDir(), "v.mp4")
	if _, err := p.ProbeMetadata(context.Background(), video); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ExtractAudio(context.Background(), video, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(logFile)
	if got := strings.Count(string(raw), "file:"+video); got != 2 {
		t.Errorf("%d calls used the file: prefix, want 2:\n%s", got, raw)
	}
}
