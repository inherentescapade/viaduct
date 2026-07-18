import { Button } from "./Button";

interface Props {
  // Null while the scope count is still being computed: the action stays
  // available (we never block on the count), and the copy reads generically
  // until the number lands.
  count: number | null;
  scopeLabel: string; // e.g. "Friends server" or "3 conversations"
  onConfirm: () => void;
  onBack: () => void;
  // When set, the action is dispatched to a remote server rather than run
  // locally, so the copy and button label adapt accordingly.
  remoteName?: string | null;
  // Whether pinned messages are preserved by this run. Spelled out here so the
  // final confirmation makes the pin behavior explicit either way.
  pinsKept?: boolean;
}

// A final confirmation for an irreversible action: a clear warning and a single
// delete/dispatch button.
export function ConfirmGate({ count, scopeLabel, onConfirm, onBack, remoteName, pinsKept }: Props) {
  const isRemote = !!remoteName;
  // "5 messages" once known, otherwise a count-agnostic phrase.
  const noun = count === null ? "messages" : `${count.toLocaleString()} message${count === 1 ? "" : "s"}`;
  const verb = isRemote ? "Dispatch" : "Delete";
  return (
    <div className="flex flex-col gap-4">
      <div className="rounded-xl border border-danger/20 bg-danger-soft/60 p-4">
        <div className="flex items-start gap-3">
          <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-danger/15 text-base">
            ⚠️
          </span>
          <div>
            <div className="text-sm font-semibold text-danger-strong">
              This permanently deletes {noun}.
            </div>
            <p className="mt-1 text-sm leading-relaxed text-danger-strong/80">
              From {scopeLabel}. Deleted messages cannot be recovered.
              {pinsKept === undefined
                ? ""
                : pinsKept
                  ? " Pinned messages are kept."
                  : " Pinned messages are deleted too."}
              {isRemote
                ? ` This runs on your server (${remoteName}) and keeps going even if you close this app.`
                : " A log of everything removed is saved locally."}
            </p>
          </div>
        </div>
      </div>

      <div className="flex items-center justify-between">
        <Button variant="ghost" onClick={onBack}>
          ← Back
        </Button>
        <Button variant="danger" size="lg" onClick={onConfirm}>
          {verb} {noun}
        </Button>
      </div>
    </div>
  );
}
