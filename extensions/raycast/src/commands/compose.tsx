import {
  Action,
  ActionPanel,
  Clipboard,
  Detail,
  Form,
  Toast,
  showToast,
} from "@raycast/api";
import { useState } from "react";
import { ServerOffline } from "../components/ServerOffline";
import { useHealth } from "../hooks/useHealth";
import { execFileNoThrow } from "../utils/execFileNoThrow";

type ComposeType = "brief" | "plan" | "summary" | "draft";

interface ComposeResult {
  // ComposeWithCitations JSON output from svc.ComposeWithCitations
  content: string;
  citations?: Array<{ id: string; ref: string }>;
}

interface FormValues {
  type: ComposeType;
  tag: string;
  mention: string;
  since: string;
  noCitations: boolean;
}

function buildArgs(values: FormValues): string[] {
  const args: string[] = ["make", values.type];
  if (values.tag) {
    args.push("--tag", values.tag);
  }
  if (values.mention) {
    args.push("--mention", values.mention);
  }
  if (values.since) {
    args.push("--since", values.since);
  }
  if (values.noCitations) {
    args.push("--no-citations");
  } else {
    // Default: export as JSON so we can extract citations and .content
    args.push("--export", "json");
  }
  return args;
}

export default function ComposeCommand() {
  const { isOnline, isChecking, recheck } = useHealth();
  const [isRunning, setIsRunning] = useState(false);
  const [result, setResult] = useState<string | null>(null);

  if (isChecking) return <Form isLoading={true} />;
  if (!isOnline) return <ServerOffline onRetry={recheck} />;

  if (result !== null) {
    return (
      <Detail
        markdown={result}
        actions={
          <ActionPanel>
            <Action
              title="Copy to Clipboard"
              onAction={() => Clipboard.copy(result!)}
            />
            <Action title="New Composition" onAction={() => setResult(null)} />
          </ActionPanel>
        }
      />
    );
  }

  async function handleSubmit(values: FormValues) {
    setIsRunning(true);

    const args = buildArgs(values);
    const res = await execFileNoThrow("ctxt", args);

    if (res.status !== 0) {
      await showToast({
        style: Toast.Style.Failure,
        title: "Compose failed",
        message: res.stderr || `exit code ${res.status}`,
      });
      setIsRunning(false);
      return;
    }

    let markdown: string;
    if (!values.noCitations) {
      // --export json: parse ComposeResult and use .content field
      try {
        const parsed = JSON.parse(res.stdout) as ComposeResult;
        markdown = parsed.content;
      } catch {
        // Fallback if output is not valid JSON
        markdown = res.stdout;
      }
    } else {
      // --no-citations: output is plain markdown
      markdown = res.stdout;
    }

    setResult(markdown);
    setIsRunning(false);
  }

  return (
    <Form
      isLoading={isRunning}
      actions={
        <ActionPanel>
          <Action.SubmitForm title="Compose" onSubmit={handleSubmit} />
        </ActionPanel>
      }
    >
      <Form.Dropdown id="type" title="Type" defaultValue="brief">
        {(["brief", "plan", "summary", "draft"] as ComposeType[]).map((t) => (
          <Form.Dropdown.Item key={t} value={t} title={t} />
        ))}
      </Form.Dropdown>
      <Form.TextField
        id="tag"
        title="Tags"
        placeholder="Comma-separated tags (e.g. ux,onboarding)"
      />
      <Form.TextField
        id="mention"
        title="Mention"
        placeholder="@namespace.slug"
      />
      <Form.TextField
        id="since"
        title="Since Date"
        placeholder="YYYY-MM-DD"
      />
      <Form.Checkbox
        id="noCitations"
        label="No Citations"
        defaultValue={false}
      />
    </Form>
  );
}
