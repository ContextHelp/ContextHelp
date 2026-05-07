// Command ben-adapter-ctxt-recall is a hop.top/ben binary plugin that
// scores ctxt's recall on a fixed corpus + query list.
//
// It implements ben's binary adapter stdio JSON protocol (see
// hop.top/ben/internal/adapter/binary.go): read one request JSON object
// from stdin, write one response JSON object to stdout, all logs to stderr.
//
// Inputs (from spec.candidate.cmd / spec.input):
//
//	corpus  — path to a YAML file with a list of {id, text, tags} objects
//	queries — path to a YAML file with a list of {id, query, expected_ids, k}
//	         The expected_ids are the relevant document IDs for the query.
//	         k defaults to 5 if omitted.
//	mode    — "fts_baseline" (default) — token-overlap recall using the
//	          same lexical-fix semantics as the post-T-0565 text.short
//	          pipeline (hyphen-tolerant, bullet-aware tokenization).
//	          Future modes ("vector_baseline", "hybrid_rrf") may be added
//	          when the embeddings code path lands (ADR-071 Phase 1).
//
// Outputs (per ben binary-adapter response shape):
//
//	{
//	  "metrics": {
//	    "recall_at_k":      0.0..1.0,  // mean recall@k across all queries
//	    "queries_total":    N,
//	    "queries_passing":  N',        // queries where any expected_id was retrieved
//	    "queries_failing":  N - N'
//	  },
//	  "output": "<human-readable summary>"
//	}
//
// The plugin never returns recall outside [0, 1]. A non-zero exit code
// signals an infrastructure failure (file not found, invalid YAML); a
// failed-recall benchmark still exits 0 with a low score.
//
// This adapter exists because ctxt's pipeline-version gating (ADR-070)
// and embedding-model gating (ADR-071) need a deterministic, side-effect-
// free recall measurement that runs in CI without LLM credits or a live
// dpkms instance. The fixture corpus + queries live under
// test/integration/testdata/ben-fixtures/ so they ship with the repo and
// can be edited by the same PR that changes a pipeline_version.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Protocol shapes — mirror hop.top/ben/internal/adapter.binaryAdapter*.
// Decoupled from ben's internal package on purpose: the adapter must
// remain runnable even if ben's internal API drifts. The on-the-wire
// JSON is a stable v1 contract.

type request struct {
	Action    string         `json:"action"`
	Candidate candidate      `json:"candidate"`
	Input     map[string]any `json:"input"`
}

type candidate struct {
	Name    string `json:"name"`
	Adapter string `json:"adapter"`
	Cmd     string `json:"cmd"`
	Model   string `json:"model"`
}

type response struct {
	Metrics map[string]float64 `json:"metrics"`
	Output  string             `json:"output"`
}

type corpusObject struct {
	ID   string   `yaml:"id"`
	Text string   `yaml:"text"`
	Tags []string `yaml:"tags,omitempty"`
}

type queryCase struct {
	ID          string   `yaml:"id"`
	Query       string   `yaml:"query"`
	ExpectedIDs []string `yaml:"expected_ids"`
	K           int      `yaml:"k,omitempty"`
}

type corpusFile struct {
	Objects []corpusObject `yaml:"objects"`
}

type queriesFile struct {
	Queries []queryCase `yaml:"queries"`
	K       int         `yaml:"k,omitempty"` // default k if per-query omitted
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "ben-adapter-ctxt-recall: %v\n", err)
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("empty request on stdin")
	}

	var req request
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}

	corpusPath, _ := req.Input["corpus"].(string)
	queriesPath, _ := req.Input["queries"].(string)
	mode, _ := req.Input["mode"].(string)
	if mode == "" {
		mode = "fts_baseline"
	}
	if corpusPath == "" {
		return fmt.Errorf("input.corpus is required")
	}
	if queriesPath == "" {
		return fmt.Errorf("input.queries is required")
	}

	corpus, err := loadCorpus(corpusPath)
	if err != nil {
		return fmt.Errorf("load corpus: %w", err)
	}
	qf, err := loadQueries(queriesPath)
	if err != nil {
		return fmt.Errorf("load queries: %w", err)
	}

	defaultK := qf.K
	if defaultK == 0 {
		defaultK = 5
	}

	switch mode {
	case "fts_baseline", "vector_baseline":
		// vector_baseline currently falls back to the lexical baseline; the
		// suite shape exists per ADR-071 Phase 3 even though the candidate
		// embedding-model code path lands later (T-0584).
	default:
		return fmt.Errorf("unsupported mode %q (valid: fts_baseline, vector_baseline)", mode)
	}

	totalQueries := len(qf.Queries)
	if totalQueries == 0 {
		return fmt.Errorf("queries file %q is empty", queriesPath)
	}

	var sumRecall float64
	passing := 0
	perQueryLines := make([]string, 0, totalQueries)
	for _, q := range qf.Queries {
		k := q.K
		if k == 0 {
			k = defaultK
		}
		ranked := scoreCorpus(corpus, q.Query, mode)
		topK := ranked
		if len(topK) > k {
			topK = topK[:k]
		}
		topIDs := make(map[string]struct{}, len(topK))
		for _, r := range topK {
			topIDs[r.id] = struct{}{}
		}
		hits := 0
		for _, expected := range q.ExpectedIDs {
			if _, ok := topIDs[expected]; ok {
				hits++
			}
		}
		var recall float64
		if len(q.ExpectedIDs) > 0 {
			recall = float64(hits) / float64(len(q.ExpectedIDs))
		}
		sumRecall += recall
		if hits > 0 {
			passing++
		}
		perQueryLines = append(perQueryLines,
			fmt.Sprintf("  %-24s  recall@%d=%0.3f  (%d/%d expected found)",
				q.ID, k, recall, hits, len(q.ExpectedIDs)))
	}

	mean := sumRecall / float64(totalQueries)
	if math.IsNaN(mean) {
		mean = 0
	}

	resp := response{
		Metrics: map[string]float64{
			"recall_at_k":     mean,
			"queries_total":   float64(totalQueries),
			"queries_passing": float64(passing),
			"queries_failing": float64(totalQueries - passing),
		},
		Output: fmt.Sprintf(
			"ctxt recall harness — mode=%s corpus=%d objects queries=%d\n"+
				"mean recall@k = %0.3f  passing=%d failing=%d\n%s",
			mode, len(corpus), totalQueries, mean, passing,
			totalQueries-passing, strings.Join(perQueryLines, "\n"),
		),
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "")
	return enc.Encode(resp)
}

