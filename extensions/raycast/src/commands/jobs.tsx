import {
  Action,
  ActionPanel,
  Color,
  Icon,
  List,
  Toast,
  showToast,
} from "@raycast/api";
import { retryJob } from "../api/endpoints";
import { Job } from "../api/types";
import { ServerOffline } from "../components/ServerOffline";
import { useHealth } from "../hooks/useHealth";
import { useJobs } from "../hooks/useJobs";

function statusColor(status: Job["status"]): Color {
  switch (status) {
    case "completed": return Color.Green;
    case "failed":    return Color.Red;
    case "running":   return Color.Yellow;
    case "pending":   return Color.SecondaryText;
  }
}

function statusIcon(status: Job["status"]): Icon {
  switch (status) {
    case "completed": return Icon.Checkmark;
    case "failed":    return Icon.XMarkCircle;
    case "running":   return Icon.CircleProgress;
    case "pending":   return Icon.Clock;
  }
}

function statusLabel(job: Job): string {
  if (job.status === "running") return "Processing…";
  if (job.status === "failed" && job.error) return job.error.slice(0, 40);
  return job.status;
}

export default function JobsCommand() {
  const { isOnline, isChecking, recheck: recheckHealth } = useHealth();
  const { jobs, isLoading, error, refresh } = useJobs();

  if (isChecking) return <List isLoading={true} />;
  if (!isOnline) return <ServerOffline onRetry={recheckHealth} />;

  return (
    <List
      isLoading={isLoading}
      searchBarPlaceholder="Filter jobs…"
    >
      <List.EmptyView
        title={error ? "Failed to load jobs" : "No jobs"}
        description={error ?? "Capture content to create jobs."}
      />
      {jobs.map((job) => (
        <List.Item
          key={job.id}
          icon={{ source: statusIcon(job.status), tintColor: statusColor(job.status) }}
          title={job.source ?? job.type}
          subtitle={job.pipeline ?? job.type}
          accessories={[
            { tag: { value: statusLabel(job), color: statusColor(job.status) } },
            { text: new Date(job.created_at).toLocaleTimeString() },
          ]}
          actions={
            <ActionPanel>
              <Action.CopyToClipboard title="Copy Job ID" content={job.id} />
              {job.status === "failed" && (
                <Action
                  title="Retry Job"
                  icon={Icon.ArrowClockwise}
                  onAction={async () => {
                    try {
                      await retryJob(job.id);
                      await showToast({
                        style: Toast.Style.Success,
                        title: "Retry queued",
                      });
                      refresh();
                    } catch (e: unknown) {
                      await showToast({
                        style: Toast.Style.Failure,
                        title: "Retry failed",
                        message: (e as Error).message,
                      });
                    }
                  }}
                />
              )}
              <Action title="Refresh" onAction={refresh} />
            </ActionPanel>
          }
        />
      ))}
    </List>
  );
}
