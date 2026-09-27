// The login code travels in the URL fragment of /ui/auth#code=… so it
// never reaches a server log, a proxy or a Referer header. It is lifted
// out and the fragment wiped from the address bar and history before
// the router starts; the sign-in page then takes it exactly once.

import { LOGIN_PATH } from './api';

let pendingCode: string | null = null;

/** Moves a login code from the URL fragment into memory. Call before the router starts. */
export function capturePendingCode(): void {
  if (window.location.pathname !== LOGIN_PATH) return;
  const params = new URLSearchParams(window.location.hash.replace(/^#/, ''));
  const code = params.get('code');
  if (window.location.hash) {
    window.history.replaceState(window.history.state, '', LOGIN_PATH);
  }
  if (code) pendingCode = code;
}

/** Returns the captured code once; later calls get null. */
export function takePendingCode(): string | null {
  const code = pendingCode;
  pendingCode = null;
  return code;
}