func loadCorpus(path string) ([]corpusObject, error) {
	data, err := os.ReadFile(path) //nolint:gosec // fixture path from suite YAML
	if err != nil {
		return nil, err
	}
	var f corpusFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if len(f.Objects) == 0 {
		return nil, fmt.Errorf("%s: no objects", path)
	}
	for i, o := range f.Objects {
		if o.ID == "" {
			return nil, fmt.Errorf("%s: object[%d] missing id", path, i)
		}
		if o.Text == "" {
			return nil, fmt.Errorf("%s: object %q missing text", path, o.ID)
		}
	}
	return f.Objects, nil
}

func loadQueries(path string) (*queriesFile, error) {
	data, err := os.ReadFile(path) //nolint:gosec // fixture path from suite YAML
	if err != nil {
		return nil, err
	}
	var f queriesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	for i, q := range f.Queries {
		if q.ID == "" {
			return nil, fmt.Errorf("%s: query[%d] missing id", path, i)
		}
		if q.Query == "" {
			return nil, fmt.Errorf("%s: query %q missing query text", path, q.ID)
		}
		if len(q.ExpectedIDs) == 0 {
			return nil, fmt.Errorf("%s: query %q missing expected_ids", path, q.ID)
		}
	}
	return &f, nil
}

type ranked struct {
	id    string
	score float64
}

// scoreCorpus returns a deterministic ranking of corpus objects for the
// given query string. The current scorer is a token-overlap baseline that
// mirrors the post-T-0565 text.short FTS behaviour:
//   - bullet markers (`-`, `*`) are treated as token separators (not part
//     of any token), so bullet items are visible to the index;
//   - hyphens inside words are preserved, which lets compound terms like
//     "auth-flow" or "uri-scheme" match end-to-end (T-0565 fix);
//   - tags are weighted higher than body tokens (matches the tagger step's
//     contribution to the FTS column).
//
// This is intentionally NOT a re-implementation of the full hybrid-search
// stack. The harness measures recall regressions in a deterministic,
// CGo-free environment; a future mode = "vector_baseline" can plug in the
// embedding-model output once T-0582/T-0584 land.
func scoreCorpus(corpus []corpusObject, query string, _ string) []ranked {
	qTokens := tokenize(query)
	if len(qTokens) == 0 {
		return nil
	}
	qSet := make(map[string]struct{}, len(qTokens))
	for _, t := range qTokens {
		qSet[t] = struct{}{}
	}

	out := make([]ranked, 0, len(corpus))
	for _, o := range corpus {
		bodyTokens := tokenize(o.Text)
		bodyHits := 0
		seen := make(map[string]struct{}, len(qTokens))
		for _, t := range bodyTokens {
			if _, ok := qSet[t]; ok {
				if _, dup := seen[t]; !dup {
					seen[t] = struct{}{}
					bodyHits++
				}
			}
		}
		tagHits := 0
		for _, tag := range o.Tags {
			for _, t := range tokenize(tag) {
				if _, ok := qSet[t]; ok {
					tagHits++
				}
			}
		}
		// Tags weighted 3× — matches the structured_metadata + tagger boost
		// that the text.short pipeline gives to tag tokens in the FTS column.
		score := float64(bodyHits) + 3*float64(tagHits)
		if score > 0 {
			out = append(out, ranked{id: o.ID, score: score})
		}
	}
	// Sort descending by score; tie-break by id for determinism.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].id < out[j].id
	})
	return out
}

// tokenize splits s into lowercased word tokens. Mirrors the bullet+hyphen
// behaviour added to the text.short pipeline by T-0565: bullet markers are
// separators (not tokens), hyphens inside words are preserved.
func tokenize(s string) []string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-' || r == '_':
			// Keep word-internal hyphens/underscores as part of the token,
			// but treat a leading hyphen (bullet) as a separator. We
			// approximate "leading bullet" as: hyphen at start-of-line or
			// preceded by whitespace.
			if i == 0 {
				b.WriteRune(' ')
				continue
			}
			prev := s[i-1]
			if prev == ' ' || prev == '\n' || prev == '\t' {
				b.WriteRune(' ')
				continue
			}
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	raw := strings.Fields(b.String())
	out := raw[:0]
	for _, t := range raw {
		// Strip stray leading/trailing hyphens left by tokenization.
		t = strings.Trim(t, "-_")
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}
