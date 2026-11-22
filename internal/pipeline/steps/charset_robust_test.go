package steps

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

func runEmail(t *testing.T, raw string, opts ...EmailParserOption) *storage.KnowledgeObject {
	t.Helper()
	out, err := NewEmailParser(opts...).Run(context.Background(), &storage.KnowledgeObject{RawContent: raw})
	if err != nil {
		t.Fatalf("email_parser: %v", err)
	}
	return out
}

func emailMsgs(d *storage.KnowledgeObject) []map[string]any {
	return d.Metadata["email_messages"].([]map[string]any)
}

// requireCleanStrings fails when any string in v (recursively) is not
// valid UTF-8 or holds NUL.
func requireCleanStrings(t *testing.T, path string, v any) {
	t.Helper()
	switch x := v.(type) {
	case string:
		if blobref.NeedsBlob(x) {
			t.Errorf("%s holds NUL or invalid UTF-8: %q", path, x)
		}
	case map[string]any:
		for k, e := range x {
			requireCleanStrings(t, path+"."+k, k)
			requireCleanStrings(t, path+"."+k, e)
		}
	case []any:
		for i, e := range x {
			requireCleanStrings(t, path, e)
			_ = i
		}
	case []map[string]any:
		for _, e := range x {
			requireCleanStrings(t, path, e)
		}
	}
}

func TestJSONLMostlyUTF8KeepsValidTextReplacesStrayByte(t *testing.T) {
	raw := "{\"content\":\"caf\xc3\xa9 cr\xc3\xa8me \xe9 l\xc3\xa0 carte d\xc3\xa9j\xc3\xa0 vu\"}\n"
	d := &storage.KnowledgeObject{RawContent: raw}
	out, err := NewJSONLParser().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	got := out.Metadata["import_records"].([]map[string]any)[0]["content"]
	if want := "café crème � là carte déjà vu"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if out.Metadata[MetaCharsetGuessed] != true {
		t.Errorf("charset_guessed not recorded: %#v", out.Metadata)
	}
}

func TestCSVLegacyInputRecordsCharsetGuessed(t *testing.T) {
	d := &storage.KnowledgeObject{RawContent: win1252(t, "content\nCafé crème\n"), Source: "x.csv"}
	out, err := NewCSVParser().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if out.Metadata[MetaCharsetGuessed] != true {
		t.Errorf("charset_guessed not recorded: %#v", out.Metadata)
	}
	clean := &storage.KnowledgeObject{RawContent: "content\nCafé\n"}
	out, _ = NewCSVParser().Run(context.Background(), clean)
	if _, ok := out.Metadata[MetaCharsetGuessed]; ok {
		t.Error("valid UTF-8 must not be marked guessed")
	}
}

func TestEmailBodyCutMidRuneIsTrimmedNotGarbled(t *testing.T) {
	body := strings.Repeat("é", 50)
	raw := "From: a@example.com\nTo: b@example.com\nSubject: x\nContent-Type: text/plain; charset=utf-8\n\n" + body + "\n"
	// 31 bytes ends inside the 16th é.
	out := runEmail(t, raw, WithEmailMaxBodyBytes(31))
	m := emailMsgs(out)[0]
	got := m["text_body"].(string)
	if got != strings.Repeat("é", 15) {
		t.Fatalf("text_body = %q", got)
	}
	if _, ok := m[MetaCharsetGuessed]; ok {
		t.Error("a trimmed cut is not a guess")
	}
}

func TestEmailBodyUndeclaredMostlyUTF8KeepsText(t *testing.T) {
	raw := "From: a@example.com\nTo: b@example.com\nSubject: x\n\n" +
		"caf\xc3\xa9 cr\xc3\xa8me \xe9 l\xc3\xa0 carte d\xc3\xa9j\xc3\xa0 vu\n"
	m := emailMsgs(runEmail(t, raw))[0]
	if got := m["text_body"].(string); !strings.Contains(got, "café crème") || strings.Contains(got, "Ã") {
		t.Fatalf("text_body = %q", got)
	}
	if m[MetaCharsetGuessed] != true {
		t.Error("charset_guessed not recorded")
	}
}

