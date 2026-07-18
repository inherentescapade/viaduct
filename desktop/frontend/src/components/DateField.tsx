import { cn } from "../lib/cn";

interface Props {
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  hint?: string;
  invalid?: boolean;
}

export function DateField({ label, value, onChange, placeholder, hint, invalid }: Props) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-sm font-medium text-ink">{label}</span>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder ?? "YYYY-MM-DD or 30d"}
        className={cn(
          "h-9 rounded-xl border bg-plate/70 px-3.5 font-mono text-sm text-ink placeholder:font-sans placeholder:text-dim focus:outline-none focus:ring-2",
          invalid
            ? "border-danger/40 focus:border-danger focus:ring-danger/20"
            : "border-line focus:border-accent focus:ring-accent/30",
        )}
      />
      <span className={cn("text-xs", invalid ? "text-danger-strong" : "text-dim")}>
        {invalid ? "Unrecognised date. Try 2024-01-01, 30d, 24h, or 60m." : hint}
      </span>
    </label>
  );
}
