import { useCallback, useEffect, useRef, useState } from "react";

// How long a row must stay continuously in view before we actually fire its
// count query. This stops a fast scroll from kicking off a search for every row
// it flies past (which would pile up and trip Discord's rate limits) — only
// rows the user pauses on get counted.
const DWELL_MS = 2000;

// useVisibleCounts fetches a numeric count per id, but ONLY for rows that scroll
// into view AND linger there for DWELL_MS, so a long server/DM list doesn't fire
// a search for every item up front (or for every item a fast scroll passes over).
// Each row registers its element via register(id); once it has been visible long
// enough the count is fetched once (cached), with a small concurrency cap. While
// a row's query is in flight its id is flagged in `loading` so the UI can show a
// spinner.
//
// Usage:
//   const { counts, loading, register } = useVisibleCounts(ids, api.guildMessageCount);
//   <button ref={register(id)} /> ... counts[id] / loading[id]
export function useVisibleCounts(
  ids: string[],
  fetcher: (id: string) => Promise<number>,
): {
  counts: Record<string, number>;
  loading: Record<string, boolean>;
  register: (id: string) => (el: HTMLElement | null) => void;
} {
  const [counts, setCounts] = useState<Record<string, number>>({});
  const [loading, setLoading] = useState<Record<string, boolean>>({});
  const key = ids.join(",");

  const observerRef = useRef<IntersectionObserver | null>(null);
  const elToId = useRef<Map<Element, string>>(new Map());
  const requested = useRef<Set<string>>(new Set());
  const queue = useRef<string[]>([]);
  const inflight = useRef(0);
  // Pending dwell timers, keyed by id — a row in view but not yet counted.
  const timers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());
  // Stable ref-callback per id so React doesn't re-run refs every render.
  const cbCache = useRef<Map<string, (el: HTMLElement | null) => void>>(new Map());

  // Rebuild the observer whenever the set of ids changes (list filtered/swapped),
  // clearing prior state so stale counts don't linger.
  useEffect(() => {
    setCounts({});
    setLoading({});
    elToId.current = new Map();
    requested.current = new Set();
    queue.current = [];
    inflight.current = 0;
    for (const t of timers.current.values()) clearTimeout(t);
    timers.current = new Map();
    cbCache.current = new Map();

    const pump = () => {
      while (inflight.current < 5 && queue.current.length > 0) {
        const id = queue.current.shift()!;
        inflight.current++;
        setLoading((prev) => ({ ...prev, [id]: true }));
        fetcher(id)
          .then((n) => setCounts((prev) => ({ ...prev, [id]: n })))
          .catch(() => {})
          .finally(() => {
            inflight.current--;
            setLoading((prev) => {
              const next = { ...prev };
              delete next[id];
              return next;
            });
            pump();
          });
      }
    };

    const obs = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          const id = elToId.current.get(e.target);
          if (!id || requested.current.has(id)) continue;
          if (e.isIntersecting) {
            // Just came into view — start the dwell timer. Only if it survives
            // DWELL_MS (i.e. the user didn't scroll straight past) do we commit
            // to fetching. Keep observing so we still hear the exit event.
            if (!timers.current.has(id)) {
              const el = e.target;
              const t = setTimeout(() => {
                timers.current.delete(id);
                if (requested.current.has(id)) return;
                requested.current.add(id);
                queue.current.push(id);
                obs.unobserve(el); // fetch once per row
                pump();
              }, DWELL_MS);
              timers.current.set(id, t);
            }
          } else {
            // Scrolled back out before dwelling long enough — cancel the pending
            // fetch so it never fires.
            const t = timers.current.get(id);
            if (t) {
              clearTimeout(t);
              timers.current.delete(id);
            }
          }
        }
      },
      { rootMargin: "120px" }, // start a little before the row is fully visible
    );
    observerRef.current = obs;
    return () => {
      obs.disconnect();
      for (const t of timers.current.values()) clearTimeout(t);
      timers.current.clear();
    };
    // fetcher is a stable module-level api function; re-run only when ids change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  const register = useCallback((id: string) => {
    let cb = cbCache.current.get(id);
    if (!cb) {
      cb = (el: HTMLElement | null) => {
        const obs = observerRef.current;
        if (!obs || !el) return;
        elToId.current.set(el, id);
        obs.observe(el);
      };
      cbCache.current.set(id, cb);
    }
    return cb;
  }, []);

  return { counts, loading, register };
}