func TestEmailHeadersDecodedPerHeader(t *testing.T) {
	// Subject is valid UTF-8; Reply-To carries one stray Latin-1 byte.
	raw := "From: a@example.com\nTo: b@example.com\nSubject: Caf\xc3\xa9 d\xc3\xa9j\xc3\xa0\nReply-To: Ren\xe9 <r@example.com>\n\nbody\n"
	out := runEmail(t, raw)
	m := emailMsgs(out)[0]
	if m["subject"] != "Café déjà" {
		t.Fatalf("subject = %q: one stray header re-decoded the others", m["subject"])
	}
	if !strings.Contains(m["reply_to"].(string), "René") {
		t.Errorf("reply_to = %q", m["reply_to"])
	}
	if out.Metadata[MetaCharsetGuessed] != true {
		t.Error("charset_guessed not recorded")
	}
}

func TestEmailUnknownCharsetParses(t *testing.T) {
	for _, cs := range []string{"x-no-such", "unknown-8bit", "x-unknown", "ibm850"} {
		raw := "From: a@example.com\nTo: b@example.com\nSubject: x\nContent-Type: text/plain; charset=" + cs + "\n\ncaf\xe9\n"
		out, err := NewEmailParser().Run(context.Background(), &storage.KnowledgeObject{RawContent: raw})
		if err != nil {
			t.Fatalf("%s: %v", cs, err)
		}
		m := emailMsgs(out)[0]
		if got := m["text_body"].(string); !strings.HasPrefix(got, "café") {
			t.Errorf("%s: text_body = %q", cs, got)
		}
		if m[MetaCharsetGuessed] != true {
			t.Errorf("%s: charset_guessed not recorded", cs)
		}
	}
}

