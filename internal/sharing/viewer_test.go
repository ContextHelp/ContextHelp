package sharing

import (
	"context"
	"testing"
)

func TestZeroViewerIsRestricted(t *testing.T) {
	var v Viewer
	if v.IsUnrestricted() {
		t.Fatal("zero Viewer must not be unrestricted")
	}
}

func TestUnrestrictedIsExplicit(t *testing.T) {
	if !Unrestricted.IsUnrestricted() {
		t.Fatal("Unrestricted must report IsUnrestricted")
	}
	if Unrestricted == (Viewer{}) {
		t.Fatal("Unrestricted must differ from the zero Viewer")
	}
}

func TestFromContextMissingViewerIsNotUnrestricted(t *testing.T) {
	v, ok := FromContext(context.Background())
	if ok {
		t.Fatal("bare context must report no viewer")
	}
	if v.IsUnrestricted() {
		t.Fatal("missing viewer must not read as unrestricted")
	}
	if v != (Viewer{}) {
		t.Fatalf("missing viewer = %+v, want zero Viewer", v)
	}
}

func TestWithViewerRoundTrip(t *testing.T) {
	ctx := WithViewer(context.Background(), Unrestricted)
	v, ok := FromContext(ctx)
	if !ok || !v.IsUnrestricted() {
		t.Fatalf("FromContext = (%+v, %v), want (Unrestricted, true)", v, ok)
	}

	// An explicitly attached zero viewer is present but restricted.
	ctx = WithViewer(context.Background(), Viewer{})
	v, ok = FromContext(ctx)
	if !ok {
		t.Fatal("explicit zero viewer must report as attached")
	}
	if v.IsUnrestricted() {
		t.Fatal("explicit zero viewer must stay restricted")
	}
}

func TestWithViewerLastAttachWins(t *testing.T) {
	ctx := WithViewer(WithViewer(context.Background(), Unrestricted), Viewer{})
	v, _ := FromContext(ctx)
	if v.IsUnrestricted() {
		t.Fatal("a later restricted attach must override an earlier Unrestricted")
	}
}
