import { createFileRoute } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { registries } from '@/api';
import { Globe, Loader2 } from 'lucide-react';

export const Route = createFileRoute('/registry')({ component: RegistryView });

interface RegistryEntry {
  url: string;
  name?: string;
  description?: string;
  entity_count?: number;
  last_synced_at?: string;
}

function RegistryView() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['registries'],
    queryFn: () => registries.list(),
  });

  const list = (data?.registries ?? []) as RegistryEntry[];

  return (
    <div className="view">
      <header className="view-header">
        <h1>Registry</h1>
        <p className="view-subtitle">Registered namespaces &amp; entity feeds</p>
      </header>

      {isLoading && (
        <div className="loading-row">
          <Loader2 size={16} className="spin" /> Loading…
        </div>
      )}

      {error && <p className="error-msg">{(error as Error).message}</p>}

      {list.length === 0 && !isLoading && (
        <div className="empty-state">
          <Globe size={40} className="empty-icon" />
          <p>No registries configured.</p>
          <p className="muted">
            Use <code>dpkms registry fetch &lt;url&gt;</code> to add one.
          </p>
        </div>
      )}

      <div className="registry-grid">
        {list.map((r) => (
          <div key={r.url} className="registry-card">
            <div className="registry-card-header">
              <Globe size={16} />
              <span className="registry-name">{r.name ?? r.url}</span>
            </div>
            {r.description && (
              <p className="registry-desc">{r.description}</p>
            )}
            <div className="registry-meta">
              <span className="registry-url muted">{r.url}</span>
              {r.entity_count !== undefined && (
                <span className="registry-count">
                  {r.entity_count} entities
                </span>
              )}
              {r.last_synced_at && (
                <span className="muted">
                  Synced {fmtDate(r.last_synced_at)}
                </span>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function fmtDate(s: string) {
  return new Date(s).toLocaleString(undefined, {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}
