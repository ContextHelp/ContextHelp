// Package youtube implements YouTubeStrategy for youtube.com + youtu.be.
//
// Captured forms supported:
//
//   - /watch?v=<id>           single video
//   - /shorts/<id>            short video
//   - /playlist?list=<id>     playlist
//   - /@handle, /c/<name>     channel handle / custom URL
//   - /channel/<UCxxxx>       canonical channel ID
//   - /user/<legacy>          legacy username
//   - youtu.be/<id>           short-form video
//
// For each, the strategy emits the canonical asset plus uploader anchor
// candidates so the resolver can dedup videos that arrive both via
// youtu.be shortlinks and full /watch URLs.
package youtube

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

const ID = "YouTubeStrategy"

const (
	CandidateTypeVideo    = "yt_video"
	CandidateTypeChannel  = "yt_channel"
	CandidateTypePlaylist = "yt_playlist"
	CandidateTypeUploads  = "yt_uploads"
	CandidateTypeShort    = "yt_short"
)

// YouTubeClient is the daemon-side fetcher. Strategy uses it to resolve
// a custom URL (handle / c / user) into its UC-prefixed channel id, and
// to attach a canonical channel to a watched video.
type YouTubeClient interface {
	ResolveChannel(ctx context.Context, vanity string) (channelID string, err error)
	VideoUploader(ctx context.Context, videoID string) (channelID string, err error)
}

type Strategy struct {
	Client YouTubeClient
}

func New(c YouTubeClient) *Strategy { return &Strategy{Client: c} }

func (*Strategy) ID() string                    { return ID }
func (*Strategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*Strategy) Preconditions() []string       { return nil }

func (*Strategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	host := hostOf(ev.SourceURL)
	switch {
	case host == "youtube.com" || strings.HasSuffix(host, ".youtube.com"):
		spec := 1
		if host != "youtube.com" {
			spec++
		}
		return lateral.AppliesResult{Matches: true, Specificity: spec}
	case host == "youtu.be":
		return lateral.AppliesResult{Matches: true, Specificity: 2}
	}
	return lateral.AppliesResult{}
}

