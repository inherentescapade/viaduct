import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../../lib/bridge";
import { dateValid } from "../../lib/datecheck";
import { formatCount } from "../../lib/format";
import { useRun } from "../../lib/useRun";
import type { CountDTO, ExportSummaryDTO, ImportRequest } from "../../lib/types";
import { Banner } from "../../components/Banner";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { ConfirmGate } from "../../components/ConfirmGate";
import { DateField } from "../../components/DateField";
import { FailureTable } from "../../components/FailureTable";
import { ProgressMeter } from "../../components/ProgressMeter";
import { Spinner } from "../../components/Spinner";
import { StepHeader } from "../../components/StepHeader";
import { WizardShell } from "../../components/WizardShell";

type Step = "source" | "filter" | "confirm" | "progress" | "done";

const STEPS = [
  { key: "source", label: "Source" },
  { key: "filter", label: "Filter" },
  { key: "confirm", label: "Confirm" },
];

function stepIndex(step: Step): number {
  const i = STEPS.findIndex((s) => s.key === step);
  return i === -1 ? STEPS.length : i;
}

function splitTokens(s: string): string[] {
  return s
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);
}

export function ImportWizard({ skipConfirm }: { skipConfirm: boolean }) {
  const run = useRun();
  const [step, setStep] = useState<Step>("source");
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    document.querySelector("main")?.scrollTo(0, 0);
  }, [step]);

  // source
  const [path, setPath] = useState("");
  const [loading, setLoading] = useState(false);
  const [summary, setSummary] = useState<ExportSummaryDTO | null>(null);

  // filter
  const [include, setInclude] = useState("");
  const [exclude, setExclude] = useState("");
  const [forgotten, setForgotten] = useState(false);
  const [noDms, setNoDms] = useState(false);
  const [before, setBefore] = useState("");
  const [after, setAfter] = useState("");
  const [count, setCount] = useState<CountDTO | null>(null);
  const [counting, setCounting] = useState(false);

  // progress
  const [stopping, setStopping] = useState(false);

  const filtersValid = dateValid(before) && dateValid(after);

  const request = (): ImportRequest => ({
    include: splitTokens(include),
    exclude: splitTokens(exclude),
    forgotten,
    noDms,
    before: before.trim(),
    after: after.trim(),
  });

  useEffect(() => {
    if (run.finished && step === "progress") {
      setStep("done");
      setStopping(false);
    }
  }, [run.finished, step]);

  // Debounced live recount whenever filter inputs change on the filter step.
  const debounce = useRef<number | undefined>(undefined);
  useEffect(() => {
    if (step !== "filter" || !summary || !filtersValid) return;
    window.clearTimeout(debounce.current);
    setCounting(true);
    debounce.current = window.setTimeout(async () => {
      try {
        setCount(await api.countImport(request()));
        setError(null);
      } catch (e) {
        setError(String(e instanceof Error ? e.message : e));
      } finally {
        setCounting(false);
      }
    }, 250);
    return () => window.clearTimeout(debounce.current);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [include, exclude, forgotten, noDms, before, after, step, summary]);

  async function browse() {
    try {
      const p = await api.pickExportFolder();
      if (p) {
        setPath(p);
        await load(p);
      }
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    }
  }

  async function load(p: string) {
    if (!p.trim()) return;
    setLoading(true);
    setError(null);
    try {
      const s = await api.loadExport(p.trim());
      setSummary(s);
      setStep("filter");
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setLoading(false);
    }
  }

  async function startImport() {
    run.reset();
    setStopping(false);
    setError(null);
    setStep("progress");
    try {
      await api.startImport(request());
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
      setStep("confirm");
    }
  }

  async function cancel() {
    setStopping(true);
    await api.cancel();
  }

  function restart() {
    run.reset();
    setStep("source");
    setSummary(null);
    setPath("");
    setCount(null);
    setInclude("");
    setExclude("");
    setForgotten(false);
    setNoDms(false);
    setBefore("");
    setAfter("");
  }

  const visibleChannels = useMemo(() => summary?.channels ?? [], [summary]);

  return (
    <WizardShell
      steps={STEPS}
      currentIndex={stepIndex(step)}
      aside={
        summary && (
          <div className="glass rounded-3xl p-3.5 text-sm">
            <div className="text-xs font-semibold uppercase tracking-wide text-dim">Package</div>
            <div className="mt-1 break-words font-mono text-xs text-ink">{summary.root}</div>
            <div className="mt-2 text-xs text-dim">
              {summary.channels.length} channels · {formatCount(summary.totalMessages)} messages
            </div>
          </div>
        )
      }
    >
      {error && (
        <div className="mb-4">
          <Banner tone="error" onDismiss={() => setError(null)}>
            {error}
          </Banner>
        </div>
      )}

      {step === "source" && (
        <Card>
          <StepHeader
            eyebrow="Step 1"
            title="Load a data package"
            subtitle={'Delete messages using a Discord “data package” export. Only servers you currently have access to can be cleared.'}
          />
          <div className="rounded-xl border border-dashed border-line bg-plate/40 p-5 text-center">
            <div className="mx-auto mb-3 grid h-11 w-11 place-items-center rounded-xl bg-accent-soft text-accent-strong">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" aria-hidden>
                <path
                  d="M4 7a2 2 0 0 1 2-2h4l2 2h6a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V7Z"
                  stroke="currentColor"
                  strokeWidth="1.7"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            </div>
            <p className="mb-4 text-sm text-dim">Select the unzipped package folder (or its Messages folder).</p>
            <Button size="lg" onClick={browse} disabled={loading}>
              {loading ? <Spinner className="border-white/40 border-t-white" /> : "Choose folder…"}
            </Button>
          </div>
          <div className="mt-3 flex items-center gap-2">
            <input
              value={path}
              onChange={(e) => setPath(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && load(path)}
              placeholder="…or paste a path"
              className="h-9 flex-1 rounded-xl border border-line bg-plate/70 px-3.5 font-mono text-sm text-ink placeholder:font-sans placeholder:text-dim focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30"
            />
            <Button variant="subtle" onClick={() => load(path)} disabled={loading || !path.trim()}>
              Load
            </Button>
          </div>
        </Card>
      )}

      {step === "filter" && summary && (
        <Card>
          <StepHeader eyebrow="Step 2" title="Choose channels" subtitle="Filters apply across the package." />

          <div className="mb-3 grid grid-cols-2 gap-3">
            <label className="flex cursor-pointer items-center gap-2.5 rounded-xl border border-line bg-plate/50 px-3.5 py-2.5 text-sm">
              <input type="checkbox" checked={forgotten} onChange={(e) => setForgotten(e.target.checked)} className="h-4 w-4 accent-white" />
              <span className="text-ink">Only “forgotten” server channels</span>
            </label>
            <label className="flex cursor-pointer items-center gap-2.5 rounded-xl border border-line bg-plate/50 px-3.5 py-2.5 text-sm">
              <input type="checkbox" checked={noDms} onChange={(e) => setNoDms(e.target.checked)} className="h-4 w-4 accent-white" />
              <span className="text-ink">Skip direct messages</span>
            </label>
          </div>

          <div className="mb-3 grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1">
              <span className="text-sm font-medium text-ink">Include</span>
              <input
                value={include}
                onChange={(e) => setInclude(e.target.value)}
                placeholder="name, ID, or type…"
                className="h-9 rounded-xl border border-line bg-plate/70 px-3.5 text-sm text-ink placeholder:text-dim focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30"
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-sm font-medium text-ink">Exclude</span>
              <input
                value={exclude}
                onChange={(e) => setExclude(e.target.value)}
                placeholder="name, ID, or type…"
                className="h-9 rounded-xl border border-line bg-plate/70 px-3.5 text-sm text-ink placeholder:text-dim focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30"
              />
            </label>
          </div>

          <div className="mb-3 grid grid-cols-2 gap-3">
            <DateField label="Before" value={before} onChange={setBefore} invalid={!dateValid(before)} />
            <DateField label="After" value={after} onChange={setAfter} invalid={!dateValid(after)} />
          </div>

          <div className="mb-4 max-h-44 overflow-y-auto rounded-xl border border-line bg-plate/40 p-1">
            {visibleChannels.map((c) => (
              <div key={c.id} className="flex items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-sm">
                <span className="flex-1 truncate text-ink">{c.name}</span>
                {c.isForgotten && (
                  <span className="rounded-md bg-warn-soft px-1.5 py-0.5 text-xs font-semibold uppercase text-warn">
                    forgotten
                  </span>
                )}
                <span className="rounded-md bg-ink/5 px-1.5 py-0.5 text-xs font-medium uppercase text-dim">
                  {c.isDm ? "dm" : c.type.toLowerCase().replace("guild_", "")}
                </span>
                <span className="w-16 shrink-0 text-right font-mono text-xs tabular-nums text-dim">
                  {formatCount(c.messageCount)}
                </span>
              </div>
            ))}
          </div>

          <div className="flex items-center justify-between">
            <Button variant="ghost" onClick={() => setStep("source")}>
              ← Back
            </Button>
            <div className="flex items-center gap-3">
              <span className="text-sm text-dim">
                {counting ? (
                  <span className="inline-flex items-center gap-2">
                    <Spinner /> counting…
                  </span>
                ) : count ? (
                  <>
                    <span className="font-semibold text-ink">{formatCount(count.messages)}</span> messages ·{" "}
                    {count.channels} channels
                  </>
                ) : (
                  "—"
                )}
              </span>
              <Button
                variant="danger"
                onClick={() => (skipConfirm ? startImport() : setStep("confirm"))}
                disabled={!filtersValid || !count || count.messages === 0}
              >
                {skipConfirm ? "Delete →" : "Continue →"}
              </Button>
            </div>
          </div>
        </Card>
      )}

      {step === "confirm" && (
        <Card>
          <StepHeader eyebrow="Step 3" title="Confirm deletion" subtitle="This can't be undone." />
          <ConfirmGate
            count={count?.messages ?? 0}
            scopeLabel={`${count?.channels ?? 0} channel${count?.channels === 1 ? "" : "s"} in this package`}
            onBack={() => setStep("filter")}
            onConfirm={startImport}
          />
        </Card>
      )}

      {step === "progress" && (
        <Card>
          <StepHeader title="Deleting" subtitle="Working through the package channel by channel." />
          <ProgressMeter progress={run.progress} variant="import" stopping={stopping} />
          <div className="mt-5 flex justify-end">
            <Button variant="subtle" onClick={cancel} disabled={stopping}>
              {stopping ? "Stopping…" : "Stop"}
            </Button>
          </div>
        </Card>
      )}

      {step === "done" && (
        <Card>
          <div className="flex flex-col items-center py-1 text-center">
            <div className="mb-3 grid h-14 w-14 place-items-center rounded-full bg-accent-soft text-xl">
              {run.finished?.cancelled ? "✋" : "✓"}
            </div>
            <h2 className="text-base font-semibold tracking-tight text-ink">
              {run.finished?.cancelled ? "Stopped" : "All done"}
            </h2>
            <p className="mt-1 text-sm text-dim">
              {formatCount(run.progress?.deleted ?? 0)} deleted · {formatCount(run.progress?.skipped ?? 0)} already gone
              {(run.progress?.failed ?? 0) > 0 && (
                <span className="text-danger-strong"> · {formatCount(run.progress?.failed ?? 0)} failed</span>
              )}
            </p>
          </div>

          {run.failures.length > 0 && (
            <div className="mt-4">
              <FailureTable failures={run.failures} />
            </div>
          )}

          <div className="mt-5 flex items-center justify-center gap-2">
            <Button variant="subtle" onClick={() => api.openLogFolder()}>
              Open log folder
            </Button>
            {run.finished?.logPath && (
              <Button variant="subtle" onClick={() => api.openPath(run.finished!.logPath)}>
                Open log file
              </Button>
            )}
            <Button onClick={restart}>Start over</Button>
          </div>
        </Card>
      )}
    </WizardShell>
  );
}
