package cmd

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// jobStates are the values --state accepts: every state a job can be in.
var jobStates = []string{
	string(storage.JobPending), string(storage.JobRunning), string(storage.JobCompleted),
	string(storage.JobFailed), string(storage.JobCancelled),
}

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Follow and manage the instance's jobs",
	Long: `List, inspect, retry and cancel the jobs queued on the dpkms instance:
analyze, capture, inbox triage, feed syncs, reprocess and migrations all
run there as jobs.

Every subcommand calls the dpkms API of the resolved instance (--server,
--instance, CTXT_INSTANCE, the current instance, then config), so it
works the same against a remote instance. Listing and status need
read:jobs; retry needs write:jobs (writer or admin); cancel needs
cancel:jobs, which only the admin role holds.

Examples:
  # List the most recent jobs
  ctxt job list

  # Only the failed ones
  ctxt job list --state failed

  # Follow one job
  ctxt job status <job-id>

  # Queue a failed job again
  ctxt job retry <job-id>

  # Stop a pending or running job
  ctxt job cancel <job-id> --confirm-token=<token>`,
}

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List jobs, newest first",
	Long: `List the instance's jobs, newest first, with their state.

Examples:
  ctxt job list
  ctxt job list --state failed --limit 20
  ctxt job list --format json`,
	Args: cobra.NoArgs,
	RunE: runJobList,
}

var jobStatusCmd = &cobra.Command{
	Use:     "status <job-id>",
	Aliases: []string{"show"},
	Short:   "Show one job's state, timing, retries and error",
	Long: `Show one job: its type, state, pipeline, timestamps, retries, the
error of a failed job and the object a completed job produced.

Examples:
  ctxt job status job_12345678
  ctxt job status job_12345678 --format json`,
	Args: cobra.ExactArgs(1),
	RunE: runJobStatus,
}

var jobRetryCmd = &cobra.Command{
	Use:   "retry <job-id>",
	Short: "Queue a failed job again",
	Long: `Put a job back in the queue as pending. Each retry counts against the
job's retry budget; a job that used it up is refused.

Examples:
  ctxt job retry job_12345678`,
	Args: cobra.ExactArgs(1),
	RunE: runJobRetry,
}

var jobCancelCmd = &cobra.Command{
	Use:   "cancel <job-id>",
	Short: "Cancel a pending or running job",
	Long: `Cancel a job that is pending or running. A running job is told to
stop; what it already wrote stays. A job that already
finished is refused (exit 4).

` + cliconv.DestructiveTokenHelp + `

Examples:
  ctxt job cancel job_12345678 --confirm-token=<token>`,
	Args: cobra.ExactArgs(1),
	RunE: runJobCancel,
}

