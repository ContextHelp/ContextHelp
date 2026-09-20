package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	kitstyles "hop.top/kit/go/console/tui/styles"
)

var jobsCmd = &cobra.Command{
	Use:   "job",
	Short: "Inspect and manage ingestion jobs",
	Long: `Manage ingestion jobs in the job queue.

Jobs have the following states:
  - Pending: Waiting to be processed
  - Running: Currently being processed
  - Completed: Successfully finished
  - Failed: Encountered an error
  - Cancelled: Manually cancelled

Examples:
  # List all jobs
  dpkms job list

  # Check status of a specific job
  dpkms job status job_12345678

  # View logs for a job
  dpkms job log job_12345678

  # Retry a failed job
  dpkms job retry job_12345678

  # Cancel a pending or running job
  dpkms job cancel job_12345678`,
}

var jobsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all jobs",
	Long:  `List all ingestion jobs with their current status.`,
	RunE:  runJobsList,
}

var jobsStatusCmd = &cobra.Command{
	Use:   "status <job-id>",
	Short: "Get status of a specific job",
	Long:  `Display detailed status information for a specific job.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runJobsStatus,
}

var jobsLogsCmd = &cobra.Command{
	Use:   "log <job-id>",
	Short: "View logs for a specific job",
	Long:  `Display execution logs for a specific job.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runJobsLogs,
}

var jobsRetryCmd = &cobra.Command{
	Use:   "retry <job-id>",
	Short: "Retry a failed job",
	Long:  `Retry execution of a failed job.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runJobsRetry,
}

var jobsCancelCmd = &cobra.Command{
	Use:   "cancel <job-id>",
	Short: "Cancel a pending or running job",
	Long:  `Cancel a job that is pending or currently running.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runJobsCancel,
}

func init() {
	rootCmd.AddCommand(jobsCmd)

	jobsCmd.AddCommand(jobsListCmd)
	jobsCmd.AddCommand(jobsStatusCmd)
	jobsCmd.AddCommand(jobsLogsCmd)
	jobsCmd.AddCommand(jobsRetryCmd)
	jobsCmd.AddCommand(jobsCancelCmd)

	jobsListCmd.Flags().String("state", "",
		"filter by state (pending|running|completed|failed|cancelled)")
	jobsListCmd.Flags().Int("limit", 50, "maximum number of jobs to return")

	viper.BindPFlag("jobs.state", jobsListCmd.Flags().Lookup("state"))
	viper.BindPFlag("jobs.limit", jobsListCmd.Flags().Lookup("limit"))

	// list/status/log only read the queue.
	cliconv.WithSideEffect(jobsListCmd, cliconv.SideEffectRead)
	cliconv.WithSideEffect(jobsStatusCmd, cliconv.SideEffectRead)
	cliconv.WithSideEffect(jobsLogsCmd, cliconv.SideEffectRead)
	// retry re-queues a failed job: a status transition that the queue
	// can undo by failing again. Write.
	cliconv.WithSideEffect(jobsRetryCmd, cliconv.SideEffectWrite)
	// cancel stops in-flight work; partial effects already applied by
	// the job are not rolled back. Destructive.
	cliconv.WithSideEffect(jobsCancelCmd, cliconv.SideEffectDestructive)
}

func runJobsList(cmd *cobra.Command, _ []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	filter := storage.JobFilter{
		Status: storage.JobStatus(viper.GetString("jobs.state")),
		Limit:  viper.GetInt("jobs.limit"),
	}
	jobs, total, err := svc.ListJobs(ctx, filter)
	if err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{"jobs": jobs, "total": total})
	}

	// csv/text/table project from jobRow's tags. Status is the raw
	// value here, not the colorized one: ANSI escapes belong in the
	// human view, and embedding them in a csv cell would corrupt the
	// field for every parser downstream.
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, cmd.OutOrStdout(), jobRows(jobs))
	}

	fmt.Printf("Jobs (%d total)\n\n", total)
	headers := []string{"ID", "Type", "Status", "Pipeline", "Created"}
	var rows [][]string
	for _, j := range jobs {
		rows = append(rows, []string{
			j.ID,
			j.Type,
			statusStyle(string(j.Status)),
			j.Pipeline,
			j.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	printAdminTable(os.Stdout, headers, rows)
	return nil
}

// jobRow is the row shape the tabular formats project from.
type jobRow struct {
	ID       string `table:"ID"`
	Type     string `table:"Type"`
	Status   string `table:"Status"`
	Pipeline string `table:"Pipeline"`
	Created  string `table:"Created"`
}

// jobRows projects queued jobs onto the row shape, carrying the plain
// status string so machine formats stay free of terminal styling.
func jobRows(jobs []*storage.Job) []jobRow {
	rows := make([]jobRow, 0, len(jobs))
	for _, j := range jobs {
		rows = append(rows, jobRow{
			ID:       j.ID,
			Type:     j.Type,
			Status:   string(j.Status),
			Pipeline: j.Pipeline,
			Created:  j.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	return rows
}

// statusStyle renders a job status string with kit's semantic palette.
func statusStyle(status string) string {
	st := kitstyles.NewStyles(root.Theme)
	switch strings.ToLower(status) {
	case "completed":
		return st.Success.Render(status)
	case "failed":
		return st.Error.Render(status)
	case "running", "processing":
		return st.Accent.Render(status)
	case "pending":
		return st.Muted.Render(status)
	default:
		return st.Secondary.Render(status)
	}
}

func runJobsStatus(cmd *cobra.Command, args []string) error {
	jobID := args[0]

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	job, err := svc.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, job)
	}

	fmt.Printf("Job: %s\n\n", job.ID)
	fmt.Printf("Type:       %s\n", job.Type)
	fmt.Printf("Status:     %s\n", job.Status)
	fmt.Printf("Pipeline:   %s\n", job.Pipeline)
	fmt.Printf("Created:    %s\n", job.CreatedAt.Format("2006-01-02 15:04:05"))
	if job.StartedAt != nil {
		fmt.Printf("Started:    %s\n", job.StartedAt.Format("2006-01-02 15:04:05"))
	}
	if job.CompletedAt != nil {
		fmt.Printf("Completed:  %s\n", job.CompletedAt.Format("2006-01-02 15:04:05"))
	}
	fmt.Printf("Retries:    %d/%d\n", job.RetryCount, job.MaxRetries)
	if job.Error != "" {
		fmt.Printf("\nError: %s\n", job.Error)
	}
	if job.ResultID != "" {
		fmt.Printf("\nResult: %s\n", job.ResultID)
	}
	return nil
}

func runJobsLogs(cmd *cobra.Command, args []string) error {
	jobID := args[0]

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	job, err := svc.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"job_id": job.ID,
			"status": job.Status,
			"error":  job.Error,
		})
	}

	fmt.Printf("Logs for job %s (status: %s)\n\n", job.ID, job.Status)
	if job.Error != "" {
		fmt.Println(job.Error)
	} else {
		fmt.Println("No log output available.")
	}
	return nil
}

func runJobsRetry(cmd *cobra.Command, args []string) error {
	jobID := args[0]

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	if err := svc.RetryJob(ctx, jobID); err != nil {
		return fmt.Errorf("retry job: %w", err)
	}

	fmt.Printf("Job %s queued for retry\n", jobID)
	return nil
}

func runJobsCancel(cmd *cobra.Command, args []string) error {
	jobID := args[0]

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	if err := svc.CancelJob(ctx, jobID); err != nil {
		return fmt.Errorf("cancel job: %w", err)
	}

	fmt.Printf("Job %s cancelled\n", jobID)
	return nil
}
