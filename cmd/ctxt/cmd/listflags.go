package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/mentions"
)

const (
	hintFlagUsage    = "tag hints for the captured object; repeatable, comma- or space-separated, leading # optional (e.g. --hint ux,research or --hint \"#ui #inspiration\")"
	mentionFlagUsage = "mentions to attach as @namespace.slug; repeatable, comma- or space-separated (e.g. --mention @client.acme,@project.foo)"
)

// splitListTokens splits every value on commas and whitespace, dropping
// empties. Repeats, commas and spaces mix freely.
func splitListTokens(vals []string) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		out = append(out, strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		})...)
	}
	return out
}

// parseHintList normalises --hint values: split on commas and
// whitespace, strip one leading '#', drop empties.
func parseHintList(vals []string) []string {
	var out []string
	for _, t := range splitListTokens(vals) {
		t = strings.TrimSpace(strings.TrimLeft(t, "#"))
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// parseMentionList normalises --mention values and refuses any token
// that is not a valid @namespace.slug (or ctxt:// URI): nothing
// malformed is ever sent.
func parseMentionList(vals []string) ([]string, error) {
	toks := splitListTokens(vals)
	for _, t := range toks {
		if mentions.IsToken(t) {
			continue
		}
		if strings.HasPrefix(t, "ctxt://") {
			if _, ok := mentions.Parse(t); ok {
				continue
			}
		}
		e := output.UsageError("invalid --mention " + quoteToken(t) + ": want @namespace.slug")
		e.SuggestedFix = "separate mentions with commas or spaces, e.g. --mention @client.acme,@project.foo"
		return nil, e
	}
	return toks, nil
}

func quoteToken(t string) string { return "\"" + t + "\"" }

// listFlag reads a list flag set with StringArray. A flag that was not
// passed falls back to viperKey (config file) when non-empty.
func listFlag(cmd *cobra.Command, name, viperKey string) []string {
	if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
		v, _ := cmd.Flags().GetStringArray(name)
		return v
	}
	if viperKey != "" {
		return viper.GetStringSlice(viperKey)
	}
	return nil
}
