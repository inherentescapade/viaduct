import { useMemo, useState } from "react";
import { cn } from "../lib/cn";
import { channelTypeLabel, formatCount } from "../lib/format";
import type { ChannelDTO } from "../lib/types";
import { Avatar } from "./Avatar";
import { Spinner } from "./Spinner";

interface Props {
  channels: ChannelDTO[];
  selected: Set<string>;
  onToggle: (id: string) => void;
  onAll: () => void;
  onNone: () => void;
  // When true, rows show a recipient avatar instead of a "#" (used for DMs).
  avatars?: boolean;
  // counts maps channel ID -> the user's message count there, shown per row as
  // it arrives. Channels without an entry yet show nothing.
  counts?: Record<string, number>;
  // loading maps channel ID -> whether its count query is currently in flight,
  // so the row can show a spinner until the count arrives.
  loading?: Record<string, boolean>;
  // register attaches a row to a visibility observer so its count is only
  // fetched when the row scrolls into view.
  register?: (id: string) => (el: HTMLElement | null) => void;
}

export function ChannelList({ channels, selected, onToggle, onAll, onNone, avatars, counts, loading, register }: Props) {
  const [query, setQuery] = useState("");
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return channels;
    return channels.filter((c) => c.name.toLowerCase().includes(q));
  }, [channels, query]);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-3">
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Filter channels…"
          className="h-9 flex-1 rounded-xl border border-line bg-plate/70 px-3.5 text-sm text-ink placeholder:text-dim focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30"
        />
        <span className="text-sm font-medium text-dim">
          {selected.size} / {channels.length} selected
        </span>
      </div>

      <div className="flex items-center gap-2 text-sm">
        <button onClick={onAll} className="rounded-full bg-ink/5 px-3 py-1 font-medium text-ink hover:bg-ink/10">
          Select all
        </button>
        <button onClick={onNone} className="rounded-full bg-ink/5 px-3 py-1 font-medium text-ink hover:bg-ink/10">
          Clear
        </button>
      </div>

      <div className="max-h-[280px] overflow-y-auto rounded-xl border border-line bg-plate/40 p-1">
        {filtered.length === 0 && (
          <div className="px-3 py-5 text-center text-sm text-dim">No channels match “{query}”.</div>
        )}
        {filtered.map((c) => {
          const on = selected.has(c.id);
          return (
            <button
              key={c.id}
              ref={register?.(c.id)}
              onClick={() => onToggle(c.id)}
              className={cn(
                "flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left transition-colors",
                on ? "bg-accent-soft/70" : "hover:bg-ink/5",
              )}
            >
              <span
                className={cn(
                  "grid h-5 w-5 shrink-0 place-items-center rounded-md border text-xs text-white transition-all",
                  on ? "border-accent-strong bg-accent-strong" : "border-ink/20 bg-plate",
                )}
              >
                {on && "✓"}
              </span>
              {avatars && <Avatar url={c.avatarUrl} name={c.name} size={24} rounded="full" />}
              <span className="flex-1 truncate text-sm text-ink">
                {!avatars && <span className="text-dim">#</span>}
                {c.name}
              </span>
              {counts?.[c.id] !== undefined ? (
                <span className="shrink-0 text-xs tabular-nums text-dim">
                  {formatCount(counts[c.id])} msg{counts[c.id] === 1 ? "" : "s"}
                </span>
              ) : loading?.[c.id] ? (
                <Spinner className="h-3.5 w-3.5 shrink-0" />
              ) : null}
              {c.nsfw && (
                <span className="rounded-md bg-danger-soft px-1.5 py-0.5 text-xs font-semibold uppercase text-danger-strong">
                  nsfw
                </span>
              )}
              <span className="rounded-md bg-ink/5 px-1.5 py-0.5 text-xs font-medium uppercase text-dim">
                {channelTypeLabel(c.type)}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
