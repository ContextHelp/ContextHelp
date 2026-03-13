import { Action, ActionPanel, Detail } from "@raycast/api";
import { execFileNoThrow } from "../utils/execFileNoThrow";

interface Props {
  onRetry?: () => void;
}

const MARKDOWN = `## ctxt server is offline

The dpkms server is not running at the configured URL.

**To start the server:**
\`\`\`
dpkms serve
\`\`\`

Or click **Start Server** below to launch it in the background.
`;

export function ServerOffline({ onRetry }: Props) {
  async function handleStart() {
    // Use execFileNoThrow to avoid shell injection; dpkms is a trusted binary
    // We fire-and-forget (no await) so the command runs in background
    execFileNoThrow("dpkms", ["serve"]).then(() => {
      // dpkms serve blocks; this resolves only if it exits immediately
    });

    // Give server 1.5s to bind, then re-check health
    setTimeout(() => {
      onRetry?.();
    }, 1500);
  }

  return (
    <Detail
      markdown={MARKDOWN}
      actions={
        <ActionPanel>
          <Action title="Start Server" onAction={handleStart} />
          {onRetry && (
            <Action title="Retry Connection" onAction={onRetry} />
          )}
        </ActionPanel>
      }
    />
  );
}
