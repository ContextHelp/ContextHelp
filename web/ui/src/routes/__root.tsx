import { createRootRoute, Outlet, Link, useLocation } from '@tanstack/react-router';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { Database, Search, Briefcase, Globe, LayoutDashboard, Loader2 } from 'lucide-react';
import { session, isAuthRequired } from '@/api';
import { Identity } from '@/components/Identity';
import { SignInHelp } from '@/components/SignInHelp';

const NAV = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard },
  { to: '/search', label: 'Search', icon: Search },
  { to: '/objects', label: 'Objects', icon: Database },
  { to: '/jobs', label: 'Jobs', icon: Briefcase },
  { to: '/registry', label: 'Registry', icon: Globe },
];

type LiveState = 'connecting' | 'open' | 'closed';

/**
 * Holds the event stream open while the page may read the API. The
 * session cookie rides along on its own (EventSource cannot send
 * headers). A stream the server closes for good (401: session over)
 * re-checks who we are.
 */
function useLiveEvents(enabled: boolean): LiveState {
  const qc = useQueryClient();
  const [state, setState] = useState<LiveState>('connecting');
  useEffect(() => {
    if (!enabled) return;
    const es = new EventSource('/api/v1/events');
    es.onopen = () => setState('open');
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) {
        setState('closed');
        void qc.invalidateQueries({ queryKey: ['whoami'] });
      } else {
        setState('connecting');
      }
    };
    return () => es.close();
  }, [enabled, qc]);
  return enabled ? state : 'closed';
}

function Shell() {
  const loc = useLocation();
  const qc = useQueryClient();
  const who = useQuery({ queryKey: ['whoami'], queryFn: () => session.whoami(), retry: false });
  const onSignIn = loc.pathname === '/auth';
  const needsSignIn = isAuthRequired(who.error);
  const live = useLiveEvents(who.isSuccess);

  const signOut = async () => {
    await session.signOut().catch(() => undefined);
    await qc.invalidateQueries();
  };

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="sidebar-brand">
          <span className="brand-mark">ctx</span>
          <span className="brand-name">ctxt</span>
        </div>
        <nav className="sidebar-nav">
          {NAV.map(({ to, label, icon: Icon }) => {
            const active = to === '/' ? loc.pathname === '/' : loc.pathname.startsWith(to);
            return (
              <Link key={to} to={to} className={`nav-item${active ? ' active' : ''}`}>
                <Icon size={16} />
                <span>{label}</span>
              </Link>
            );
          })}
        </nav>
        <div className="sidebar-footer">
          <Identity who={who.data} onSignOut={signOut} />
          <div className="sidebar-status">
            <span
              className={`live-dot live-${live}`}
              data-live={live}
              title={live === 'open' ? 'Live updates connected' : 'Live updates not connected'}
            />
            <span className="version">v0.1</span>
          </div>
        </div>
      </aside>
      <main className="content">
        {needsSignIn && !onSignIn ? (
          <div className="view">
            <header className="view-header">
              <h1>Sign in</h1>
              <p className="view-subtitle">{window.location.host}</p>
            </header>
            <section className="panel auth-panel" data-auth="required">
              <SignInHelp />
            </section>
          </div>
        ) : who.isPending && !onSignIn ? (
          <div className="loading-row">
            <Loader2 size={16} className="spin" /> Loading…
          </div>
        ) : (
          <Outlet />
        )}
      </main>
    </div>
  );
}

export const Route = createRootRoute({ component: Shell });
