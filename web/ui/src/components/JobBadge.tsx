import type { JobStatus } from '@/types';

interface Props { status: JobStatus; }

const LABELS: Record<JobStatus, string> = {
  pending: 'pending',
  running: 'running',
  completed: 'done',
  failed: 'failed',
};

export function JobBadge({ status }: Props) {
  return <span className={`badge badge-${status}`}>{LABELS[status]}</span>;
}
