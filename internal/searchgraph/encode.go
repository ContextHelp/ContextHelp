package searchgraph

import (
	"encoding/json"
	"fmt"
	"io"
)

// Encode writes the document as JSON followed by a newline. Strings are
// written verbatim: unlike json.Marshal it does not rewrite <, > and & as
// \u escapes, because escaping belongs to whoever renders the text (a
// consumer inlining the JSON into an HTML <script> must escape it there).
// Invalid UTF-8 never reaches the output; encoding/json replaces it with
// U+FFFD. pretty selects two-space indentation.
func (d *Document) Encode(w io.Writer, pretty bool) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(d); err != nil {
		return fmt.Errorf("searchgraph: encode: %w", err)
	}
	return nil
}
