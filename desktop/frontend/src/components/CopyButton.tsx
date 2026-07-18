import { useState } from "react";
import { cn } from "../lib/cn";

interface Props {
  value: string;
  label?: string;
  className?: string;
}

// A small inline "copy to clipboard" button with brief confirmation. Used in the
// self-hosting setup where the user copies keys and commands to their server.
export function CopyButton({ value, label = "Copy", className }: Props) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
    } catch {
      // Fallback for webviews without the async clipboard API.
      const ta = document.createElement("textarea");
      ta.value = value;
      document.body.appendChild(ta);
      ta.select();
      try {
        document.execCommand("copy");
      } catch {
        /* ignore */
      }
      document.body.removeChild(ta);
    }
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }

  return (
    <button
      onClick={copy}
      className={cn(
        "shrink-0 rounded-lg border border-line bg-plate/60 px-2.5 py-1 text-xs font-medium text-dim transition-colors hover:text-ink",
        className,
      )}
    >
      {copied ? "Copied ✓" : label}
    </button>
  );
}
