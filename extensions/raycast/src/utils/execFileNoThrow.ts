import { execFile as nodeExecFile } from "child_process";
import { promisify } from "util";

const execFileAsync = promisify(nodeExecFile);

export interface ExecResult {
  stdout: string;
  stderr: string;
  /** exit code, or -1 if process failed to spawn */
  status: number;
}

/**
 * Run a command with args safely via execFile (no shell interpolation).
 * Never throws — returns structured output including errors.
 *
 * @param cmd  Absolute or PATH-resolved binary name
 * @param args Array of arguments (no shell quoting needed)
 * @param opts Optional: cwd, env, maxBuffer
 */
export async function execFileNoThrow(
  cmd: string,
  args: string[],
  opts?: { cwd?: string; env?: NodeJS.ProcessEnv; maxBuffer?: number }
): Promise<ExecResult> {
  // Resolve PATH to include common install locations for macOS
  const defaultPaths = [
    "/usr/local/bin",
    "/opt/homebrew/bin",
    `${process.env.HOME ?? ""}/.local/bin`,
    `${process.env.GOPATH ?? (process.env.HOME ?? "") + "/go"}/bin`,
    "/usr/bin",
    "/bin",
  ].join(":");

  const env: NodeJS.ProcessEnv = {
    ...process.env,
    PATH: `${defaultPaths}:${process.env.PATH ?? ""}`,
    ...opts?.env,
  };

  try {
    const { stdout, stderr } = await execFileAsync(cmd, args, {
      cwd: opts?.cwd,
      env,
      maxBuffer: opts?.maxBuffer ?? 10 * 1024 * 1024, // 10MB
    });
    return { stdout: stdout ?? "", stderr: stderr ?? "", status: 0 };
  } catch (e: unknown) {
    const err = e as Error & { stdout?: string; stderr?: string; code?: number | string };
    return {
      stdout: err.stdout ?? "",
      stderr: err.stderr ?? err.message ?? "unknown error",
      status: typeof err.code === "number" ? err.code : 1,
    };
  }
}
