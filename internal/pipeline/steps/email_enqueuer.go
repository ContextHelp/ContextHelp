package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// EmailEnqueuer reads email messages and their routing results, then stages
// them as Sections on the draft for downstream ingestion.
// It reads:
//   - draft.Metadata["email_messages"]
//   - draft.Metadata["email_routes"]  (optional; produced by EmailFilter)
//
// It produces:
//   - draft.Sections  (one per non-dropped message)
//   - draft.Metadata["email_queued"]
//   - draft.Metadata["email_skipped"]
type EmailEnqueuer struct {
	pipeline.BaseContract
	defaultPipeline string
}

// EmailEnqueuerOption configures an EmailEnqueuer.
type EmailEnqueuerOption func(*EmailEnqueuer)

// WithDefaultEmailPipeline sets the fallback pipeline when no route is present.
func WithDefaultEmailPipeline(name string) EmailEnqueuerOption {
	return func(e *EmailEnqueuer) { e.defaultPipeline = name }
}

// NewEmailEnqueuer creates an EmailEnqueuer.
func NewEmailEnqueuer(opts ...EmailEnqueuerOption) *EmailEnqueuer {
	e := &EmailEnqueuer{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Metadata"},
			Produces: []string{"Sections", "Metadata"},
		}),
		defaultPipeline: "text.long",
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func (s *EmailEnqueuer) Name() string { return "email_enqueuer" }

func (s *EmailEnqueuer) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	raw, ok := draft.Metadata["email_messages"]
	if !ok {
		draft.Metadata["email_queued"] = 0
		draft.Metadata["email_skipped"] = 0
		return draft, nil
	}
	messages, ok := raw.([]map[string]any)
	if !ok {
		return nil, fmt.Errorf("email_enqueuer: email_messages has unexpected type %T", raw)
	}

	// Build route index by message_index if available.
	routeByIdx := make(map[int]map[string]any)
	if routesRaw, exists := draft.Metadata["email_routes"]; exists {
		if routes, ok := routesRaw.([]map[string]any); ok {
			for _, r := range routes {
				if idx, ok := r["message_index"].(int); ok {
					routeByIdx[idx] = r
				}
			}
		}
	}

	var sections []storage.Section
	queued := 0
	skipped := 0

	for i, msg := range messages {
		route := routeByIdx[i]

		// Skip dropped messages.
		if route != nil {
			if dropped, _ := route["dropped"].(bool); dropped {
				skipped++
				continue
			}
		}

		subject, _ := msg["subject"].(string)
		from, _ := msg["from"].(string)
		textBody, _ := msg["text_body"].(string)
		messageID, _ := msg["message_id"].(string)
		contentHash, _ := msg["content_hash"].(string)

		// Build section content: prefer text body, fall back to subject line.
		body := textBody
		if strings.TrimSpace(body) == "" {
			body = subject
		}

		// Build section title.
		title := subject
		if title == "" {
			title = fmt.Sprintf("Email from %s", from)
		}
		if len(title) > 120 {
			title = title[:120]
		}

		// Determine pipeline from route.
		pipelineName := s.defaultPipeline
		if route != nil {
			if p, _ := route["pipeline"].(string); p != "" {
				pipelineName = p
			}
		}

		sectionMeta := map[string]any{
			"message_id":   messageID,
			"content_hash": contentHash,
			"from":         from,
			"pipeline":     pipelineName,
			"source_type":  "email",
		}
		if route != nil {
			sectionMeta["rule_id"] = route["rule_id"]
			if tags, ok := route["set_tags"].([]string); ok && len(tags) > 0 {
				sectionMeta["tags"] = tags
			}
			if subtype, _ := route["set_subtype"].(string); subtype != "" {
				sectionMeta["subtype"] = subtype
			}
		}

		sections = append(sections, storage.Section{
			Title:    title,
			Content:  body,
			Order:    queued,
			Metadata: sectionMeta,
		})
		queued++
	}

	draft.Sections = sections
	draft.Metadata["email_queued"] = queued
	draft.Metadata["email_skipped"] = skipped
	return draft, nil
}
