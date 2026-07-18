import { formatTimeShort } from "../lib/format";
import type { MessageDTO } from "../lib/types";
import { Avatar } from "./Avatar";

// MessageRain shows recently deleted messages cascading down, newest at top.
// Each card mimics a compact Discord message row so the user can see what was
// actually deleted rather than just raw strings.
export function MessageRain({ messages }: { messages: MessageDTO[] }) {
  const recent = messages.slice(-12).reverse();

  return (
    <div className="relative h-72 overflow-hidden rounded-2xl border border-line bg-plate/30 p-2">
      {recent.length === 0 ? (
        <div className="grid h-full place-items-center text-xs text-dim">
          Deleted messages will appear here…
        </div>
      ) : (
        <div className="flex flex-col gap-1.5">
          {recent.map((m, i) => {
            const img = m.attachments?.find((a) => a.isImage);
            const nonImageAttachments = m.attachments?.filter((a) => !a.isImage) ?? [];
            const opacity = Math.max(0.15, 1 - i * 0.08);
            return (
              <div
                key={m.id}
                style={{ opacity }}
                className="glass flex animate-rain-in items-start gap-2 rounded-xl px-2.5 py-2"
              >
                <Avatar
                  url={m.authorAvatarUrl}
                  name={m.authorName || "?"}
                  size={22}
                  rounded="full"
                />
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-baseline gap-x-1.5">
                    <span className="max-w-[7rem] truncate text-xs font-semibold text-ink">
                      {m.authorName || "You"}
                    </span>
                    {m.channelName && (
                      <span className="text-[10px] font-medium text-accent-strong">
                        #{m.channelName}
                      </span>
                    )}
                    <span className="shrink-0 text-[10px] text-dim">
                      {formatTimeShort(m.timestamp)}
                    </span>
                  </div>
                  {m.content ? (
                    <div className="line-clamp-2 text-xs text-ink/70">{m.content}</div>
                  ) : img ? (
                    <div className="text-[10px] italic text-dim">📎 {img.filename}</div>
                  ) : nonImageAttachments.length > 0 ? (
                    <div className="text-[10px] italic text-dim">
                      📎 {nonImageAttachments[0].filename}
                    </div>
                  ) : (
                    <div className="text-[10px] italic text-dim">Embed / sticker</div>
                  )}
                </div>
                {img && (
                  <img
                    src={img.url}
                    alt=""
                    loading="lazy"
                    onError={(e) => (e.currentTarget.style.display = "none")}
                    className="h-8 w-8 shrink-0 rounded-lg object-cover ring-1 ring-line"
                  />
                )}
              </div>
            );
          })}
        </div>
      )}
      {/* Fade tail so cards dissolve as they sink */}
      <div className="pointer-events-none absolute inset-x-0 bottom-0 h-20 bg-gradient-to-t from-canvas to-transparent" />
    </div>
  );
}
