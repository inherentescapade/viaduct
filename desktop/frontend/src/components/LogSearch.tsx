import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Download, Paperclip, Search, Trash2, TriangleAlert, X } from "lucide-react";
import { api } from "../lib/bridge";
import { searchCombined } from "../lib/combineLogs";
import type { ChannelStatDTO, LogSearchRequest, SearchHitDTO, SearchResultDTO } from "../lib/types";
import type { RemoteStatus } from "../lib/useRemoteStatus";
import { formatCount, formatTimestamp } from "../lib/format";
import { cn } from "../lib/cn";
import { Card } from "./Card";
import { Badge } from "./ui/badge";
import { Button } from "./Button";
import { Input } from "./ui/input";
import { Spinner } from "./Spinner";
import { Avatar } from "./Avatar";

interface Props {
  // Channels surfaced by the dashboard, used to populate the channel filter so a
  // user can narrow to a known target without typing an ID.
  channels: ChannelStatDTO[];
  // Called after logs are deleted/purged so the parent can refresh its stats.
  onChanged?: () => void;
  remote: RemoteStatus;
}

type Kind = "" | "deleted" | "failed";

interface Confirm {
  title: string;
  body: string;
  confirmLabel: string;
  action: () => Promise<void>;
}

const PAGE_SIZE = 25;
const DEBOUNCE_MS = 300;

