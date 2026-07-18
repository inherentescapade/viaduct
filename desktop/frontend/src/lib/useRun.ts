import { useCallback, useEffect, useRef, useState } from "react";
import { EV, on } from "./bridge";
import type { FailReasonDTO, FinishedPayload, MessageDTO, ProgressDTO } from "./types";

// useRun centralises subscription to a deletion/dry-run's lifecycle events. It
// is mounted once at the wizard level (not per-step) so progress that arrives
// while the user is mid-transition is never dropped.
export interface RunState {
  progress: ProgressDTO | null;
  error: string | null;
  finished: FinishedPayload | null;
  failures: FailReasonDTO[];
  messages: MessageDTO[];
  notices: string[];
  enumCount: number | null;
  verifying: boolean;
  reset: () => void;
}

export function useRun(): RunState {
  const [progress, setProgress] = useState<ProgressDTO | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [finished, setFinished] = useState<FinishedPayload | null>(null);
  const [failures, setFailures] = useState<FailReasonDTO[]>([]);
  const [messages, setMessages] = useState<MessageDTO[]>([]);
  const [enumCount, setEnumCount] = useState<number | null>(null);
  const [verifying, setVerifying] = useState(false);
  const [notices, setNotices] = useState<string[]>([]);

  // Buffer dry-run messages and flush on a frame to avoid re-rendering on every
  // single emit during a fast enumeration.
  const buffer = useRef<MessageDTO[]>([]);

  useEffect(() => {
    const offs = [
      on<ProgressDTO>(EV.progress, (p) => setProgress(p)),
      on<{ message: string }>(EV.error, (e) => setError(e.message)),
      on<FinishedPayload>(EV.finished, (f) => setFinished(f)),
      on<FailReasonDTO[]>(EV.importDone, (f) => setFailures(f ?? [])),
      on<{ count: number }>(EV.enumDone, (d) => setEnumCount(d.count)),
      on(EV.verifying, () => setVerifying(true)),
      on<{ message: string }>(EV.notice, (n) =>
        setNotices((prev) => [...prev, n.message].slice(-50)),
      ),
      on<MessageDTO>(EV.message, (m) => {
        buffer.current = [...buffer.current, m].slice(-1000);
        setMessages(buffer.current);
      }),
    ];
    return () => offs.forEach((off) => off());
  }, []);

  const reset = useCallback(() => {
    buffer.current = [];
    setProgress(null);
    setError(null);
    setFinished(null);
    setFailures([]);
    setMessages([]);
    setNotices([]);
    setEnumCount(null);
    setVerifying(false);
  }, []);

  return { progress, error, finished, failures, messages, notices, enumCount, verifying, reset };
}
