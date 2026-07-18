import { useState } from "react";
import { cn } from "../lib/cn";

interface Props {
  url?: string;
  name: string;
  size?: number; // px
  rounded?: "full" | "2xl";
  className?: string;
}

// An image avatar/icon that gracefully falls back to a coloured initial when
// the URL is empty or fails to load (so a missing CDN image never breaks layout).
export function Avatar({ url, name, size = 32, rounded = "2xl", className }: Props) {
  const [broken, setBroken] = useState(false);
  const initial = (name || "?").trim().charAt(0).toUpperCase() || "?";
  const radius = rounded === "full" ? "rounded-full" : "rounded-xl";
  const dim = { width: size, height: size };

  if (!url || broken) {
    return (
      <span
        style={dim}
        className={cn(
          "grid shrink-0 place-items-center bg-ink/5 font-semibold text-dim",
          radius,
          className,
        )}
      >
        {initial}
      </span>
    );
  }

  return (
    <img
      src={url}
      alt={name}
      style={dim}
      loading="lazy"
      onError={() => setBroken(true)}
      className={cn("shrink-0 object-cover", radius, className)}
    />
  );
}