// LogSearch is the queryable, full-text view over every logged deletion. Where
// the dashboard answers "how much / where / when" in aggregate, this lets you
// page through the actual records — filtered by text, channel, kind,
// attachments and date — export exactly what you're looking at as NDJSON, and
// delete logs (a single run, or everything matching the current filter).
export function LogSearch({ channels, onChanged, remote }: Props) {
  const [text, setText] = useState("");
  const [channelId, setChannelId] = useState("");
  const [kind, setKind] = useState<Kind>("");
  const [withAttachments, setWithAttachments] = useState(false);
  const [before, setBefore] = useState("");
  const [after, setAfter] = useState("");
  const [offset, setOffset] = useState(0);
  // Bumped after a delete/purge to force the current query to re-run.
  const [reloadKey, setReloadKey] = useState(0);

  const [result, setResult] = useState<SearchResultDTO | null>(null);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false); // a destructive op is in flight
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<Confirm | null>(null);

  // Any filter change resets paging to the first page.
  const resetPaging = useCallback(() => setOffset(0), []);

  // Guards against out-of-order async results: only the most recently issued
  // query is allowed to commit its result.
  const seqRef = useRef(0);

  const req: LogSearchRequest = useMemo(
    () => ({
      text: text.trim(),
      channelId,
      kind,
      withAttachments,
      before: before.trim(),
      after: after.trim(),
      limit: PAGE_SIZE,
      offset,
    }),
    [text, channelId, kind, withAttachments, before, after, offset],
  );

  useEffect(() => {
    const seq = ++seqRef.current;
    const handle = window.setTimeout(() => {
      setLoading(true);
      setError(null);
      searchCombined(req, remote.configured)
        .then((res) => {
          if (seq === seqRef.current) setResult(res);
        })
        .catch((e) => {
          if (seq === seqRef.current) {
            setResult(null);
            setError(String(e instanceof Error ? e.message : e));
          }
        })
        .finally(() => {
          if (seq === seqRef.current) setLoading(false);
        });
    }, DEBOUNCE_MS);
    return () => window.clearTimeout(handle);
  }, [req, reloadKey, remote.configured]);

  // refresh re-runs the current query and lets the parent update its stats.
  const refresh = useCallback(() => {
    setReloadKey((k) => k + 1);
    onChanged?.();
  }, [onChanged]);

  const hasFilters =
    !!text.trim() || !!channelId || !!kind || withAttachments || !!before.trim() || !!after.trim();
  const clearFilters = () => {
    setText("");
    setChannelId("");
    setKind("");
    setWithAttachments(false);
    setBefore("");
    setAfter("");
    resetPaging();
  };

  const total = result?.total ?? 0;

  // exportResults saves local matches (one dialog) and, if a server is
  // configured, server matches too (a second dialog) — one merged file isn't
  // possible without routing the write through Go, so each source gets its
  // own save prompt.
  const exportResults = useCallback(async () => {
    setNote(null);
    const saved: string[] = [];
    const errors: string[] = [];
    try {
      const path = await api.exportSearch(req);
      if (path) saved.push(`local → ${path}`);
    } catch (e) {
      errors.push(String(e instanceof Error ? e.message : e));
    }
    if (remote.configured) {
      try {
        const path = await api.remoteExportSearch(req);
        if (path) saved.push(`server → ${path}`);
      } catch (e) {
        errors.push(String(e instanceof Error ? e.message : e));
      }
    }
    if (saved.length) setNote(`Exported: ${saved.join("; ")}`);
    if (errors.length) setNote((n) => (n ? `${n} (${errors.join("; ")})` : errors.join("; ")));
  }, [req, remote.configured]);

  const downloadRun = useCallback(async (file: string, source?: "local" | "server") => {
    setNote(null);
    try {
      const saved = source === "server" ? await api.remoteDownloadRun(file) : await api.downloadLog(file);
      if (saved) setNote(`Saved to ${saved}`);
    } catch (e) {
      setNote(String(e instanceof Error ? e.message : e));
    }
  }, []);

  // runConfirmed executes a confirmed destructive op with a busy guard.
  const runConfirmed = useCallback(async () => {
    if (!confirm) return;
    const action = confirm.action;
    setConfirm(null);
    setBusy(true);
    setNote(null);
    try {
      await action();
    } catch (e) {
      setNote(String(e instanceof Error ? e.message : e));
    } finally {
      setBusy(false);
    }
  }, [confirm]);

  const askPurge = () =>
    setConfirm({
      title: "Delete matching logs",
      body: `Permanently remove all ${formatCount(total)} record${total === 1 ? "" : "s"} matching your current filters from ${remote.configured ? "this machine's and your server's" : "the"} logs. This only erases the local record — it can't affect Discord, and it can't be undone.`,
      confirmLabel: "Delete matches",
      action: async () => {
        let removed = await api.purgeLogs(req);
        const errors: string[] = [];
        if (remote.configured) {
          try {
            removed += await api.remotePurgeLogs(req);
          } catch (e) {
            errors.push(String(e instanceof Error ? e.message : e));
          }
        }
        setNote(
          `Removed ${formatCount(removed)} record${removed === 1 ? "" : "s"}` +
            (errors.length ? ` (server: ${errors.join("; ")})` : ""),
        );
        resetPaging();
        refresh();
      },
    });

  const askDeleteRun = (file: string, source?: "local" | "server") =>
    setConfirm({
      title: "Delete this run's log",
      body: `Permanently delete the entire log for this run${source === "server" ? " on your server" : ""}, including every message it recorded. This only erases the local record — it can't affect Discord, and it can't be undone.`,
      confirmLabel: "Delete log",
      action: async () => {
        if (source === "server") {
          await api.remoteDeleteRun(file);
        } else {
          await api.deleteLog(file);
        }
        setNote("Deleted run log");
        refresh();
      },
    });

  const shownFrom = total === 0 ? 0 : offset + 1;
  const shownTo = Math.min(offset + PAGE_SIZE, total);
  // Dim and freeze the current page while the next one (or a reload) is in
  // flight, so the in-between state reads as transitional.
  const transitioning = (loading || busy) && !!result && result.hits.length > 0;

  return (
    <Card className="flex flex-col gap-4">
      <div className="flex items-center gap-2 text-sm font-semibold text-foreground">
        <Search className="h-4 w-4 text-muted-foreground" />
        Search deletions
      </div>

      {/* Filters */}
      <div className="flex flex-col gap-2.5">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              resetPaging();
            }}
            placeholder="Search message text or failure reason…"
            className="pl-9"
          />
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <FilterSelect
            value={channelId}
            onChange={(v) => {
              setChannelId(v);
              resetPaging();
            }}
            aria-label="Channel filter"
          >
            <option value="">All channels</option>
            {channels.map((c) => (
              <option key={c.id} value={c.id}>
                {c.label}
              </option>
            ))}
          </FilterSelect>

          <FilterSelect
            value={kind}
            onChange={(v) => {
              setKind(v as Kind);
              resetPaging();
            }}
            aria-label="Kind filter"
          >
            <option value="">Deleted &amp; failed</option>
            <option value="deleted">Deleted only</option>
            <option value="failed">Failed only</option>
          </FilterSelect>

          <Input
            value={after}
            onChange={(e) => {
              setAfter(e.target.value);
              resetPaging();
            }}
            placeholder="After (e.g. 2024-01-01)"
            className="h-8 w-40 text-xs"
          />
          <Input
            value={before}
            onChange={(e) => {
              setBefore(e.target.value);
              resetPaging();
            }}
            placeholder="Before (e.g. 30d)"
            className="h-8 w-40 text-xs"
          />

          <button
            onClick={() => {
              setWithAttachments((v) => !v);
              resetPaging();
            }}
            className={cn(
              "inline-flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-xs font-medium transition-colors",
              withAttachments
                ? "border-white/20 bg-white/10 text-foreground"
                : "border-input text-muted-foreground hover:text-foreground",
            )}
          >
            <Paperclip className="h-3.5 w-3.5" />
            Has attachment
          </button>

          {hasFilters && (
            <Button variant="ghost" size="md" onClick={clearFilters} className="h-8 px-2 text-xs">
              <X className="h-3.5 w-3.5" />
              Clear
            </Button>
          )}
        </div>
      </div>

      {note && (
        <div className="rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-xs text-muted-foreground">
          {note}
        </div>
      )}

      {/* Results header: status on the left, actions + paging on the right. */}
      <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
        <span className="flex items-center gap-2">
          {(loading || busy) && <Spinner />}
          {error ? (
            <span className="text-destructive">{error}</span>
          ) : total > 0 ? (
            <>
              Showing {formatCount(shownFrom)}–{formatCount(shownTo)} of {formatCount(total)}
              {result?.truncated ? "+" : ""}
            </>
          ) : !loading ? (
            "No matches"
          ) : (
            "Searching…"
          )}
        </span>
        <span className="flex items-center gap-1.5">
          <Button
            variant="subtle"
            size="md"
            className="h-7 px-2 text-xs"
            disabled={total === 0 || loading || busy}
            onClick={exportResults}
            title={`Export ${formatCount(total)} matching record${total === 1 ? "" : "s"} as NDJSON`}
          >
            <Download className="h-3.5 w-3.5" />
            Export
          </Button>
          {hasFilters && (
            <Button
              variant="danger"
              size="md"
              className="h-7 px-2 text-xs"
              disabled={total === 0 || loading || busy}
              onClick={askPurge}
              title="Delete every record matching the current filters"
            >
              <Trash2 className="h-3.5 w-3.5" />
              Delete matches
            </Button>
          )}
          {total > PAGE_SIZE && (
            <>
              <Button
                variant="subtle"
                size="md"
                className="h-7 px-2 text-xs"
                disabled={offset === 0 || loading || busy}
                onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
              >
                Prev
              </Button>
              <Button
                variant="subtle"
                size="md"
                className="h-7 px-2 text-xs"
                disabled={shownTo >= total || loading || busy}
                onClick={() => setOffset((o) => o + PAGE_SIZE)}
              >
                Next
              </Button>
            </>
          )}
        </span>
      </div>

      {/* Results list. While a page load or a delete is in flight we dim it and
          disable interaction so the in-between state reads as transitional. */}
      {result && result.hits.length > 0 && (
        <div
          className={cn(
            "flex flex-col gap-1.5 transition-opacity",
            transitioning && "pointer-events-none opacity-50",
          )}
          aria-busy={transitioning}
        >
          {result.hits.map((h, i) => (
            <HitRow
              key={`${h.source}:${h.file}:${h.kind}:${h.id || i}`}
              hit={h}
              onDownload={() => downloadRun(h.file, h.source)}
              onDelete={() => askDeleteRun(h.file, h.source)}
            />
          ))}
        </div>
      )}

      {confirm && (
        <ConfirmModal
          confirm={confirm}
          onCancel={() => setConfirm(null)}
          onConfirm={runConfirmed}
        />
      )}
    </Card>
  );
}

