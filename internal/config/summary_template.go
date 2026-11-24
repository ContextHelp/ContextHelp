package config

import (
	"errors"
	"fmt"
	"io"
	"text/template"
	"text/template/parse"
)

// SummaryTemplateData is what a summarization prompt template sees:
// .Text, .Medium and .Language.
type SummaryTemplateData struct {
	Text     string
	Medium   string
	Language string
}

// ErrSummaryTemplate marks a prompt template that cannot be used.
var ErrSummaryTemplate = errors.New("invalid summarization prompt template")

// ParseSummaryTemplate parses a prompt template and checks it: it must
// place the text with an unconditional top-level {{.Text}} (no pipe, not
// inside an if, range or with), and it must run for every medium and
// language, so an unknown field in a branch fails here and not on some
// later request. It is used at config load and again by the providers
// package, which renders with the result.
func ParseSummaryTemplate(text string) (*template.Template, error) {
	t, err := template.New("summary").Option("missingkey=error").Parse(text)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSummaryTemplate, err)
	}
	if !placesText(t) {
		return nil, fmt.Errorf("%w: needs an unconditional {{.Text}} outside any if, range or with", ErrSummaryTemplate)
	}
	for _, medium := range []string{"", "audio", "video", "meeting", "other"} {
		for _, lang := range []string{"", "x"} {
			if err := t.Execute(io.Discard, SummaryTemplateData{Text: "x", Medium: medium, Language: lang}); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrSummaryTemplate, err)
			}
		}
	}
	return t, nil
}

// placesText reports whether the template has a top-level action that is
// exactly {{.Text}}.
func placesText(t *template.Template) bool {
	if t.Tree == nil || t.Root == nil {
		return false
	}
	for _, n := range t.Root.Nodes {
		act, ok := n.(*parse.ActionNode)
		if !ok || act.Pipe == nil || len(act.Pipe.Decl) > 0 || len(act.Pipe.Cmds) != 1 {
			continue
		}
		args := act.Pipe.Cmds[0].Args
		if len(args) != 1 {
			continue
		}
		if f, ok := args[0].(*parse.FieldNode); ok && len(f.Ident) == 1 && f.Ident[0] == "Text" {
			return true
		}
	}
	return false
}
