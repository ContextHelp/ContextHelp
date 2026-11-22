package steps

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func win1252(t *testing.T, s string) string {
	t.Helper()
	b, err := charmap.Windows1252.NewEncoder().String(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestJSONLParserDecodesWindows1252(t *testing.T) {
	d := &storage.KnowledgeObject{RawContent: win1252(t, `{"content":"Café à 5€"}`+"\n")}
	out, err := NewJSONLParser().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	recs := out.Metadata["import_records"].([]map[string]any)
	if len(recs) != 1 || recs[0]["content"] != "Café à 5€" {
		t.Fatalf("records = %#v", recs)
	}
}

func TestJSONLParserHonorsDeclaredCharset(t *testing.T) {
	// 0xE9 is é in ISO-8859-2 and 0xF8 is ř there, ø in Windows-1252.
	d := &storage.KnowledgeObject{
		RawContent: "{\"content\":\"\xf8\"}\n",
		Metadata:   map[string]any{"content_type": "application/x-ndjson; charset=iso-8859-2"},
	}
	out, err := NewJSONLParser().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Metadata["import_records"].([]map[string]any)[0]["content"]; got != "ř" {
		t.Fatalf("content = %q, want ř", got)
	}
}

func TestCSVParserDecodesWindows1252(t *testing.T) {
	d := &storage.KnowledgeObject{RawContent: win1252(t, "content\nCafé crème\n"), Source: "x.csv"}
	out, err := NewCSVParser().Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	rows := out.Metadata["csv_rows"].([]map[string]string)
	if len(rows) != 1 || rows[0]["content"] != "Café crème" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestParsersRejectUndecodableContentPermanently(t *testing.T) {
	bin := string([]byte{0x89, 'P', 'N', 'G', 0x00, 0xff, 0xfe})
	for name, step := range map[string]pipeline.PipelineStep{
		"csv": NewCSVParser(), "jsonl": NewJSONLParser(), "email": NewEmailParser(),
	} {
		_, err := step.Run(context.Background(), &storage.KnowledgeObject{RawContent: bin})
		if err == nil || !pipeline.IsPermanent(err) {
			t.Errorf("%s: err = %v, want permanent", name, err)
		}
	}
}

func TestEmailParserDecodesEachPartByItsCharset(t *testing.T) {
	raw := win1252(t, "From: Zoë <zoe@example.com>\nTo: bob@example.com\nSubject: Café\n"+
		"Message-Id: <m@example.com>\nMIME-Version: 1.0\n"+
		"Content-Type: multipart/alternative; boundary=BB\n\n"+
		"--BB\nContent-Type: text/plain; charset=iso-8859-1\nContent-Transfer-Encoding: 8bit\n\nplain: crème brûlée\n"+
		"--BB\nContent-Type: text/html; charset=windows-1252\nContent-Transfer-Encoding: quoted-printable\n\n<p>5=80 =E0 la carte</p>\n"+
		"--BB--\n")
	out, err := NewEmailParser().Run(context.Background(), &storage.KnowledgeObject{RawContent: raw})
	if err != nil {
		t.Fatal(err)
	}
	m := out.Metadata["email_messages"].([]map[string]any)[0]
	if m["subject"] != "Café" || !strings.Contains(m["from"].(string), "zoe@") {
		t.Fatalf("headers = %#v", m)
	}
	if got := m["text_body"].(string); !strings.Contains(got, "crème brûlée") {
		t.Errorf("text_body = %q", got)
	}
	if got := m["html_body"].(string); !strings.Contains(got, "5€ à la carte") {
		t.Errorf("html_body = %q", got)
	}
	for k, v := range m {
		if s, ok := v.(string); ok && !utf8.ValidString(s) {
			t.Errorf("%s is not valid UTF-8: %q", k, s)
		}
	}
}

func TestEmailParserDecodesEncodedWordInLegacyCharset(t *testing.T) {
	raw := "From: a@example.com\nTo: b@example.com\nSubject: =?iso-8859-2?Q?=F8ekni?=\nContent-Type: text/plain\n\nbody\n"
	out, err := NewEmailParser().Run(context.Background(), &storage.KnowledgeObject{RawContent: raw})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Metadata["email_messages"].([]map[string]any)[0]["subject"]; got != "řekni" {
		t.Fatalf("subject = %q", got)
	}
}

func TestUndeclaredUTF8StrayBytesVersusLegacy(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		guessed bool
	}{
		{"caf\xc3\xa9 \xe9t\xe9", "cafÃ© été", true}, // ambiguous: lossless Windows-1252
		{"hello \xe9", "hello é", true},
		{"\xe9t\xe9", "été", true},
		{"caf\xc3\xa9 ok", "café ok", false},
	}
	for _, c := range cases {
		got, guessed := undeclaredUTF8([]byte(c.in))
		if got != c.want || guessed != c.guessed {
			t.Errorf("undeclaredUTF8(%q) = %q, %v; want %q, %v", c.in, got, guessed, c.want, c.guessed)
		}
	}
}

// Short legacy CJK text is often half-valid UTF-8 by chance. It must take
// the Windows-1252 path (one byte, one rune: recoverable), never have its
// invalid bytes replaced with U+FFFD.
func TestUndeclaredShortLegacyCJKIsNotReplaced(t *testing.T) {
	cases := map[string]string{
		"gbk 说明":        "\xcb\xb5\xc3\xf7",
		"gbk 日本":        "\xc8\xd5\xb1\xbe",
		"shift-jis 繁體字": "\x94\xc9\xe9\x93\x8e\x9a",
		"euc-kr 한글":     "\xc7\xd1\xb1\xdb",
		"euc-kr 안녕":     "\xbe\xc8\xb3\xe7",
	}
	for name, in := range cases {
		got, guessed := undeclaredUTF8([]byte(in))
		if strings.ContainsRune(got, utf8.RuneError) {
			t.Errorf("%s: %q replaced bytes with U+FFFD: %q", name, in, got)
		}
		if want := decodeWindows1252([]byte(in)); got != want || !guessed {
			t.Errorf("%s: got %q, %v; want Windows-1252 %q, true", name, got, guessed, want)
		}
		back, err := charmap.Windows1252.NewEncoder().String(got)
		if err != nil || back != in {
			t.Errorf("%s: bytes not recoverable: %q, %v", name, back, err)
		}
	}
}
