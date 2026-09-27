import { LogOut } from 'lucide-react';
import type { WhoAmI } from '@/api';

/** Who the browser is signed in as, with a sign-out for sessions. */
export function Identity({ who, onSignOut }: { who: WhoAmI | undefined; onSignOut: () => void }) {
  if (!who?.authenticated) return null;
  return (
    <div className="identity" data-principal={who.principal}>
      <span className="identity-name" title={`Signed in as ${who.principal}`}>
        {who.principal}
      </span>
      {who.via === 'session' && (
        <button type="button" className="identity-signout" onClick={onSignOut} title="Sign out">
          <LogOut size={13} />
        </button>
      )}
    </div>
  );
}
