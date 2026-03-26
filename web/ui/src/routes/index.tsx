import { createFileRoute, Link } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { objects, jobs } from '@/api';
import { JobBadge } from '@/components/JobBadge';
import { Clock, Box } from 'lucide-react';

export const Route = createFileRoute('/')({ component: Dashboard });

function Dashboard() {
  const { data: recObjs } = useQuery({
    queryKey: ['objects', 'recent'],
    queryFn: () => objects.list({ limit: 8 }),
  });
  const { data: activeJobs } = useQuery({
    queryKey: ['jobs', 'active'],
    queryFn: () => jobs.list({ status: 'running', limit: 5 }),
    refetchInterval: 3000,
  });
  const { data: pendingJobs } = useQuery({
    queryKey: ['jobs', 'pending'],
    queryFn: () => jobs.list({ status: 'pending', limit: 5 }),
    refetchInterval: 5000,
  });

  const runningCount = activeJobs?.total ?? 0;
  const pendingCount = pendingJobs?.total ?? 0;
  const objectCount = recObjs?.total ?? 0;

  return (
    <div className="view">
      <header className="view-header">
        <h1>Dashboard</h1>
        <p className="view-subtitle">Knowledge base at a glance</p>
      </header>

      <div className="stat-row">
        <div className="stat-card">
          <Box size={20} className="stat-icon" />
          <div>
            <div className="stat-value">{objectCount.toLocaleString()}</div>
            <div className="stat-label">Objects</div>
          </div>
        </div>
        <div className="stat-card accent">
          <Clock size={20} className="stat-icon" />
          <div>
            <div className="stat-value">{runningCount}</div>
            <div className="stat-label">Running jobs</div>
          </div>
        </div>
        <div className="stat-card">
          <Clock size={20} className="stat-icon muted" />
          <div>
            <div className="stat-value">{pendingCount}</div>
            <div className="stat-label">Queued jobs</div>
          </div>
        </div>
      </div>

      <section className="panel">
        <div className="panel-header">
          <h2>Recent Objects</h2>
          <Link to="/objects" className="panel-link">View all</Link>
        </div>
        {recObjs?.data.length === 0 && <p className="empty">No objects yet.</p>}
        <ul className="object-list">
          {recObjs?.data.map((o) => (
            <li key={o.id}>
              <Link to="/objects/$id" params={{ id: o.id }} className="object-row">
                <span className="obj-type">{o.type}</span>
                <span className="obj-source">{o.source}</span>
                <span className="obj-time">{fmtDate(o.created_at)}</span>
              </Link>
            </li>
          ))}
        </ul>
      </section>

      {(runningCount > 0 || pendingCount > 0) && (
        <section className="panel">
          <div className="panel-header">
            <h2>Active Jobs</h2>
            <Link to="/jobs" className="panel-link">View all</Link>
          </div>
          <ul className="job-list">
            {[...(activeJobs?.data ?? []), ...(pendingJobs?.data ?? [])].slice(0, 5).map((j) => (
              <li key={j.id} className="job-row">
                <JobBadge status={j.status} />
                <span className="job-type">{j.type}</span>
                <span className="job-pipeline">{j.pipeline}</span>
                <span className="job-time">{fmtDate(j.created_at)}</span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

function fmtDate(s: string) {
  return new Date(s).toLocaleString(undefined, {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}
