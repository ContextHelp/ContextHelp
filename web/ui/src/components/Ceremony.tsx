import { Link } from '@tanstack/react-router';
import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { KeyRound, Loader2, LogOut } from 'lucide-react';
import { session, isAuthRequired, ApiError, SESSION_ACTIVE, type SignedIn, type WhoAmI } from '@/api';
import { takePendingSecret } from '@/auth-code';

type CeremonyState =
  | { kind: 'no-code' }
  | { kind: 'working' }
  | { kind: 'signed-in'; who: WhoAmI }
  | { kind: 'already-signed-in'; as: string; secret: string }
  | { kind: 'not-kept'; warning?: string }
  | { kind: 'failed'; message: string }
  | { kind: 'signed-out' };

export interface CeremonyProps {
  /** Page heading. */
  title: string;
  /** The web UI path whose fragment carried the secret. */
  path: string;
  /** Trades the secret for the session cookie. */
  redeem: (secret: string) => Promise<SignedIn>;
  /** How to get a new link, shown when there is no secret or it failed. */
  help: ReactNode;
  /** A line under the signed-in name, e.g. what an invitee can expect. */
  note?: (who: WhoAmI) => ReactNode;
}

/**
 * Runs one sign-in ceremony: trades the secret the fragment carried for
 * the session cookie, then asks the server who we are, so a cookie the
 * browser refused to keep (Secure over plain HTTP) shows up here instead
 * of as a silent 401 later. Success is shown, not skipped over, so a
 * sign-in someone else started is visible before anything else happens.
 * A browser already signed in is told so: the server refuses the
 * ceremony until it signs out, and the page offers to do both.
 */
export function SignInCeremony({ title, path, redeem, help, note }: CeremonyProps) {
  const qc = useQueryClient();
  const started = useRef(false);
  const [state, setState] = useState<CeremonyState>({ kind: 'working' });

  const run = async (secret: string) => {
    setState({ kind: 'working' });
    let warning: string | undefined;
    try {
      warning = (await redeem(secret)).warning;
    } catch (err) {
      if (err instanceof ApiError && err.code === SESSION_ACTIVE) {
        setState({ kind: 'already-signed-in', as: signedInAs(err.message), secret });
        return;
      }
      setState({ kind: 'failed', message: (err as Error).message });
      return;
    }
    try {
      const who = await session.whoami();
      if (who.via === 'session') {
        await qc.invalidateQueries();
        setState({ kind: 'signed-in', who });
        return;
      }
    } catch (err) {
      if (!isAuthRequired(err)) {
        setState({ kind: 'failed', message: (err as Error).message });
        return;
      }
    }
    setState({ kind: 'not-kept', warning });
  };

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    const secret = takePendingSecret(path);
    if (!secret) {
      setState({ kind: 'no-code' });
      return;
    }
    void run(secret);
    // run reads only stable props and setters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path]);

  const signOut = async () => {
    await session.signOut().catch(() => undefined);
    await qc.invalidateQueries();
    setState({ kind: 'signed-out' });
  };

  const signOutAndContinue = async (secret: string) => {
    await session.signOut().catch(() => undefined);
    await qc.invalidateQueries();
    await run(secret);
  };

  return (
    <div className="view">
      <header className="view-header">
        <h1>{title}</h1>
        <p className="view-subtitle">{window.location.host}</p>
      </header>
      <section className="panel auth-panel" data-auth={state.kind}>
        {state.kind === 'working' && (
          <div className="loading-row">
            <Loader2 size={16} className="spin" /> Signing in…
          </div>
        )}
        {state.kind === 'signed-in' && (
          <>
            <p className="auth-who">
              <KeyRound size={16} /> Signed in as <strong>{state.who.principal}</strong>
              {state.who.name && state.who.name !== state.who.principal ? ` (${state.who.name})` : ''} on{' '}
              <code>{window.location.host}</code>
            </p>
            {note?.(state.who)}
            {state.who.session && (
              <p className="muted">
                This browser stays signed in until {fmtDate(state.who.session.expires_at)} at most, and
                signs out after a long idle spell.
              </p>
            )}
            <div className="auth-actions">
              <Link to="/" className="btn btn-primary">Continue</Link>
              <button type="button" className="btn btn-ghost" onClick={signOut}>
                <LogOut size={14} /> Not you? Sign out
              </button>
            </div>
          </>
        )}
        {state.kind === 'already-signed-in' && (
          <>
            <p className="auth-who">
              <KeyRound size={16} /> This browser is already signed in as <strong>{state.as}</strong>.
            </p>
            <p className="muted">
              A link signs in one person at a time. Sign out first, and the link is then used for the new
              session; it has not been spent yet.
            </p>
            <div className="auth-actions">
              <button type="button" className="btn btn-primary" onClick={() => void signOutAndContinue(state.secret)}>
                <LogOut size={14} /> Sign out and continue
              </button>
              <Link to="/" className="btn btn-ghost">Stay signed in</Link>
            </div>
          </>
        )}
        {state.kind === 'not-kept' && (
          <>
            <p className="error-msg">
              The server accepted the sign-in, but this browser did not keep the session cookie.
            </p>
            <p className="muted">
              {state.warning ??
                'Browsers keep the session cookie only over HTTPS, or over HTTP on localhost. Reach this instance through a TLS proxy.'}
            </p>
          </>
        )}
        {state.kind === 'failed' && (
          <>
            <p className="error-msg">{state.message}</p>
            {help}
          </>
        )}
        {state.kind === 'no-code' && help}
        {state.kind === 'signed-out' && (
          <>
            <p>Signed out.</p>
            {help}
          </>
        )}
      </section>
    </div>
  );
}

/** Lifts the principal out of the server's SESSION_ACTIVE message. */
function signedInAs(message: string): string {
  const m = /signed in as (\S+?):/.exec(message);
  return m ? m[1] : 'someone else';
}

function fmtDate(s: string) {
  return new Date(s).toLocaleString(undefined, {
    weekday: 'short', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}
