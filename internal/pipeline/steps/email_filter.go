package steps

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// FilterRule describes a single email routing rule.
type FilterRule struct {
	ID       string          `json:"id" yaml:"id"`
	Priority int             `json:"priority" yaml:"priority"`
	When     FilterPredicate `json:"when" yaml:"when"`
	Action   FilterAction    `json:"action" yaml:"action"`
}

// FilterPredicate holds all predicate fields for a rule.
// Multiple fields within a single predicate are ANDed together.
// The Any slice contains OR'd sub-predicates.
type FilterPredicate struct {
	Always          bool              `json:"always,omitempty" yaml:"always,omitempty"`
	Drop            bool              `json:"drop,omitempty" yaml:"drop,omitempty"`
	FromRegex       string            `json:"from_regex,omitempty" yaml:"from_regex,omitempty"`
	FromDomainIn    []string          `json:"from_domain_in,omitempty" yaml:"from_domain_in,omitempty"`
	SubjectRegex    string            `json:"subject_regex,omitempty" yaml:"subject_regex,omitempty"`
	SubjectContains string            `json:"subject_contains,omitempty" yaml:"subject_contains,omitempty"`
	BodyContains    string            `json:"body_contains,omitempty" yaml:"body_contains,omitempty"`
	HeaderExists    string            `json:"header_exists,omitempty" yaml:"header_exists,omitempty"`
	HasAttachment   *bool             `json:"has_attachment,omitempty" yaml:"has_attachment,omitempty"`
	SenderIn        []string          `json:"sender_in,omitempty" yaml:"sender_in,omitempty"`
	Folder          string            `json:"folder,omitempty" yaml:"folder,omitempty"`
	Any             []FilterPredicate `json:"any,omitempty" yaml:"any,omitempty"`
}

// FilterAction describes what to do when a rule matches.
type FilterAction struct {
	// RoutePipeline is the content pipeline a matched message is ingested
	// on as an object of its own; it must embed what it ingests.
	RoutePipeline string   `json:"route_pipeline,omitempty" yaml:"route_pipeline,omitempty"`
	SetType       string   `json:"set_type,omitempty" yaml:"set_type,omitempty"`
	SetSubtype    string   `json:"set_subtype,omitempty" yaml:"set_subtype,omitempty"`
	SetTags       []string `json:"set_tags,omitempty" yaml:"set_tags,omitempty"`
	SetSource     string   `json:"set_source,omitempty" yaml:"set_source,omitempty"`
	Drop          bool     `json:"drop,omitempty" yaml:"drop,omitempty"`
}

// FilterRuleset is an ordered list of rules evaluated priority-first.
type FilterRuleset struct {
	Rules []FilterRule `json:"rules" yaml:"rules"`
}

// FilterMatch describes the result of rule evaluation for a single message.
type FilterMatch struct {
	RuleID    string
	Action    FilterAction
	Matched   bool
	Dropped   bool
	Explained []string // human-readable list of matched predicates
}

// EmailFilter applies a FilterRuleset to each email message in
// draft.Metadata["email_messages"] and writes routing results to
// draft.Metadata["email_routes"].
type EmailFilter struct {
	pipeline.BaseContract
	ruleset  FilterRuleset
	compiled map[string]*regexp.Regexp // cached compiled regexes
}

// NewEmailFilter creates an EmailFilter with the given ruleset.
func NewEmailFilter(rs FilterRuleset) (*EmailFilter, error) {
	f := &EmailFilter{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Metadata"},
			Produces: []string{"Metadata"},
		}),
		ruleset:  rs,
		compiled: make(map[string]*regexp.Regexp),
	}
	// Sort rules by priority ascending (first-match-wins).
	sort.Slice(f.ruleset.Rules, func(i, j int) bool {
		return f.ruleset.Rules[i].Priority < f.ruleset.Rules[j].Priority
	})
	// Pre-compile regexes for fast evaluation.
	if err := f.precompile(); err != nil {
		return nil, fmt.Errorf("email_filter: precompile: %w", err)
	}
	return f, nil
}

// NewEmailFilterFromRules is a convenience constructor.
func NewEmailFilterFromRules(rules []FilterRule) (*EmailFilter, error) {
	return NewEmailFilter(FilterRuleset{Rules: rules})
}

func (s *EmailFilter) Name() string { return "email_filter" }

