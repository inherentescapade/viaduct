export function formatDuration(ms: number): string {
  if (!ms || ms < 0) return "0:00";
  const total = Math.floor(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const mm = String(m).padStart(h > 0 ? 2 : 1, "0");
  const ss = String(s).padStart(2, "0");
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`;
}

export function formatEta(ms: number): string {
  if (!ms || ms <= 0) return "—";
  return `~${formatDuration(ms)}`;
}

export function formatRate(perSec: number): string {
  if (!perSec || perSec <= 0) return "0.0/s";
  return `${perSec.toFixed(1)}/s`;
}

export function formatCount(n: number): string {
  return n.toLocaleString();
}

// formatBytes renders a byte count as a compact human-readable size (e.g.
// "3.2 MB"), used by the export download progress bar.
export function formatBytes(n: number): string {
  if (!n || n < 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  const value = n / Math.pow(1024, i);
  return `${i === 0 ? value : value.toFixed(1)} ${units[i]}`;
}

export function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

// Compact timestamp: time only for today, otherwise "Jan 1" or "Jan 1, 2023".
export function formatTimeShort(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const now = new Date();
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();
  if (sameDay) {
    return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  }
  const opts: Intl.DateTimeFormatOptions =
    d.getFullYear() === now.getFullYear()
      ? { month: "short", day: "numeric" }
      : { month: "short", day: "numeric", year: "numeric" };
  return d.toLocaleDateString(undefined, opts);
}

// channelTypeLabel maps a Discord channel type integer to a short label.
export function channelTypeLabel(type: number): string {
  switch (type) {
    case 0:
      return "text";
    case 1:
      return "dm";
    case 3:
      return "group";
    case 5:
      return "news";
    default:
      return `type ${type}`;
  }
}
