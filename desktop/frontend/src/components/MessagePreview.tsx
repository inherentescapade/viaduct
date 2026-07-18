import { formatTimestamp } from "../lib/format";
import type { MessageDTO } from "../lib/types";
import { Avatar } from "./Avatar";

interface Props {
  messages: MessageDTO[];
  max?: number;
}

// A compact, readable list of messages with author, timestamp, content, and
// inline image thumbnails. Used in the server-scope preview and the dry-run list.
export function MessagePreview({ messages, max = 200 }: Props) {
  const rows = messages.slice(0, max);
  return (
    <div className="flex flex-col divide-y divide-line/70">
      {rows.map((m) => {
        const images = m.attachments?.filter((a) => a.isImage) ?? [];
        return (
          <div key={m.id} className="flex gap-2.5 py-2">
            <Avatar url={m.authorAvatarUrl} name={m.authorName} size={28} rounded="full" />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                <span className="text-sm font-semibold text-ink">
                  {m.authorName || "You"}
                </span>
                {m.channelName && (
                  <span className="text-xs text-accent-strong">#{m.channelName}</span>
                )}
                <span className="shrink-0 text-xs text-dim">{formatTimestamp(m.timestamp)}</span>
              </div>
              {m.content && (
                <div className="mt-0.5 whitespace-pre-wrap break-words text-sm text-ink/80">
                  {m.content}
                </div>
              )}
              {images.length > 0 && (
                <div className="mt-2 flex flex-wrap gap-2">
                  {images.slice(0, 4).map((a, i) => (
                    <img
                      key={i}
                      src={a.url}
                      alt={a.filename}
                      loading="lazy"
                      onError={(e) => (e.currentTarget.style.display = "none")}
                      className="h-16 w-16 rounded-xl object-cover ring-1 ring-line"
                    />
                  ))}
                </div>
              )}
              {!m.content && images.length === 0 && (
                <div className="mt-0.5 text-sm italic text-dim">Embed / sticker</div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
