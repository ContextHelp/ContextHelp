import {
  Action,
  ActionPanel,
  Color,
  Detail,
  List,
  Toast,
  showToast,
} from "@raycast/api";
import { useEffect, useState } from "react";
import { listObjects } from "../api/endpoints";
import { KnowledgeObject, ServerOfflineError } from "../api/types";
import { ServerOffline } from "../components/ServerOffline";
import { useHealth } from "../hooks/useHealth";

function truncate(s: string, n: number): string {
  return s.length > n ? s.slice(0, n - 1) + "…" : s;
}

function buildObjectMarkdown(obj: KnowledgeObject): string {
  const lines: string[] = [];
  lines.push(`# ${truncate(obj.text_content ?? obj.raw_content, 80)}`);
  lines.push(`\n**Type:** \`${obj.type}\`${obj.subtype ? ` / \`${obj.subtype}\`` : ""}`);
  if (obj.pipeline) lines.push(`**Pipeline:** \`${obj.pipeline}\``);
  if (obj.source) lines.push(`**Source:** ${obj.source}`);
  lines.push(`**Created:** ${new Date(obj.created_at).toLocaleString()}`);
  if (obj.summaries?.length) {
    lines.push("\n## Summary");
    obj.summaries.forEach((s) => lines.push(s));
  }
  if (obj.tags?.length) {
    lines.push("\n## Tags");
    lines.push(obj.tags.map((t) => `\`${t.label}\``).join(" "));
  }
  if (obj.mentions?.length) {
    lines.push("\n## Mentions");
    lines.push(obj.mentions.join(", "));
  }
  if (obj.decisions?.length) {
    lines.push("\n## Decisions");
    obj.decisions.forEach((d) =>
      lines.push(`- **${d.title}** — ${d.status} / ${d.impact}`)
    );
  }
  return lines.join("\n");
}

function ObjectDetail({ obj }: { obj: KnowledgeObject }) {
  return (
    <Detail
      markdown={buildObjectMarkdown(obj)}
      actions={
        <ActionPanel>
          <Action.CopyToClipboard title="Copy ID" content={obj.id} />
          {obj.source && (
            <Action.OpenInBrowser title="Open Source" url={obj.source} />
          )}
        </ActionPanel>
      }
    />
  );
}

export default function RecentCommand() {
  const { isOnline, isChecking, recheck } = useHealth();
  const [objects, setObjects] = useState<KnowledgeObject[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    if (!isOnline) return;
    let cancelled = false;
    setIsLoading(true);

    listObjects({ sort: "updated_at", dir: "desc", limit: "30" })
      .then((res) => {
        if (!cancelled) setObjects(res.data ?? []);
      })
      .catch(async (e: unknown) => {
        if (!cancelled && !(e instanceof ServerOfflineError)) {
          await showToast({
            style: Toast.Style.Failure,
            title: "Failed to load recent",
            message: (e as Error).message,
          });
        }
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });

    return () => { cancelled = true; };
  }, [isOnline]);

  if (isChecking) return <List isLoading={true} />;
  if (!isOnline) return <ServerOffline onRetry={recheck} />;

  return (
    <List isLoading={isLoading} searchBarPlaceholder="Filter recent…">
      {objects.map((obj) => (
        <List.Item
          key={obj.id}
          title={truncate(obj.text_content ?? obj.raw_content, 60)}
          subtitle={`${obj.type}${obj.pipeline ? ` · ${obj.pipeline}` : ""}`}
          accessories={[
            { text: new Date(obj.updated_at).toLocaleDateString() },
            ...(obj.tags
              ?.slice(0, 2)
              .map((t) => ({ tag: { value: t.label, color: Color.Blue } })) ?? []),
          ]}
          actions={
            <ActionPanel>
              <Action.Push title="View Details" target={<ObjectDetail obj={obj} />} />
              <Action.CopyToClipboard title="Copy ID" content={obj.id} />
              {obj.source && (
                <Action.OpenInBrowser title="Open Source" url={obj.source} />
              )}
            </ActionPanel>
          }
        />
      ))}
    </List>
  );
}
