import type { FailReasonDTO } from "../lib/types";

export function FailureTable({ failures }: { failures: FailReasonDTO[] }) {
  if (failures.length === 0) return null;
  return (
    <div className="rounded-xl border border-line bg-plate/50 p-3.5">
      <div className="mb-1.5 text-sm font-semibold text-ink">Failures by reason</div>
      <div className="flex flex-col divide-y divide-line">
        {failures.map((f) => (
          <div key={f.reason} className="flex items-center justify-between gap-4 py-2 text-sm">
            <span className="truncate text-dim">{f.reason}</span>
            <span className="font-mono tabular-nums text-danger-strong">{f.count.toLocaleString()}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
