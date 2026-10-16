package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
)

// A sharing change by someone other than the owner that could widen
// what anyone can see is not applied: the instance files an approval
// request for it and answers 202 (ADR-083 §10 B). The commands that
// make such changes send them through sendChange, which prints the
// pending request in place of the result.

// pendingHelp is the paragraph the help of those commands adds.
const pendingHelp = `If you are not the owner and the change could widen what someone can see, it is not applied. The command files an approval request, prints its id and exits 0. An approver (the owner, or anyone holding approve:shares) decides it with 'ctxt approval approve'. A change that narrows access, or alters no one's view, still applies at once.`

// pendingNotice is the explanation printed under a pending request.
const pendingNotice = `This change could widen what someone can see, so it waits for an approver. The approver has to be someone other than you, and not anyone the change benefits.`

// sendChange sends req, which may be a sharing change that waits for an
// approver, and decodes a result into out. When the instance answers
// 202 the change was not made: the pending request is printed and
// pending is true, and the caller prints nothing more.
func sendChange(cmd *cobra.Command, client *dpkmsclient.Client, req dpkmsclient.Request, out any) (pending bool, err error) {
	var a approvalReply
	accepted, err := client.DoAccepted(cmdContext(cmd), req, out, &a)
	if err != nil || !accepted {
		return false, err
	}
	return true, printPending(cmd, a)
}

// printPending prints the request a change was filed as: its id on
// stdout, and on stderr what it is and how to follow it.
func printPending(cmd *cobra.Command, a approvalReply) error {
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{"pending": true, "request": a})
	}
	fmt.Fprintln(cmd.OutOrStdout(), a.ID)
	w := cmd.ErrOrStderr()
	fmt.Fprintf(w, "Waiting for approval: %s (%s), expires %s.\n", a.ID, a.Action, fmtStamp(a.ExpiresAt))
	fmt.Fprintln(w, pendingNotice)
	fmt.Fprintf(w, "See it with: ctxt approval show %s\n", a.ID)
	return nil
}
