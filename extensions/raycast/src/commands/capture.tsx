import {
  Action,
  ActionPanel,
  Clipboard,
  Form,
  LaunchProps,
  Toast,
  showToast,
} from "@raycast/api";
import { useEffect, useState } from "react";
import { analyzeContent } from "../api/endpoints";
import { AnalyzeRequest, ServerOfflineError } from "../api/types";
import { ServerOffline } from "../components/ServerOffline";
import { useHealth } from "../hooks/useHealth";
import { getFrontmostTabUrl, isUrl } from "../utils/browser";

interface LaunchContext {
  url?: string;
  text?: string;
}

interface FormValues {
  content: string;
  type: string;
  pipeline: string;
  hints: string;
  source: string;
}

const TYPE_OPTIONS = ["auto", "url", "text", "note", "document", "image", "audio", "video"];

export default function CaptureCommand(props: LaunchProps<{ launchContext: LaunchContext }>) {
  const { isOnline, isChecking, recheck } = useHealth();
  const [isSubmitting, setIsSubmitting] = useState(false);

  // Pre-fill state
  const [defaultContent, setDefaultContent] = useState("");
  const [defaultType, setDefaultType] = useState("auto");
  const [defaultSource, setDefaultSource] = useState("");

  useEffect(() => {
    async function prefill() {
      // 1. Deeplink args take highest priority
      if (props.launchContext?.url) {
        setDefaultContent(props.launchContext.url);
        setDefaultType("url");
        setDefaultSource(props.launchContext.url);
        return;
      }
      if (props.launchContext?.text) {
        setDefaultContent(props.launchContext.text);
        setDefaultType("text");
        return;
      }

      // 2. Check frontmost browser tab
      const tab = await getFrontmostTabUrl();
      if (tab) {
        setDefaultContent(tab.url);
        setDefaultType("url");
        setDefaultSource(tab.url);
        return;
      }

      // 3. Fall back to clipboard
      const clip = await Clipboard.readText();
      if (clip) {
        if (isUrl(clip)) {
          setDefaultContent(clip);
          setDefaultType("url");
          setDefaultSource(clip);
        } else {
          setDefaultContent(clip);
          setDefaultType("text");
        }
      }
    }

    prefill();
  }, []);

  if (isChecking) {
    return <Form isLoading={true} />;
  }

  if (!isOnline) {
    return <ServerOffline onRetry={recheck} />;
  }

  async function handleSubmit(values: FormValues) {
    setIsSubmitting(true);

    const req: AnalyzeRequest = {
      content: values.content,
      type: values.type === "auto" ? undefined : values.type,
      pipeline: values.pipeline || undefined,
      hints: values.hints
        ? values.hints.split(",").map((h) => h.trim()).filter(Boolean)
        : undefined,
      source: values.source || undefined,
    };

    try {
      const res = await analyzeContent(req);
      await showToast({
        style: Toast.Style.Success,
        title: "Captured",
        message: `Job ${res.job_id} queued`,
      });
    } catch (e: unknown) {
      if (e instanceof ServerOfflineError) {
        await showToast({ style: Toast.Style.Failure, title: "Server offline" });
      } else {
        await showToast({
          style: Toast.Style.Failure,
          title: "Capture failed",
          message: (e as Error).message,
        });
      }
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <Form
      isLoading={isSubmitting}
      actions={
        <ActionPanel>
          <Action.SubmitForm title="Capture" onSubmit={handleSubmit} />
        </ActionPanel>
      }
    >
      <Form.TextArea
        id="content"
        title="Content"
        placeholder="Paste URL, text, or content to capture…"
        defaultValue={defaultContent}
      />
      <Form.Dropdown id="type" title="Type" defaultValue={defaultType}>
        {TYPE_OPTIONS.map((t) => (
          <Form.Dropdown.Item key={t} value={t} title={t} />
        ))}
      </Form.Dropdown>
      <Form.TextField
        id="pipeline"
        title="Pipeline"
        placeholder="e.g. url.generic (optional)"
      />
      <Form.TextField
        id="hints"
        title="Hints"
        placeholder="Comma-separated hints (optional)"
      />
      <Form.TextField
        id="source"
        title="Source URL"
        placeholder="Override source URL (optional)"
        defaultValue={defaultSource}
      />
    </Form>
  );
}
