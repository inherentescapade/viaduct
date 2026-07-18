import type { FeedMessageDTO, UserDTO } from "../lib/types";
import { formatTimeShort } from "../lib/format";
import { Avatar } from "./Avatar";

interface Props {
  messages: FeedMessageDTO[];
  // empty placeholder text
  empty?: string;
  max?: number;
  // The signed-in Discord account, used as the name/avatar fallback for
  // messages the backend didn't tag with author info (every deleted message
  // is always this user's own).
  self?: UserDTO | null;
}

// DeletionFeed renders a live, newest-first list of deleted messages, the same
// kind of feed shown during a local deletion, for remote jobs and monitors.
// Each row leads with the author's avatar and name to match the local views.
export function DeletionFeed({ messages, empty = "Waiting for deletions…", max = 60, self }: Props) {
  if (messages.length === 0) {
    return <div className="px-3.5 py-6 text-center text-xs text-dim">{empty}</div>;
  }
  const selfName = self ? self.globalName || self.username : "";
  const rows = [...messages].slice(-max).reverse();
  return (
    <div className="max-h-72 overflow-y-auto rounded-xl border border-line bg-plate/40">
      <div className="divide-y divide-line/60">
        {rows.map((m, i) => (
          <div key={`${m.timestamp}-${i}`} className="flex items-start gap-2.5 px-3 py-1.5">
            <Avatar
              url={m.authorAvatarUrl || self?.avatarUrl}
              name={m.authorName || selfName || "?"}
              size={20}
              rounded="full"
              className="mt-0.5"
            />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-baseline gap-x-1.5">
                <span className="max-w-[7rem] truncate text-[11px] font-semibold text-ink">
                  {m.authorName || selfName || "You"}
                </span>
                {m.channel && (
                  <span className="max-w-[120px] truncate text-[11px] text-accent-strong/80">{m.channel}</span>
                )}
                <span className="shrink-0 font-mono text-[10px] text-dim">
                  {m.timestamp ? formatTimeShort(m.timestamp) : ""}
                </span>
              </div>
              <div className="truncate text-xs text-ink/90">
                {m.content.trim() || <span className="text-dim">[no text]</span>}
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
