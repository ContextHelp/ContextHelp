package jobs

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func ambientJob(id, payload, session string) *storage.Job {
	j := makeJob(id)
	j.Payload = payload
	j.UserAmbient = &storage.AmbientContext{
		Source: "clipboard", Fingerprint: "sha256:" + session, SessionID: session,
		Metadata: map[string]any{"app_name": "Terminal", "ambient_source": "spoof"},
	}
	return j
}

// The ambient envelope rides the job to the persisted object.
func TestProcessJobPersistsAmbientFields(t *testing.T) {
	e := newSubmissionEnv(t)
	got := e.run(t, ambientJob("job-amb-1", "clipboard text about quokkas on rottnest island", "sess_a"))

	obj := e.object(t, got.ResultID)
	if obj.SessionID != "sess_a" {
		t.Errorf("session id %q", obj.SessionID)
	}
	if obj.Metadata["ambient_source"] != "clipboard" {
		t.Errorf("ambient_source %v (client metadata must not override)", obj.Metadata["ambient_source"])
	}
	if obj.Metadata["fingerprint"] != "sha256:sess_a" {
		t.Errorf("fingerprint %v", obj.Metadata["fingerprint"])
	}
	client, _ := obj.Metadata["client"].(map[string]any)
	if client["app_name"] != "Terminal" || client["ambient_source"] != "spoof" {
		t.Errorf("client metadata not stored under metadata.client: %v", obj.Metadata)
	}
	if _, flat := obj.Metadata["app_name"]; flat {
		t.Errorf("client metadata flat-merged: %v", obj.Metadata)
	}
}

// Steps may replace the draft metadata map; the ambient keys still land.
func TestProcessJobAmbientSurvivesStepsReplacingMetadata(t *testing.T) {
	spy := &seenDraft{}
	e := newSubmissionEnv(t, spy)
	j := ambientJob("job-amb-spy", "clipboard text about wombats and their cubic droppings", "sess_s")
	j.Pipeline = "test.seen"
	got := e.run(t, j)
	if obj := e.object(t, got.ResultID); obj.SessionID != "sess_s" || obj.Metadata["ambient_source"] != "clipboard" {
		t.Errorf("object: session %q metadata %v", obj.SessionID, obj.Metadata)
	}
}

// First wins: the same content captured again in another session
// reinforces the stored object and leaves its session and ambient keys.
func TestProcessJobDedupAcrossSessionsFirstWins(t *testing.T) {
	e := newSubmissionEnv(t)
	const body = "clipboard text about thylacine sightings in tasmania"
	first := e.run(t, ambientJob("job-amb-first", body, "sess_one"))

	second := ambientJob("job-amb-second", body, "sess_two")
	second.UserAmbient.Source = "browser"
	got := e.run(t, second)

	if got.ResultID != first.ResultID {
		t.Fatalf("second capture made object %s, want the first %s", got.ResultID, first.ResultID)
	}
	obj := e.object(t, first.ResultID)
	if obj.SessionID != "sess_one" {
		t.Errorf("session id %q, want first-wins sess_one", obj.SessionID)
	}
	if obj.Metadata["ambient_source"] != "clipboard" || obj.Metadata["fingerprint"] != "sha256:sess_one" {
		t.Errorf("ambient keys overwritten: %v", obj.Metadata)
	}
	if obj.ReinforcementCount < 2 {
		t.Errorf("reinforcement %d, want the dedup path taken", obj.ReinforcementCount)
	}
	if n := e.count(t); n != 1 {
		t.Errorf("objects %d, want 1", n)
	}
}

// Client metadata cannot steer the worker or the store: every control
// key it sends stays inert under metadata.client.
func TestProcessJobClientMetadataCannotSteerServer(t *testing.T) {
	const body = "clipboard text about dugongs grazing seagrass meadows"
	for name, md := range map[string]map[string]any{
		"suppress_output+duplicate_of": {"suppress_output": true, "duplicate_of": "some-object"},
		"blob_key":                     {"blob_key": "evil", "content_truncated": true},
		"items_to_enqueue":             {"items_to_enqueue": []any{map[string]any{"type": "url", "content": "http://x"}}},
		"title":                        {"title": "spoofed title"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newSubmissionEnv(t)
			j := makeJob("job-steer-" + name)
			j.Payload = body + " " + name
			j.UserAmbient = &storage.AmbientContext{Source: "clipboard", Metadata: md}
			got := e.run(t, j)

			if got.ResultID == "" || got.ResultID == "some-object" {
				t.Fatalf("result %q: the capture was dropped or redirected", got.ResultID)
			}
			obj := e.object(t, got.ResultID)
			if obj.RawContent != j.Payload {
				t.Errorf("content rewritten: %q", obj.RawContent)
			}
			for k := range md {
				if _, top := obj.Metadata[k]; top {
					t.Errorf("client key %q reached top-level metadata", k)
				}
			}
			if n := e.count(t); n != 1 {
				t.Errorf("objects %d, want 1 (no item jobs may spawn objects)", n)
			}
		})
	}
}
