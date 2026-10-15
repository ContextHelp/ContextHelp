import { createFileRoute } from '@tanstack/react-router';
import { session, SIGNIN_PATH } from '@/api';
import { SignInCeremony } from '@/components/Ceremony';

export const Route = createFileRoute('/signin')({ component: SignInWithLink });

/** Redeems a sign-in link the owner minted for an existing principal. */
function SignInWithLink() {
  return <SignInCeremony title="Sign in" path={SIGNIN_PATH} redeem={session.redeemLink} help={<LinkHelp />} />;
}

function LinkHelp() {
  return (
    <div className="auth-help">
      <p>A sign-in link works once, within ten minutes. Ask the owner of this instance for a new one:</p>
      <pre className="auth-cmd">ctxt principal link &lt;your principal id&gt;</pre>
      <p className="muted">
        If you have a ctxt API key of your own, <code>ctxt ui open</code> signs this browser in without a link.
      </p>
    </div>
  );
}
