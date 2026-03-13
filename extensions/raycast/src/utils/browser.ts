import { execFileNoThrow } from "./execFileNoThrow";

export interface BrowserTab {
  url: string;
  title: string;
}

/**
 * Get the URL and title of the frontmost browser tab.
 * Supports Safari, Chrome, Arc, and Brave. Returns null for unsupported
 * browsers or if AppleScript access is denied.
 *
 * Uses osascript via execFileNoThrow — no shell injection risk since no
 * user input is passed to the script.
 */
export async function getFrontmostTabUrl(): Promise<BrowserTab | null> {
  // Tab delimiter avoids issues with commas in titles
  const script = `
tell application "System Events"
  set frontApp to name of first application process whose frontmost is true
end tell
set tabURL to ""
set tabTitle to ""
if frontApp is "Safari" then
  tell application "Safari"
    set tabURL to URL of current tab of window 1
    set tabTitle to name of current tab of window 1
  end tell
else if frontApp is "Google Chrome" then
  tell application "Google Chrome"
    set tabURL to URL of active tab of window 1
    set tabTitle to title of active tab of window 1
  end tell
else if frontApp is "Arc" then
  tell application "Arc"
    set tabURL to URL of active tab of window 1
    set tabTitle to title of active tab of window 1
  end tell
else if frontApp is "Brave Browser" then
  tell application "Brave Browser"
    set tabURL to URL of active tab of window 1
    set tabTitle to title of active tab of window 1
  end tell
end if
return tabURL & "\t" & tabTitle
`;

  try {
    const res = await execFileNoThrow("/usr/bin/osascript", ["-e", script]);
    if (res.status !== 0 || !res.stdout.trim()) return null;
    const parts = res.stdout.split("\t");
    const url = parts[0]?.trim() ?? "";
    const title = parts[1]?.trim() ?? "";
    if (!url || !url.startsWith("http")) return null;
    return { url, title };
  } catch {
    // AppleScript permission denied or unsupported browser
    return null;
  }
}

export function isUrl(text: string): boolean {
  try {
    const u = new URL(text.trim());
    return u.protocol === "http:" || u.protocol === "https:";
  } catch {
    return false;
  }
}
