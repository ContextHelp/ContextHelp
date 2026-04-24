package himalaya

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// Adapter implements ingest.Adapter for himalaya email.
type Adapter struct {
	Binary      string // path to himalaya binary
	Account     string // himalaya account name
	Folder      string // IMAP folder (default INBOX)
	MaxItems    int    // max envelopes to fetch
}

func (a *Adapter) Name() string { return "himalaya" }

func (a *Adapter) Fetch(
	ctx context.Context,
) ([]ingest.Object, error) {
	bin := a.Binary
	if bin == "" {
		bin = "himalaya"
	}
	folder := a.Folder
	if folder == "" {
		folder = "INBOX"
	}

	// List envelopes
	args := []string{"envelope", "list", "-f", folder, "-o", "json"}
	if a.Account != "" {
		args = append(args, "-a", a.Account)
	}

	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("himalaya list: %w", err)
	}

	envelopes, err := ParseEnvelopes(out)
	if err != nil {
		return nil, fmt.Errorf("parse envelopes: %w", err)
	}

	// Limit
	if a.MaxItems > 0 && len(envelopes) > a.MaxItems {
		envelopes = envelopes[:a.MaxItems]
	}

	// Read each message body
	var msgs []Message
	for _, env := range envelopes {
		readArgs := []string{"message", "read", env.ID, "-o", "json"}
		if a.Account != "" {
			readArgs = append(readArgs, "-a", a.Account)
		}

		body, err := exec.CommandContext(
			ctx, bin, readArgs...,
		).Output()
		if err != nil {
			continue // skip unreadable messages
		}

		var bodyStr string
		if err := json.Unmarshal(body, &bodyStr); err != nil {
			bodyStr = string(body)
		}

		msg := BuildMessage(env, bodyStr)
		msgs = append(msgs, msg)
	}

	// Group threads + flatten back to messages with ThreadID set
	threads := GroupThreads(msgs)
	var threaded []Message
	for _, t := range threads {
		for _, m := range t.Messages {
			m.ThreadID = t.ID
			threaded = append(threaded, m)
		}
	}

	// Transform to ingest.Object
	source := a.Account
	if source == "" {
		source = "default"
	}
	ctxtObjs := ToCtxtObjects(threaded, source)

	// Convert CtxtObject → ingest.Object
	objects := make([]ingest.Object, len(ctxtObjs))
	for i, co := range ctxtObjs {
		meta := make(map[string]any, len(co.Metadata))
		for k, v := range co.Metadata {
			meta[k] = v
		}
		objects[i] = ingest.Object{
			ID:       co.ID,
			Type:     co.Type,
			Content:  co.Content,
			Tags:     co.Tags,
			Metadata: meta,
		}
	}

	return objects, nil
}
