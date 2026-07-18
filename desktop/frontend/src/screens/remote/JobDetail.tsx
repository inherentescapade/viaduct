import { useCallback, useState } from "react";
import { Download } from "lucide-react";
import { api, EV, on } from "../../lib/bridge";
import type { ExportProgressDTO, JobDTO, UserDTO } from "../../lib/types";
import { formatBytes, formatTimestamp } from "../../lib/format";
import { usePoll } from "../../lib/usePoll";
import { Banner } from "../../components/Banner";
import { Button } from "../../components/Button";
import { DeletionFeed } from "../../components/DeletionFeed";
import { RemoteProgress } from "../../components/RemoteProgress";
import { Spinner } from "../../components/Spinner";

const POLL_MS = 1500;

// feedEmpty picks the placeholder shown while the deletion feed has no rows yet.
// A DM full of undeletable system messages (call notices, joins, pins) produces
// no deletions for a while, so instead of a static "Waiting for deletions…"
// that reads as frozen, report that we're actively scanning past them.
function feedEmpty(job: JobDTO): string {
  const active = job.state === "running" || job.state === "pending";
  if (!active) return "No messages recorded.";
  const ignored = job.ignored ?? 0;
  if (job.deleted === 0 && ignored > 0) {
    return `Scanning… skipped ${ignored.toLocaleString()} system message${
      ignored === 1 ? "" : "s"
    } that can't be deleted (call notices, joins, pins).`;
  }
  return "Waiting for deletions…";
}

interface Props {
  jobId: string;
  actingAs?: string | null;
  self?: UserDTO | null;
  onClose: () => void;
  onRestarted?: (newJobId: string) => void;
}

// JobDetail polls a single dispatched job and shows a live progress bar plus the
// feed of messages it's deleting, the same experience as a local run, fed by
// polling since the job runs on a remote server. Polling backs off if the
// server becomes unreachable.
export function JobDetail({ jobId, actingAs, self, onClose, onRestarted }: Props) {
  const [job, setJob] = useState<JobDTO | null>(null);
  const [downloading, setDownloading] = useState(false);
  const [progress, setProgress] = useState<ExportProgressDTO | null>(null);
  const [saved, setSaved] = useState<string | null>(null);
  const [exportError, setExportError] = useState<string | null>(null);
  const [restarting, setRestarting] = useState(false);
  const [restartError, setRestartError] = useState<string | null>(null);

  const fetcher = useCallback(async () => {
    setJob(await api.remoteJob(jobId));
  }, [jobId]);
  const { error, refresh } = usePoll(fetcher, { baseMs: POLL_MS });

  const active = job && (job.state === "running" || job.state === "pending");
  const retryable = job && (job.state === "failed" || job.state === "canceled");

  async function restart() {
    if (restarting) return;
    setRestarting(true);
    setRestartError(null);
    try {
      const restarted = await api.remoteRetryJob(jobId);
      onRestarted?.(restarted.id);
    } catch (e) {
      setRestartError(String(e instanceof Error ? e.message : e));
    } finally {
      setRestarting(false);
    }
  }

  async function downloadExport() {
    if (downloading) return;
    setDownloading(true);
    setExportError(null);
    setSaved(null);
    setProgress(null);
    // Track byte progress streamed from the backend for this job's download.
    const off = on<ExportProgressDTO>(EV.exportProgress, (p) => {
      if (p.jobId === jobId) setProgress(p);
    });
    try {
      const path = await api.remoteExportJob(jobId);
      if (path) setSaved(path);
    } catch (e) {
      setExportError(String(e instanceof Error ? e.message : e));
    } finally {
      off();
      setDownloading(false);
      setProgress(null);
    }
  }

  // Show a byte-progress bar only for downloads large enough to take a moment;
  // small logs finish near-instantly and the button spinner is enough.
  const showProgress =
    downloading && progress !== null && progress.total > 4 * 1024 * 1024;
  const progressPct =
    progress && progress.total > 0
      ? Math.min(100, Math.round((progress.received / progress.total) * 100))
      : 0;

  return (
    <div className="rounded-2xl border border-line bg-plate/40 p-3.5">
      <div className="mb-3 flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold text-ink">
            {actingAs ? `${actingAs}'s messages` : "Your messages"}
          </div>
          <div className="truncate text-xs text-dim">
            in {job?.description || jobId}
            {job?.created ? ` · started ${formatTimestamp(job.created)}` : ""}
          </div>
        </div>
        <div className="flex shrink-0 gap-2">
          {!active && job?.hasExport && (
            <Button variant="subtle" onClick={downloadExport} disabled={downloading}>
              {downloading ? <Spinner className="border-accent/30 border-t-accent" /> : <Download />}
              Download export
            </Button>
          )}
          {active ? (
            <Button
              variant="subtle"
              onClick={async () => {
                await api.remoteCancel(jobId).catch(() => {});
                void refresh();
              }}
            >
              Stop
            </Button>
          ) : (
            <>
              {retryable && (
                <Button variant="subtle" onClick={restart} disabled={restarting}>
                  {restarting ? <Spinner className="border-accent/30 border-t-accent" /> : null}
                  Restart
                </Button>
              )}
              <Button
                variant="ghost"
                onClick={async () => {
                  await api.remoteRemoveJob(jobId).catch(() => {});
                  onClose();
                }}
              >
                Remove
              </Button>
            </>
          )}
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
        </div>
      </div>

      {error && <Banner tone="error">{error}</Banner>}
      {restartError && <Banner tone="error" onDismiss={() => setRestartError(null)}>{restartError}</Banner>}
      {exportError && <Banner tone="error" onDismiss={() => setExportError(null)}>{exportError}</Banner>}
      {saved && (
        <Banner tone="info" onDismiss={() => setSaved(null)}>
          Export saved to {saved}
        </Banner>
      )}
      {showProgress && progress && (
        <div className="mb-3 flex flex-col gap-1.5">
          <div className="flex items-center justify-between text-xs text-dim">
            <span>Downloading export…</span>
            <span className="font-mono tabular-nums">
              {formatBytes(progress.received)} / {formatBytes(progress.total)} ({progressPct}%)
            </span>
          </div>
          <div className="h-2 w-full overflow-hidden rounded-full bg-plate">
            <div
              className="h-full rounded-full bg-accent-strong transition-[width] duration-300"
              style={{ width: `${progressPct}%` }}
            />
          </div>
        </div>
      )}

      {job && (
        <div className="flex flex-col gap-3">
          <RemoteProgress
            deleted={job.deleted}
            total={job.total}
            failed={job.failed}
            state={job.state}
            ratePerSec={job.ratePerSec}
            etaMs={job.etaMs}
          />
          {job.error && <Banner tone="error">{job.error}</Banner>}
          {job.state === "done" && job.residual > 0 && (
            <Banner tone="warn">{job.residual} message(s) could not be removed (no permission).</Banner>
          )}
          <div>
            <div className="mb-1.5 text-xs font-medium uppercase tracking-wide text-dim">
              {active ? "Deleting now" : "Deleted"}
            </div>
            <DeletionFeed
              messages={job.recent ?? []}
              empty={feedEmpty(job)}
              self={self}
            />
          </div>
        </div>
      )}
    </div>
  );
}