func init() {
	rootCmd.AddCommand(jobCmd)
	jobCmd.AddCommand(jobListCmd, jobStatusCmd, jobRetryCmd, jobCancelCmd)

	jobListCmd.Flags().String("state", "", "keep jobs in one state ("+strings.Join(jobStates, "|")+")")
	jobListCmd.Flags().Int("limit", 50, "maximum jobs to list")
	jobListCmd.Flags().Int("offset", 0, "jobs to skip")

	cliconv.WithSideEffect(jobListCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(jobListCmd, []cliconv.Example{
		{Title: "List recent jobs", Command: "ctxt job list"},
		{Title: "List failed jobs", Command: "ctxt job list --state failed --limit 20"},
	})
	cliconv.WithNextSteps(jobListCmd, []cliconv.NextStep{
		{When: "to follow one job", Suggest: "ctxt job status <job-id>", Reason: "see its timing, retries and error"},
	})

	cliconv.WithSideEffect(jobStatusCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(jobStatusCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(jobStatusCmd, []cliconv.Example{
		{Title: "Show one job", Command: "ctxt job status job_12345678"},
	})
	cliconv.WithNextSteps(jobStatusCmd, []cliconv.NextStep{
		{When: "if the job failed", Suggest: "ctxt job retry <job-id>", Reason: "queue it again once the cause is fixed"},
	})

	// retry re-queues the job: a status transition the queue undoes by
	// failing it again. Each run spends one retry, so not idempotent.
	cliconv.WithSideEffect(jobRetryCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(jobRetryCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(jobRetryCmd, []cliconv.Example{
		{Title: "Retry a failed job", Command: "ctxt job retry job_12345678"},
	})
	cliconv.WithNextSteps(jobRetryCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt job status <job-id>", Reason: "follow the job through the pipeline again"},
	})

	// cancel abandons work in flight; what the job already wrote is not
	// rolled back. Destructive. A second cancel finds it cancelled.
	cliconv.WithSideEffect(jobCancelCmd, cliconv.SideEffectDestructive)
	cliconv.WithDestructiveToken(jobCancelCmd)
	cliconv.WithIdempotency(jobCancelCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(jobCancelCmd, []cliconv.Example{
		{Title: "Cancel a job", Command: "ctxt job cancel job_12345678 --confirm-token=<token>"},
	})
	cliconv.WithNextSteps(jobCancelCmd, []cliconv.NextStep{
		{When: "after cancel", Suggest: "ctxt job list --state cancelled", Reason: "confirm the job stopped"},
	})
}

func runJobList(cmd *cobra.Command, _ []string) error {
	state, _ := cmd.Flags().GetString("state")
	limit, _ := cmd.Flags().GetInt("limit")
	offset, _ := cmd.Flags().GetInt("offset")
	if state != "" && !slices.Contains(jobStates, state) {
		e := output.UsageError(fmt.Sprintf("--state %q: want one of %s", state, strings.Join(jobStates, ", ")))
		e.SuggestedFix = "e.g. ctxt job list --state failed"
		return e
	}

	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	jobs, total, err := client.ListJobs(cmd.Context(), dpkmsclient.JobListRequest{
		Status: state, Limit: limit, Offset: offset,
	})
	if err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}

	out := cmd.OutOrStdout()
	if isJSONOutput() {
		if jobs == nil {
			jobs = []*storage.Job{}
		}
		return outputJSON(out, map[string]any{"jobs": jobs, "total": total})
	}
	rows := clientJobRows(jobs)
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, out, rows)
	}
	fmt.Fprintf(out, "Jobs (%d total)\n\n", total)
	headers := []string{"ID", "Type", "Status", "Pipeline", "Created"}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.ID, r.Type, r.Status, r.Pipeline, r.Created})
	}
	printTable(out, headers, table)
	return nil
}

// clientJobRow is the row shape the tabular formats project from.
type clientJobRow struct {
	ID       string `table:"ID"`
	Type     string `table:"Type"`
	Status   string `table:"Status"`
	Pipeline string `table:"Pipeline"`
	Created  string `table:"Created"`
}

func clientJobRows(jobs []*storage.Job) []clientJobRow {
	rows := make([]clientJobRow, 0, len(jobs))
	for _, j := range jobs {
		rows = append(rows, clientJobRow{
			ID:       j.ID,
			Type:     j.Type,
			Status:   string(j.Status),
			Pipeline: j.Pipeline,
			Created:  j.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	return rows
}

func runJobStatus(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	job, err := client.GetJob(cmd.Context(), args[0])
	if err != nil {
		return fmt.Errorf("job %s: %w", args[0], err)
	}

	out := cmd.OutOrStdout()
	if isJSONOutput() {
		return outputJSON(out, job)
	}
	const ts = "2006-01-02 15:04:05"
	fmt.Fprintf(out, "Job: %s\n\n", job.ID)
	fmt.Fprintf(out, "Type:       %s\n", job.Type)
	fmt.Fprintf(out, "Status:     %s\n", job.Status)
	fmt.Fprintf(out, "Pipeline:   %s\n", job.Pipeline)
	fmt.Fprintf(out, "Created:    %s\n", job.CreatedAt.Format(ts))
	if job.StartedAt != nil {
		fmt.Fprintf(out, "Started:    %s\n", job.StartedAt.Format(ts))
	}
	if job.CompletedAt != nil {
		fmt.Fprintf(out, "Completed:  %s\n", job.CompletedAt.Format(ts))
	}
	fmt.Fprintf(out, "Retries:    %d/%d\n", job.RetryCount, job.MaxRetries)
	if job.Error != "" {
		fmt.Fprintf(out, "\nError: %s\n", job.Error)
	}
	if job.ResultID != "" {
		fmt.Fprintf(out, "\nResult: %s\n", job.ResultID)
	}
	return nil
}

func runJobRetry(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	job, err := client.RetryJob(cmd.Context(), args[0])
	if err != nil {
		return fmt.Errorf("retry job %s: %w", args[0], err)
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), job)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Job %s queued for retry (%d/%d)\n", job.ID, job.RetryCount, job.MaxRetries)
	return nil
}

func runJobCancel(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	job, err := client.CancelJob(cmd.Context(), args[0])
	if err != nil {
		return fmt.Errorf("cancel job %s: %w", args[0], err)
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), job)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Job %s cancelled\n", job.ID)
	return nil
}
