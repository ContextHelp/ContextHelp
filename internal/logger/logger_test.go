package logger_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/logger"
)

func TestDefaultLevelIsWarn(t *testing.T) {
	logger.Init(false)
	if slog.Default().Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("debug should be disabled by default")
	}
	if slog.Default().Enabled(context.TODO(), slog.LevelInfo) {
		t.Error("info should be disabled by default")
	}
	if !slog.Default().Enabled(context.TODO(), slog.LevelWarn) {
		t.Error("warn should be enabled by default")
	}
}

func TestVerboseEnablesDebug(t *testing.T) {
	logger.Init(true)
	defer logger.Init(false)

	if !slog.Default().Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("debug should be enabled when verbose=true")
	}
}

func TestVerboseOutputIncludesDebug(t *testing.T) {
	var buf bytes.Buffer
	logger.InitWithWriter(true, &buf)
	defer logger.Init(false)

	slog.Debug("test debug message", "key", "val")

	out := buf.String()
	if !strings.Contains(out, "test debug message") {
		t.Errorf("expected debug message in output, got: %s", out)
	}
}

func TestNonVerboseOutputExcludesDebug(t *testing.T) {
	var buf bytes.Buffer
	logger.InitWithWriter(false, &buf)
	defer logger.Init(false)

	slog.Debug("should not appear")

	if buf.Len() > 0 {
		t.Errorf("expected no output at warn level, got: %s", buf.String())
	}
}
