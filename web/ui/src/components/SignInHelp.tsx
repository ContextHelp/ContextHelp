/** How to get a sign-in link. */
export function SignInHelp() {
  return (
    <div className="auth-help">
      <p>To sign in to this instance, run this on a machine whose ctxt has a token for it:</p>
      <pre className="auth-cmd">ctxt ui open</pre>
      <p className="muted">
        It opens a single-use link, valid for 60 seconds. Add <code>--server {window.location.origin}</code>{' '}
        if this is not your default instance, or <code>--no-browser</code> to print the link instead.
      </p>
    </div>
  );
}
