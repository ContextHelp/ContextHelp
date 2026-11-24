package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/keys"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// WithKeyProvider attaches a storage KEK provider to Service for per-capture
// and selective natural-language content encryption/decryption.
func WithKeyProvider(p keys.Provider) Option {
	return func(s *Service) { s.keyProvider = p }
}

// SetEncryptSpanLocator installs an optional LLM-backed SpanLocator used for
// free-form natural-language encrypt_scope instructions.
func (s *Service) SetEncryptSpanLocator(loc keys.SpanLocator) {
	s.encryptLocator = loc
}

// EncryptObject seals obj's fields or natural-language spans in place using p
// and marks obj.Encrypted = true, obj.EncryptScope = scope. Fails closed when
// p is nil or when a free-form natural-language scope has no SpanLocator.
func EncryptObject(ctx context.Context, p keys.Provider, obj *storage.KnowledgeObject, scope string, locator keys.SpanLocator) error {
	if obj == nil {
		return nil
	}
	if p == nil {
		return fmt.Errorf("encrypt object %s: %w", obj.ID, keys.ErrUnavailable)
	}
	scope = strings.TrimSpace(scope)
	plan := keys.ClassifyEncryptScope(scope)
	if obj.TextContent == "" && obj.RawContent != "" && !strings.HasPrefix(obj.RawContent, "blob://") {
		obj.TextContent = obj.RawContent
	}
	sealer := objectSealer{ctx: ctx, p: p, id: obj.ID, scope: scope, plan: plan, locator: locator}
	if err := sealer.sealBodies(obj); err != nil {
		return err
	}
	if err := sealer.sealSummaries(obj); err != nil {
		return err
	}
	if err := sealer.sealInboxNote(obj); err != nil {
		return err
	}
	obj.Encrypted = true
	obj.EncryptScope = scope
	return nil
}

type objectSealer struct {
	ctx     context.Context
	p       keys.Provider
	id      string
	scope   string
	plan    keys.EncryptScopePlan
	locator keys.SpanLocator
}

func (s objectSealer) hasSelective() bool {
	return len(s.plan.Patterns) > 0 || s.plan.NeedsLLM
}

func (s objectSealer) sealField(field, val string) (string, error) {
	return keys.SealFieldText(s.ctx, s.p, val, objectFieldAAD(s.id, field))
}

func (s objectSealer) sealSpans(field, val string) (string, error) {
	out, _, err := keys.SealSelectiveText(s.ctx, s.p, val, s.scope, objectSpanAAD(s.id, field), s.plan, s.locator)
	return out, err
}

func (s objectSealer) sealBodies(obj *storage.KnowledgeObject) error {
	var fn func(string, string) (string, error)
	switch {
	case s.plan.WholeContent || s.plan.RawContent:
		fn = s.sealField
	case s.hasSelective():
		fn = s.sealSpans
	default:
		return nil
	}
	var err error
	if obj.RawContent, err = fn("raw_content", obj.RawContent); err != nil {
		return err
	}
	obj.TextContent, err = fn("text_content", obj.TextContent)
	return err
}

func (s objectSealer) sealSummaries(obj *storage.KnowledgeObject) error {
	var fn func(string, string) (string, error)
	switch {
	case s.plan.WholeContent || s.plan.Summaries:
		fn = s.sealField
	case !s.plan.RawContent && s.hasSelective():
		fn = s.sealSpans
	default:
		return nil
	}
	for i, sum := range obj.Summaries {
		enc, err := fn("summaries", sum)
		if err != nil {
			return err
		}
		obj.Summaries[i] = enc
	}
	return nil
}

func (s objectSealer) sealInboxNote(obj *storage.KnowledgeObject) error {
	var fn func(string, string) (string, error)
	switch {
	case s.plan.WholeContent || s.plan.InboxNote:
		fn = s.sealField
	case !s.plan.RawContent && obj.InboxNote != "" && s.hasSelective():
		fn = s.sealSpans
	default:
		return nil
	}
	enc, err := fn("inbox_note", obj.InboxNote)
	if err != nil {
		return err
	}
	obj.InboxNote = enc
	return nil
}

// DecryptObject unseals any whole-field or inline selective ciphertexts on obj
// in place using p. Objects without encryption markers are untouched.
func DecryptObject(ctx context.Context, p keys.Provider, obj *storage.KnowledgeObject) error {
	if obj == nil || !obj.Encrypted {
		return nil
	}
	var err error
	if obj.RawContent, err = keys.DecryptFieldOrSpans(ctx, p, obj.RawContent, objectFieldAAD(obj.ID, "raw_content"), objectSpanAAD(obj.ID, "raw_content")); err != nil {
		return err
	}
	if obj.TextContent, err = keys.DecryptFieldOrSpans(ctx, p, obj.TextContent, objectFieldAAD(obj.ID, "text_content"), objectSpanAAD(obj.ID, "text_content")); err != nil {
		return err
	}
	for i, sum := range obj.Summaries {
		if obj.Summaries[i], err = keys.DecryptFieldOrSpans(ctx, p, sum, objectFieldAAD(obj.ID, "summaries"), objectSpanAAD(obj.ID, "summaries")); err != nil {
			return err
		}
	}
	if obj.InboxNote, err = keys.DecryptFieldOrSpans(ctx, p, obj.InboxNote, objectFieldAAD(obj.ID, "inbox_note"), objectSpanAAD(obj.ID, "inbox_note")); err != nil {
		return err
	}
	return nil
}

// DecryptObject unseals an encrypted object using the service's configured KeyProvider.
func (s *Service) DecryptObject(ctx context.Context, obj *storage.KnowledgeObject) error {
	return DecryptObject(ctx, s.keyProvider, obj)
}

func objectFieldAAD(id, field string) []byte { return []byte("object:" + id + ":" + field) }
func objectSpanAAD(id, field string) []byte  { return []byte("object:" + id + ":" + field + ":span") }
