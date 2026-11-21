package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/tagfilter"
)

const (
	taggedFlagUsage = `select by tag: comma list and/or repeated flag; plain tags are optional, one must match; ` +
		`a leading + requires a tag (+a,+b all; +a,b,+c needs a and c, b only ranks); ` +
		`results matching more of the listed tags rank first; a tag may hold spaces (quote it), tags are case-sensitive`
	sinceFlagUsage = "created at or after: " + dateFlagForms
	untilFlagUsage = "created before (a date takes its whole day): " + dateFlagForms
	// mentionedFlagUsage is the usage of --mentioned-since / --mentioned-until.
	mentionedSinceUsage = "dates mentioned in content on or after: " + dateFlagForms
	mentionedUntilUsage = "dates mentioned in content on or before: " + dateFlagForms
)

// addTaggedFlag declares --tagged on cmd.
func addTaggedFlag(cmd *cobra.Command) {
	cmd.Flags().StringArray("tagged", nil, taggedFlagUsage)
}

// parseTaggedFlag reads --tagged into a tag filter. A malformed list is a
// usage error.
func parseTaggedFlag(cmd *cobra.Command) (tagfilter.Filter, error) {
	f, err := tagfilter.Parse(taggedValues(cmd))
	if err != nil {
		e := output.UsageError("--tagged: " + err.Error())
		e.SuggestedFix = `list tags with commas or repeat the flag; "+a,+b" requires both, a plain tag is optional, "+" needs a label, "-" is reserved: --tagged +a,b`
		return tagfilter.Filter{}, e
	}
	return f, nil
}

// refuseTagRanking is a usage error when f orders results by tags matched
// (two or more tags with none required, or a plain tag beside a required
// one) and flag needs a fixed
// order, named by order (creation, change).
func refuseTagRanking(flag, order string, f tagfilter.Filter) error {
	if !f.Ranks() {
		return nil
	}
	e := output.UsageError(flag + " cannot be combined with this --tagged list: results rank by tags matched, " + flag + " needs " + order + " order")
	e.SuggestedFix = "use one tag, or only + tags (+a,+b), or list without " + flag
	return e
}

// taggedValues is every --tagged value as given. The flag is read raw:
// GetStringArray round-trips through CSV and loses an empty value, which
// must stay visible so it is refused. With no flag on the command line the
// value comes from viper (config file or environment) under
// "<command>.tagged", the key the flag is bound to; a config string is one
// value, never split on whitespace, since a label may hold spaces.
func taggedValues(cmd *cobra.Command) []string {
	if fl := cmd.Flags().Lookup("tagged"); fl != nil && fl.Changed {
		if sv, ok := fl.Value.(pflag.SliceValue); ok {
			return sv.GetSlice()
		}
		return nil
	}
	var out []string
	switch v := viper.Get(cmd.Name() + ".tagged").(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	case []string:
		out = append(out, v...)
	case []any:
		for _, e := range v {
			out = append(out, fmt.Sprint(e))
		}
	}
	return out
}

// dateRange resolves a --since/--until pair, as RFC 3339 text for the
// API; either may be empty. A malformed value is a usage error.
func dateRange(since, until string) (string, string, error) {
	var from, to string
	var err error
	if since != "" {
		if from, err = resolveInstant("since", since, false); err != nil {
			return "", "", err
		}
	}
	if until != "" {
		if to, err = resolveInstant("until", until, true); err != nil {
			return "", "", err
		}
	}
	return from, to, nil
}

// mentionedRange resolves a --mentioned-since/--mentioned-until pair to
// the YYYY-MM-DD days the API compares with dates_mentioned.
func mentionedRange(since, until string) (string, string, error) {
	var from, to string
	var err error
	if since != "" {
		if from, err = resolveDay("mentioned-since", since, dateNow()); err != nil {
			return "", "", err
		}
	}
	if until != "" {
		if to, err = resolveDay("mentioned-until", until, dateNow()); err != nil {
			return "", "", err
		}
	}
	if from != "" && to != "" && strings.Compare(from, to) > 0 {
		return "", "", output.UsageError("--mentioned-since " + from + " is after --mentioned-until " + to)
	}
	return from, to, nil
}
