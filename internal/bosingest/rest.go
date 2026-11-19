package bosingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/bosaccess"
)

// ErrUnmapped: no served read command is known for the record's type; the
// record is neither stored nor retracted.
var ErrUnmapped = errors.New("bosingest: no read command for the record type")

// DefaultCommands maps a record type to the served read command that shows
// one record of it (the command path; the record id is its one argument).
// bos serves its command tree over REST as
// POST /v1/commands/<path> {"args": [...], "flags": {...}}.
var DefaultCommands = map[string][]string{
	"core.parties":        {"party", "get"},
	"core.documents":      {"document", "get"},
	"billing.documents":   {"billing", "get"},
	"contracts.contracts": {"contract", "show"},
}

// RESTReader reads records through the authority's served read commands over
// REST, with ctxt's own credential: the authority's access decision applies as
// for any caller. A refusal (403) or a record the credential cannot find (404)
// is ErrNoAccess; nothing else is stored from a refused call.
type RESTReader struct {
	BaseURL string
	// Token is the bearer credential of the service principal ctxt reads as.
	Token string
	HTTP  *http.Client
	// Commands overrides DefaultCommands per record type.
	Commands map[string][]string
}

// Read implements ContentReader. The record's text is its fields as the
// command returns them, one per line, sorted; its metadata names the type,
// its id and its title when the row has one.
func (r *RESTReader) Read(ctx context.Context, address string, revision int64) (Record, error) {
	typ := bosaccess.RecordTypeKey(address)
	path := r.Commands[typ]
	if path == nil {
		path = DefaultCommands[typ]
	}
	if path == nil {
		return Record{}, ErrUnmapped
	}
	id := address[strings.LastIndex(address, "/")+1:]
	body, _ := json.Marshal(map[string]any{"args": []string{id}, "flags": map[string]any{}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.BaseURL+"/v1/commands/"+strings.Join(path, "/"), bytes.NewReader(body))
	if err != nil {
		return Record{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if r.Token != "" {
		req.Header.Set("Authorization", "Bearer "+r.Token)
	}
	hc := r.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Record{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return Record{}, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden, http.StatusNotFound:
		return Record{}, ErrNoAccess
	default:
		return Record{}, fmt.Errorf("bosingest: %s answered %d", strings.Join(path, " "), resp.StatusCode)
	}
	var env struct {
		ExitCode int                      `json:"exit_code"`
		Data     []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.ExitCode != 0 {
		return Record{}, fmt.Errorf("bosingest: %s answered an unreadable body", strings.Join(path, " "))
	}
	if len(env.Data) == 0 {
		return Record{}, ErrNoAccess
	}
	row := env.Data[0]
	var lines []string
	flatten("", row, &lines)
	sort.Strings(lines)
	meta := map[string]any{"bos_record_id": id}
	if revision > 0 {
		meta["bos_revision"] = revision
	}
	for _, k := range []string{"title", "name", "display_name"} {
		if v, ok := row[k].(string); ok && v != "" {
			meta["title"] = v
			break
		}
	}
	return Record{Content: strings.Join(lines, "\n"), Metadata: meta}, nil
}

// flatten renders scalars of v as "path: value" lines; arrays index their
// elements. Empty values are left out.
func flatten(prefix string, v any, out *[]string) {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, e := range x {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			flatten(p, e, out)
		}
	case []interface{}:
		for i, e := range x {
			flatten(fmt.Sprintf("%s[%d]", prefix, i), e, out)
		}
	case nil:
	case string:
		if x != "" {
			*out = append(*out, prefix+": "+x)
		}
	default:
		*out = append(*out, fmt.Sprintf("%s: %v", prefix, x))
	}
}
