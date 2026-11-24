# Testing media providers with cassettes

Provider tests for images, audio and video replay recorded tool output, so
CI needs no tesseract, whisper, ffmpeg, pyannote or Ollama. Cassettes are
[xrr](https://github.com/hop-top/xrr) YAML, committed next to the tests.
Helpers live in `internal/providers/providertest`.

## Fixtures

Small real media with known content (`providertest.Media*`):

| Constant | File | Known content |
|----------|------|---------------|
| `MediaImage` | `text.png` | `ImageLines`: "Cassette fixture", "invoice 4821" |
| `MediaAudio` | `speech.wav` | `SpeechTurns`: two voices, two lines, 8.4 s |
| `MediaVideo` | `clip.mp4` | `VideoCards`: two text cards + the speech audio, 8.4 s |
| `MediaSubSRT`, `MediaSubVTT` | `speech.srt`, `speech.vtt` | captions of the audio |

```go
img := providertest.MediaBytes(t, providertest.MediaImage)   // bytes
path := providertest.MediaFile(t, providertest.MediaVideo)   // copy in a temp dir
```

Fixtures are embedded, so any package's tests reach them regardless of the
working directory. Licences, provenance and regeneration:
`internal/providers/providertest/testdata/media/README.md` and
`scripts/gen-media-fixtures.sh`. Changing a fixture changes its bytes, so
re-record every cassette that used it.

## Exec cassettes (CLI tools)

Providers run tools through `providers.RunCommand` and find them with
`providers.LookupToolContext`. A context carrying the harness replaces both:

```go
r := providertest.ExecCassette(t, "testdata/cassettes/tesseract")
ctx := providers.WithCommandRunner(context.Background(), r.Run)
ctx = providers.WithToolLookup(ctx, r.Lookup)
res, err := providers.NewTesseractOCRProvider("eng").Extract(ctx, img, "image/png")
```

For a factory, `factory.WithContext(ctx)` carries the same seams to its
tool probes.

- One cassette dir per scenario; steps of a chained flow share a dir.
- Replay does not run the recorded tools, so they need not be installed. A
  call with no cassette fails with an error that matches
  `xrr.ErrCassetteMiss` and `providers.IsCommandNotRecorded`.
- Argv is normalised before fingerprinting (`providertest.NormalizeArg`):
  an existing temp file becomes `<file:HASH.ext>` (hash of its bytes), a
  not-yet-existing temp path becomes `<tmp>/NAME` (base name only). Random
  temp names and per-test temp dirs therefore do not matter. A `file:`
  prefix on an absolute path (the input guard of ffmpeg and ffprobe) is
  dropped before this. A call whose
  argument is a non-temp path (a fixed output location, say) is keyed on
  that path as written.
- Files the tool creates at a `<tmp>/NAME` argument (extracted audio) are stored as `blob-*.bin` beside the cassette and recreated on
  replay, so a later step that reads them gets the same bytes. Files a tool
  writes anywhere else (for example whisper's `-oj`, which writes next to
  its input) are not captured. Neither are files a provider creates itself
  and then passes to the tool (an `os.CreateTemp` input): they exist before
  the call, so they are inputs, keyed by content.
- Replay writes only to the call's own output arguments, and only from
  `blob-<16 hex>.bin` files in the cassette dir. A cassette that names any
  other argument, or another blob path, makes the call fail.
- Recorded stdout and stderr have temp dirs and the developer's real home
  directory (`testguard.ScrubHome`, which knows the pre-isolation HOME and matches
  whole path components) replaced by `<tmp>` and `<home>`. Argv gets the
  same home scrub.
- Recording fails when an argument is an absolute path outside the temp
  dirs, including one under the home directory, since it would tie the
  cassette to one machine.
- `r.Calls()` returns the normalised argv per call. Assert the expected
  count in every exec test: with the seam bypassed, the real tool may run
  where it is installed and the test would still pass, but the runner sees
  no calls.

## HTTP cassettes (Ollama vision, chat, embeddings)

```go
client, calls := providertest.OllamaClient(t, "testdata/cassettes/ollama-vision")
p := vision.NewOllamaProvider("http://127.0.0.1:11434", "qwen2.5vl:3b").WithHTTPClient(client)
```

`calls.URLs()` and `calls.Bodies()` show what the provider sent, for
example that the original image bytes reached the model. `Factory.WithHTTPClient`
carries the same client to the Ollama LLM and vision providers.

## Record

Record once, on a machine with the tools and models, then commit the
cassettes:

```bash
XRR_MODE=record go test -tags fts5 -count=1 -run Cassette ./internal/providers/...
```

- `XRR_MODE=record` runs the real tool or calls the real Ollama and writes
  the cassette. Without it tests replay.
- `XRR_OLLAMA_UPSTREAM` picks the Ollama that answers (default
  `http://127.0.0.1:11434`). Pull the models the test names first.
- Recording needs: `tesseract` (`eng`), `ffmpeg`/`ffprobe`, Ollama with
  `qwen2.5vl:3b` and `qwen3:4b-instruct-2507-q4_K_M`. For audio:
  `whisper-cli` or `openai-whisper` plus a model; for diarization:
  `pyannote-audio` with a Hugging Face token.
- Delete a cassette dir before re-recording it, so stale entries and blobs
  go.
- Review the diff before committing: no secrets, no machine paths.

## Replay

Plain `go test`. `testguard` isolates HOME and refuses default server
ports. Replay opens no sockets and does not run the recorded tools or call
Ollama; the harness's own tests run `sh` and `cat` to exercise the
record-then-replay path.

## Adding a provider

1. Run its tools through `providers.RunCommand` and find them with
   `LookupToolContext` (or take an HTTP client).
2. Write the test against a fixture, asserting the fixture's known text and
   the number of recorded calls.
3. Record, and commit the cassette dir under `testdata/cassettes/<name>`.
4. Keep the test deterministic: no assertions on timings or model wording
   beyond what the fixture guarantees.