func (s *Strategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, err := url.Parse(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(u.Hostname())
	apex := "https://www.youtube.com"

	// youtu.be/<id> → video shortlink. Extract only the first non-empty
	// path segment so trailing segments (e.g. `youtu.be/<id>/extra`) or
	// trailing slashes don't bleed into the canonical /watch?v= URL.
	if host == "youtu.be" {
		segs := pathSegments(u.Path)
		if len(segs) == 0 || segs[0] == "" {
			return nil, nil
		}
		return s.videoCandidates(ctx, apex, segs[0]), nil
	}

	q := u.Query()
	parts := pathSegments(u.Path)

	switch {
	case len(parts) >= 1 && parts[0] == "watch":
		v := q.Get("v")
		if v == "" {
			return nil, nil
		}
		return s.videoCandidates(ctx, apex, v), nil
	case len(parts) >= 2 && parts[0] == "shorts":
		id := parts[1]
		out := s.videoCandidates(ctx, apex, id)
		if len(out) > 0 {
			out[0].CandidateType = CandidateTypeShort
		}
		return out, nil
	case len(parts) >= 1 && parts[0] == "playlist":
		listID := q.Get("list")
		if listID == "" {
			return nil, nil
		}
		return []lateral.Candidate{{
			URL:           apex + "/playlist?list=" + listID,
			CandidateType: CandidateTypePlaylist,
			Strategy:      ID,
			Preview:       identitykey.Set(map[string]any{"playlist_id": listID}, identitykey.Build("youtube", identitykey.EntityPlaylist, listID)),
		}}, nil
	case len(parts) >= 1 && strings.HasPrefix(parts[0], "@"):
		handle := strings.TrimPrefix(parts[0], "@")
		return s.channelCandidates(ctx, apex, "handle", handle), nil
	case len(parts) >= 2 && parts[0] == "c":
		return s.channelCandidates(ctx, apex, "custom", parts[1]), nil
	case len(parts) >= 2 && parts[0] == "user":
		return s.channelCandidates(ctx, apex, "user", parts[1]), nil
	case len(parts) >= 2 && parts[0] == "channel":
		// Canonical UCxxxx channel ID — no client lookup needed.
		ch := parts[1]
		return []lateral.Candidate{
			{
				URL:           apex + "/channel/" + ch,
				CandidateType: CandidateTypeChannel,
				Strategy:      ID,
				Preview:       identitykey.Set(map[string]any{"channel_id": ch}, identitykey.Build("youtube", identitykey.EntityChannel, ch)),
			},
			{
				URL:           apex + "/channel/" + ch + "/videos",
				CandidateType: CandidateTypeUploads,
				Strategy:      ID,
				Preview:       identitykey.Set(map[string]any{"channel_id": ch, "facet": "uploads"}, identitykey.Build("youtube", identitykey.EntityChannel, ch, "uploads")),
			},
		}, nil
	}
	return nil, nil
}

// videoCandidates emits the canonical /watch URL plus uploader probe
// when the daemon client can resolve it.
func (s *Strategy) videoCandidates(ctx context.Context, apex, videoID string) []lateral.Candidate {
	out := []lateral.Candidate{{
		URL:           apex + "/watch?v=" + videoID,
		CandidateType: CandidateTypeVideo,
		Strategy:      ID,
		Preview:       identitykey.Set(map[string]any{"video_id": videoID}, identitykey.Build("youtube", identitykey.EntityVideo, videoID)),
	}}
	if s.Client != nil {
		if ch, err := s.Client.VideoUploader(ctx, videoID); err == nil && ch != "" {
			out = append(out, lateral.Candidate{
				URL:           apex + "/channel/" + ch,
				CandidateType: CandidateTypeChannel,
				Strategy:      ID,
				Preview:       identitykey.Set(map[string]any{"channel_id": ch}, identitykey.Build("youtube", identitykey.EntityChannel, ch)),
			})
		}
	}
	return out
}

// channelCandidates handles vanity URLs (handle/c/user). When the
// daemon client resolves the vanity, the canonical /channel/UCxxxx
// candidate replaces the vanity-keyed identity_key so dedup collapses.
func (s *Strategy) channelCandidates(ctx context.Context, apex, kind, vanity string) []lateral.Candidate {
	url := apex + "/"
	switch kind {
	case "handle":
		url += "@" + vanity
	default:
		url += kind + "/" + vanity
	}
	out := []lateral.Candidate{{
		URL:           url,
		CandidateType: CandidateTypeChannel,
		Strategy:      ID,
		Preview:       identitykey.Set(map[string]any{"vanity": vanity, "kind": kind}, identitykey.Build("youtube", identitykey.EntityChannel, kind+"_"+vanity)),
	}}
	if s.Client != nil {
		if ch, err := s.Client.ResolveChannel(ctx, vanity); err == nil && ch != "" {
			out = append(out,
				lateral.Candidate{
					URL:           apex + "/channel/" + ch,
					CandidateType: CandidateTypeChannel,
					Strategy:      ID,
					Preview:       identitykey.Set(map[string]any{"channel_id": ch, "vanity": vanity}, identitykey.Build("youtube", identitykey.EntityChannel, ch)),
				},
				lateral.Candidate{
					URL:           apex + "/channel/" + ch + "/videos",
					CandidateType: CandidateTypeUploads,
					Strategy:      ID,
					Preview:       identitykey.Set(map[string]any{"channel_id": ch, "facet": "uploads"}, identitykey.Build("youtube", identitykey.EntityChannel, ch, "uploads")),
				},
			)
		}
	}
	return out
}

func hostOf(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func pathSegments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}
