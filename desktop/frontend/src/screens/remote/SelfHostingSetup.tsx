import { useState } from "react";
import { api } from "../../lib/bridge";
import type { ActingAsDTO } from "../../lib/types";
import { Button } from "../../components/Button";
import { Banner } from "../../components/Banner";
import { Card } from "../../components/Card";
import { CopyButton } from "../../components/CopyButton";
import { StepHeader } from "../../components/StepHeader";
import { WizardShell } from "../../components/WizardShell";
import type { Step } from "../../components/Stepper";

const steps: Step[] = [
  { key: "intro", label: "How it works" },
  { key: "pair", label: "Pair" },
  { key: "finish", label: "Finish" },
];

const inputCls =
  "h-9 w-full rounded-xl border border-line bg-plate/70 px-3.5 text-sm text-ink placeholder:text-dim focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30";

interface Props {
  onConfigured: () => void;
}

// A code/command line with a copy button.
function CommandRow({ value }: { value: string }) {
  return (
    <div className="flex items-center gap-2 rounded-xl border border-line bg-canvas/70 px-3 py-2">
      <code className="flex-1 select-all break-all font-mono text-xs text-ink">{value}</code>
      <CopyButton value={value} />
    </div>
  );
}

export function SelfHostingSetup({ onConfigured }: Props) {
  const [index, setIndex] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [address, setAddress] = useState("");
  const [code, setCode] = useState("");
  const [requested, setRequested] = useState(false);
  const [paired, setPaired] = useState(false);
  const [actingAs, setActingAs] = useState<ActingAsDTO | null>(null);

  function next() {
    setError(null);
    setIndex((i) => Math.min(i + 1, steps.length - 1));
  }
  function back() {
    setError(null);
    setIndex((i) => Math.max(i - 1, 0));
  }

  async function run<T>(fn: () => Promise<T>, after?: (v: T) => void) {
    setBusy(true);
    setError(null);
    try {
      const v = await fn();
      after?.(v);
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <WizardShell steps={steps} currentIndex={index}>
      <Card>
        {error && (
          <div className="mb-3">
            <Banner tone="error">{error}</Banner>
          </div>
        )}

        {index === 0 && (
          <>
            <StepHeader
              eyebrow="Self-hosting"
              title="Run viaduct on a machine that's always on"
              subtitle="Put the viaduct server on a cheap always-on box (a VPS). It holds your job and keeps deleting (or running automatic cleanups) even when this app is closed. You control it from here over an end-to-end encrypted channel that only you can pair into."
            />
            <div className="space-y-3 text-sm text-dim">
              <p>On your server, install viaduct and start it:</p>
              <CommandRow value="viaduct serve --port 21776" />
              <p>
                Keep that terminal handy. On the next step you'll request a{" "}
                <span className="text-ink">6-digit code</span> that appears there — enter it back
                here to pair. No keys to copy.
              </p>
            </div>
            <div className="mt-5 flex justify-end">
              <Button onClick={next}>Continue</Button>
            </div>
          </>
        )}

        {index === 1 && (
          <>
            <StepHeader
              eyebrow="Step 1"
              title="Pair with your server"
              subtitle="Enter the address you're running the server on, then request a code. It appears in the server's terminal — enter it back here to authorize this app. Your client key is created and the server's key learned automatically; nothing is copied by hand."
            />
            <div className="space-y-3">
              <div>
                <label className="mb-1 block text-xs font-medium text-dim">Server address</label>
                <input
                  className={inputCls}
                  placeholder="vps.example.com:21776"
                  value={address}
                  disabled={requested}
                  onChange={(e) => setAddress(e.target.value)}
                />
              </div>
              {requested && (
                <div>
                  <label className="mb-1 block text-xs font-medium text-dim">
                    Code shown in your server's terminal
                  </label>
                  <input
                    className={inputCls}
                    placeholder="123456"
                    inputMode="numeric"
                    autoFocus
                    value={code}
                    onChange={(e) => setCode(e.target.value)}
                  />
                </div>
              )}
            </div>
            <div className="mt-5 flex justify-between">
              <Button variant="ghost" onClick={back}>
                Back
              </Button>
              {!requested ? (
                <Button
                  disabled={busy || !address.trim()}
                  onClick={() =>
                    run(
                      () => api.remotePairRequest(address.trim()),
                      () => setRequested(true),
                    )
                  }
                >
                  {busy ? "Requesting…" : "Request code"}
                </Button>
              ) : (
                <Button
                  disabled={busy || !code.trim()}
                  onClick={() =>
                    run(
                      () => api.remotePairComplete(address.trim(), code.trim()),
                      (a) => {
                        setPaired(true);
                        setActingAs(a);
                        next();
                      },
                    )
                  }
                >
                  {busy ? "Pairing…" : "Pair"}
                </Button>
              )}
            </div>
          </>
        )}

        {index === 2 && (
          <>
            <StepHeader
              eyebrow="Step 2"
              title="You're paired"
              subtitle="This app is now authorized on your server over an end-to-end encrypted channel."
            />
            {actingAs ? (
              <Banner tone="info">
                Done. Your server is now acting as{" "}
                <span className="font-semibold">{actingAs.username}</span>. You can manage tasks
                from here.
              </Banner>
            ) : (
              <p className="text-sm text-dim">
                Paired. Send your Discord token so the server can act as you — it's detected here
                and sent inside the encrypted channel, never shown or stored in plain text on the
                wire.
              </p>
            )}
            <div className="mt-5 flex justify-between">
              <Button variant="ghost" onClick={back}>
                Back
              </Button>
              {actingAs ? (
                <Button onClick={onConfigured}>Open Server tab</Button>
              ) : (
                <div className="flex gap-2">
                  <Button
                    variant="subtle"
                    disabled={busy || !paired}
                    onClick={() => run(() => api.remoteConnect(), (a) => setActingAs(a))}
                  >
                    {busy ? "Sending…" : "Send token"}
                  </Button>
                  <Button onClick={onConfigured}>Open Server tab</Button>
                </div>
              )}
            </div>
          </>
        )}
      </Card>
    </WizardShell>
  );
}
