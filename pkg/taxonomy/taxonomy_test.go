package taxonomy_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/pkg/taxonomy"
)

func TestTypeConstants(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"TypeURL", taxonomy.TypeURL, "url"},
		{"TypeText", taxonomy.TypeText, "text"},
		{"TypeImage", taxonomy.TypeImage, "image"},
		{"TypeAudio", taxonomy.TypeAudio, "audio"},
		{"TypeVideo", taxonomy.TypeVideo, "video"},
		{"TypeFeed", taxonomy.TypeFeed, "feed"},
		{"TypeDocument", taxonomy.TypeDocument, "document"},
		{"TypeCode", taxonomy.TypeCode, "code"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("got %q, want %q", c.got, c.want)
			}
		})
	}
}

func TestSubtypeConstants(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		// text subtypes
		{"SubtypeTextShort", taxonomy.SubtypeTextShort, "short"},
		{"SubtypeTextLong", taxonomy.SubtypeTextLong, "long"},
		// document subtypes
		{"SubtypeDocumentPDF", taxonomy.SubtypeDocumentPDF, "pdf"},
		{"SubtypeDocumentMarkdown", taxonomy.SubtypeDocumentMarkdown, "markdown"},
		{"SubtypeDocumentCode", taxonomy.SubtypeDocumentCode, "code"},
		{"SubtypeDocumentOffice", taxonomy.SubtypeDocumentOffice, "office"},
		{"SubtypeDocumentEPUB", taxonomy.SubtypeDocumentEPUB, "epub"},
		{"SubtypeDocumentHTML", taxonomy.SubtypeDocumentHTML, "html"},
		// email subtypes
		{"SubtypeEmailBilling", taxonomy.SubtypeEmailBilling, "billing"},
		{"SubtypeEmailNewsletter", taxonomy.SubtypeEmailNewsletter, "newsletter"},
		{"SubtypeEmailAttachment", taxonomy.SubtypeEmailAttachment, "email-attachment"},
		{"SubtypeEmail", taxonomy.SubtypeEmail, "email"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("got %q, want %q", c.got, c.want)
			}
		})
	}
}
