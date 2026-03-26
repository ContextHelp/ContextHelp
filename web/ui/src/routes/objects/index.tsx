import { createFileRoute, Link } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { objects as objectsApi } from '@/api';
import { Loader2, ChevronLeft, ChevronRight } from 'lucide-react';

export const Route = createFileRoute('/objects/')({ component: ObjectList });

const PAGE_SIZE = 20;

function ObjectList() {
  const [page, setPage] = useState(0);
  const [typeFilter, setTypeFilter] = useState('');

  const { data, isFetching, error } = useQuery({
    queryKey: ['objects', page, typeFilter],
    queryFn: () =>
      objectsApi.list({
        limit: PAGE_SIZE,
        offset: page * PAGE_SIZE,
        type: typeFilter || undefined,
      }),
  });

  const totalPages = Math.ceil((data?.total ?? 0) / PAGE_SIZE);

  return (
    <div className="view">
      <header className="view-header">
        <h1>Objects</h1>
        <p className="view-subtitle">
          {data ? `${data.total.toLocaleString()} total` : '—'}
        </p>
      </header>

      <div className="toolbar">
        <input
          className="filter-input"
          placeholder="Filter by type…"
          value={typeFilter}
          onChange={(e) => { setTypeFilter(e.target.value); setPage(0); }}
        />
        {isFetching && <Loader2 size={14} className="spin" />}
      </div>

      {error && <p className="error-msg">{(error as Error).message}</p>}

      <section className="panel">
        {data?.data.length === 0 && <p className="empty">No objects found.</p>}
        <ul className="object-list">
          {data?.data.map((o) => (
            <li key={o.id}>
              <Link to="/objects/$id" params={{ id: o.id }} className="object-row">
                <span className="obj-id font-mono">{o.id.slice(0, 8)}</span>
                <span className="obj-type">{o.type}</span>
                <span className="obj-source obj-source--clamp">{o.source}</span>
                <span className="obj-time">{fmtDate(o.created_at)}</span>
              </Link>
            </li>
          ))}
        </ul>
      </section>

      {totalPages > 1 && (
        <div className="pagination">
          <button
            className="btn btn-ghost"
            onClick={() => setPage((p) => Math.max(0, p - 1))}
            disabled={page === 0}
          >
            <ChevronLeft size={14} />
          </button>
          <span className="page-info">{page + 1} / {totalPages}</span>
          <button
            className="btn btn-ghost"
            onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
            disabled={page >= totalPages - 1}
          >
            <ChevronRight size={14} />
          </button>
        </div>
      )}
    </div>
  );
}

function fmtDate(s: string) {
  return new Date(s).toLocaleString(undefined, {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}