func (s *EmailFilter) precompile() error {
	for _, rule := range s.ruleset.Rules {
		if err := s.compilePredicateRegexes(rule.When); err != nil {
			return fmt.Errorf("rule %q: %w", rule.ID, err)
		}
	}
	return nil
}

func (s *EmailFilter) compilePredicateRegexes(p FilterPredicate) error {
	if p.FromRegex != "" {
		if _, err := s.getRegex(p.FromRegex); err != nil {
			return fmt.Errorf("from_regex %q: %w", p.FromRegex, err)
		}
	}
	if p.SubjectRegex != "" {
		if _, err := s.getRegex(p.SubjectRegex); err != nil {
			return fmt.Errorf("subject_regex %q: %w", p.SubjectRegex, err)
		}
	}
	for _, sub := range p.Any {
		if err := s.compilePredicateRegexes(sub); err != nil {
			return err
		}
	}
	return nil
}

func (s *EmailFilter) getRegex(pattern string) (*regexp.Regexp, error) {
	if re, ok := s.compiled[pattern]; ok {
		return re, nil
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, err
	}
	s.compiled[pattern] = re
	return re, nil
}

func (s *EmailFilter) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	raw, ok := draft.Metadata["email_messages"]
	if !ok {
		draft.Metadata["email_routes"] = []map[string]any{}
		return draft, nil
	}
	messages, ok := raw.([]map[string]any)
	if !ok {
		return nil, fmt.Errorf("email_filter: email_messages has unexpected type %T", raw)
	}

	routes := make([]map[string]any, 0, len(messages))
	for i, msg := range messages {
		match := s.evaluate(msg)
		route := map[string]any{
			"message_index": i,
			"rule_id":       match.RuleID,
			"matched":       match.Matched,
			"dropped":       match.Dropped,
			"pipeline":      match.Action.RoutePipeline,
			"set_type":      match.Action.SetType,
			"set_subtype":   match.Action.SetSubtype,
			"set_tags":      match.Action.SetTags,
			"set_source":    match.Action.SetSource,
			"explain":       match.Explained,
		}
		routes = append(routes, route)
	}
	draft.Metadata["email_routes"] = routes
	return draft, nil
}

// evaluate runs all rules against a message map and returns the first match.
func (s *EmailFilter) evaluate(msg map[string]any) FilterMatch {
	for _, rule := range s.ruleset.Rules {
		explain, matched := s.matchPredicate(rule.When, msg)
		if matched {
			return FilterMatch{
				RuleID:    rule.ID,
				Action:    rule.Action,
				Matched:   true,
				Dropped:   rule.Action.Drop,
				Explained: explain,
			}
		}
	}
	return FilterMatch{Matched: false}
}

