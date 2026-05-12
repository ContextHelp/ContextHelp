package youtube

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

type stubClient struct {
	channel  string
	uploader string
	err      error
}

func (s stubClient) ResolveChannel(_ context.Context, _ string) (string, error) {
	return s.channel, s.err
}

func (s stubClient) VideoUploader(_ context.Context, _ string) (string, error) {
	return s.uploader, s.err
}

func TestApplies(t *testing.T) {
	s := New(nil)
	cases := []struct {
		url     string
		matches bool
	}{
		{"https://www.youtube.com/watch?v=abc", true},
		{"https://youtu.be/abc", true},
		{"https://m.youtube.com/watch?v=abc", true},
		{"https://example.com/watch?v=abc", false},
	}
	for _, tc := range cases {
		if got := s.Applies(context.Background(), lateral.CapturedEvent{SourceURL: tc.url}); got.Matches != tc.matches {
			t.Errorf("%s: matches=%v want=%v", tc.url, got.Matches, tc.matches)
		}
	}
}

func TestProbe_Watch(t *testing.T) {
	s := New(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://www.youtube.com/watch?v=ABCdef12345"}, lateral.ActiveContext{})
	if !hasType(cands, CandidateTypeVideo) {
		t.Fatal("watch URL should produce video candidate")
	}
}

func TestProbe_ShortLink(t *testing.T) {
	s := New(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://youtu.be/ABCdef12345"}, lateral.ActiveContext{})
	if !hasType(cands, CandidateTypeVideo) {
		t.Fatal("youtu.be should produce video candidate")
	}
	// URL must be normalised to /watch?v= form for resolver dedup.
	if cands[0].URL != "https://www.youtube.com/watch?v=ABCdef12345" {
		t.Fatalf("URL not normalised: %s", cands[0].URL)
	}
}

func TestProbe_ShortLink_TrailingSegments(t *testing.T) {
	s := New(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://youtu.be/ABCdef12345/extra/junk"}, lateral.ActiveContext{})
	if len(cands) == 0 {
		t.Fatal("expected video candidate even with trailing path segments")
	}
	if cands[0].URL != "https://www.youtube.com/watch?v=ABCdef12345" {
		t.Fatalf("trailing path bled into canonical URL: %s", cands[0].URL)
	}
}

func TestProbe_ShortLink_TrailingSlash(t *testing.T) {
	s := New(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://youtu.be/ABCdef12345/"}, lateral.ActiveContext{})
	if len(cands) == 0 {
		t.Fatal("expected video candidate with trailing slash")
	}
	if cands[0].URL != "https://www.youtube.com/watch?v=ABCdef12345" {
		t.Fatalf("trailing slash bled into canonical URL: %s", cands[0].URL)
	}
}

func TestProbe_Shorts(t *testing.T) {
	s := New(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://www.youtube.com/shorts/abc123"}, lateral.ActiveContext{})
	if !hasType(cands, CandidateTypeShort) {
		t.Fatal("shorts URL should produce short candidate")
	}
}

func TestProbe_Channel_UCID(t *testing.T) {
	s := New(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://www.youtube.com/channel/UC1234"}, lateral.ActiveContext{})
	if !hasType(cands, CandidateTypeChannel) || !hasType(cands, CandidateTypeUploads) {
		t.Fatalf("UCxxxx channel should produce channel + uploads, got %v", typesOf(cands))
	}
}

func TestProbe_Channel_Handle(t *testing.T) {
	s := New(stubClient{channel: "UC9999"})
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://www.youtube.com/@somehandle"}, lateral.ActiveContext{})
	if len(cands) < 2 {
		t.Fatalf("handle + resolved channel should produce ≥2 candidates, got %d", len(cands))
	}
}

func TestProbe_Playlist(t *testing.T) {
	s := New(nil)
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://www.youtube.com/playlist?list=PLabc"}, lateral.ActiveContext{})
	if !hasType(cands, CandidateTypePlaylist) {
		t.Fatal("playlist URL should produce playlist candidate")
	}
}

func TestProbe_VideoUploaderFromClient(t *testing.T) {
	s := New(stubClient{uploader: "UCdef"})
	cands, _ := s.Probe(context.Background(), lateral.CapturedEvent{SourceURL: "https://www.youtube.com/watch?v=abc"}, lateral.ActiveContext{})
	if !hasType(cands, CandidateTypeChannel) {
		t.Fatal("video uploader resolution should add channel candidate")
	}
}

func hasType(cs []lateral.Candidate, t string) bool {
	for _, c := range cs {
		if c.CandidateType == t {
			return true
		}
	}
	return false
}

func typesOf(cs []lateral.Candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.CandidateType)
	}
	return out
}
