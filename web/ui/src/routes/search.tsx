import {
  createFileRoute,
  useNavigate,
  Link,
} from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { useState, useEffect } from 'react';
import { search as searchApi } from '@/api';
import { SearchIcon, Loader2 } from 'lucide-react';
import { z } from 'zod';

const searchSchema = z.object({
  q: z.string().optional(),
});

export const Route = createFileRoute('/search')({
  validateSearch: (raw) => searchSchema.parse(raw),
  component: SearchView,
});

function SearchView() {
  const { q } = Route.useSearch();
  const navigate = useNavigate({ from: '/search' });
  const [input, setInput] = useState(q ?? '');

  useEffect(() => { setInput(q ?? ''); }, [q]);

  const { data, isFetching, error } = useQuery({
    queryKey: ['search', q],
    queryFn: () => searchApi.query(q!, { limit: 30 }),
    enabled: !!q,
  });

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const trimmed = input.trim();
    void navigate({ search: { q: trimmed || undefined } });
  }

  return (
    <div className="view">
      <header className="view-header">
        <h1>Search</h1>
        <p className="view-subtitle">Full-text &amp; RSQL queries</p>
      </header>

      <form onSubmit={handleSubmit} className="search-form">
        <div className="search-box">
          <SearchIcon size={16} className="search-icon" />
          <input
            className="search-input"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="type:note source~=github…"
            autoFocus
          />
          <button type="submit" className="btn btn-primary">Search</button>
        </div>
        <p className="search-hint">
          Supports RSQL: <code>type==note</code>, <code>source=like=*github*</code>,
          or plain text for FTS.
        </p>
      </form>

      {isFetching && (
        <div className="loading-row">
          <Loader2 size={16} className="spin" /> Searching…
        </div>
      )}

      {error && <p className="error-msg">{(error as Error).message}</p>}

      {data && (
        <section className="panel">
          <div className="panel-header">
            <h2>Results</h2>
            <span className="muted">{data.total} total</span>
          </div>
          {data.data.length === 0 && <p className="empty">No results for &ldquo;{q}&rdquo;.</p>}
          <ul className="object-list">
            {data.data.map((o) => (
              <li key={o.id}>
                <Link to="/objects/$id" params={{ id: o.id }} className="object-row">
                  <span className="obj-type">{o.type}</span>
                  <span className="obj-source obj-source--clamp">{o.source}</span>
                  <span className="obj-snippet">{o.text_content?.slice(0, 120)}</span>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
