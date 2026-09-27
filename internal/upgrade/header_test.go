package upgrade

import (
	"strings"
	"testing"
)

// The header carries only what the banner renders, as an RFC 8941
// dictionary: states and buckets as tokens, counts as integers, free
// text as strings.
func TestEncodeHeader(t *testing.T) {
	cases := []struct {
		name string
		st   Status
		want string
	}{
		{"idle", Status{State: StateIdle}, ""},
		{"zero", Status{}, ""},
		{
			"in_progress",
			Status{State: StateInProgress, Bucket: BucketReingestSelective, Done: 47, Total: 120, Progress: 0.39, EtaSeconds: 32},
			"state=in_progress, bucket=reingest_selective, done=47, total=120, eta=32",
		},
		{
			"target and failed count",
			Status{State: StateInProgress, Bucket: BucketEmbeddingsMigrate, Target: "nomic-embed:v1.5", Done: 3, Total: 9, Failed: 1},
			`state=in_progress, bucket=embeddings_migrate, target="nomic-embed:v1.5", done=3, total=9, failed=1, eta=0`,
		},
		{
			"failed",
			Status{State: StateFailed, Bucket: BucketReindexAuto, Done: 2, Total: 5, LastError: `disk "full" \ retry`},
			`state=failed, bucket=reindex_auto, done=2, total=5, eta=0, error="disk \"full\" \\ retry"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EncodeHeader(tc.st); got != tc.want {
				t.Errorf("EncodeHeader = %q\n want %q", got, tc.want)
			}
		})
	}
}

// Header values are printable ASCII: an error message carrying control
// bytes, newlines or non-ASCII text cannot inject into the header or
// into the terminal that prints the banner.
func TestEncodeHeaderSanitizesError(t *testing.T) {
	v := EncodeHeader(Status{State: StateFailed, Bucket: BucketReindexAuto, LastError: "bad\r\nX-Evil: 1\x1b[31m é"})
	for _, b := range []byte(v) {
		if b < 0x20 || b > 0x7e {
			t.Fatalf("header %q carries byte %#x", v, b)
		}
	}
	st, err := DecodeHeader(v)
	if err != nil {
		t.Fatalf("decode %q: %v", v, err)
	}
	if strings.ContainsAny(st.LastError, "\r\n\x1b") {
		t.Fatalf("LastError = %q still carries control bytes", st.LastError)
	}
}

// A long error is cut so the header stays compact.
func TestEncodeHeaderTruncatesError(t *testing.T) {
	v := EncodeHeader(Status{State: StateFailed, Bucket: BucketReindexAuto, LastError: strings.Repeat("x", 4096)})
	if len(v) > 512 {
		t.Fatalf("header is %d bytes, want a compact value", len(v))
	}
}

// DecodeHeader inverts EncodeHeader for every field the banner reads, and
// derives progress from done and total.
func TestDecodeHeaderRoundTrip(t *testing.T) {
	for _, st := range []Status{
		{State: StateInProgress, Bucket: BucketReingestSelective, Done: 47, Total: 120, EtaSeconds: 32},
		{State: StateInProgress, Bucket: BucketEmbeddingsMigrate, Target: "model-b@1", Done: 3, Total: 9, Failed: 1},
		{State: StateFailed, Bucket: BucketReindexAuto, Done: 2, Total: 5, LastError: `disk "full" \ retry`},
		{State: StateAwaitingConsent, Bucket: BucketReingestAll},
	} {
		got, err := DecodeHeader(EncodeHeader(st))
		if err != nil {
			t.Fatalf("decode %+v: %v", st, err)
		}
		want := st
		if want.Total > 0 {
			want.Progress = float64(want.Done) / float64(want.Total)
		}
		if got != want {
			t.Errorf("round trip:\n got %+v\nwant %+v", got, want)
		}
	}
}

// Unknown members and parameters are skipped, so the server can add
// fields without breaking older clients; token and string forms both
// decode.
func TestDecodeHeaderTolerant(t *testing.T) {
	st, err := DecodeHeader(`bucket="reindex_auto";p=1, future=?1, rate=1.5, blob=:AAE=:, state=in_progress , done=1,total=4`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.State != StateInProgress || st.Bucket != BucketReindexAuto || st.Done != 1 || st.Total != 4 || st.Progress != 0.25 {
		t.Fatalf("decoded %+v", st)
	}
}

// A value that is not a dictionary, or carries no state, is an error: the
// client shows no banner rather than a wrong one.
func TestDecodeHeaderRejects(t *testing.T) {
	for _, v := range []string{
		"",
		"bucket=reindex_auto",
		"state=idle",
		"state=",
		`state="unterminated`,
		"state=in_progress,,done=1",
		"State=in_progress",
		"state=in_progress done=1",
		"done=abc, state=in_progress",
		"state=in_progress, done=12345678901234567",
		`state=in_progress, error="tab	inside"`,
	} {
		if st, err := DecodeHeader(v); err == nil {
			t.Errorf("DecodeHeader(%q) = %+v, want an error", v, st)
		}
	}
}
