import { useEffect, useRef, useState } from "react";
import { cn } from "../lib/cn";
import { formatCount, formatDuration, formatEta, formatRate } from "../lib/format";
import type { ProgressDTO } from "../lib/types";
import { Spinner } from "./Spinner";

interface Props {
  progress: ProgressDTO | null;
  // import mode also surfaces skipped counts and the current channel
  variant: "live" | "import";
  stopping?: boolean;
  verifying?: boolean;
}

function Stat({ label, value, tone }: { label: string; value: string; tone?: "danger" | "default" }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-xs font-medium uppercase tracking-wide text-dim">{label}</span>
      <span
        className={cn(
          "font-mono text-lg tabular-nums",
          tone === "danger" ? "text-danger-strong" : "text-ink",
        )}
      >
        {value}
      </span>
    </div>
  );
}

export function ProgressMeter({ progress, variant, stopping, verifying }: Props) {
  const p = progress;

  // Smoothed ETA: blend raw backend estimates (α=0.15) and count down every second
  // so the display drifts gradually rather than jumping on each delete.
  const smoothedEtaRef = useRef<number | null>(null);
  const [displayEta, setDisplayEta] = useState<number>(0);

  useEffect(() => {
    const raw = p?.etaMs ?? 0;
    if (raw <= 0) {
      smoothedEtaRef.current = null;
    } else if (smoothedEtaRef.current === null) {
      smoothedEtaRef.current = raw;
    } else {
      smoothedEtaRef.current = 0.85 * smoothedEtaRef.current + 0.15 * raw;
    }
  }, [p?.etaMs]);

  useEffect(() => {
    const id = setInterval(() => {
      if (smoothedEtaRef.current !== null && smoothedEtaRef.current > 0) {
        smoothedEtaRef.current = Math.max(0, smoothedEtaRef.current - 1000);
        setDisplayEta(Math.round(smoothedEtaRef.current));
      } else {
        setDisplayEta(0);
      }
    }, 1000);
    return () => clearInterval(id);
  }, []);

  const processed = p ? p.deleted + p.failed + p.skipped : 0;
  const total = p?.total ?? 0;
  const rawPct = total > 0 ? Math.min(100, (processed / total) * 100) : 0;
  // During verification the bar pins at 100% with a spinner.
  const pct = verifying ? 100 : rawPct;

  // Before the engine has counted anything (deep-index wait), show an
  // indeterminate "preparing" state instead of a frozen 0%.
  const indexing = !verifying && (!p || p.starting || (total === 0 && !p.done));

  const ignored = p?.ignored ?? 0;
  // A DM full of call notices / joins deletes nothing for a while; say we're
  // actively scanning past them so the bar sitting at 0% doesn't read as frozen.
  const scanningSystem =
    !verifying && !indexing && !p?.done && (p?.deleted ?? 0) === 0 && ignored > 0;

  return (
    <div className="flex flex-col gap-4">
      <div>
        <div className="mb-1.5 flex items-center justify-between text-sm">
          <span className="flex items-center gap-2 font-medium text-ink">
            {verifying && !stopping && <Spinner />}
            {stopping
              ? "Stopping…"
              : verifying
                ? "Verifying: clearing any stragglers…"
                : indexing
                  ? "Preparing: Discord is indexing your history…"
                  : scanningSystem
                    ? "Scanning: skipping system messages that can't be deleted…"
                    : variant === "import"
                      ? p?.channel
                        ? `Deleting from ${p.channel}`
                        : "Deleting…"
                      : "Deleting messages…"}
          </span>
          <span className="font-mono tabular-nums text-dim">
            {indexing ? "" : `${pct.toFixed(0)}%`}
          </span>
        </div>

        <div className="h-3 overflow-hidden rounded-full bg-ink/5">
          {indexing ? (
            <div className="relative h-full w-full overflow-hidden">
              <div className="absolute inset-y-0 left-0 w-1/3 animate-indeterminate rounded-full bg-gradient-to-r from-transparent via-white to-transparent" />
            </div>
          ) : (
            <div
              className="h-full rounded-full bg-gradient-to-r from-accent to-accent-strong transition-all duration-300 ease-out"
              style={{ width: `${pct}%` }}
            />
          )}
        </div>
      </div>

      <div
        className={cn(
          "grid gap-3 rounded-xl bg-plate/50 p-3.5",
          variant === "import" ? "grid-cols-3 sm:grid-cols-6" : "grid-cols-2 sm:grid-cols-5",
        )}
      >
        <Stat label="Deleted" value={formatCount(p?.deleted ?? 0)} />
        <Stat label="Total" value={formatCount(total)} />
        {variant === "import" && <Stat label="Skipped" value={formatCount(p?.skipped ?? 0)} />}
        {ignored > 0 && <Stat label="Skipped (system)" value={formatCount(ignored)} />}
        <Stat label="Failed" value={formatCount(p?.failed ?? 0)} tone={p?.failed ? "danger" : "default"} />
        <Stat label="Rate" value={formatRate(p?.ratePerSec ?? 0)} />
        {variant === "import" ? (
          <Stat label="Elapsed" value={formatDuration(p?.elapsedMs ?? 0)} />
        ) : (
          <Stat label="ETA" value={formatEta(displayEta)} />
        )}
      </div>
    </div>
  );
}
