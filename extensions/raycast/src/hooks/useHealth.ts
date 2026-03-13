import { useEffect, useState } from "react";
import { checkHealth } from "../api/endpoints";

export interface UseHealthResult {
  isOnline: boolean;
  isChecking: boolean;
  recheck: () => void;
}

export function useHealth(): UseHealthResult {
  const [isOnline, setIsOnline] = useState(false);
  const [isChecking, setIsChecking] = useState(true);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setIsChecking(true);
    checkHealth()
      .then(() => {
        if (!cancelled) setIsOnline(true);
      })
      .catch(() => {
        if (!cancelled) setIsOnline(false);
      })
      .finally(() => {
        if (!cancelled) setIsChecking(false);
      });
    return () => {
      cancelled = true;
    };
  }, [tick]);

  return {
    isOnline,
    isChecking,
    recheck: () => setTick((n) => n + 1),
  };
}
