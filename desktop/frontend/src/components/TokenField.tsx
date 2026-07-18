import { useState } from "react";
import { cn } from "../lib/cn";

interface Props {
  value: string;
  onChange: (v: string) => void;
  onSubmit?: () => void;
  invalid?: boolean;
}

export function TokenField({ value, onChange, onSubmit, invalid }: Props) {
  const [show, setShow] = useState(false);
  return (
    <div className="relative">
      <input
        type={show ? "text" : "password"}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && onSubmit?.()}
        placeholder="Paste your Discord token"
        autoComplete="off"
        spellCheck={false}
        className={cn(
          "h-10 w-full rounded-xl border bg-plate/80 px-3.5 pr-20 font-mono text-sm text-ink placeholder:font-sans placeholder:text-dim focus:outline-none focus:ring-2",
          invalid
            ? "border-danger/40 focus:border-danger focus:ring-danger/20"
            : "border-line focus:border-accent focus:ring-accent/30",
        )}
      />
      <button
        type="button"
        onClick={() => setShow((s) => !s)}
        className="absolute right-2 top-1/2 -translate-y-1/2 rounded-xl px-3 py-1.5 text-xs font-medium text-dim hover:bg-ink/5"
      >
        {show ? "Hide" : "Show"}
      </button>
    </div>
  );
}
