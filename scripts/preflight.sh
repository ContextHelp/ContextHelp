#!/usr/bin/env bash
# Pre-flight: reports whether build tools, ingestor binaries and API keys
# are present. Read-only; exits 1 when a required binary is missing.
# `make preflight` runs it; `make deps` installs what is missing.
set -u

PASS=0; WARN=0; FAIL=0
ok()   { printf '  ✓  %-30s %s\n' "$1" "${2:-}"; PASS=$((PASS+1)); }
warn() { printf '  ⚠  %-30s %s\n' "$1" "${2:-}"; WARN=$((WARN+1)); }
fail() { printf '  ✗  %-30s %s\n' "$1" "${2:-}"; FAIL=$((FAIL+1)); }
check_bin() {
  local bin="$1"; local label="${2:-$1}"; local note="${3:-}"
  if p=$(command -v "$bin" 2>/dev/null); then ok "$label" "$p"
  else fail "$label" "${note:-not found}"; fi
}
check_env() {
  local var="$1"; local label="${2:-$var}"; local sev="${3:-warn}"
  if [ -n "${!var:-}" ]; then ok "$label" "(set)"
  elif [ "$sev" = fail ]; then fail "$label" "not set"
  else warn "$label" "not set — vision/llm pipelines may be limited"; fi
}

PATH="/opt/homebrew/bin:/usr/local/bin:$HOME/.local/bin:$PATH"

echo "── Build tools ──────────────────────────────────────────────"
check_bin go           "go"
check_bin golangci-lint "golangci-lint"
check_bin govulncheck  "govulncheck"

echo "── OCR ──────────────────────────────────────────────────────"
check_bin tesseract    "tesseract"

echo "── Audio / Transcription ────────────────────────────────────"
check_bin ffmpeg       "ffmpeg"
check_bin ffprobe      "ffprobe"
if command -v whisper >/dev/null 2>&1; then
  ok "whisper" "$(command -v whisper)"
elif command -v whisper-cpp >/dev/null 2>&1; then
  ok "whisper-cpp" "$(command -v whisper-cpp)"
else
  fail "whisper / whisper-cpp" "required for audio transcription"
fi

echo "── Diarization ──────────────────────────────────────────────"
if command -v pyannote-audio >/dev/null 2>&1; then
  ok "pyannote-audio" "$(command -v pyannote-audio)"
elif command -v pyannote >/dev/null 2>&1; then
  ok "pyannote" "$(command -v pyannote)"
else
  warn "pyannote-audio" "not found — diarization pipeline disabled"
fi

echo "── PDF ──────────────────────────────────────────────────────"
check_bin pdftotext    "pdftotext (poppler)"

echo "── Office documents ─────────────────────────────────────────"
if command -v soffice >/dev/null 2>&1; then
  ok "soffice (LibreOffice)" "$(command -v soffice)"
else
  warn "soffice (LibreOffice)" "not found — Office doc pipeline disabled (make deps)"
fi

echo "── Markdown / Docs ──────────────────────────────────────────"
check_bin pandoc       "pandoc"

echo "── API keys (vision / LLM) ──────────────────────────────────"
check_env OPENAI_API_KEY      "OPENAI_API_KEY"
check_env ANTHROPIC_API_KEY   "ANTHROPIC_API_KEY"
check_env GEMINI_API_KEY      "GEMINI_API_KEY"
check_env OPENROUTER_API_KEY  "OPENROUTER_API_KEY"
if command -v ollama >/dev/null 2>&1; then
  ok "ollama (local vision/LLM)" "$(command -v ollama)"
else
  warn "ollama" "not found — local vision/LLM unavailable"
fi

echo ""
echo "Result: ${PASS} ok  ${WARN} warnings  ${FAIL} failures"
[ "$FAIL" -eq 0 ] || exit 1
