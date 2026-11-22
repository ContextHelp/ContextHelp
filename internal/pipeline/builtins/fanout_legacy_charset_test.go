package builtins

import (
	"context"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/charmap"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// latin1 is s as Windows-1252 bytes (a superset of Latin-1).
func latin1(t *testing.T, s string) string {
	t.Helper()
	b, err := charmap.Windows1252.NewEncoder().String(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// itemTexts is the text of every item object.
func itemTexts(items []*storage.KnowledgeObject) string {
	var sb strings.Builder
	for _, it := range items {
		sb.WriteString(it.RawContent)
		sb.WriteString("\n")
		sb.WriteString(it.TextContent)
		sb.WriteString("\n")
	}
	return sb.String()
}

func requireLegacyFanOut(t *testing.T, name string, tc fanOutJob, want ...string) {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	done := runContainer(t, driver, name, tc)
	_, items := splitObjects(t, driver, done.ResultID)
	if len(items) != len(tc.words) {
		t.Fatalf("%d item objects, want %d", len(items), len(tc.words))
	}
	text := itemTexts(items)
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("item text lacks %q:\n%s", w, text)
		}
	}
	if strings.ContainsRune(text, '\ufffd') {
		t.Errorf("item text holds U+FFFD:\n%s", text)
	}
}

// A Latin-1 message (8-bit, declared charset) is parsed and its text
// reaches the child job as UTF-8.
func TestLegacyCharsetEMLFansOut(t *testing.T) {
	eml := latin1(t, "From: digest@wildlife.example\nTo: bob@example.com\nSubject: Résumé hebdomadaire\n"+
		"List-Id: <digest.wildlife.example>\nMessage-Id: <l1@example.com>\n"+
		"Date: Wed, 03 Jan 2024 09:00:00 +0000\nMIME-Version: 1.0\n"+
		"Content-Type: text/plain; charset=iso-8859-1\nContent-Transfer-Encoding: 8bit\n\n"+
		"La colonie de numbats a grandi: café à l'été.\n")
	requireLegacyFanOut(t, "email.newsletter",
		fanOutJob{payload: eml, source: "latin1.eml", words: []string{"numbats"}},
		"café à l'été")
}

// One Latin-1 message does not drop the mbox: every message fans out.
func TestLegacyCharsetMboxFansOutAll(t *testing.T) {
	msg := func(from, subj, ct, body string) string {
		return "From " + from + " Mon Jan 01 10:00:00 2024\nFrom: " + from + "\nTo: bob@example.com\nSubject: " + subj +
			"\nMessage-Id: <" + subj + "@example.com>\nDate: Mon, 01 Jan 2024 10:00:00 +0000\n" + ct + "\n\n" + body + "\n\n"
	}
	mbox := msg("a@example.com", "one", "Content-Type: text/plain", "A dunnart crossed the ridge.") +
		latin1(t, msg("b@example.com", "two", "Content-Type: text/plain; charset=iso-8859-1\nContent-Transfer-Encoding: 8bit", "Le quoll a mangé l'appât.")) +
		msg("c@example.com", "three", "Content-Type: text/plain; charset=utf-8", "A bettong dug up truffles — naïve.")
	requireLegacyFanOut(t, "email.general",
		fanOutJob{payload: mbox, source: "mixed.mbox", words: []string{"dunnart", "quoll", "bettong"}},
		"dunnart", "mangé l'appât", "naïve")
}

// A Windows-1252 CSV export fans its rows out as correct UTF-8.
func TestLegacyCharsetCSVFansOut(t *testing.T) {
	csv := latin1(t, "content,source\nCafé crème at 5€ a cup,survey:1\nLe pâtissier a gagné “l’or”,survey:2\n")
	requireLegacyFanOut(t, "batch.csv",
		fanOutJob{payload: csv, source: "records.csv", words: []string{"crème", "pâtissier"}},
		"Café crème at 5€ a cup", "Le pâtissier a gagné “l’or”")
}

// Content no decoder can read fails the job with a reason: it does not
// complete with one blob object and no children.
func TestUndecodableContainerFailsLoudly(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	png := string([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff, 0xfe, 0x80})
	for _, name := range []string{"email.newsletter", "email.general", "batch.csv", "batch.jsonl"} {
		t.Run(name, func(t *testing.T) {
			q := jobs.NewQueue(driver.Jobs())
			now := time.Now().Truncate(time.Second)
			payload, enc := jobs.Payload(png)
			id := "bad-" + name
			if err := q.Enqueue(context.Background(), &storage.Job{
				ID: id, Type: "ingest:" + name, Status: storage.JobPending, Payload: payload, PayloadEncoding: enc,
				Pipeline: name, Source: "x.bin", MaxRetries: 1, CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatal(err)
			}
			drain(t, jobs.NewWorkerPool(q, Registry(), driver, 1, nil, config.JobsConfig{
				PollInterval: 20 * time.Millisecond, StaleTimeout: time.Minute, MaxRetries: 1, MaxHops: 5,
			}), driver.Jobs())
			got, err := q.Get(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != storage.JobFailed || got.Error == "" {
				t.Fatalf("job %s err=%q, want failed with a reason", got.Status, got.Error)
			}
		})
	}
}

// A long multi-byte subject is cut to a title on a rune boundary: the
// mbox completes and its message is stored, not failed on a split rune.
func TestLongCJKSubjectMboxFansOut(t *testing.T) {
	subject := "Re: " + strings.Repeat("日本語", 20)
	mbox := "From a@example.com Mon Jan 01 10:00:00 2024\nFrom: a@example.com\nTo: bob@example.com\nSubject: " + subject +
		"\nMessage-Id: <cjk@example.com>\nDate: Mon, 01 Jan 2024 10:00:00 +0000\nContent-Type: text/plain; charset=utf-8\n\n" +
		"A numbat crossed the road.\n\n"
	requireLegacyFanOut(t, "email.general",
		fanOutJob{payload: mbox, source: "cjk.mbox", words: []string{"numbat"}},
		"numbat")
}
