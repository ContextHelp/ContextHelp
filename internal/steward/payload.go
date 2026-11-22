package steward

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Request is the filing payload of a steward.grant request, as the
// route received it. The steward is named by StewardID, which must be
// the filing's subject. Exactly one of Scope and AsAudience gives the
// scope.
type Request struct {
	// StewardID is the principal who will read.
	StewardID string `json:"steward_id"`
	// MatterRef is an optional external reference (a case or ticket
	// number), free text.
	MatterRef string `json:"matter_ref,omitempty"`
	// Scope is one share-policy spec (ADR-080 §2: data,
	// exclude_classes, fields, capabilities; no redact) with
	// include_classes beside its keys. It freezes as a scope of one
	// spec.
	Scope json.RawMessage `json:"scope,omitempty"`
	// AsAudience names an audience whose current policies freeze into
	// the scope, one spec each ("what partners could see").
	AsAudience string `json:"as_audience,omitempty"`
	// NotBefore opens the window; zero means the moment of filing.
	NotBefore time.Time `json:"not_before,omitempty"`
	// ExpiresAt closes it; required.
	ExpiresAt time.Time `json:"expires_at"`
	// RenewsID names the grant this request renews, when it does.
	RenewsID string `json:"renews_id,omitempty"`
}

// Payload is the frozen operation of a steward.grant request: what the
// approvers approve and what the final approval writes. The scope is
// the compiled spec, never a reference: the digest binds each approval
// to exactly this text.
type Payload struct {
	StewardID string `json:"steward_id"`
	MatterRef string `json:"matter_ref,omitempty"`
	// Scope is the frozen scope in its stored form (a list of policy
	// specs and include_classes, sharing.ParseStewardScope);
	// ScopeDigest its digest.
	Scope       json.RawMessage `json:"scope"`
	ScopeDigest string          `json:"scope_digest"`
	// AsAudience records the audience a shorthand scope was compiled
	// from, for the approvers; it has no effect after the freeze.
	AsAudience string `json:"as_audience,omitempty"`
	// DroppedCapabilities lists, for as_audience, the capabilities the
	// audience's policies open that a steward scope does not (mcp,
	// events, and export unless steward.allow_export is on), so the
	// approvers see what the grant will not carry.
	DroppedCapabilities []string `json:"dropped_capabilities,omitempty"`
	// MaskingInert is set when a spec of the scope carries a redact (an
	// as_audience policy that masks): the masking is asked for but not
	// applied yet, to the audience's members or the steward, so the
	// approvers do not read the scope as masking. Like the rest of the
	// payload it is under the digest the approvers bind to.
	MaskingInert bool      `json:"masking_inert,omitempty"`
	NotBefore    time.Time `json:"not_before"`
	ExpiresAt    time.Time `json:"expires_at"`
	RenewsID     string    `json:"renews_id,omitempty"`
}

// decodeRequest decodes a filing payload strictly: a misspelled key is
// refused rather than ignored.
func decodeRequest(raw json.RawMessage) (Request, error) {
	var req Request
	if err := decodeStrict(raw, &req); err != nil {
		return Request{}, fmt.Errorf("payload: %w", err)
	}
	req.StewardID, req.MatterRef = strings.TrimSpace(req.StewardID), strings.TrimSpace(req.MatterRef)
	req.AsAudience, req.RenewsID = strings.TrimSpace(req.AsAudience), strings.TrimSpace(req.RenewsID)
	return req, nil
}

// DecodePayload decodes a frozen payload.
func DecodePayload(raw json.RawMessage) (Payload, error) {
	var p Payload
	if err := decodeStrict(raw, &p); err != nil {
		return Payload{}, fmt.Errorf("frozen payload: %w", err)
	}
	return p, nil
}

func decodeStrict(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after the object")
	}
	return nil
}
