import { useCallback, useState } from "react";
import { api } from "../../lib/bridge";
import { formatEta, formatTimeShort } from "../../lib/format";
import { usePoll } from "../../lib/usePoll";
import type { JobDTO, PingDTO, RemoteDTO, UserDTO } from "../../lib/types";
import { remoteMonitorApi } from "../../lib/monitorApi";
import { Banner } from "../../components/Banner";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { StepHeader } from "../../components/StepHeader";
import { JobDetail } from "./JobDetail";
import { MonitorPanel } from "./MonitorPanel";

const POLL_MS = 3000;

interface Props {
  remote: RemoteDTO;
  user: UserDTO;
  onForget: () => void;
  onStatusChange?: () => void;
}

const stateTone: Record<string, string> = {
  running: "text-accent-strong",
  canceling: "text-warn",
  done: "text-dim",
  failed: "text-danger-strong",
  canceled: "text-warn",
  pending: "text-dim",
};

// Tasker is the connected dashboard: server status, dispatched tasks, and
// server-side monitors. Tasks/monitors live on the remote server, so we poll
// for them rather than using the local event bus.
export function Tasker({ remote, user, onForget, onStatusChange }: Props) {
  const [ping, setPing] = useState<PingDTO | null>(null);
  const [jobs, setJobs] = useState<JobDTO[]>([]);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [selectedJob, setSelectedJob] = useState<string | null>(null);

  const fetcher = useCallback(async () => {
    const [p, j] = await Promise.all([api.remotePing(), api.remoteJobs()]);
    setPing(p);
    setJobs(j);
  }, []);
  // Polling backs off when the server is unreachable, and surfaces a friendly
  // error rather than the raw Go message.
  const { error: pingError, refresh } = usePoll(fetcher, { baseMs: POLL_MS });

  async function cancelJob(id: string) {
    setBusyId(id);
    try {
      // The server flips the job to "canceling" the moment it signals the stop,
      // so apply that real response right away instead of waiting up to a full
      // poll interval for the row to stop saying "running".
      const canceling = await api.remoteCancel(id);
      setJobs((cur) => cur.map((j) => (j.id === id ? canceling : j)));
    } catch {
      /* surfaced on next poll */
    }
    setBusyId(null);
    void refresh();
  }

  async function removeJob(id: string) {
    setBusyId(id);
    try {
      await api.remoteRemoveJob(id);
    } catch {
      /* surfaced on next poll */
    }
    setBusyId(null);
    if (selectedJob === id) setSelectedJob(null);
    void refresh();
  }

  async function retryJob(id: string) {
    setBusyId(id);
    try {
      const restarted = await api.remoteRetryJob(id);
      setSelectedJob(restarted.id);
    } catch {
      /* surfaced on next poll */
    }
    setBusyId(null);
    void refresh();
  }

  async function sendToken() {
    try {
      await api.remoteConnect();
    } catch {
      /* surfaced on next poll */
    }
    onStatusChange?.();
    void refresh();
  }

  async function forget() {
    await api.forgetRemote().catch(() => {});
    onForget();
  }

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-3 px-5 pb-6">
      {/* Status header */}
      <Card>
        <div className="flex items-start justify-between gap-3">
          <div>
            <div className="text-xs font-semibold uppercase tracking-[0.14em] text-accent-strong">Server</div>
            <div className="mt-0.5 text-base font-semibold text-ink">{remote.address}</div>
            <div className="mt-1 text-sm text-dim">
              {ping?.actingAs
                ? `Acting as ${ping.actingAs.username}`
                : ping?.hasToken
                  ? "Token loaded"
                  : "No Discord token sent yet"}
              {ping ? ` · ${ping.jobs} task(s) · ${ping.monitors} monitor(s)` : ""}
            </div>
          </div>
          <div className="flex gap-2">
            <Button variant="subtle" onClick={() => void refresh()}>
              Refresh
            </Button>
            <Button variant="ghost" onClick={forget}>
              Forget
            </Button>
          </div>
        </div>
        {pingError && (
          <div className="mt-3">
            <Banner tone="error">{pingError}</Banner>
          </div>
        )}
        {ping && !ping.hasToken && (
          <div className="mt-3">
            <Banner tone="warn">
              The server has no Discord token. Send one so it can act as you.{" "}
              <button className="font-semibold underline" onClick={sendToken}>
                Send token
              </button>
            </Banner>
          </div>
        )}
      </Card>

      {/* Tasks */}
      <Card>
        <StepHeader
          title="Tasks"
          subtitle="One-off deletions dispatched from the Live tab. They run on the server."
        />
        {selectedJob && (
          <div className="mb-3">
            <JobDetail
              key={selectedJob}
              jobId={selectedJob}
              actingAs={ping?.actingAs?.username ?? null}
              self={user}
              onClose={() => setSelectedJob(null)}
              onRestarted={(newId) => {
                setSelectedJob(newId);
                void refresh();
              }}
            />
          </div>
        )}
        {jobs.length === 0 ? (
          <p className="text-sm text-dim">
            No tasks yet. Create one from the <span className="text-ink">Live deletion</span> tab, and it will be
            dispatched here.
          </p>
        ) : (
          <div className="space-y-2">
            {jobs
              .slice()
              .reverse()
              .map((j) => (
                <div
                  key={j.id}
                  className={`flex items-center justify-between gap-3 rounded-xl bg-plate/50 p-3 ${selectedJob === j.id ? "ring-1 ring-accent/40" : ""}`}
                >
                  <button
                    className="min-w-0 flex-1 text-left"
                    onClick={() => setSelectedJob((cur) => (cur === j.id ? null : j.id))}
                  >
                    <div className="truncate text-sm font-medium text-ink">{j.description || j.kind}</div>
                    <div className="text-xs text-dim">
                      <span className={stateTone[j.state] ?? "text-dim"}>{j.state}</span>
                      {" · "}
                      {j.deleted}/{j.total} deleted
                      {j.failed > 0 ? ` · ${j.failed} failed` : ""}
                      {j.state === "running" && j.etaMs > 0 ? ` · ${formatEta(j.etaMs)} left` : ""}
                      {j.created ? ` · ${formatTimeShort(j.created)}` : ""}
                      {" · "}
                      <span className="text-accent-strong/80">view feed</span>
                    </div>
                  </button>
                  {j.state === "running" || j.state === "pending" ? (
                    <Button variant="subtle" disabled={busyId === j.id} onClick={() => cancelJob(j.id)}>
                      Cancel
                    </Button>
                  ) : j.state === "canceling" ? (
                    <Button variant="subtle" disabled>
                      Canceling…
                    </Button>
                  ) : j.state === "failed" || j.state === "canceled" ? (
                    <div className="flex gap-2">
                      <Button variant="subtle" disabled={busyId === j.id} onClick={() => retryJob(j.id)}>
                        Restart
                      </Button>
                      <Button variant="ghost" disabled={busyId === j.id} onClick={() => removeJob(j.id)}>
                        Remove
                      </Button>
                    </div>
                  ) : (
                    <Button variant="ghost" disabled={busyId === j.id} onClick={() => removeJob(j.id)}>
                      Remove
                    </Button>
                  )}
                </div>
              ))}
          </div>
        )}
      </Card>

      {/* Monitors run on the server, so their list belongs here. Creating them
          lives on the Monitors tab, so this view is list-only. */}
      <Card>
        <StepHeader
          title="Monitors"
          subtitle="Automatic cleanups running on the server, 24/7. Toggle each on or off; create new ones on the Monitors tab."
        />
        <MonitorPanel mapi={remoteMonitorApi} self={user} allowNew={false} />
      </Card>
    </div>
  );
}
