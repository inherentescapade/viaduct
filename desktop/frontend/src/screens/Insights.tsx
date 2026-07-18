import { useCallback, useEffect, useState } from "react";
import type { LucideIcon } from "lucide-react";
import {
  Activity,
  CalendarClock,
  Hash,
  Image as ImageIcon,
  Paperclip,
  RefreshCw,
  ServerOff,
  Target,
  Trash2,
  TriangleAlert,
  Type,
} from "lucide-react";
import { api } from "../lib/bridge";
import type { BucketDTO, ChannelStatDTO, FailReasonDTO, LogStatsDTO, RunStatDTO } from "../lib/types";
import type { RemoteStatus } from "../lib/useRemoteStatus";
import { formatCount, formatTimestamp } from "../lib/format";
import { cn } from "../lib/cn";
import { Card } from "../components/Card";
import { Badge } from "../components/ui/badge";
import { Separator } from "../components/ui/separator";
import { Button } from "../components/Button";
import { Spinner } from "../components/Spinner";
import { LogSearch } from "../components/LogSearch";
import { RunsList } from "../components/RunsList";

interface Props {
  remote: RemoteStatus;
}

// Insights turns Viaduct's append-only deletion logs into an at-a-glance
// dashboard: how much you've removed, where, when, and what failed. Local and
// server logs are merged into one combined view when self-hosting.
export function Insights({ remote }: Props) {
  const [stats, setStats] = useState<LogStatsDTO | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [serverNote, setServerNote] = useState<string | null>(null);
  const [includesServer, setIncludesServer] = useState(false);
  // Bumped whenever logs change (a delete/purge) so the runs list refetches
  // alongside the reloaded stats.
  const [logsVersion, setLogsVersion] = useState(0);

  const load = useCallback(() => {
    setLoading(true);
    setError(null);
    setServerNote(null);
    const localP = api.logStats();
    const serverP = remote.configured
      ? api.remoteLogStats().catch((e) => {
          setServerNote(String(e instanceof Error ? e.message : e));
          return null;
        })
      : Promise.resolve<LogStatsDTO | null>(null);

    Promise.all([localP, serverP])
      .then(([local, server]) => {
        setIncludesServer(!!server);
        setStats(server ? mergeStats(local, server) : local);
      })
      .catch((e) => {
        setStats(null);
        setError(String(e instanceof Error ? e.message : e));
      })
      .finally(() => setLoading(false));
  }, [remote.configured]);

  useEffect(() => {
    load();
  }, [load]);

  // Reload stats and signal the runs list after logs are deleted/purged.
  const onLogsChanged = useCallback(() => {
    load();
    setLogsVersion((v) => v + 1);
  }, [load]);

  const empty = stats && stats.runs === 0;

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 px-5 pb-10 pt-2">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-xl font-semibold tracking-tight text-foreground">Insights</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            Everything Viaduct has deleted, read back from its logs
            {includesServer ? " (this machine and your server combined)" : ""}.
          </p>
        </div>
        <Button variant="subtle" size="md" onClick={load} disabled={loading}>
          <RefreshCw className={loading ? "animate-spin" : ""} />
          Refresh
        </Button>
      </div>

      {serverNote && (
        <Card inset className="flex items-center gap-2.5 border-warn/30 text-sm text-muted-foreground">
          <ServerOff className="h-4 w-4 shrink-0 text-warn" />
          Showing local logs only; couldn't reach the server ({serverNote}).
        </Card>
      )}

      {loading && !stats && (
        <Card className="grid place-items-center py-16 text-muted-foreground">
          <div className="flex items-center gap-3">
            <Spinner /> Reading logs…
          </div>
        </Card>
      )}

      {error && (
        <Card className="flex items-center gap-3 border-destructive/40 py-6 text-sm">
          <ServerOff className="h-5 w-5 shrink-0 text-destructive" />
          <div>
            <div className="font-medium text-foreground">Couldn't load insights</div>
            <div className="text-muted-foreground">{error}</div>
          </div>
        </Card>
      )}

      {stats && !error && (empty ? <EmptyState /> : <Dashboard stats={stats} />)}

      {stats && !error && !empty && (
        <RunsList channels={stats.topChannels} version={logsVersion} remote={remote} />
      )}

      {stats && !error && !empty && (
        <LogSearch channels={stats.topChannels} onChanged={onLogsChanged} remote={remote} />
      )}
    </div>
  );
}

function EmptyState() {
  return (
    <Card className="grid place-items-center gap-2 py-16 text-center">
      <Trash2 className="h-7 w-7 text-muted-foreground" />
      <div className="text-sm font-medium text-foreground">No deletions logged yet</div>
      <div className="max-w-sm text-sm text-muted-foreground">
        Once you run a deletion, this page fills with totals, targets, and a timeline.
      </div>
    </Card>
  );
}

