// Combines local + server deletion-log data into one merged view, the same
// pattern Insights.tsx already uses for the aggregate stats — but for the
// run-centric list and full-text search, which (unlike the stats endpoint)
// have no server-side notion of "the other machine's logs too".
import { api } from "./bridge";
import type { LogSearchRequest, RunStatDTO, SearchResultDTO } from "./types";

// MAX_COMBINED_LIMIT mirrors the server's own per-query search cap
// (logstats.maxSearchLimit). Deep pagination beyond it is reported truncated
// rather than silently wrong — see searchCombined.
const MAX_COMBINED_LIMIT = 500;

function runSortKey(r: RunStatDTO): string {
  return r.startedAt || r.file;
}

// combineRuns tags each run with its source and merges local + server (if
// present) into one newest-first list.
export function combineRuns(local: RunStatDTO[], remote: RunStatDTO[] | null): RunStatDTO[] {
  const tagged: RunStatDTO[] = [
    ...local.map((r) => ({ ...r, source: "local" as const })),
    ...(remote ?? []).map((r) => ({ ...r, source: "server" as const })),
  ];
  return tagged.sort((a, b) => {
    const ka = runSortKey(a);
    const kb = runSortKey(b);
    return ka < kb ? 1 : ka > kb ? -1 : 0;
  });
}

function hitSortKey(h: { timestamp: string; runAt: string; file: string }): string {
  return (h.timestamp || h.runAt) + h.file;
}

// searchCombined runs req against local logs and, if a server is configured,
// the server too, merging both into one newest-first page. Each source is
// queried from offset 0 up to (req.offset + req.limit) — capped at
// MAX_COMBINED_LIMIT — so a correct merged page can be sliced out
// client-side without true distributed pagination; deep pagination beyond
// that cap is marked truncated instead of silently dropping results. A
// server that's configured but unreachable degrades to local-only, matching
// how the rest of Insights already treats a down server as non-fatal.
export async function searchCombined(
  req: LogSearchRequest,
  remoteConfigured: boolean,
): Promise<SearchResultDTO> {
  const needed = Math.min(req.offset + req.limit, MAX_COMBINED_LIMIT);
  const wideReq: LogSearchRequest = { ...req, offset: 0, limit: needed };

  const localP = api.searchLogs(wideReq);
  const remoteP = remoteConfigured ? api.remoteSearchLogs(wideReq).catch(() => null) : Promise.resolve(null);
  const [local, remote] = await Promise.all([localP, remoteP]);

  const merged = [
    ...local.hits.map((h) => ({ ...h, source: "local" as const })),
    ...(remote?.hits.map((h) => ({ ...h, source: "server" as const })) ?? []),
  ].sort((a, b) => {
    const ka = hitSortKey(a);
    const kb = hitSortKey(b);
    return ka < kb ? 1 : ka > kb ? -1 : 0;
  });

  const total = local.total + (remote?.total ?? 0);
  const cappedShort = needed >= MAX_COMBINED_LIMIT && merged.length < total;

  return {
    hits: merged.slice(req.offset, req.offset + req.limit),
    total,
    offset: req.offset,
    limit: req.limit,
    scanned: local.scanned + (remote?.scanned ?? 0),
    truncated: local.truncated || (remote?.truncated ?? false) || cappedShort,
  };
}
