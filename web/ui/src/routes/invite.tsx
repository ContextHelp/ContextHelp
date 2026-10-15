import { createFileRoute } from '@tanstack/react-router';
import { session, INVITE_PATH } from '@/api';
import { SignInCeremony } from '@/components/Ceremony';

export const Route = createFileRoute('/invite')({ component: AcceptInvite });

/**
 * Accepts an invite link: the server creates the person and signs this
 * browser in as them. Nothing is shared with a new person yet.
 */
function AcceptInvite() {
  return (
    <SignInCeremony
      title="Accept invite"
      path={INVITE_PATH}
      redeem={session.acceptInvite}
      help={<InviteHelp />}
      note={() => (
        <p className="muted">
          Welcome. Nothing is shared with you yet: what you can see here grows as the owner shares it.
        </p>
      )}
    />
  );
}

function InviteHelp() {
  return (
    <div className="auth-help">
      <p>An invite link works once and for a limited time. Ask the owner of this instance for a new one:</p>
      <pre className="auth-cmd">ctxt share invite --name &quot;your name&quot;</pre>
      <p className="muted">Open the new link in this browser; the invite signs you in as a new person on this instance.</p>
    </div>
  );
}
