package cliformat

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hop.top/kit/go/console/output"
)

// Rows reports whether the active format is one kit can project from a
// `table:""`-tagged row slice: the tabular formats, as opposed to the
// json/yaml documents Encode owns and the bespoke human view each
// command renders itself.
//
// Commands branch three ways:
//
//	Structured() -> Encode          (json, yaml: the full record)
//	Rows()       -> DispatchRows    (table, csv, text: the row view)
//	otherwise    -> the command's own human renderer
//
// human is deliberately NOT in this set. kit's human formatter falls
// back to its table renderer, which would throw away the prose these
// commands have always printed — counts, default markers, config-path
// footers. Honoring csv and text is a bug fix; silently rewriting what
// an operator sees is a regression, so human keeps its existing
// renderer and only the machine-facing formats change.
//
// For the same reason Rows is false when --format was not given at all.
// kit reports an unset flag as "table", so reading Active() alone would
// route every default invocation — every bare `ctxt list` — through the
// projection and drop the prose with it. Only a table the caller asked
// for by name dispatches; the default stays exactly as it was.
//
// "Not given" is read off the pflag's Changed state, not off viper.
// Both roots BindPFlag --format, which makes viper report the flag's
// DEFAULT ("table") for a run that never passed it — indistinguishable
// from an explicit --format table. Changed is the only signal that
// separates the two.
func Rows() bool {
	if !formatExplicit() {
		return false
	}
	return IsRows(Active())
}

// formatExplicit reports whether this run actually asked for a format,
// on the command line or through config/env.
//
// The pflag's Changed state covers the command line. Viper covers a
// config file or environment variable, but only when it holds a value
// that did not come from the bound flag's default — hence the compare
// against DefValue rather than against "".
func formatExplicit() bool {
	if changedFormatFlag() != "" {
		return true
	}
	v := strings.TrimSpace(viper.GetString("format"))
	if v == "" {
		return false
	}
	if activeCmd != nil {
		if pf := activeCmd.Root().PersistentFlags().Lookup("format"); pf != nil {
			return v != pf.DefValue
		}
	}
	return v != Table
}

// IsRows reports whether format names one of the row-projection
// formats.
func IsRows(format string) bool {
	switch format {
	case output.Table, output.CSV, output.Text:
		return true
	default:
		return false
	}
}

// DispatchRows renders rows — a slice of structs carrying `table:""`
// tags — through kit's formatter registry, so table, csv and text all
// project from that one declaration rather than from three hand-rolled
// writers that drift apart.
//
// rows must be a slice (typed, possibly empty), not a pointer or a bare
// struct: these are list commands, and the empty list is a result they
// have to be able to express.
//
// An empty slice is the reason this wrapper exists rather than a bare
// output.Dispatch call. kit's csv, text and table formatters all return
// early on a zero-length slice and write nothing at all, which leaves a
// caller unable to tell an empty result from a failed one. For csv that
// is worse than unhelpful: a header row is what makes the output
// parseable, and every CSV reader expects one. So the empty case emits
// the header line recovered from the row type's tags for csv, and
// nothing for table and text, where a bare header would read as a
// phantom record.
func DispatchRows(cmd *cobra.Command, w io.Writer, rows any) error {
	rv := reflect.ValueOf(rows)
	if rv.Kind() != reflect.Slice {
		return fmt.Errorf("cliformat: DispatchRows needs a slice of row structs, got %T", rows)
	}

	if rv.Len() == 0 {
		return renderEmptyRows(cmd, w, reflect.TypeOf(rows))
	}

	// kit resolves its own writer from cmd.OutOrStdout(), or from the
	// file named by -o. To honor the caller's w we hand Dispatch a
	// throwaway child of cmd rather than re-pointing cmd itself.
	//
	// Mutating cmd and restoring it in a defer looks equivalent and is
	// not: cmd is long-lived, and the in-process test harnesses point
	// the root at an os.Pipe they close after each case. A deferred
	// SetOut puts that closed pipe back, so every later command in the
	// process writes into a dead fd — which is how this first showed
	// up: unrelated tests started seeing empty output.
	//
	// Instead the proxy is parented to cmd for the duration of the
	// call and detached again. Dispatch walks Parent() to find the
	// --format/--cols/-o values and the formatter registry, so the
	// link has to be real while it runs; RemoveCommand then severs it
	// and drops the proxy from cmd's child list, leaving the command
	// tree, its help and its completions exactly as they were.
	proxy := &cobra.Command{Use: cmd.Name()}
	// SetOut is unconditional: Dispatch resolves its own writer before
	// consulting the command, and a -o path wins over OutOrStdout()
	// there, so this only supplies the destination for the no-path
	// case. Guarding it on -o was measured to change nothing.
	proxy.SetOut(w)
	cmd.AddCommand(proxy)
	defer cmd.RemoveCommand(proxy)

	return output.Dispatch(proxy, viper.GetViper(), rows)
}

// outputPathSet reports whether the caller asked for the result to be
// written to a file with -o/--output.
//
// The lookup order mirrors kit's own: a flag the user actually typed
// wins, and viper is the fallback so a config file or environment
// binding reaches the same decision. Disagreeing with kit here would
// be worse than not checking at all — this function only decides who
// owns the writer, so a false negative sends output to stdout while
// Dispatch is opening the file, and a false positive drops the
// caller's writer on the floor.
//
// "-" means stdout by kit's convention, so it is not a path.
func outputPathSet(cmd *cobra.Command) bool {
	if cmd != nil {
		// Flags() spans the persistent flags of every ancestor, which is
		// where -o is registered (kit puts it on the root).
		if f := cmd.Flags().Lookup("output"); f != nil && f.Changed {
			return f.Value.String() != "" && f.Value.String() != "-"
		}
	}
	p := viper.GetViper().GetString("output")
	return p != "" && p != "-"
}

