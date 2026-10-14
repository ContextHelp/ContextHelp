package composition

import (
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestCompose(t *testing.T) {
	objects := []*storage.KnowledgeObject{
		{ID: "o-1", Summaries: []string{"first summary"}},
		{ID: "o-2", RawContent: "raw body"},
		{ID: "o-3"},
	}
	got := Compose(objects, "brief")
	want := "# brief\n\nGenerated from 3 knowledge objects.\n\n" +
		"## o-1\nfirst summary\n\n## o-2\nraw body\n\n## o-3\n"
	if got != want {
		t.Errorf("Compose =\n%q\nwant\n%q", got, want)
	}

	r := WithCitations(objects, "plan")
	for _, s := range []string{"# plan\n", "first summary [ref:o-1]", "raw body [ref:o-2]", "## o-3\n[ref:o-3]"} {
		if !strings.Contains(r.Content, s) {
			t.Errorf("WithCitations content lacks %q:\n%s", s, r.Content)
		}
	}
	if r.Type != "plan" || len(r.Citations) != 3 || strings.Join(r.SourceIDs, ",") != "o-1,o-2,o-3" || r.GeneratedAt.IsZero() {
		t.Errorf("WithCitations result = %+v", r)
	}
}
