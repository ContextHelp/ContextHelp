package cmd

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
)

// The enqueue commands (analyze, bare `ctxt <content>`, capture tabs,
// capture history) hand content to the one resolved dpkms instance over
// POST /api/v1/analyze. There is no local queue behind it: when the
// instance cannot be reached the command exits 70, when it rejects the
// token it exits 5, and neither is retried (ADR-077 §2).

// enqueueClient resolves this invocation's endpoint and returns a client
// for it, with the resolution (whose Key scopes client-side state).
func enqueueClient(cmd *cobra.Command) (*dpkmsclient.Client, dpkmsclient.Resolved, error) {
	r, err := resolveEndpoint(cmd)
	if err != nil {
		return nil, r, err
	}
	c, err := dpkmsclient.New(r.Endpoint)
	if err != nil {
		return nil, r, err
	}
	return c, r, nil
}

// urlCaptureRequest is the request `ctxt capture <url>` builds for one
// URL: the server picks the pipeline.
func urlCaptureRequest(u, focusProfile string) dpkmsclient.AnalyzeRequest {
	return dpkmsclient.AnalyzeRequest{Content: u, Source: u, Type: "text", Profile: focusProfile}
}

// endsBatch reports an enqueue failure after which a batch stops: nothing
// answered at the instance, the instance rejected the token, or the run
// was canceled. The next item would fail the same way, and a rejected
// token is never retried.
func endsBatch(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	var ke *output.Error
	return errors.As(err, &ke) &&
		(ke.Code == output.CodePrerequisite || ke.Code == output.CodeUnauthorized)
}

// sendFailure renders one item's enqueue failure for a report: the
// instance's answer when it gave one.
func sendFailure(err error) string {
	var re *dpkmsclient.RemoteError
	if errors.As(err, &re) {
		return re.Error()
	}
	return err.Error()
}
