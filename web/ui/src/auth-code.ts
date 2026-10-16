// A sign-in secret travels in the URL fragment: /ui/auth#code=… for a
// login code from `ctxt ui open`, /ui/invite#t=… for an invite and
// /ui/signin#t=… for a sign-in link. The fragment never reaches a
// server log, a proxy or a Referer header. It is lifted out and wiped
// from the address bar and history before the router starts; the page
// for that path then takes it exactly once.

import { LOGIN_PATH, INVITE_PATH, SIGNIN_PATH } from './api';

const FRAGMENT_PARAM: Record<string, string> = {
  [LOGIN_PATH]: 'code',
  [INVITE_PATH]: 't',
  [SIGNIN_PATH]: 't',
};

let pending: { path: string; secret: string } | null = null;

/** Moves a sign-in secret from the URL fragment into memory. Call before the router starts. */
export function capturePendingCode(): void {
  const path = window.location.pathname;
  const param = FRAGMENT_PARAM[path];
  if (!param) return;
  const params = new URLSearchParams(window.location.hash.replace(/^#/, ''));
  const secret = params.get(param);
  if (window.location.hash) {
    window.history.replaceState(window.history.state, '', path);
  }
  if (secret) pending = { path, secret };
}

/** Returns the secret captured for path once; later calls get null. */
export function takePendingSecret(path: string): string | null {
  if (!pending || pending.path !== path) return null;
  const secret = pending.secret;
  pending = null;
  return secret;
}
