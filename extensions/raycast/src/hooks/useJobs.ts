import { useEffect, useRef, useState } from "react";
import { listJobs } from "../api/endpoints";
import { Job, JobStatus } from "../api/types";

const POLL_INTERVAL_MS = 3000;
const ACTIVE_STATUSES: JobStatus[] = ["pending", "running"];

export interface UseJobsResult {
  jobs: Job[];
  isLoading: boolean;
  error: string | null;
  refresh: () => void;
}

export function useJobs(): UseJobsResult {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function fetchJobs() {
      try {
        const res = await listJobs({ limit: "50" });
        if (!cancelled) {
          setJobs(res.data ?? []);
          setError(null);

          // Only schedule next poll if active jobs remain
          const hasActive = (res.data ?? []).some((j) =>
            ACTIVE_STATUSES.includes(j.status)
          );
          if (hasActive) {
            timerRef.current = setTimeout(() => {
              if (!cancelled) setTick((n) => n + 1);
            }, POLL_INTERVAL_MS);
          }
        }
      } catch (e: unknown) {
        if (!cancelled) {
          setError((e as Error).message ?? "Failed to load jobs");
        }
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    }

    setIsLoading(true);
    fetchJobs();

    return () => {
      cancelled = true;
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [tick]);

  return {
    jobs,
    isLoading,
    error,
    refresh: () => setTick((n) => n + 1),
  };
}
