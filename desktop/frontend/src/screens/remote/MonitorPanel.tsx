import { useCallback, useEffect, useState } from "react";
import { api } from "../../lib/bridge";
import { friendlyError } from "../../lib/friendlyError";
import type { MonitorApi } from "../../lib/monitorApi";
import type { ChannelDTO, MonitorDTO, UserDTO } from "../../lib/types";
import { usePoll } from "../../lib/usePoll";
import { Banner } from "../../components/Banner";
import { Button } from "../../components/Button";
import { DeletionFeed } from "../../components/DeletionFeed";
import { Spinner } from "../../components/Spinner";
import { NewMonitorForm } from "./NewMonitorForm";

const POLL_MS = 2500;

// unitShort renders a retention unit as a compact suffix (m/h/d/w).
function unitShort(unit: string): string {
  switch (unit) {
    case "minutes":
      return "m";
    case "hours":
      return "h";
    case "weeks":
      return "w";
    default:
      return "d";
  }
}

function fmtWhen(iso: string): string {
  if (!iso) return "never";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "never";
  return d.toLocaleString();
}

// relFuture renders a coarse "in 2h 5m" / "due now" for an upcoming time.
function relFuture(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const ms = d.getTime() - Date.now();
  if (ms <= 0) return "due now";
  const mins = Math.round(ms / 60000);
  if (mins < 60) return `in ${mins}m`;
  const h = Math.floor(mins / 60);
  const m = mins % 60;
  return m ? `in ${h}h ${m}m` : `in ${h}h`;
}

// scopeId normalizes a monitor's stored scope ("" is accepted server-side as an
// alias for the "@me" DM scope).
function scopeId(m: MonitorDTO): string {
  return m.scope === "" ? "@me" : m.scope;
}

interface Props {
  mapi: MonitorApi;
  self?: UserDTO | null;
  // allowNew shows the "New monitor" / "Edit" affordances. The Server tab lists
  // monitors read-only (creation and editing live on the Monitors tab), so it
  // passes false.
  allowNew?: boolean;
}

