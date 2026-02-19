package steps

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
	"with": true, "by": true, "from": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "been": true, "being": true, "have": true, "has": true,
	"had": true, "do": true, "does": true, "did": true, "will": true, "would": true,
	"could": true, "should": true, "may": true, "might": true, "shall": true,
	"can": true, "need": true, "must": true, "it": true, "its": true, "this": true,
	"that": true, "these": true, "those": true, "i": true, "you": true, "he": true,
	"she": true, "we": true, "they": true, "me": true, "him": true, "her": true,
	"us": true, "them": true, "my": true, "your": true, "his": true, "our": true,
	"their": true, "what": true, "which": true, "who": true, "whom": true,
	"not": true, "no": true, "if": true, "then": true, "so": true, "as": true,
}

type Tagger struct {
	pipeline.BaseContract
	maxTags int
}

func NewTagger() *Tagger {
	return &Tagger{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Tags"},
		}),
		maxTags: 10,
	}
}

func (t *Tagger) Name() string { return "tagger" }

func (t *Tagger) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	words := tokenize(draft.RawContent)
	if len(words) == 0 {
		draft.Tags = nil
		return draft, nil
	}

	freq := make(map[string]int)
	for _, w := range words {
		if !stopWords[w] && len(w) > 2 {
			freq[w]++
		}
	}

	type wordCount struct {
		word  string
		count int
	}
	var counts []wordCount
	for w, c := range freq {
		counts = append(counts, wordCount{w, c})
	}
	sort.Slice(counts, func(i, j int) bool {
		return counts[i].count > counts[j].count
	})

	limit := t.maxTags
	if len(counts) < limit {
		limit = len(counts)
	}

	maxCount := 1
	if len(counts) > 0 {
		maxCount = counts[0].count
	}

	tags := make([]storage.Tag, limit)
	for i := 0; i < limit; i++ {
		tags[i] = storage.Tag{
			Label:  counts[i].word,
			Weight: float64(counts[i].count) / float64(maxCount),
			Source: "auto",
		}
	}

	draft.Tags = tags
	return draft, nil
}

func tokenize(text string) []string {
	var words []string
	f := func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsDigit(c)
	}
	for _, w := range strings.FieldsFunc(text, f) {
		words = append(words, strings.ToLower(w))
	}
	return words
}