// matchPredicate evaluates a single predicate against a message.
// Returns (explanation lines, matched).
func (s *EmailFilter) matchPredicate(p FilterPredicate, msg map[string]any) ([]string, bool) {
	if p.Always {
		return []string{"always"}, true
	}

	var explain []string

	// Handle OR block first.
	if len(p.Any) > 0 {
		for _, sub := range p.Any {
			subExplain, matched := s.matchPredicate(sub, msg)
			if matched {
				explain = append(explain, "any: "+strings.Join(subExplain, ", "))
				return explain, true
			}
		}
		return nil, false
	}

	// All remaining predicates are AND'd together.
	from, _ := msg["from"].(string)
	fromDomain, _ := msg["from_domain"].(string)
	subject, _ := msg["subject"].(string)
	textBody, _ := msg["text_body"].(string)
	htmlBody, _ := msg["html_body"].(string)
	listID, _ := msg["list_id"].(string)
	attachmentsRaw, _ := msg["attachments"].([]map[string]any)
	headersRaw, _ := msg["headers"].(map[string]any)

	if p.FromRegex != "" {
		re, _ := s.getRegex(p.FromRegex)
		if re == nil || !re.MatchString(from) {
			return nil, false
		}
		explain = append(explain, fmt.Sprintf("from_regex:%q", p.FromRegex))
	}

	if len(p.FromDomainIn) > 0 {
		matched := false
		for _, d := range p.FromDomainIn {
			if strings.EqualFold(fromDomain, d) {
				matched = true
				break
			}
		}
		if !matched {
			return nil, false
		}
		explain = append(explain, fmt.Sprintf("from_domain_in:[%s]", strings.Join(p.FromDomainIn, ",")))
	}

	if p.SubjectRegex != "" {
		re, _ := s.getRegex(p.SubjectRegex)
		if re == nil || !re.MatchString(subject) {
			return nil, false
		}
		explain = append(explain, fmt.Sprintf("subject_regex:%q", p.SubjectRegex))
	}

	if p.SubjectContains != "" {
		if !strings.Contains(strings.ToLower(subject), strings.ToLower(p.SubjectContains)) {
			return nil, false
		}
		explain = append(explain, fmt.Sprintf("subject_contains:%q", p.SubjectContains))
	}

	if p.BodyContains != "" {
		body := textBody + " " + htmlBody
		if !strings.Contains(strings.ToLower(body), strings.ToLower(p.BodyContains)) {
			return nil, false
		}
		explain = append(explain, fmt.Sprintf("body_contains:%q", p.BodyContains))
	}

	if p.HeaderExists != "" {
		key := strings.ToLower(p.HeaderExists)
		// Check headers map and also list_id for List-Id.
		found := false
		if headersRaw != nil {
			if v, ok := headersRaw[key]; ok && v != "" {
				found = true
			}
		}
		if key == "list-id" && listID != "" {
			found = true
		}
		if !found {
			return nil, false
		}
		explain = append(explain, fmt.Sprintf("header_exists:%q", p.HeaderExists))
	}

	if p.HasAttachment != nil {
		hasAtt := len(attachmentsRaw) > 0
		if hasAtt != *p.HasAttachment {
			return nil, false
		}
		explain = append(explain, fmt.Sprintf("has_attachment:%v", *p.HasAttachment))
	}

	if len(p.SenderIn) > 0 {
		matched := false
		for _, addr := range p.SenderIn {
			if strings.EqualFold(from, addr) {
				matched = true
				break
			}
		}
		if !matched {
			return nil, false
		}
		explain = append(explain, fmt.Sprintf("sender_in:[%s]", strings.Join(p.SenderIn, ",")))
	}

	if p.Folder != "" {
		folder, _ := msg["folder"].(string)
		if !strings.EqualFold(folder, p.Folder) {
			return nil, false
		}
		explain = append(explain, fmt.Sprintf("folder:%q", p.Folder))
	}

	if len(explain) == 0 {
		// No predicates specified → no match (prevents accidental wildcard rules
		// from matching unless always:true is set).
		return nil, false
	}

	return explain, true
}

// EvaluateMessage evaluates a single message map against the ruleset (used for dry-run explain).
func (s *EmailFilter) EvaluateMessage(msg map[string]any) FilterMatch {
	return s.evaluate(msg)
}

// DefaultRuleset returns a built-in ruleset suitable for general-purpose email
// routing. Every rule ingests on text.long; the subtype and tags carry the
// classification.
func DefaultRuleset() FilterRuleset {
	trueVal := true
	return FilterRuleset{
		Rules: []FilterRule{
			{
				ID:       "billing-invoices",
				Priority: 10,
				When: FilterPredicate{
					FromDomainIn: []string{"stripe.com", "aws.amazon.com", "openai.com", "paypal.com"},
					SubjectRegex: `(?i)(invoice|receipt|payment|billing)`,
				},
				Action: FilterAction{
					RoutePipeline: "text.long",
					SetSubtype:    "billing",
					SetTags:       []string{"billing", "invoice"},
				},
			},
			{
				ID:       "newsletters",
				Priority: 20,
				When: FilterPredicate{
					Any: []FilterPredicate{
						{HeaderExists: "List-Id"},
						{FromRegex: `newsletter|digest|substack|mailchimp`},
					},
				},
				Action: FilterAction{
					RoutePipeline: "text.long",
					SetSubtype:    "newsletter",
					SetTags:       []string{"newsletter"},
				},
			},
			{
				ID:       "has-attachment",
				Priority: 50,
				When: FilterPredicate{
					HasAttachment: &trueVal,
				},
				Action: FilterAction{
					RoutePipeline: "text.long",
					SetSubtype:    "email-attachment",
					SetTags:       []string{"has-attachment"},
				},
			},
			{
				ID:       "fallback",
				Priority: 9999,
				When:     FilterPredicate{Always: true},
				Action: FilterAction{
					RoutePipeline: "text.long",
					SetSubtype:    "email",
				},
			},
		},
	}
}
