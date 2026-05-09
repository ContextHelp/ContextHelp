package medium

import (
	"context"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/customdomain"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// ProfileStrategy claims captures of /@username on medium.com.
// Specificity: 3 — shadows the parent on /@user paths.
type ProfileStrategy struct {
	Client MediumClient
}

func NewProfile(c MediumClient) *ProfileStrategy { return &ProfileStrategy{Client: c} }

func (*ProfileStrategy) ID() string                    { return IDProfile }
func (*ProfileStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*ProfileStrategy) Preconditions() []string       { return nil }

func (*ProfileStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	d := customdomain.Detect(ev.SourceURL, hintsFromEvent(ev))
	if d.Platform != customdomain.PlatformMedium {
		return lateral.AppliesResult{}
	}
	_, parts, err := parseURL(ev.SourceURL)
	if err != nil || len(parts) == 0 {
		return lateral.AppliesResult{}
	}
	if strings.HasPrefix(parts[0], "@") && len(parts[0]) > 1 {
		return lateral.AppliesResult{Matches: true, Specificity: 3}
	}
	return lateral.AppliesResult{}
}

func (*ProfileStrategy) Probe(_ context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	_, parts, err := parseURL(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "@") {
		return nil, nil
	}
	username := strings.TrimPrefix(parts[0], "@")
	apex := "https://medium.com"
	idKey := identitykey.Build("medium", identitykey.EntityProfile, username)
	return []lateral.Candidate{
		{
			URL:           apex + "/@" + username,
			CandidateType: CandidateTypeAuthor,
			Strategy:      IDProfile,
			Preview:       identitykey.Set(map[string]any{"username": username}, idKey),
		},
		{
			URL:           apex + "/@" + username + "/following",
			CandidateType: CandidateTypeAuthor,
			Strategy:      IDProfile,
			Preview:       identitykey.Set(map[string]any{"username": username, "facet": "following"}, idKey),
		},
		{
			URL:           apex + "/feed/@" + username,
			CandidateType: CandidateTypeFeed,
			Strategy:      IDProfile,
			Preview:       identitykey.Set(map[string]any{"username": username}, identitykey.Build("medium", "feed", username)),
		},
	}, nil
}
