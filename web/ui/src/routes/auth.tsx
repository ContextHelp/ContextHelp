import { createFileRoute, Link } from '@tanstack/react-router';
import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { KeyRound, Loader2, LogOut } from 'lucide-react';
import { session, isAuthRequired, type WhoAmI } from '@/api';
import { takePendingCode } from '@/auth-code';
import { SignInHelp } from '@/components/SignInHelp';

export const Route = createFileRoute('/auth')({ component: SignIn });

type SignInState =
  | { kind: 'no-code' }
  | { kind: 'working' }
  | { kind: 'signed-in'; who: WhoAmI }
  | { kind: 'not-kept'; warning?: string }
  | { kind: 'failed'; message: string }
  | { kind: 'signed-out' };

/**
 * Trades the code from `ctxt ui open` for the session cookie, then asks
 * the server who we are: a cookie the browser refused to keep (Secure
 * over plain HTTP) shows up here instead of as a silent 401 later.
 * Success is shown, not skipped over, so a sign-in someone else
 * started is visible before anything else happens.
 */
function SignIn() {
  const qc = useQueryClient();
  const started = useRef(false);
  const [state, setState] = useState<SignInState>({ kind: 'working' });

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    (async () => {
      const code = takePendingCode();
      if (!code) {
        setState({ kind: 'no-code' });
        return;
      }
      let warning: string | undefined;
      try {
        warning = (await session.exchange(code)).warning;
      } catch (err) {
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
    })();
  }, [qc]);

  const signOut = async () => {
    await session.signOut().catch(() => undefined);
    await qc.invalidateQueries();
    setState({ kind: 'signed-out' });
  };

  return (
    <div className="view">
      <header className="view-header">
        <h1>Sign in</h1>
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
              <KeyRound size={16} /> Signed in as <strong>{state.who.principal}</strong> on{' '}
              <code>{window.location.host}</code>
            </p>
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
            <SignInHelp />
          </>
        )}
        {state.kind === 'no-code' && <SignInHelp />}
        {state.kind === 'signed-out' && (
          <>
            <p>Signed out.</p>
            <SignInHelp />
          </>
        )}
      </section>
    </div>
  );
}

function fmtDate(s: string) {
  return new Date(s).toLocaleString(undefined, {
    weekday: 'short', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}
