// Package helpflags checks that a command tree's help names only flags
// the commands declare.
//
// Help text is written by hand and drifts: an example keeps showing
// --json after the command moved to kit's --format json, or a
// destructive command's examples leave out the --confirm-token kit's
// gate demands. Nothing executes help text, so the drift is silent
// until a user copies the example and it fails. Check reads every
// Long, Example, kit/examples and kit/next-steps string in the tree
// and reports each flag that no command it can resolve declares.
package helpflags

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
)

// DestructiveTokenAnnotation is the kit annotation that makes kit's
// confirmation gate require --confirm-token=<sha>. Kit adds the
// --confirm-token flag to such commands when the tree is prepared for
// Execute, so a tree built for a test does not declare it yet.
const DestructiveTokenAnnotation = "kit/destructive-token"

const confirmTokenFlag = "confirm-token"

// spanRe matches a backticked or double-quoted span.
var spanRe = regexp.MustCompile("`([^`]+)`|\"([^\"]+)\"")

// flagRe matches a long flag name: --name or --name=value.
var flagRe = regexp.MustCompile(`(^|[\s\x60"'(\[])--([a-z][a-z0-9-]*[a-z0-9])`)

// Check walks root and returns one line per problem found:
//
//   - an invocation of root's binary names a command that does not
//     exist, or passes a --flag that command does not declare (own,
//     persistent or inherited). Invocations are kit/examples and
//     kit/next-steps entries, Example lines, indented Long lines, and
//     backticked or double-quoted spans in prose, that start with
//     root.Name();
//   - prose in a command's Long or Example names a --flag that no
//     command in the tree declares;
//   - an invocation of a command carrying kit/destructive-token omits
//     --confirm-token without being a --dry-run;
//   - the Long text of such a command never mentions --confirm-token.
//
// Invocations of another program are skipped: their flags belong to a
// tree Check cannot see. Hidden and deprecated commands are skipped
// too.
func Check(root *cobra.Command) []string {
	var out []string
	walk(root, func(c *cobra.Command) {
		// Hidden and deprecated commands are not documented surface.
		if (c.Hidden || c.Deprecated != "") && c != root {
			return
		}
		for _, src := range texts(c) {
			for _, raw := range strings.Split(src.text, "\n") {
				out = append(out, checkLine(root, c.CommandPath(), src, raw)...)
			}
		}
		if requiresToken(c) && c.Runnable() && !strings.Contains(c.Long, "--"+confirmTokenFlag) {
			out = append(out, fmt.Sprintf("%q Long does not mention --%s, which kit's gate requires to run it",
				c.CommandPath(), confirmTokenFlag))
		}
	})
	return out
}

// checkLine checks one line of src, found in the help of the command
// at path.
func checkLine(root *cobra.Command, path string, src text, raw string) []string {
	line := strings.TrimSpace(raw)
	if line == "" {
		return nil
	}
	bin := root.Name()
	var out []string
	// Whole strings, Example lines and indented Long lines are command
	// lines; a Long line flush with the margin is prose even when it
	// opens with the binary name.
	asCommand := src.whole || src.field == "Example" ||
		strings.HasPrefix(line, "$ ") || raw != strings.TrimLeft(raw, " \t")
	if asCommand {
		if invs := invocations(bin, line); len(invs) > 0 {
			for _, inv := range invs {
				out = append(out, checkInvocation(root, path, src.field, inv)...)
			}
			return out
		}
	}
	if src.whole {
		return nil
	}
	prose := line
	for _, span := range quotedSpans(line) {
		words := shellWords(span)
		if len(words) == 0 || strings.HasPrefix(words[0], "-") {
			continue
		}
		if words[0] == bin {
			out = append(out, checkInvocation(root, path, src.field, words[1:])...)
		}
		// Either way its flags are judged there or belong to another
		// program: drop it from the prose scan.
		prose = strings.Replace(prose, span, "", 1)
	}
	for _, name := range flagNames(prose) {
		if !declaredAnywhere(root, name) {
			out = append(out, fmt.Sprintf("%q %s mentions --%s, which no %s command declares",
				path, src.field, name, bin))
		}
	}
	return out
}

type text struct {
	field string
	text  string
	// whole marks a string that is an invocation as a whole (a kit
	// example or next step) rather than prose that may contain one.
	whole bool
}

func texts(c *cobra.Command) []text {
	ts := []text{{field: "Long", text: c.Long}, {field: "Example", text: c.Example}}
	if ex, ok := kitcli.GetExamples(c); ok {
		for _, e := range ex {
			ts = append(ts, text{field: "kit/examples", text: e.Command, whole: true})
		}
	}
	if ns, ok := kitcli.GetNextSteps(c); ok {
		for _, n := range ns {
			ts = append(ts, text{field: "kit/next-steps", text: n.Suggest, whole: true})
		}
	}
	return ts
}

