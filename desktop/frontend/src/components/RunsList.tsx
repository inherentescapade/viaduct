import { useCallback, useEffect, useState } from "react";
import { Activity, Download } from "lucide-react";
import { api } from "../lib/bridge";
import { combineRuns } from "../lib/combineLogs";
import type { ChannelStatDTO, RunStatDTO } from "../lib/types";
import type { RemoteStatus } from "../lib/useRemoteStatus";
import { formatCount, formatTimestamp } from "../lib/format";
import { Card } from "./Card";
import { Badge } from "./ui/badge";
import { Spinner } from "./Spinner";

interface Props {
  // Channels from the dashboard, used to name a run's busiest channel when the
  // backend couldn't resolve it.
  channels: ChannelStatDTO[];
  // Bumped by the parent when logs change (a delete/purge) so the list refetches.
  version: number;
  remote: RemoteStatus;
}

// RunsList is the run-centric browser: every deletion pass — local and, when
// self-hosting, the server's too — newest first, with its counts and busiest
// channel, and a button to export that run's raw log.
export function RunsList({ channels, version, remote }: Props) {
  const [runs, setRuns] = useState<RunStatDTO[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    setError(null);
    const localP = api.listRuns();
    const remoteP = remote.configured ? api.remoteListRuns().catch(() => null) : Promise.resolve(null);
    Promise.all([localP, remoteP])
      .then(([local, server]) => setRuns(combineRuns(local, server)))
      .catch((e) => {
        setRuns(null);
        setError(String(e instanceof Error ? e.message : e));
      })
      .finally(() => setLoading(false));
  }, [version, remote.configured]);

  const labelById = new Map(channels.map((c) => [c.id, c.label]));
  const runChannel = (r: RunStatDTO) =>
    r.topChannelLabel || (r.topChannel ? labelById.get(r.topChannel) ?? shortId(r.topChannel) : "");

  const exportRun = useCallback(async (file: string, source?: "local" | "server") => {
    setNote(null);
    try {
      const saved = source === "server" ? await api.remoteDownloadRun(file) : await api.downloadLog(file);
      if (saved) setNote(`Exported to ${saved}`);
    } catch (e) {
      setNote(String(e instanceof Error ? e.message : e));
    }
  }, []);

  return (
    <Card className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 text-sm font-semibold text-foreground">
          <Activity className="h-4 w-4 text-muted-foreground" />
          Runs
          {runs && runs.length > 0 && (
            <span className="text-xs font-normal text-muted-foreground">
              {formatCount(runs.length)} deletion pass{runs.length === 1 ? "" : "es"}
            </span>
          )}
        </div>
        {loading && <Spinner />}
      </div>

      {note && (
        <div className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-xs text-muted-foreground">
          {note}
        </div>
      )}

      {error ? (
        <div className="text-xs text-destructive">{error}</div>
      ) : runs && runs.length === 0 ? (
        <div className="py-4 text-center text-xs text-muted-foreground">No runs recorded yet.</div>
      ) : runs ? (
        <div className="flex max-h-96 flex-col gap-1.5 overflow-y-auto pr-0.5">
          {runs.map((r) => {
            const channel = runChannel(r);
            return (
              <div
                key={`${r.source}:${r.file}`}
                className="group flex items-center gap-3 rounded-lg border border-white/5 bg-white/[0.02] px-3 py-2 transition-colors hover:border-white/10 hover:bg-white/[0.04]"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5 truncate text-sm text-foreground">
                    {r.startedAt ? formatTimestamp(r.startedAt) : r.file}
                    {r.source === "server" && (
                      <Badge variant="outline" className="shrink-0 py-0 text-[10px]">
                        server
                      </Badge>
                    )}
                  </div>
                  {channel && (
                    <div className="truncate text-xs text-muted-foreground">mostly {channel}</div>
                  )}
                </div>
                <div className="flex shrink-0 items-center gap-1.5">
                  <Badge variant="secondary">{formatCount(r.deleted)} deleted</Badge>
                  {r.failed > 0 && <Badge variant="destructive">{formatCount(r.failed)}</Badge>}
                  <button
                    onClick={() => exportRun(r.file, r.source)}
                    title="Export this run's log (NDJSON)"
                    className="rounded-md p-1.5 text-muted-foreground opacity-0 transition-all hover:bg-white/10 hover:text-foreground group-hover:opacity-100"
                  >
                    <Download className="h-4 w-4" />
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      ) : null}
    </Card>
  );
}

function shortId(id: string): string {
  if (id.length <= 8) return id;
  return `${id.slice(0, 4)}…${id.slice(-4)}`;
}
