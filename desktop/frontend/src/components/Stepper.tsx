import { Check } from "lucide-react";
import { cn } from "../lib/cn";

export interface Step {
  key: string;
  label: string;
}

interface Props {
  steps: Step[];
  currentIndex: number;
}

// A vertical progress rail. Completed steps fill solid white (dark check),
// the active step gets a ringed badge, upcoming steps stay muted. A faint
// connector line threads the dots together.
export function Stepper({ steps, currentIndex }: Props) {
  return (
    <nav className="flex flex-col">
      {steps.map((step, i) => {
        const state = i < currentIndex ? "done" : i === currentIndex ? "active" : "todo";
        const last = i === steps.length - 1;
        return (
          <div key={step.key} className="relative flex items-center gap-2.5 py-1.5">
            {/* connector */}
            {!last && (
              <span
                className={cn(
                  "absolute left-3 top-[1.85rem] h-[calc(100%-1rem)] w-px -translate-x-1/2",
                  i < currentIndex ? "bg-foreground/40" : "bg-border",
                )}
              />
            )}
            <span
              className={cn(
                "relative z-10 grid h-6 w-6 shrink-0 place-items-center rounded-full text-xs font-semibold transition-all",
                state === "done" && "bg-primary text-primary-foreground shadow-glow",
                state === "active" && "bg-card text-foreground ring-2 ring-foreground shadow-glow",
                state === "todo" && "bg-foreground/[0.06] text-muted-foreground ring-1 ring-border",
              )}
            >
              {state === "done" ? <Check className="h-3.5 w-3.5" strokeWidth={3} /> : i + 1}
            </span>
            <span
              className={cn(
                "text-sm transition-colors",
                state === "active" ? "font-semibold text-foreground" : "text-muted-foreground",
              )}
            >
              {step.label}
            </span>
          </div>
        );
      })}
    </nav>
  );
}
