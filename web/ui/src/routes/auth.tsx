import { createFileRoute } from '@tanstack/react-router';
import { session, LOGIN_PATH } from '@/api';
import { SignInHelp } from '@/components/SignInHelp';
import { SignInCeremony } from '@/components/Ceremony';

export const Route = createFileRoute('/auth')({ component: SignIn });

/** Trades the code from `ctxt ui open` for the session cookie. */
function SignIn() {
  return <SignInCeremony title="Sign in" path={LOGIN_PATH} redeem={session.exchange} help={<SignInHelp />} />;
}
