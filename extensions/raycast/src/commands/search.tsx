import {
  Action,
  ActionPanel,
  Color,
  Detail,
  Icon,
  List,
  LaunchProps,
  Toast,
  showToast,
} from "@raycast/api";
import { useEffect, useState } from "react";
import { searchObjects } from "../api/endpoints";
import { KnowledgeObject, ServerOfflineError } from "../api/types";
import { ServerOffline } from "../components/ServerOffline";
import { useHealth } from "../hooks/useHealth";

interface LaunchContext {
  q?: string;
}

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
  if (obj.tasks?.length) {
    lines.push("\n## Tasks");
    obj.tasks.forEach((t) =>
      lines.push(`- [${t.status === "done" ? "x" : " "}] ${t.title}`)
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

export default function SearchCommand(props: LaunchProps<{ launchContext: LaunchContext }>) {
  const { isOnline, isChecking, recheck } = useHealth();
  const [query, setQuery] = useState(props.launchContext?.q ?? "");
  const [results, setResults] = useState<KnowledgeObject[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    if (!isOnline || !query.trim()) {
      setResults([]);
      return;
    }

    let cancelled = false;
    setIsLoading(true);

    searchObjects(query.trim())
      .then((res) => {
        if (!cancelled) setResults(res.data ?? []);
      })
      .catch(async (e: unknown) => {
        if (!cancelled && !(e instanceof ServerOfflineError)) {
          await showToast({
            style: Toast.Style.Failure,
            title: "Search failed",
            message: (e as Error).message,
          });
        }
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [query, isOnline]);

  if (isChecking) return <List isLoading={true} />;
  if (!isOnline) return <ServerOffline onRetry={recheck} />;

  return (
    <List
      isLoading={isLoading}
      searchText={query}
      onSearchTextChange={setQuery}
      searchBarPlaceholder="Search knowledge (RSQL or text)…"
      throttle
    >
      {results.map((obj) => (
        <List.Item
          key={obj.id}
          title={truncate(obj.text_content ?? obj.raw_content, 60)}
          subtitle={`${obj.type}${obj.pipeline ? ` · ${obj.pipeline}` : ""}`}
          accessories={[
            ...(obj.tags
              ?.slice(0, 3)
              .map((t) => ({ tag: { value: t.label, color: Color.Blue } })) ?? []),
          ]}
          actions={
            <ActionPanel>
              <Action.Push
                title="View Details"
                icon={Icon.Eye}
                target={<ObjectDetail obj={obj} />}
              />
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
