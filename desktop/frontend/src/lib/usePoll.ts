import { useCallback, useEffect, useRef, useState } from "react";
import { friendlyError } from "./friendlyError";

interface Options {
  baseMs: number;
  // Cap the backed-off interval after repeated failures.
  maxMs?: number;
}

interface Poll {
  error: string | null;
  // failing is true once a fetch has failed (so the UI can show a calmer state).
  failing: boolean;
  // refresh runs an immediate fetch and resets the cadence to baseMs.
  refresh: () => void;
}

// usePoll runs fetcher on an interval, pausing while the window is hidden and
// BACKING OFF on failure: each consecutive error doubles the delay up to maxMs,
// so a dead connection settles to an occasional retry instead of hammering at
// the fast rate. A success resets to baseMs. fetcher should throw on failure.
export function usePoll(fetcher: () => Promise<void>, { baseMs, maxMs = 30000 }: Options): Poll {
  const [error, setError] = useState<string | null>(null);
  const [failing, setFailing] = useState(false);

  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;
  const intervalRef = useRef(baseMs);
  const aliveRef = useRef(true);

  const runOnce = useCallback(async () => {
    try {
      await fetcherRef.current();
      intervalRef.current = baseMs;
      if (aliveRef.current) {
        setError(null);
        setFailing(false);
      }
    } catch (e) {
      intervalRef.current = Math.min(intervalRef.current * 2, maxMs);
      if (aliveRef.current) {
        setError(friendlyError(e));
        setFailing(true);
      }
    }
  }, [baseMs, maxMs]);

  const refresh = useCallback(() => {
    intervalRef.current = baseMs;
    void runOnce();
  }, [baseMs, runOnce]);

  useEffect(() => {
    aliveRef.current = true;
    let timer: number | undefined;
    const loop = async () => {
      if (!document.hidden) await runOnce();
      if (aliveRef.current) timer = window.setTimeout(loop, intervalRef.current);
    };
    void loop();
    return () => {
      aliveRef.current = false;
      if (timer) window.clearTimeout(timer);
    };
  }, [runOnce]);

  return { error, failing, refresh };
}
