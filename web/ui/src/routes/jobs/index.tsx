import { createFileRoute } from '@tanstack/react-router';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { jobs as jobsApi } from '@/api';
import { JobBadge } from '@/components/JobBadge';
import { Loader2, RefreshCw, ChevronLeft, ChevronRight } from 'lucide-react';
import type { JobStatus } from '@/types';

export const Route = createFileRoute('/jobs/')({ component: JobList });

const STATUSES: { value: '' | JobStatus; label: string }[] = [
  { value: '', label: 'All' },
  { value: 'running', label: 'Running' },
  { value: 'pending', label: 'Pending' },
  { value: 'completed', label: 'Completed' },
  { value: 'failed', label: 'Failed' },
];

const PAGE_SIZE = 20;

function JobList() {
  const [status, setStatus] = useState<'' | JobStatus>('');
  const [page, setPage] = useState(0);
  const qc = useQueryClient();

  const { data, isFetching, error } = useQuery({
    queryKey: ['jobs', status, page],
    queryFn: () =>
      jobsApi.list({ status: status || undefined, limit: PAGE_SIZE, offset: page * PAGE_SIZE }),
    refetchInterval: status === 'running' || status === '' ? 5000 : false,
  });

  const retryMut = useMutation({
    mutationFn: (id: string) => jobsApi.retry(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['jobs'] }),
  });

  const totalPages = Math.ceil((data?.total ?? 0) / PAGE_SIZE);

  return (
    <div className="view">
      <header className="view-header">
        <div className="header-row">
          <h1>Jobs</h1>
          <button
            className="btn btn-ghost"
            onClick={() => qc.invalidateQueries({ queryKey: ['jobs'] })}
          >
            <RefreshCw size={14} className={isFetching ? 'spin' : ''} />
            Refresh
          </button>
        </div>
        <p className="view-subtitle">
          {data ? `${data.total.toLocaleString()} total` : '—'}
        </p>
      </header>

      <div className="toolbar">
        <div className="tab-group">
          {STATUSES.map(({ value, label }) => (
            <button
              key={value}
              className={`tab${status === value ? ' active' : ''}`}
              onClick={() => { setStatus(value); setPage(0); }}
            >
              {label}
            </button>
          ))}
        </div>
        {isFetching && <Loader2 size={14} className="spin" />}
      </div>

      {error && <p className="error-msg">{(error as Error).message}</p>}

      <section className="panel">
        {data?.data.length === 0 && <p className="empty">No jobs found.</p>}
        <ul className="job-list">
          {data?.data.map((j) => (
            <li key={j.id} className="job-row">
              <JobBadge status={j.status} />
              <span className="job-type">{j.type}</span>
              <span className="job-pipeline muted">{j.pipeline}</span>
              <span className="job-time muted">{fmtDate(j.created_at)}</span>
              {j.error && <span className="job-error" title={j.error}>!</span>}
              {j.status === 'failed' && (
                <button
                  className="btn btn-ghost btn-xs"
                  onClick={() => retryMut.mutate(j.id)}
                  disabled={retryMut.isPending}
                >
                  Retry
                </button>
              )}
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
