import { cn } from "../lib/cn";

type Tone = "error" | "warn" | "info";

interface Props {
  tone?: Tone;
  children: React.ReactNode;
  onDismiss?: () => void;
}

const tones: Record<Tone, string> = {
  error: "bg-danger-soft text-danger-strong border-danger/20",
  warn: "bg-warn-soft text-warn border-warn/30",
  info: "bg-accent-soft text-accent-strong border-accent/20",
};

export function Banner({ tone = "info", children, onDismiss }: Props) {
  return (
    <div
      className={cn(
        "flex items-start gap-3 rounded-xl border px-3.5 py-2.5 text-sm animate-fade-up",
        tones[tone],
      )}
    >
      <div className="flex-1 leading-relaxed">{children}</div>
      {onDismiss && (
        <button
          onClick={onDismiss}
          className="shrink-0 rounded-lg px-1 text-current/70 hover:bg-black/5"
          aria-label="Dismiss"
        >
          ✕
        </button>
      )}
    </div>
  );
}
