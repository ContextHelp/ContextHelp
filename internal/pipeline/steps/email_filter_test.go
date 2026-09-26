package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestEmailFilterBillingRule(t *testing.T) {
	trueVal := true
	_ = trueVal
	rules := []FilterRule{
		{
			ID:       "billing",
			Priority: 10,
			When: FilterPredicate{
				FromDomainIn: []string{"stripe.com"},
				SubjectRegex: `invoice|receipt`,
			},
			Action: FilterAction{
				RoutePipeline: "email.billing",
				SetSubtype:    "billing",
				SetTags:       []string{"billing"},
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
	}
	f, err := NewEmailFilterFromRules(rules)
	if err != nil {
		t.Fatalf("create filter: %v", err)
	}

	msgs := []map[string]any{
		{
			"from":        "billing@stripe.com",
			"from_domain": "stripe.com",
			"subject":     "Invoice #INV-001",
			"text_body":   "Your invoice is ready.",
			"list_id":     "",
			"attachments": []map[string]any{},
			"headers":     map[string]any{},
		},
		{
			"from":        "hello@friend.com",
			"from_domain": "friend.com",
			"subject":     "How are you?",
			"text_body":   "Just checking in.",
			"list_id":     "",
			"attachments": []map[string]any{},
			"headers":     map[string]any{},
		},
	}

	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"email_messages": msgs},
	}
	got, err := f.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	routes, ok := got.Metadata["email_routes"].([]map[string]any)
	if !ok || len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %v", got.Metadata["email_routes"])
	}

	// First message should hit billing rule.
	if routes[0]["rule_id"] != "billing" {
		t.Errorf("route[0] rule_id: got %q, want billing", routes[0]["rule_id"])
	}
	if routes[0]["pipeline"] != "email.billing" {
		t.Errorf("route[0] pipeline: got %q", routes[0]["pipeline"])
	}

	// Second message should hit fallback.
	if routes[1]["rule_id"] != "fallback" {
		t.Errorf("route[1] rule_id: got %q, want fallback", routes[1]["rule_id"])
	}
	if routes[1]["pipeline"] != "text.long" {
		t.Errorf("route[1] pipeline: got %q", routes[1]["pipeline"])
	}
}

func TestEmailFilterNewsletterAnyPredicate(t *testing.T) {
	rules := []FilterRule{
		{
			ID:       "newsletter",
			Priority: 10,
			When: FilterPredicate{
				Any: []FilterPredicate{
					{HeaderExists: "List-Id"},
					{FromRegex: `newsletter`},
				},
			},
			Action: FilterAction{
				RoutePipeline: "email.newsletter",
				SetSubtype:    "newsletter",
			},
		},
		{
			ID:       "fallback",
			Priority: 9999,
			When:     FilterPredicate{Always: true},
			Action:   FilterAction{RoutePipeline: "text.long"},
		},
	}
	f, err := NewEmailFilterFromRules(rules)
	if err != nil {
		t.Fatalf("create filter: %v", err)
	}

	// Message with List-Id header.
	msgWithListID := map[string]any{
		"from":        "noreply@service.com",
		"from_domain": "service.com",
		"subject":     "Updates",
		"list_id":     "updates.service.com",
		"text_body":   "",
		"attachments": []map[string]any{},
		"headers":     map[string]any{"list-id": "updates.service.com"},
	}
	// Message matching from_regex.
	msgFromNewsletter := map[string]any{
		"from":        "daily-newsletter@news.com",
		"from_domain": "news.com",
		"subject":     "Daily News",
		"list_id":     "",
		"text_body":   "",
		"attachments": []map[string]any{},
		"headers":     map[string]any{},
	}
	// Plain message.
	msgPlain := map[string]any{
		"from":        "friend@example.com",
		"from_domain": "example.com",
		"subject":     "Hi",
		"list_id":     "",
		"text_body":   "Hey",
		"attachments": []map[string]any{},
		"headers":     map[string]any{},
	}

	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"email_messages": []map[string]any{msgWithListID, msgFromNewsletter, msgPlain},
		},
	}
	got, err := f.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	routes, ok := got.Metadata["email_routes"].([]map[string]any)
	if !ok || len(routes) != 3 {
		t.Fatalf("expected 3 routes, got %v", got.Metadata["email_routes"])
	}

	if routes[0]["rule_id"] != "newsletter" {
		t.Errorf("msg with list-id: rule_id=%q, want newsletter", routes[0]["rule_id"])
	}
	if routes[1]["rule_id"] != "newsletter" {
		t.Errorf("msg from newsletter: rule_id=%q, want newsletter", routes[1]["rule_id"])
	}
	if routes[2]["rule_id"] != "fallback" {
		t.Errorf("plain msg: rule_id=%q, want fallback", routes[2]["rule_id"])
	}
}

