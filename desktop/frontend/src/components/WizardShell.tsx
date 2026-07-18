import type { ReactNode } from "react";
import { Stepper, type Step } from "./Stepper";

interface Props {
  steps: Step[];
  currentIndex: number;
  aside?: ReactNode;
  children: ReactNode;
}

// Two-column wizard scaffold: a calm stepper rail on the left, the active step's
// content on the right.
export function WizardShell({ steps, currentIndex, aside, children }: Props) {
  return (
    <div className="mx-auto flex min-h-[calc(100vh-6rem)] w-full max-w-5xl items-center px-5 py-6">
      <div className="grid w-full grid-cols-[200px_1fr] gap-5">
        <div className="flex flex-col gap-3">
          <div className="glass edge-top rounded-3xl p-4">
            <Stepper steps={steps} currentIndex={currentIndex} />
          </div>
          {aside}
        </div>
        <div className="min-w-0">{children}</div>
      </div>
    </div>
  );
}
