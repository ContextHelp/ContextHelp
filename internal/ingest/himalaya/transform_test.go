package himalaya

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestToCtxtObject_Basic(t *testing.T) {
	m := Message{
		EnvelopeID: "100",
		MessageID:  "msg100@example.com",
		From:       Address{Name: strPtr("Alice"), Addr: "alice@example.com"},
		To:         Address{Name: strPtr("Bob"), Addr: "bob@example.com"},
		Subject:    "Project update",
		Body:       "Here is the update.",
		Date:       time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC),
		ThreadID:   "thread_abc123",
	}

	obj := ToCtxtObject(m, "ideacrafters")
	if obj.ID != "himalaya:ideacrafters:100" {
		t.Errorf("ID: got %q", obj.ID)
	}
	if obj.Type != "email" {
		t.Errorf("Type: got %q", obj.Type)
	}

	// Tags.
	tagSet := make(map[string]bool)
	for _, tag := range obj.Tags {
		tagSet[tag] = true
	}
	for _, want := range []string{
		"source:himalaya",
		"type:email",
		"from:alice@example.com",
		"to:bob@example.com",
		"thread:thread_abc123",
		"contact:alice@example.com",
	} {
		if !tagSet[want] {
			t.Errorf("missing tag %q", want)
		}
	}

	// Subject keywords.
	if !tagSet["subject:project"] || !tagSet["subject:update"] {
		t.Errorf("missing subject keyword tags; tags: %v", obj.Tags)
	}

	// Metadata.
	if obj.Metadata["envelope_id"] != "100" {
		t.Errorf("metadata envelope_id: got %q", obj.Metadata["envelope_id"])
	}
	if obj.Metadata["message_id"] != "msg100@example.com" {
		t.Errorf("metadata message_id: got %q", obj.Metadata["message_id"])
	}
	if obj.Metadata["thread_id"] != "thread_abc123" {
		t.Errorf("metadata thread_id: got %q", obj.Metadata["thread_id"])
	}
	if obj.Metadata["source"] != "himalaya" {
		t.Errorf("metadata source: got %q", obj.Metadata["source"])
	}
	if obj.Metadata["account"] != "ideacrafters" {
		t.Errorf("metadata account: got %q", obj.Metadata["account"])
	}
}

func TestToCtxtObject_Reply(t *testing.T) {
	m := Message{
		EnvelopeID: "101",
		MessageID:  "reply@example.com",
		InReplyTo:  "parent@example.com",
		From:       Address{Addr: "bob@example.com"},
		To:         Address{Addr: "alice@example.com"},
		Subject:    "Re: Project update",
		Body:       "Thanks!",
		Date:       time.Date(2024, 7, 1, 11, 0, 0, 0, time.UTC),
	}

	obj := ToCtxtObject(m, "test")
	if !strings.Contains(obj.Content, "(reply)") {
		t.Error("reply content should be marked")
	}
	if obj.Metadata["in_reply_to"] != "parent@example.com" {
		t.Errorf("in_reply_to: got %q", obj.Metadata["in_reply_to"])
	}
}

func TestToCtxtObjects_Batch(t *testing.T) {
	msgs := []Message{
		{EnvelopeID: "1", From: Address{Addr: "a@test.com"},
			Subject: "A", Date: time.Unix(1000, 0)},
		{EnvelopeID: "2", From: Address{Addr: "b@test.com"},
			Subject: "B", Date: time.Unix(2000, 0)},
	}

	objs := ToCtxtObjects(msgs, "src")
	if len(objs) != 2 {
		t.Fatalf("expected 2 objects, got %d", len(objs))
	}
	if objs[0].ID == objs[1].ID {
		t.Error("IDs should be unique")
	}
}

func TestCtxtObject_JSONRoundtrip(t *testing.T) {
	m := Message{
		EnvelopeID: "42",
		MessageID:  "msg42@example.com",
		From:       Address{Addr: "sender@example.com"},
		To:         Address{Addr: "recv@example.com"},
		Subject:    "JSON test",
		Body:       "Body text",
		Date:       time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		ThreadID:   "thread_xyz",
	}

	obj := ToCtxtObject(m, "test")
	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded CtxtObject
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != obj.ID {
		t.Errorf("ID mismatch: %q vs %q", decoded.ID, obj.ID)
	}
	if decoded.Type != "email" {
		t.Errorf("Type: got %q", decoded.Type)
	}
	if len(decoded.Tags) != len(obj.Tags) {
		t.Errorf("Tags count: %d vs %d", len(decoded.Tags), len(obj.Tags))
	}
	if decoded.Metadata["thread_id"] != "thread_xyz" {
		t.Errorf("thread_id: got %q", decoded.Metadata["thread_id"])
	}
}

func TestRenderContent(t *testing.T) {
	m := Message{
		From:    Address{Name: strPtr("Alice"), Addr: "alice@example.com"},
		To:      Address{Addr: "bob@example.com"},
		Subject: "Hello",
		Body:    "Message body here.",
		Date:    time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC),
	}

	content := RenderContent(m)
	for _, want := range []string{
		"# Email",
		"**Subject:** Hello",
		"**From:** Alice <alice@example.com>",
		"**To:** bob@example.com",
		"Message body here.",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("content missing %q; got:\n%s", want, content)
		}
	}
}

func TestRenderContent_Reply(t *testing.T) {
	m := Message{
		InReplyTo: "parent@example.com",
		Subject:   "Re: Hello",
		Body:      "Reply body",
	}
	content := RenderContent(m)
	if !strings.Contains(content, "(reply)") {
		t.Error("reply should be marked in content")
	}
}

func TestSubjectKeywords(t *testing.T) {
	tests := []struct {
		subject string
		want    []string
		notWant []string
	}{
		{
			"Re: Project kickoff meeting",
			[]string{"project", "kickoff", "meeting"},
			[]string{"re"},
		},
		{
			"[repo-name] Run failed: CI - main",
			[]string{"run", "failed", "main"},
			[]string{"repo-name"},
		},
		{
			"Fwd: Short",
			[]string{"short"},
			[]string{"fwd"},
		},
		{
			"Hi",
			nil, // too short
			[]string{"hi"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.subject, func(t *testing.T) {
			kws := subjectKeywords(tc.subject)
			kwSet := make(map[string]bool)
			for _, kw := range kws {
				kwSet[kw] = true
			}
			for _, w := range tc.want {
				if !kwSet[w] {
					t.Errorf("missing keyword %q from %v", w, kws)
				}
			}
			for _, w := range tc.notWant {
				if kwSet[w] {
					t.Errorf("unexpected keyword %q in %v", w, kws)
				}
			}
		})
	}
}

func TestContactScopeTag(t *testing.T) {
	m := Message{
		EnvelopeID: "1",
		From:       Address{Addr: "Alice@Example.COM"},
		Subject:    "Test",
	}
	obj := ToCtxtObject(m, "test")

	found := false
	for _, tag := range obj.Tags {
		if tag == "contact:alice@example.com" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing normalized contact tag; tags: %v", obj.Tags)
	}
}
