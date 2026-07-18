import type { HTMLAttributes } from "react";
import { cn } from "../lib/cn";
import { Card as UICard } from "./ui/card";

interface Props extends HTMLAttributes<HTMLDivElement> {
  inset?: boolean;
}

// The frosted surface used for every primary panel: the shadcn/ui Card lifted
// with Viaduct's glass treatment, a top-edge highlight, padding and entrance
// animation so panels sit above the canvas instead of melting into it.
export function Card({ className, inset, ...rest }: Props) {
  return (
    <UICard
      className={cn(
        "glass edge-top rounded-3xl shadow-card",
        inset ? "p-3.5" : "p-5",
        "animate-fade-up",
        className,
      )}
      {...rest}
    />
  );
}
