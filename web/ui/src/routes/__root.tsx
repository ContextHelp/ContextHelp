import { createRootRoute, Outlet, Link, useLocation } from '@tanstack/react-router';
import { Database, Search, Briefcase, Globe, LayoutDashboard } from 'lucide-react';

const NAV = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard },
  { to: '/search', label: 'Search', icon: Search },
  { to: '/objects', label: 'Objects', icon: Database },
  { to: '/jobs', label: 'Jobs', icon: Briefcase },
  { to: '/registry', label: 'Registry', icon: Globe },
];

function Shell() {
  const loc = useLocation();
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
          <span className="version">v0.1</span>
        </div>
      </aside>
      <main className="content">
        <Outlet />
      </main>
    </div>
  );
}

export const Route = createRootRoute({ component: Shell });
