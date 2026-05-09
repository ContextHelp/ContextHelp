package substack

import (
	"context"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/customdomain"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

// NotesStrategy claims Substack Notes URLs. Recognised forms:
//
//   - https://substack.com/notes
//   - https://substack.com/note/<id>
//   - https://substack.com/profile/<id>-<handle>/note/<noteid>
//
// Specificity 3.
type NotesStrategy struct {
	Client SubstackClient
}

func NewNotes(c SubstackClient) *NotesStrategy { return &NotesStrategy{Client: c} }

func (*NotesStrategy) ID() string                    { return IDNotes }
func (*NotesStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*NotesStrategy) Preconditions() []string       { return nil }

func (*NotesStrategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	d := customdomain.Detect(ev.SourceURL, hintsFromEvent(ev))
	if d.Platform != customdomain.PlatformSubstack {
		return lateral.AppliesResult{}
	}
	u, parts, err := parseURL(ev.SourceURL)
	if err != nil || len(parts) == 0 {
		return lateral.AppliesResult{}
	}
	host := strings.ToLower(u.Hostname())
	if host != "substack.com" {
		return lateral.AppliesResult{}
	}
	switch parts[0] {
	case "notes", "note":
		return lateral.AppliesResult{Matches: true, Specificity: 3}
	case "profile":
		for _, p := range parts {
			if p == "note" {
				return lateral.AppliesResult{Matches: true, Specificity: 3}
			}
		}
	}
	return lateral.AppliesResult{}
}

func (*NotesStrategy) Probe(_ context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	_, parts, err := parseURL(ev.SourceURL)
	if err != nil || len(parts) == 0 {
		return nil, err
	}
	apex := "https://substack.com"
	switch parts[0] {
	case "notes":
		return []lateral.Candidate{{
			URL:           apex + "/notes",
			CandidateType: CandidateTypeNote,
			Strategy:      IDNotes,
			Preview:       identitykey.Set(map[string]any{"feed": "global"}, identitykey.Build("substack", identitykey.EntityNote, "feed_global")),
		}}, nil
	case "note":
		if len(parts) < 2 {
			return nil, nil
		}
		id := parts[1]
		return []lateral.Candidate{{
			URL:           apex + "/note/" + id,
			CandidateType: CandidateTypeNote,
			Strategy:      IDNotes,
			Preview:       identitykey.Set(map[string]any{"note_id": id}, identitykey.Build("substack", identitykey.EntityNote, id)),
		}}, nil
	case "profile":
		if len(parts) < 4 || parts[2] != "note" {
			return nil, nil
		}
		profile, noteID := parts[1], parts[3]
		return []lateral.Candidate{
			{
				URL:           apex + "/profile/" + profile,
				CandidateType: CandidateTypeAuthor,
				Strategy:      IDNotes,
				Preview:       identitykey.Set(map[string]any{"profile": profile}, identitykey.Build("substack", identitykey.EntityProfile, profile)),
			},
			{
				URL:           apex + "/profile/" + profile + "/note/" + noteID,
				CandidateType: CandidateTypeNote,
				Strategy:      IDNotes,
				Preview:       identitykey.Set(map[string]any{"profile": profile, "note_id": noteID}, identitykey.Build("substack", identitykey.EntityNote, profile, noteID)),
			},
		}, nil
	}
	return nil, nil
}