func TestEmailFilterDropAction(t *testing.T) {
	rules := []FilterRule{
		{
			ID:       "spam-drop",
			Priority: 1,
			When: FilterPredicate{
				SubjectContains: "SPAM",
			},
			Action: FilterAction{Drop: true},
		},
		{
			ID:       "fallback",
			Priority: 9999,
			When:     FilterPredicate{Always: true},
			Action:   FilterAction{RoutePipeline: "text.long"},
		},
	}
	f, err := NewEmailFilterFromRules(rules)
	if err != nil {
		t.Fatalf("create filter: %v", err)
	}

	msgs := []map[string]any{
		{
			"from":        "spammer@bad.com",
			"from_domain": "bad.com",
			"subject":     "You won SPAM",
			"text_body":   "",
			"list_id":     "",
			"attachments": []map[string]any{},
			"headers":     map[string]any{},
		},
	}

	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"email_messages": msgs},
	}
	got, err := f.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	routes, ok := got.Metadata["email_routes"].([]map[string]any)
	if !ok || len(routes) != 1 {
		t.Fatalf("expected 1 route")
	}
	if dropped, _ := routes[0]["dropped"].(bool); !dropped {
		t.Errorf("expected message to be dropped")
	}
}

func TestEmailFilterHasAttachment(t *testing.T) {
	trueVal := true
	rules := []FilterRule{
		{
			ID:       "with-attachment",
			Priority: 10,
			When:     FilterPredicate{HasAttachment: &trueVal},
			Action:   FilterAction{RoutePipeline: "email.attachment"},
		},
		{
			ID:       "fallback",
			Priority: 9999,
			When:     FilterPredicate{Always: true},
			Action:   FilterAction{RoutePipeline: "text.long"},
		},
	}
	f, err := NewEmailFilterFromRules(rules)
	if err != nil {
		t.Fatalf("create filter: %v", err)
	}

	msgs := []map[string]any{
		{
			"from":        "a@example.com",
			"from_domain": "example.com",
			"subject":     "With attachment",
			"text_body":   "See attached.",
			"list_id":     "",
			"headers":     map[string]any{},
			"attachments": []map[string]any{
				{"filename": "doc.pdf", "content_type": "application/pdf"},
			},
		},
		{
			"from":        "b@example.com",
			"from_domain": "example.com",
			"subject":     "No attachment",
			"text_body":   "No files.",
			"list_id":     "",
			"headers":     map[string]any{},
			"attachments": []map[string]any{},
		},
	}

	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"email_messages": msgs},
	}
	got, err := f.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	routes, ok := got.Metadata["email_routes"].([]map[string]any)
	if !ok || len(routes) != 2 {
		t.Fatalf("expected 2 routes")
	}
	if routes[0]["pipeline"] != "email.attachment" {
		t.Errorf("msg with attachment: pipeline=%q, want email.attachment", routes[0]["pipeline"])
	}
	if routes[1]["pipeline"] != "text.long" {
		t.Errorf("msg without attachment: pipeline=%q, want text.long", routes[1]["pipeline"])
	}
}

func TestEmailFilterInvalidRegex(t *testing.T) {
	rules := []FilterRule{
		{
			ID:       "bad-regex",
			Priority: 1,
			When:     FilterPredicate{FromRegex: `[invalid`},
			Action:   FilterAction{RoutePipeline: "text.long"},
		},
	}
	_, err := NewEmailFilterFromRules(rules)
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

func TestEmailFilterDefaultRuleset(t *testing.T) {
	rs := DefaultRuleset()
	f, err := NewEmailFilter(rs)
	if err != nil {
		t.Fatalf("create filter with default ruleset: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil filter")
	}

	// Verify the default ruleset routes a billing message correctly.
	match := f.EvaluateMessage(map[string]any{
		"from":        "billing@stripe.com",
		"from_domain": "stripe.com",
		"subject":     "Invoice for your subscription",
		"text_body":   "",
		"list_id":     "",
		"attachments": []map[string]any{},
		"headers":     map[string]any{},
	})
	if !match.Matched {
		t.Fatal("expected default ruleset to match billing message")
	}
	// The message is ingested on a content pipeline; the rule's subtype
	// carries the billing classification.
	if match.Action.RoutePipeline != "text.long" || match.Action.SetSubtype != "billing" {
		t.Errorf("route: pipeline %q subtype %q, want text.long billing",
			match.Action.RoutePipeline, match.Action.SetSubtype)
	}
}

func TestEmailFilterNoMessages(t *testing.T) {
	f, err := NewEmailFilterFromRules([]FilterRule{})
	if err != nil {
		t.Fatalf("create filter: %v", err)
	}
	draft := &storage.KnowledgeObject{}
	got, err := f.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	routes, ok := got.Metadata["email_routes"].([]map[string]any)
	if !ok || len(routes) != 0 {
		t.Errorf("expected empty routes, got %v", got.Metadata["email_routes"])
	}
}
