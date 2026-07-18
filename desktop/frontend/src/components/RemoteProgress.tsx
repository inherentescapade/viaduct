import { formatCount, formatEta, formatRate } from "../lib/format";

interface Props {
  deleted: number;
  total: number;
  failed?: number;
  state: string;
  ratePerSec?: number;
  etaMs?: number;
}

const stateLabel: Record<string, string> = {
  pending: "Starting…",
  running: "Deleting",
  canceling: "Stopping…",
  done: "Done",
  failed: "Failed",
  canceled: "Stopped",
};

// RemoteProgress is a simple, pretty progress bar for a dispatched job. Rate
// and ETA are computed server-side (from Created + current counts) and
// carried in on each poll, since the client only sees periodic snapshots
// rather than a live stream.
export function RemoteProgress({ deleted, total, failed = 0, state, ratePerSec = 0, etaMs = 0 }: Props) {
  const pct = total > 0 ? Math.min(100, Math.round((deleted / total) * 100)) : state === "done" ? 100 : 0;
  const barColor =
    state === "failed" ? "bg-danger" : state === "canceled" || state === "canceling" ? "bg-warn" : "bg-accent-strong";
  // No known total yet but already running → sweep an indeterminate bar rather
  // than show an empty track.
  const indeterminate = state === "running" && total === 0;

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-end justify-between">
        <div className="font-mono text-2xl font-semibold tabular-nums text-ink">
          {formatCount(deleted)}
          <span className="text-base text-dim"> / {formatCount(total)}</span>
        </div>
        <div className="text-sm text-dim">
          {stateLabel[state] ?? state}
          {failed > 0 && <span className="text-danger-strong"> · {formatCount(failed)} failed</span>}
          {state === "running" && ratePerSec > 0 && <span> · {formatRate(ratePerSec)}</span>}
          {state === "running" && etaMs > 0 && <span> · {formatEta(etaMs)} left</span>}
        </div>
      </div>
      <div className="h-2.5 w-full overflow-hidden rounded-full bg-plate">
        {indeterminate ? (
          <div className="relative h-full w-full overflow-hidden">
            <div className="absolute inset-y-0 left-0 w-1/3 animate-indeterminate rounded-full bg-gradient-to-r from-transparent via-white to-transparent" />
          </div>
        ) : (
          <div
            className={`h-full rounded-full ${barColor} transition-[width] duration-500`}
            style={{ width: `${pct}%` }}
          />
        )}
      </div>
    </div>
  );
}
