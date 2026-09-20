package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"hop.top/kit/go/console/output"
)

// helpWord is cobra's built-in help command, and the long form of
// its help flag minus the dashes. Named once because both the
// reserved-dispatch check and the help-flag test key on it.
const helpWord = "help"

// checkUnknownSubcommand refuses a leading word that names no child
// of a non-runnable command, returning an [output.UsageError] so the
// taxonomy gives exit 2.
//
// Cobra hands a non-runnable command straight to its help renderer:
// there is no RunE, so no Args validator and none of kit's RunE
// middleware ever runs, and the invocation exits 0 as if it had
// succeeded. `ctxt profile bogus` printed the profile group's help
// and reported SUCCESS, so an automated caller recorded a typo as a
// completed step. A runnable command needs no help here — cobra
// validates its Args and kit's middleware classifies the result — so
// this check is aimed only at that gap.
//
// kit's Root.Execute performs the same check, but ctxt drives fang
// directly (see Execute) and kit's implementation is unexported, so
// the gap is closed here. Should kit export it, this file collapses
// to a call.
//
// Reports nil for everything that is not the gap: a resolved
// runnable command, a bare non-runnable command (help, exit 0, the
// documented behavior), a completion request or a leading `help`,
// and a malformed flag, which is the first thing wrong with the
// command line and is diagnosed as the flag error it is.
func checkUnknownSubcommand(root *cobra.Command, args []string) error {
	if root == nil {
		return nil
	}

	// Completion drives itself through hidden commands that are not
	// children of the root, and `help` is cobra's own command taking
	// a command path as its operand.
	if len(args) > 0 && isReservedDispatchWord(args[0]) {
		return nil
	}

	// A Find failure is cobra's own resolution complaining, and cobra
	// will report it on the path it is about to take. This check only
	// adds a refusal cobra would not raise at all, so it stays quiet
	// about everything cobra already handles.
	target, rest, findErr := root.Find(args)
	if findErr != nil || target == nil || target.Runnable() {
		return nil //nolint:nilerr // a resolution cobra itself reports is not this check to raise
	}

	// Find leaves flags in rest, so strip them the way cobra does
	// before parsing. A help flag (pflag.ErrHelp) means this is a
	// help request; any other parse failure means a malformed flag is
	// the first thing wrong, so let the flag machinery name it. Both,
	// and an empty word list, leave the invocation to cobra.
	words, parseErr := strippedWords(target, rest)
	if errors.Is(parseErr, pflag.ErrHelp) || parseErr != nil || len(words) == 0 {
		return nil //nolint:nilerr // a malformed flag is diagnosed by the flag machinery, not here
	}

	// A help flag standing ahead of the unknown word is a help
	// request too, on cobra's positional terms: cobra tests the flag
	// inside the resolved command's execute, so `ctxt --help bogus`
	// prints help while `ctxt profile bogus --help` is refused.
	if helpFlagPrecedes(target, rest, words[0]) {
		return nil
	}

	return output.UsageError(fmt.Sprintf("unknown command %q for %q%s",
		words[0], target.CommandPath(), suggestionsFor(target, words[0])))
}

// refuseUnknownSubcommand silences cobra's own printer so the
// refusal is not doubled, and returns the envelope so Execute's
// caller reads the taxonomy code off it.
func refuseUnknownSubcommand(root *cobra.Command, err error) error {
	root.SilenceErrors = true
	root.SilenceUsage = true
	return err
}

// isReservedDispatchWord reports whether w is a leading word cobra
// routes itself.
func isReservedDispatchWord(w string) bool {
	switch w {
	case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd, helpWord:
		return true
	}
	return false
}

// helpFlagPrecedes reports whether a help flag appears in args before
// word — the position at which cobra would have reached its own help
// check and never looked at the word. Stops at "--", after which
// nothing is a flag.
func helpFlagPrecedes(cmd *cobra.Command, args []string, word string) bool {
	for _, a := range args {
		if a == "--" || a == word {
			return false
		}
		if isHelpFlagToken(cmd, a) {
			return true
		}
	}
	return false
}

// isHelpFlagToken reports whether tok is a help flag: the long or
// short help flag, or any --help-<id> variant kit registers on the
// root. The value half of a --flag=value token is ignored.
func isHelpFlagToken(cmd *cobra.Command, tok string) bool {
	if tok == "-h" || tok == "--"+helpWord {
		return true
	}
	name, _, _ := strings.Cut(tok, "=")
	switch {
	case strings.HasPrefix(name, "--"):
		name = strings.TrimPrefix(name, "--")
	case strings.HasPrefix(name, "-"):
		return strings.Contains(strings.TrimPrefix(name, "-"), "h")
	default:
		return false
	}
	if name == helpWord {
		return true
	}
	return strings.HasPrefix(name, helpWord+"-") && cmd.Root().Flags().Lookup(name) != nil
}

// strippedWords returns the non-flag words of args as cobra's own
// resolution sees them, reporting a parse error when a flag is
// malformed — including pflag.ErrHelp for a help flag. Parsing runs
// against a throwaway copy of the command's flag set so the real
// flags keep their values for the parse cobra is about to do.
func strippedWords(cmd *cobra.Command, args []string) ([]string, error) {
	probe := pflag.NewFlagSet(cmd.Name(), pflag.ContinueOnError)
	probe.ParseErrorsAllowlist.UnknownFlags = true
	probe.SetOutput(discardWriter{})
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		probe.AddFlag(&pflag.Flag{
			Name:        f.Name,
			Shorthand:   f.Shorthand,
			Usage:       f.Usage,
			Value:       noopValue{typ: f.Value.Type(), def: f.DefValue},
			DefValue:    f.DefValue,
			NoOptDefVal: f.NoOptDefVal,
		})
	})
	if err := probe.Parse(args); err != nil {
		return nil, err
	}
	return probe.Args(), nil
}

// suggestionsFor returns cobra's own "did you mean" block for word,
// so the refusal reads exactly the way cobra's does at the root. The
// candidate set and the distance rule are cobra's.
func suggestionsFor(cmd *cobra.Command, word string) string {
	if cmd.DisableSuggestions {
		return ""
	}
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2
	}
	names := cmd.SuggestionsFor(word)
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nDid you mean this?\n")
	for _, n := range names {
		fmt.Fprintf(&b, "\t%v\n", n)
	}
	return b.String()
}

// noopValue stands in for a real flag value while the probe parse
// walks the command line: it accepts anything and stores nothing.
type noopValue struct {
	typ string
	def string
}

// String returns the flag's declared default, which is all the probe
// parse needs to know about the value.
func (v noopValue) String() string { return v.def }

// Set discards the value: the probe is locating words, not reading
// flags, and the real flag set is parsed separately by cobra.
func (v noopValue) Set(string) error { return nil }

// Type reports the original flag's type so pflag decides correctly
// whether the flag consumes the next argument.
func (v noopValue) Type() string { return v.typ }

// discardWriter swallows pflag's own error rendering; the caller
// reports the parse failure by returning it.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