// FilterSelect is a compact native select styled to match the filter row.
function FilterSelect({
  value,
  onChange,
  children,
  ...rest
}: {
  value: string;
  onChange: (v: string) => void;
  children: React.ReactNode;
} & Omit<React.SelectHTMLAttributes<HTMLSelectElement>, "value" | "onChange">) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="h-8 max-w-[12rem] rounded-md border border-input bg-transparent px-2 text-xs text-foreground transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring [&>option]:bg-background [&>option]:text-foreground"
      {...rest}
    >
      {children}
    </select>
  );
}

function HitRow({
  hit,
  onDownload,
  onDelete,
}: {
  hit: SearchHitDTO;
  onDownload: () => void;
  onDelete: () => void;
}) {
  const failed = hit.kind === "failed";
  const when = hit.timestamp || hit.runAt;
  return (
    <div className="group flex items-start gap-2.5 rounded-lg border border-white/5 bg-white/[0.02] px-3 py-2 transition-colors hover:border-white/10 hover:bg-white/[0.04]">
      <Avatar url={hit.authorAvatarUrl} name={hit.authorName || "?"} size={26} rounded="full" />
      <div className="min-w-0 flex-1">
        {/* Author name [channel label] (timestamp) */}
        <div className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-xs">
          <span className="font-semibold text-foreground">{hit.authorName || "You"}</span>
          {hit.channelLabel && (
            <span className="text-accent-strong" title={hit.channelLabel}>
              [{hit.channelLabel}]
            </span>
          )}
          {when && <span className="text-muted-foreground">({formatTimestamp(when)})</span>}
          {hit.source === "server" && (
            <Badge variant="outline" className="py-0 text-[10px]">
              server
            </Badge>
          )}
          {failed ? (
            <Badge variant="destructive" className="gap-1 py-0">
              <TriangleAlert className="h-3 w-3" />
              {hit.error}
            </Badge>
          ) : hit.attachments > 0 ? (
            <Badge variant="secondary" className="gap-1 py-0">
              <Paperclip className="h-3 w-3" />
              {hit.attachments}
            </Badge>
          ) : null}
        </div>
        {!failed && (
          <div className="mt-0.5 break-words text-sm text-foreground/90">
            {hit.content || <span className="italic text-muted-foreground">(no text)</span>}
          </div>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
        <button
          onClick={onDownload}
          title="Download this run's log"
          className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-white/10 hover:text-foreground"
        >
          <Download className="h-4 w-4" />
        </button>
        <button
          onClick={onDelete}
          title="Delete this run's log"
          className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-destructive/20 hover:text-destructive"
        >
          <Trash2 className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}

// ConfirmModal is a small blocking confirmation for the destructive log actions.
function ConfirmModal({
  confirm,
  onCancel,
  onConfirm,
}: {
  confirm: Confirm;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-black/50 p-4"
      onClick={onCancel}
      role="presentation"
    >
      <Card
        className="w-full max-w-sm"
        onClick={(e) => e.stopPropagation()}
        role="alertdialog"
        aria-modal="true"
      >
        <div className="flex items-start gap-2.5">
          <TriangleAlert className="mt-0.5 h-5 w-5 shrink-0 text-destructive" />
          <div>
            <div className="text-sm font-semibold text-foreground">{confirm.title}</div>
            <p className="mt-1 text-sm leading-relaxed text-muted-foreground">{confirm.body}</p>
          </div>
        </div>
        <div className="mt-4 flex items-center justify-end gap-2">
          <Button variant="ghost" size="md" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="danger" size="md" onClick={onConfirm}>
            <Trash2 className="h-4 w-4" />
            {confirm.confirmLabel}
          </Button>
        </div>
      </Card>
    </div>
  );
}