// renderEmptyRows writes the zero-row rendering for the active format.
//
// Only csv gets a body: its header row is part of the format's contract
// with a parser, so emitting it keeps `--format csv` machine-readable
// whether or not anything matched. table and text emit nothing, which
// matches how kit renders them and keeps a header from being misread as
// a record with empty fields.
//
// The header is written here rather than delegated because kit's csv
// formatter returns before writing anything when the slice is empty —
// it reads its columns off the first element, which a zero-length slice
// does not have. output.TableHeaders reads the same `table:""` tags off
// the element TYPE instead, so the header emitted for no rows is the
// one the formatter would have emitted for one.
func renderEmptyRows(cmd *cobra.Command, w io.Writer, rowsType reflect.Type) error {
	if Active() != output.CSV {
		return nil
	}
	headers := output.TableHeaders(rowsType)
	if len(headers) == 0 {
		return nil
	}
	// Honor --cols so the empty rendering carries the same columns,
	// in the same order, as a populated one. An unknown name is the
	// same caller mistake here as it is with rows present, so it is
	// reported rather than ignored.
	if selected := selectedCols(cmd); len(selected) > 0 {
		projected, err := projectHeaders(headers, selected)
		if err != nil {
			return err
		}
		headers = projected
	}
	// The header is the whole document when nothing matched, so it has
	// to land where a populated result would have landed. Dispatch is
	// what normally honors -o, and this path returns before reaching
	// it: writing to w unconditionally would print the header to
	// stdout and leave the requested file uncreated, which is the
	// empty-result half of the same defect.
	out, closer, err := resolveRowsWriter(cmd, w)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, strings.Join(headers, ","))
	// A close failure on a write path can mean the bytes never landed,
	// so it is reported rather than discarded — but the write error, if
	// there is one, names the problem more precisely and wins.
	if closer != nil {
		if cerr := closer(); err == nil {
			err = cerr
		}
	}
	return err
}

// resolveRowsWriter returns the writer a row rendering should go to,
// plus a closer when it opened a file.
//
// Mirrors kit's own resolveWriter: the file named by -o when one was
// asked for, otherwise the caller's writer.
//
// The mode is 0600 rather than the 0644 kit uses. These documents are
// whatever the command was asked to emit — knowledge objects, secret
// listings, job payloads — and the operator named a path, not a
// permission. Group- and world-readable is the wrong default for
// content this package cannot inspect, so the narrower mode wins over
// matching kit byte for byte.
func resolveRowsWriter(cmd *cobra.Command, w io.Writer) (io.Writer, func() error, error) {
	if !outputPathSet(cmd) {
		return w, nil, nil
	}
	path := outputPath(cmd)
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
		return nil, nil, fmt.Errorf("output path %q is a directory", path)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open output %q: %w", path, err)
	}
	return f, f.Close, nil
}

// outputPath returns the -o value, preferring the typed flag over
// viper for the same reason outputPathSet does.
func outputPath(cmd *cobra.Command) string {
	if cmd != nil {
		if f := cmd.Flags().Lookup("output"); f != nil && f.Changed {
			return f.Value.String()
		}
	}
	return viper.GetViper().GetString("output")
}

// selectedCols returns the --cols/--columns selection for this run,
// comma-splitting each value the way kit does.
func selectedCols(cmd *cobra.Command) []string {
	if cmd == nil {
		return nil
	}
	var raw []string
	for _, name := range []string{"cols", "columns"} {
		if f := cmd.Root().PersistentFlags().Lookup(name); f != nil && f.Changed {
			if sv, ok := f.Value.(interface{ GetSlice() []string }); ok {
				raw = append(raw, sv.GetSlice()...)
			}
		}
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			if _, dup := seen[p]; dup {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}

// projectHeaders restricts headers to selected, in SELECTED order —
// matching kit's filterColumns, where the user's order wins over the
// struct's. An unrecognized name yields the same diagnostic kit emits,
// naming the columns that do exist.
func projectHeaders(headers, selected []string) ([]string, error) {
	have := make(map[string]struct{}, len(headers))
	for _, h := range headers {
		have[h] = struct{}{}
	}
	out := make([]string, 0, len(selected))
	for _, s := range selected {
		if _, ok := have[s]; !ok {
			return nil, fmt.Errorf("unknown column %q (valid: %s)",
				s, strings.Join(headers, ", "))
		}
		out = append(out, s)
	}
	return out, nil
}

// EncodeTo writes v in the active machine format to the destination
// the caller asked for: the file named by -o when one was given,
// otherwise w.
//
// Encode deliberately keeps its narrower contract of writing to
// exactly the writer it is handed — it is what the encoder tests
// assert against, and a function that silently redirected its output
// would make those tests lie. So the -o decision lives here, in the
// wrapper the commands call, where it is visible at the callsite that
// a destination is being resolved rather than assumed.
// cmd may be nil: kit binds --output into viper, which outputPathSet
// reads when it has no command to consult, so the many callsites that
// hold only a writer still resolve -o. Measured, not assumed — the
// fallback that threaded Bind's command through here instead turned
// out to be dead code.
func EncodeTo(cmd *cobra.Command, w io.Writer, v any) error {
	out, closer, err := resolveRowsWriter(cmd, w)
	if err != nil {
		return err
	}
	err = Encode(out, v)
	if closer != nil {
		if cerr := closer(); err == nil {
			err = cerr
		}
	}
	return err
}