// invocations returns the arguments of each command in line that runs
// binary bin: line is split on "&&", "||", "|" and ";", an optional
// "$ " prompt and a trailing " # comment" are dropped, and a segment
// counts when its first word is bin.
func invocations(bin, line string) [][]string {
	line = strings.TrimPrefix(line, "$ ")
	if i := strings.Index(line, " #"); i >= 0 {
		line = line[:i]
	}
	var out [][]string
	for _, seg := range splitSegments(line) {
		words := shellWords(strings.TrimSpace(seg))
		if len(words) > 0 && words[0] == bin {
			out = append(out, words[1:])
		}
	}
	return out
}

// quotedSpans returns the contents of each backticked or double-quoted
// span in line.
func quotedSpans(line string) []string {
	var spans []string
	for _, m := range spanRe.FindAllStringSubmatch(line, -1) {
		if m[1] != "" {
			spans = append(spans, m[1])
		} else {
			spans = append(spans, m[2])
		}
	}
	return spans
}

func splitSegments(line string) []string {
	r := strings.NewReplacer("&&", "\x00", "||", "\x00", "|", "\x00", ";", "\x00")
	return strings.Split(r.Replace(line), "\x00")
}

// shellWords splits s on blanks, keeping single- or double-quoted
// runs together and dropping the quotes.
func shellWords(s string) []string {
	var words []string
	var cur strings.Builder
	var quote rune
	inWord := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	for i, w := range words {
		words[i] = strings.TrimRight(w, ".,:;)")
	}
	return words
}

func checkInvocation(root *cobra.Command, owner, field string, args []string) []string {
	target, rest, err := root.Find(args)
	if err == nil && target != nil && !target.Runnable() && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		err = fmt.Errorf("%q has no subcommand %q", target.CommandPath(), rest[0])
	}
	if err != nil || target == nil {
		return []string{fmt.Sprintf("%q %s runs %q, which is not a command: %v",
			owner, field, strings.Join(append([]string{root.Name()}, args...), " "), err)}
	}
	var out []string
	var hasToken, dryRun bool
	for _, a := range args {
		if !strings.HasPrefix(a, "--") || a == "--" {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		switch name {
		case confirmTokenFlag:
			hasToken = true
		case "dry-run":
			dryRun = true
		}
		if strings.ContainsAny(name, "<[*") {
			continue // a placeholder such as --<flag>
		}
		if !declared(target, name) {
			out = append(out, fmt.Sprintf("%q %s passes --%s to %q, which does not declare it",
				owner, field, name, target.CommandPath()))
		}
	}
	if requiresToken(target) && !hasToken && !dryRun {
		out = append(out, fmt.Sprintf("%q %s runs %q without --%s; kit's gate refuses it",
			owner, field, target.CommandPath(), confirmTokenFlag))
	}
	return out
}

// declared reports whether c accepts --name: its own and persistent
// flags, its ancestors' persistent flags, cobra's help flag, and the
// --confirm-token kit adds to kit/destructive-token commands at
// Execute.
func declared(c *cobra.Command, name string) bool {
	if name == "help" {
		return true
	}
	if name == confirmTokenFlag && requiresToken(c) {
		return true
	}
	return c.Flag(name) != nil || c.InheritedFlags().Lookup(name) != nil
}

// declaredAnywhere widens declared to the whole tree under root.
// Prose often names another command's flag ("list with --tagged"), and
// which command it means is not machine-readable; a flag no command
// declares at all is wrong either way.
func declaredAnywhere(root *cobra.Command, name string) bool {
	found := false
	walk(root, func(sub *cobra.Command) {
		if !found && declared(sub, name) {
			found = true
		}
	})
	return found
}

func requiresToken(c *cobra.Command) bool {
	return c.Annotations[DestructiveTokenAnnotation] != ""
}

func flagNames(line string) []string {
	matches := flagRe.FindAllStringSubmatchIndex(line, -1)
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		name := line[m[4]:m[5]]
		if end := m[5]; end < len(line) && strings.ContainsRune("-*<", rune(line[end])) {
			continue // a pattern such as --embedding-* or --<name>
		}
		names = append(names, name)
	}
	return names
}

func walk(c *cobra.Command, fn func(*cobra.Command)) {
	fn(c)
	for _, sub := range c.Commands() {
		walk(sub, fn)
	}
}