// MonitorPanel lists monitor policies — what they target, which channels they
// touch, and their run history — with on/off toggles, editing, and an optional
// create form. It is backed by a MonitorApi, so the same UI serves local and
// server monitors.
export function MonitorPanel({ mapi, self, allowNew = true }: Props) {
  const [monitors, setMonitors] = useState<MonitorDTO[]>([]);
  const [showNew, setShowNew] = useState(false);
  const [editing, setEditing] = useState<MonitorDTO | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [detailsId, setDetailsId] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  // Names for display: scope IDs -> server names, and per-scope channel lists so
  // a monitor's channel IDs can be shown as real channel/conversation names.
  const [guildNames, setGuildNames] = useState<Record<string, string>>({});
  const [chanCache, setChanCache] = useState<Record<string, ChannelDTO[]>>({});
  const [chanLoading, setChanLoading] = useState<string | null>(null);

  const fetcher = useCallback(async () => {
    setMonitors(await mapi.list());
  }, [mapi]);
  // Polling backs off automatically when the server is unreachable.
  const { error: pollError, refresh } = usePoll(fetcher, { baseMs: POLL_MS });
  const error = actionError ?? pollError;

  // Resolve guild names once so monitors show "My Server" instead of a raw ID.
  // Best-effort: on failure the raw scope still renders.
  useEffect(() => {
    let alive = true;
    api
      .listGuilds()
      .then((gs) => {
        if (!alive) return;
        setGuildNames(Object.fromEntries(gs.map((g) => [g.id, g.isDm ? "Direct Messages" : g.name])));
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  async function toggle(m: MonitorDTO) {
    setBusyId(m.id);
    try {
      await mapi.set({
        id: m.id,
        name: m.name,
        enabled: !m.enabled,
        scope: m.scope,
        mode: m.mode,
        channels: m.channels ?? [],
        maxAgeAmount: m.maxAgeAmount,
        maxAgeUnit: m.maxAgeUnit,
        intervalHrs: m.intervalHrs,
        includePinned: m.includePinned,
      });
    } catch (e) {
      setActionError(friendlyError(e));
    }
    setBusyId(null);
    refresh();
  }

  async function remove(id: string) {
    setBusyId(id);
    try {
      await mapi.remove(id);
    } catch (e) {
      setActionError(friendlyError(e));
    }
    setBusyId(null);
    if (editing?.id === id) setEditing(null);
    refresh();
  }

  // toggleDetails expands the "which chats" section, lazily fetching the scope's
  // channel list the first time so IDs can be shown as names.
  async function toggleDetails(m: MonitorDTO) {
    const scope = scopeId(m);
    const opening = detailsId !== m.id;
    setDetailsId(opening ? m.id : null);
    if (opening && (m.channels?.length ?? 0) > 0 && !chanCache[scope]) {
      setChanLoading(m.id);
      try {
        const list = await api.listChannels(scope);
        setChanCache((c) => ({ ...c, [scope]: list }));
      } catch {
        // Names unavailable (e.g. signed out locally) — fall back to raw IDs.
        setChanCache((c) => ({ ...c, [scope]: [] }));
      }
      setChanLoading(null);
    }
  }

  return (
    <div className="flex flex-col gap-3">
      {error && <Banner tone="error" onDismiss={() => setActionError(null)}>{error}</Banner>}

      {allowNew && (
        <div className="flex justify-end">
          <Button
            onClick={() => {
              setShowNew((v) => !v);
              setEditing(null);
            }}
            disabled={showNew}
          >
            New monitor
          </Button>
        </div>
      )}

      {allowNew && showNew && (
        <div className="rounded-2xl border border-line bg-plate/40 p-3.5">
          <NewMonitorForm
            mapi={mapi}
            onCancel={() => setShowNew(false)}
            onSaved={() => {
              setShowNew(false);
              void refresh();
            }}
          />
        </div>
      )}

      {allowNew && editing && (
        <div className="rounded-2xl border border-line bg-plate/40 p-3.5">
          <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-dim">
            Editing “{editing.name}”
          </p>
          <NewMonitorForm
            key={editing.id}
            mapi={mapi}
            initial={editing}
            onCancel={() => setEditing(null)}
            onSaved={() => {
              setEditing(null);
              void refresh();
            }}
          />
        </div>
      )}

      {monitors.length === 0 ? (
        <p className="text-sm text-dim">
          {allowNew ? "No monitors yet." : "No monitors yet. Create one on the Monitors tab."}
        </p>
      ) : (
        <div className="space-y-2">
          {monitors.map((m) => {
            const scope = scopeId(m);
            const isDM = scope === "@me";
            const target = isDM ? "Direct Messages" : (guildNames[scope] ?? m.scope);
            const noun = isDM ? "conversation" : "channel";
            const nChans = m.channels?.length ?? 0;
            // coverage summarizes which chats the policy touches; the details
            // expander below lists them by name.
            const coverage =
              m.mode === "include"
                ? `only ${nChans} ${noun}${nChans === 1 ? "" : "s"}`
                : nChans > 0
                  ? `all ${noun}s except ${nChans}`
                  : `all ${noun}s`;
            const hasFeed = (m.recent?.length ?? 0) > 0;
            const schedule = m.running
              ? "running now…"
              : m.enabled
                ? `next run ${relFuture(m.nextRun) || "soon"}`
                : "paused";
            const chans = chanCache[scope];
            const chanName = (id: string) => {
              const c = chans?.find((x) => x.id === id);
              return c ? (isDM ? c.name : `#${c.name}`) : id;
            };
            return (
              <div key={m.id} className="rounded-xl bg-plate/50 p-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-sm font-medium text-ink">{m.name}</span>
                      {m.running && (
                        <span className="shrink-0 rounded-full bg-accent-soft px-2 py-0.5 text-[10px] font-semibold text-accent-strong">
                          running
                        </span>
                      )}
                    </div>
                    <div className="text-xs text-dim">
                      {target} ·{" "}
                      <button
                        className="text-accent-strong/80 underline decoration-dotted underline-offset-2 hover:text-accent-strong"
                        onClick={() => void toggleDetails(m)}
                        title={detailsId === m.id ? "Hide affected chats" : "Show affected chats"}
                      >
                        {coverage}
                      </button>{" "}
                      · keep ≤ {m.maxAgeAmount}{unitShort(m.maxAgeUnit)} · every {m.intervalHrs}h ·{" "}
                      <span className={m.running ? "text-accent-strong" : ""}>{schedule}</span>
                    </div>
                    <div className="text-xs text-dim">
                      last run: {fmtWhen(m.lastRun)}
                      {m.lastRun ? ` · ${m.lastDeleted} deleted` : ""}
                      {hasFeed && (
                        <button
                          className="ml-2 text-accent-strong/80 hover:text-accent-strong"
                          onClick={() => setExpanded((cur) => (cur === m.id ? null : m.id))}
                        >
                          {expanded === m.id ? "hide activity" : "view activity"}
                        </button>
                      )}
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Button
                      variant={m.enabled ? "subtle" : "primary"}
                      disabled={busyId === m.id}
                      onClick={() => toggle(m)}
                    >
                      {m.enabled ? "Turn off" : "Turn on"}
                    </Button>
                    {allowNew && (
                      <Button
                        variant="subtle"
                        disabled={busyId === m.id}
                        onClick={() => {
                          setShowNew(false);
                          setEditing(m);
                        }}
                      >
                        Edit
                      </Button>
                    )}
                    <Button variant="ghost" disabled={busyId === m.id} onClick={() => remove(m.id)}>
                      Delete
                    </Button>
                  </div>
                </div>
                {detailsId === m.id && (
                  <div className="mt-2 rounded-lg bg-plate/60 p-2.5 text-xs text-dim">
                    {nChans === 0 ? (
                      <p>
                        Cleans every {noun} in {target}, including ones that appear later.
                      </p>
                    ) : chanLoading === m.id ? (
                      <span className="flex items-center gap-2">
                        <Spinner /> Loading {noun} names…
                      </span>
                    ) : (
                      <>
                        <p className="mb-1.5">
                          {m.mode === "include"
                            ? `Cleans only these ${noun}s — everything else is left alone:`
                            : `Cleans every ${noun} in ${target} except these protected ones:`}
                        </p>
                        <div className="flex flex-wrap gap-1">
                          {(m.channels ?? []).map((id) => (
                            <span
                              key={id}
                              className="rounded-md bg-ink/5 px-1.5 py-0.5 font-medium text-ink"
                            >
                              {chanName(id)}
                            </span>
                          ))}
                        </div>
                      </>
                    )}
                  </div>
                )}
                {m.running && (
                  <div className="mt-2">
                    <div className="mb-1 flex justify-between text-[11px] text-dim">
                      <span>deleting…</span>
                      <span className="font-mono tabular-nums">
                        {m.lastDeleted}
                        {m.total > 0 ? ` / ${m.total}` : ""}
                      </span>
                    </div>
                    <div className="h-1.5 overflow-hidden rounded-full bg-plate">
                      <div
                        className="h-full rounded-full bg-accent-strong transition-[width] duration-500"
                        style={{
                          width:
                            m.total > 0
                              ? `${Math.min(100, Math.round((m.lastDeleted / m.total) * 100))}%`
                              : "100%",
                          opacity: m.total > 0 ? 1 : 0.5,
                        }}
                      />
                    </div>
                  </div>
                )}
                {expanded === m.id && hasFeed && (
                  <div className="mt-2">
                    <DeletionFeed messages={m.recent} empty="No recent deletions." self={self} />
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