const tinyPNG = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\x00IEND\xaeB`\x82"

func relatedMail(imageFirst bool) string {
	png := "Content-Type: image/png\nContent-Transfer-Encoding: base64\nContent-ID: <logo>\nContent-Disposition: inline; filename=\"logo.png\"\n\n" +
		base64.StdEncoding.EncodeToString([]byte(tinyPNG)) + "\n"
	html := "Content-Type: text/html; charset=utf-8\n\n<p>Hi <img src=\"cid:logo\"></p>\n"
	parts := "--RR\n" + html + "--RR\n" + png
	if imageFirst {
		parts = "--RR\n" + png + "--RR\n" + html
	}
	return "From: a@example.com\nTo: b@example.com\nSubject: Logo\nMIME-Version: 1.0\nContent-Type: multipart/related; boundary=RR\n\n" + parts + "--RR--\n"
}

func TestEmailInlineImageIsAttachmentNotBody(t *testing.T) {
	for _, imageFirst := range []bool{false, true} {
		raw := relatedMail(imageFirst)
		for name, input := range map[string]string{
			"eml":  raw,
			"mbox": "From a@example.com Mon Jan  1 00:00:00 2024\n" + raw,
		} {
			m := emailMsgs(runEmail(t, input))
			if len(m) != 1 {
				t.Fatalf("%s image-first=%v: %d messages, want 1", name, imageFirst, len(m))
			}
			if got := m[0]["html_body"].(string); !strings.Contains(got, "cid:logo") {
				t.Errorf("%s image-first=%v: html_body = %q", name, imageFirst, got)
			}
			if m[0]["text_body"] != "" {
				t.Errorf("%s: text_body = %q, want empty", name, m[0]["text_body"])
			}
			atts := m[0]["attachments"].([]map[string]any)
			if len(atts) != 1 || atts[0]["content_type"] != "image/png" || atts[0]["filename"] != "logo.png" {
				t.Errorf("%s image-first=%v: attachments = %#v", name, imageFirst, atts)
			}
			requireCleanStrings(t, name, m[0])
		}
	}
}

func TestEmailBareImageMessageIsAttachment(t *testing.T) {
	raw := "From: a@example.com\nTo: b@example.com\nSubject: pic\nContent-Type: image/png; name=\"p.png\"\nContent-Transfer-Encoding: base64\n\n" +
		base64.StdEncoding.EncodeToString([]byte(tinyPNG)) + "\n"
	m := emailMsgs(runEmail(t, raw))[0]
	atts := m["attachments"].([]map[string]any)
	if len(atts) != 1 || atts[0]["content_type"] != "image/png" || m["text_body"] != "" {
		t.Fatalf("message = %#v", m)
	}
}

func TestEmailCutInsidePartHeaderKeepsEarlierParts(t *testing.T) {
	raw := "From: a@example.com\nTo: b@example.com\nSubject: big\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=BB\n\n" +
		"--BB\nContent-Type: text/plain\n\nthe first part survives\n" +
		"--BB\nContent-Type: text/html\nContent-Disposition: inline\n\n<p>second</p>\n--BB--\n"
	cut := strings.Index(raw, "Content-Type: text/html") + len("Content-Ty")
	// The body limit lands inside the second part's header: a message of
	// well over the limit that ends mid header.
	max := cut - strings.Index(raw, "\n\n--BB") - 2
	out := runEmail(t, raw, WithEmailMaxBodyBytes(max))
	m := emailMsgs(out)[0]
	if got := m["text_body"].(string); !strings.Contains(got, "the first part survives") {
		t.Fatalf("text_body = %q: earlier part lost", got)
	}
}

func TestEmailEncodedWordsAndFilenamesAreClean(t *testing.T) {
	// =?utf-8?B?eAD/eQ==?= decodes to x NUL 0xFF y.
	raw := "From: a@example.com\nTo: b@example.com\nSubject: =?utf-8?B?eAD/eQ==?=\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=BB\n\n" +
		"--BB\nContent-Type: text/plain\n\nhi\n" +
		"--BB\nContent-Type: application/pdf\nContent-Disposition: attachment; filename=\"=?utf-8?B?eAD/eQ==?=\"\n\nxx\n--BB--\n"
	m := emailMsgs(runEmail(t, raw))[0]
	requireCleanStrings(t, "message", m)
	if m["subject"] != "x�y" {
		t.Errorf("subject = %q", m["subject"])
	}
	atts := m["attachments"].([]map[string]any)
	if len(atts) != 1 || atts[0]["filename"] != "x�y" {
		t.Errorf("attachments = %#v", atts)
	}
}

func TestEmailTextPartWithNULKeepsMessage(t *testing.T) {
	raw := "From: a@example.com\nTo: b@example.com\nSubject: x\nContent-Type: text/plain\n\nab\x00cd\n"
	m := emailMsgs(runEmail(t, raw))[0]
	if m["text_body"] != "abcd\n" {
		t.Errorf("text_body = %q", m["text_body"])
	}
}

func TestMboxNULInOneMessageSkipsOnlyThatMessage(t *testing.T) {
	good := "From a@example.com Mon Jan  1 00:00:00 2024\nFrom: a@example.com\nTo: b@example.com\nSubject: good\n\nok\n"
	bad := "From a@example.com Mon Jan  1 00:00:00 2024\nFrom: a@example.com\nTo: b@example.com\nSubject: ba\x00d\n\nno\n"
	out := runEmail(t, good+bad+good)
	if n := len(emailMsgs(out)); n != 2 {
		t.Fatalf("%d messages, want 2", n)
	}
	_, err := NewEmailParser().Run(context.Background(), &storage.KnowledgeObject{RawContent: bad})
	if err == nil || !pipeline.IsPermanent(err) {
		t.Fatalf("all-bad mbox: err = %v, want permanent", err)
	}
}

func TestJSONLNULEscapesAreDropped(t *testing.T) {
	d := &storage.KnowledgeObject{RawContent: `{"content":"a\u0000b","k\u0000":["x\u0000",{"n":"\u0000y"}]}` + "\n"}
	out, err := NewJSONLParser().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	rec := out.Metadata["import_records"].([]map[string]any)[0]
	requireCleanStrings(t, "record", rec)
	if rec["content"] != "ab" {
		t.Errorf("content = %q", rec["content"])
	}
}

func TestBatchEnqueuerTitleCutsOnRuneBoundary(t *testing.T) {
	d := &storage.KnowledgeObject{Metadata: map[string]any{"import_records": []map[string]any{{"content": strings.Repeat("é", 100)}}}}
	out, err := NewBatchEnqueuer().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	title := out.Sections[0].Title
	if !utf8.ValidString(title) || utf8.RuneCountInString(title) != 80 {
		t.Fatalf("title = %q (%d runes)", title, utf8.RuneCountInString(title))
	}
	items := out.Metadata[itemsToEnqueueKey].([]map[string]any)
	if !utf8.ValidString(items[0]["title"].(string)) {
		t.Errorf("item title not valid UTF-8")
	}
}

func TestTypeDetectorTreatsBlobRefToBinaryAsBinary(t *testing.T) {
	d := &storage.KnowledgeObject{
		RawContent:  blobref.For("k1"),
		BlobContent: map[string]string{"k1": pngSig},
	}
	out, err := NewTypeDetector().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if out.ContentType != "image/png" || out.Subtype != "" || out.Type != "" {
		t.Fatalf("type %q subtype %q content type %q: binary behind a ref was labelled as text", out.Type, out.Subtype, out.ContentType)
	}
}

// runChain runs steps over draft the way the worker does, skipping the
// ones that do not take binary content while the draft holds any.
func runChain(t *testing.T, draft *storage.KnowledgeObject, steps ...pipeline.PipelineStep) *storage.KnowledgeObject {
	t.Helper()
	for _, s := range steps {
		if pipeline.SkipsBinary(s, draft) {
			continue
		}
		var err error
		if draft, err = s.Run(context.Background(), draft); err != nil {
			t.Fatalf("%s: %v", s.Name(), err)
		}
	}
	return draft
}

// Legacy-charset input is not text as far as the worker's binary check
// goes, yet the whole batch and email chains must still run on it.
func TestLegacyCharsetInputRunsWholeParserChains(t *testing.T) {
	filter, err := NewEmailFilterFromRules(nil)
	if err != nil {
		t.Fatal(err)
	}
	csv := runChain(t, &storage.KnowledgeObject{RawContent: win1252(t, "content\nCafé crème\n"), Source: "x.csv"},
		NewCSVParser(), NewRecordValidator(), NewBatchEnqueuer())
	if len(csv.Sections) != 1 || len(csv.Metadata[itemsToEnqueueKey].([]map[string]any)) != 1 {
		t.Errorf("csv chain: sections %d metadata %#v", len(csv.Sections), csv.Metadata)
	}
	jl := runChain(t, &storage.KnowledgeObject{RawContent: win1252(t, `{"content":"Café"}`+"\n")},
		NewJSONLParser(), NewRecordValidator(), NewBatchEnqueuer())
	if len(jl.Sections) != 1 || jl.Sections[0].Content != "Café" {
		t.Errorf("jsonl chain: sections %#v", jl.Sections)
	}
	mail := runChain(t, &storage.KnowledgeObject{RawContent: win1252(t,
		"From a@example.com Mon Jan  1 00:00:00 2024\nFrom: a@example.com\nTo: b@example.com\nSubject: Café\nMessage-Id: <1@x>\n\nplain crème brûlée plain crème brûlée plain\n")},
		NewEmailParser(), filter, NewEmailEnqueuer())
	if n := len(mail.Metadata[itemsToEnqueueKey].([]map[string]any)); n != 1 {
		t.Errorf("email chain staged %d items, metadata %#v", n, mail.Metadata)
	}
}