function Dashboard({ stats }: { stats: LogStatsDTO }) {
  // Map a run's top-channel ID to the name resolved for the targets list.
  const labelById = new Map(stats.topChannels.map((c) => [c.id, c.label]));
  const runChannel = (id: string) => (id ? labelById.get(id) ?? shortId(id) : "");

  return (
    <div className="flex flex-col gap-4">
      {/* Headline figures */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <StatCard icon={Trash2} label="Messages deleted" value={formatCount(stats.totalDeleted)} headline />
        <StatCard icon={Target} label="Targets" value={formatCount(stats.channels)} sub="channels & DMs" />
        <StatCard icon={Activity} label="Runs" value={formatCount(stats.runs)} sub="deletion passes" />
        <StatCard
          icon={TriangleAlert}
          label="Failed"
          value={formatCount(stats.totalFailed)}
          sub={stats.totalFailed > 0 ? "couldn't delete" : "none"}
          tone={stats.totalFailed > 0 ? "warn" : "default"}
        />
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        <StatCard
          icon={Paperclip}
          label="With attachments"
          value={formatCount(stats.withAttachments)}
          sub={`${formatCount(stats.attachments)} files`}
          small
        />
        <StatCard icon={Type} label="Characters removed" value={compact(stats.totalChars)} small />
        <StatCard icon={ImageIcon} label="Span" value={spanLabel(stats)} small />
      </div>

      {stats.byMonth.some((b) => b.count > 0) && (
        <Panel icon={CalendarClock} title="When your deleted messages were posted">
          <MonthChart buckets={stats.byMonth} />
        </Panel>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        {stats.topChannels.length > 0 && (
          <Panel icon={Hash} title="Top targets">
            <div className="flex flex-col gap-2.5">
              <TargetBars channels={stats.topChannels} />
            </div>
          </Panel>
        )}

        <Panel icon={Activity} title="Recent runs">
          <div className="flex flex-col">
            {stats.recent.slice(0, 8).map((r, i) => (
              <div key={r.file} className="-mx-2 rounded-lg px-2 transition-colors hover:bg-white/[0.04]">
                {i > 0 && <Separator className="my-0" />}
                <div className="flex items-center justify-between gap-3 py-2 text-sm">
                  <div className="min-w-0">
                    <div className="truncate text-foreground">
                      {r.startedAt ? formatTimestamp(r.startedAt) : r.file}
                    </div>
                    {r.topChannel && (
                      <div className="truncate text-xs text-muted-foreground">
                        mostly {runChannel(r.topChannel)}
                      </div>
                    )}
                  </div>
                  <div className="flex shrink-0 items-center gap-1.5">
                    <Badge variant="secondary">{formatCount(r.deleted)} deleted</Badge>
                    {r.failed > 0 && <Badge variant="destructive">{formatCount(r.failed)}</Badge>}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </Panel>
      </div>

      {stats.failures.length > 0 && (
        <Panel icon={TriangleAlert} title="Why deletes failed" iconClass="text-warn">
          <div className="flex flex-wrap gap-2">
            {stats.failures.map((f) => (
              <Badge key={f.reason} variant="outline" className="gap-1.5 py-1">
                {f.reason}
                <span className="text-muted-foreground">{formatCount(f.count)}</span>
              </Badge>
            ))}
          </div>
        </Panel>
      )}
    </div>
  );
}

function Panel({
  icon: Icon,
  title,
  iconClass,
  children,
}: {
  icon: LucideIcon;
  title: string;
  iconClass?: string;
  children: React.ReactNode;
}) {
  return (
    <Card className="lift hover:border-white/15">
      <div className="mb-3 flex items-center gap-2 text-sm font-semibold text-foreground">
        <Icon className={cn("h-4 w-4 text-muted-foreground", iconClass)} />
        {title}
      </div>
      {children}
    </Card>
  );
}

function StatCard({
  icon: Icon,
  label,
  value,
  sub,
  headline,
  small,
  tone = "default",
}: {
  icon: LucideIcon;
  label: string;
  value: string;
  sub?: string;
  headline?: boolean;
  small?: boolean;
  tone?: "default" | "warn";
}) {
  return (
    <Card className="lift p-4 hover:border-white/20">
      <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        <Icon className={cn("h-3.5 w-3.5", tone === "warn" && "text-warn")} />
        {label}
      </div>
      <div
        className={cn(
          "mt-1.5 font-semibold tabular-nums",
          headline ? "text-gradient text-4xl" : small ? "text-xl text-foreground" : "text-3xl text-foreground",
        )}
      >
        {value}
      </div>
      {sub && <div className="mt-0.5 text-xs text-muted-foreground">{sub}</div>}
    </Card>
  );
}

const CHART_H = 124; // px height of the tallest bar

function MonthChart({ buckets }: { buckets: BucketDTO[] }) {
  const shown = buckets.slice(-24);
  const max = Math.max(1, ...shown.map((b) => b.count));
  return (
    <div className="flex items-end gap-1" style={{ height: CHART_H + 18 }}>
      {shown.map((b, i) => {
        const h = b.count > 0 ? Math.max(4, Math.round((b.count / max) * CHART_H)) : 0;
        const showLabel = i === 0 || i === shown.length - 1 || i === Math.floor(shown.length / 2);
        return (
          <div
            key={b.label}
            className="group flex h-full min-w-0 flex-1 flex-col items-center justify-end gap-1"
            title={`${b.label}: ${b.count}`}
          >
            <div
              className="w-full rounded-t bg-gradient-to-t from-white/15 to-white/90 transition-colors duration-150 group-hover:to-white"
              style={{ height: `${h}px` }}
            />
            <div className="h-3 w-full truncate text-center text-[9px] text-muted-foreground">
              {showLabel ? b.label.slice(2) : ""}
            </div>
          </div>
        );
      })}
    </div>
  );
}

function TargetBars({ channels }: { channels: ChannelStatDTO[] }) {
  const max = Math.max(1, ...channels.map((c) => c.count));
  return (
    <>
      {channels.slice(0, 8).map((c) => (
        <div key={c.id} className="group flex items-center gap-3 text-sm">
          <div className="w-32 shrink-0 truncate text-foreground" title={c.label}>
            {c.label}
          </div>
          <div className="h-2 flex-1 overflow-hidden rounded-full bg-foreground/10">
            <div
              className="h-full rounded-full bg-gradient-to-r from-white/40 to-white transition-all duration-300 group-hover:from-white/60"
              style={{ width: `${Math.max((c.count / max) * 100, 3)}%` }}
            />
          </div>
          <div className="w-12 shrink-0 text-right tabular-nums text-muted-foreground">{formatCount(c.count)}</div>
        </div>
      ))}
    </>
  );
}

// ---- helpers ----

// mergeStats combines two reports (local + server) into one.
function mergeStats(a: LogStatsDTO, b: LogStatsDTO): LogStatsDTO {
  const channelMap = new Map<string, ChannelStatDTO>();
  for (const c of [...a.topChannels, ...b.topChannels]) {
    const prev = channelMap.get(c.id);
    if (prev) {
      prev.count += c.count;
      if (looksLikeId(prev.label) && !looksLikeId(c.label)) prev.label = c.label;
    } else {
      channelMap.set(c.id, { ...c });
    }
  }
  const topChannels = [...channelMap.values()].sort((x, y) => y.count - x.count).slice(0, 15);

  const monthMap = new Map<string, number>();
  for (const m of [...a.byMonth, ...b.byMonth]) monthMap.set(m.label, (monthMap.get(m.label) ?? 0) + m.count);
  const byMonth: BucketDTO[] = [...monthMap.entries()]
    .sort((x, y) => (x[0] < y[0] ? -1 : 1))
    .map(([label, count]) => ({ label, count }));

  const byHour = Array.from({ length: 24 }, (_, i) => (a.byHour[i] ?? 0) + (b.byHour[i] ?? 0));

  const failMap = new Map<string, number>();
  for (const f of [...a.failures, ...b.failures]) failMap.set(f.reason, (failMap.get(f.reason) ?? 0) + f.count);
  const failures: FailReasonDTO[] = [...failMap.entries()]
    .sort((x, y) => y[1] - x[1])
    .map(([reason, count]) => ({ reason, count }));

  const recent: RunStatDTO[] = [...a.recent, ...b.recent]
    .sort((x, y) => (x.startedAt < y.startedAt ? 1 : x.startedAt > y.startedAt ? -1 : x.file < y.file ? 1 : -1))
    .slice(0, 50);

  return {
    source: "combined",
    runs: a.runs + b.runs,
    totalDeleted: a.totalDeleted + b.totalDeleted,
    totalFailed: a.totalFailed + b.totalFailed,
    withAttachments: a.withAttachments + b.withAttachments,
    attachments: a.attachments + b.attachments,
    totalChars: a.totalChars + b.totalChars,
    channels: a.channels + b.channels,
    firstPostedAt: minIso(a.firstPostedAt, b.firstPostedAt),
    lastPostedAt: maxIso(a.lastPostedAt, b.lastPostedAt),
    firstRunAt: minIso(a.firstRunAt, b.firstRunAt),
    lastRunAt: maxIso(a.lastRunAt, b.lastRunAt),
    topChannels,
    byMonth,
    byHour,
    recent,
    failures,
  };
}

function looksLikeId(label: string): boolean {
  return /^\d{5,}$/.test(label) || label.includes("…");
}

function minIso(a: string, b: string): string {
  if (!a) return b;
  if (!b) return a;
  return a < b ? a : b;
}
function maxIso(a: string, b: string): string {
  if (!a) return b;
  if (!b) return a;
  return a > b ? a : b;
}

function shortId(id: string): string {
  if (id.length <= 8) return id;
  return `${id.slice(0, 4)}…${id.slice(-4)}`;
}

// compact renders large counts as e.g. 12.3k / 4.1M.
function compact(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}

function spanLabel(stats: LogStatsDTO): string {
  if (!stats.firstPostedAt || !stats.lastPostedAt) return "—";
  const a = new Date(stats.firstPostedAt);
  const b = new Date(stats.lastPostedAt);
  if (Number.isNaN(a.getTime()) || Number.isNaN(b.getTime())) return "—";
  const days = Math.max(1, Math.round((b.getTime() - a.getTime()) / 86_400_000));
  if (days < 60) return `${days}d`;
  if (days < 730) return `${Math.round(days / 30)}mo`;
  return `${(days / 365).toFixed(1)}y`;
}
