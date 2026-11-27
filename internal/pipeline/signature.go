package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// baseName strips a trailing "@vN" version suffix from a pipeline name,
// so the bare and "@v0" forms of one pipeline share a signature.
func baseName(name string) string {
	i := strings.LastIndex(name, "@v")
	if i < 0 || i+2 >= len(name) {
		return name
	}
	for _, c := range name[i+2:] {
		if c < '0' || c > '9' {
			return name
		}
	}
	return name[:i]
}

// sign hashes entries quoted one per line: a quoted entry holds no raw
// newline, so no entry (a model string, say) can pass for two.
func sign(name string, entries []string) string {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(strconv.Quote(e))
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return baseName(name) + "#" + hex.EncodeToString(sum[:])[:16]
}

// Signature identifies which pipeline and which step versions derive an
// object: "<name>#<16 hex>", the hash covering the ordered list of
// stepName@version. Compute it over the configured steps, before
// capability pruning, so a provider that is down at boot does not change it.
func Signature(name string, steps []PipelineStep) string {
	refs := make([]StepRef, len(steps))
	for i, s := range steps {
		refs[i] = StepRef{Name: s.Name(), Version: StepVersion(s)}
	}
	return SignatureOf(name, refs)
}

// StepRef names a step and its derivation version, as a pipeline
// definition lists it.
type StepRef struct {
	Name    string
	Version int
}

// SignatureOf is Signature over step names and versions, plus models: one
// "resource=backend/model" entry per generative resource (llm, vision) the
// pipeline's steps declare, as configured (never as resolved), so
// switching a model changes the signature. The registry
// builders sign the names the definition lists, so a pipeline that cannot
// be built (BaselineSignatureOf) and one that can share one naming.
func SignatureOf(name string, steps []StepRef, models ...string) string {
	entries := make([]string, 0, len(steps)+len(models))
	for _, s := range steps {
		v := s.Version
		if v <= 0 {
			v = 1
		}
		entries = append(entries, fmt.Sprintf("%s@%d", s.Name, v))
	}
	return sign(name, append(entries, models...))
}

// BaselineSignature is Signature with every step at version 1: the
// signature objects derived before signatures were recorded are taken to
// carry.
func BaselineSignature(name string, steps []PipelineStep) string {
	names := make([]string, len(steps))
	for i, s := range steps {
		names[i] = s.Name()
	}
	return BaselineSignatureOf(name, names)
}

// BaselineSignatureOf is BaselineSignature over step names alone, for a
// pipeline whose steps cannot be built (a provider is missing).
func BaselineSignatureOf(name string, stepNames []string, models ...string) string {
	entries := make([]string, 0, len(stepNames)+len(models))
	for _, n := range stepNames {
		entries = append(entries, n+"@1")
	}
	return sign(name, append(entries, models...))
}

// CurrentSignatures lists, sorted and without duplicates, the current
// signature of every pipeline definition r knows: a registered pipeline's
// own, and for one left out for a missing provider its baseline, so that
// provider being down does not make the pipeline's rows look stale. It is
// the set to pass to DerivationStore.ListStaleDerivations.
func CurrentSignatures(r Registry) []string {
	baselines := Baselines(r)
	names := r.List()
	for name := range baselines {
		names = append(names, name)
	}
	set := map[string]bool{}
	for _, name := range names {
		if p, err := r.Get(name); err == nil && p.Signature != "" {
			set[p.Signature] = true
		} else if b := baselines[name]; b != "" {
			set[b] = true
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
