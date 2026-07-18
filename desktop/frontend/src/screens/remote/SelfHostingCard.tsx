import { useEffect, useState } from "react";
import { api } from "../../lib/bridge";
import type { PingDTO, RemoteDTO } from "../../lib/types";
import { Banner } from "../../components/Banner";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { StepHeader } from "../../components/StepHeader";

// SelfHostingCard surfaces the configured server in Settings: status, re-send
// token, and forget. Setup itself lives on the "Server" tab. onChange notifies
// the app shell when the connection state changes.
export function SelfHostingCard({ onChange }: { onChange?: () => void }) {
  const [remote, setRemote] = useState<RemoteDTO | null | undefined>(undefined);
  const [ping, setPing] = useState<PingDTO | null>(null);
  const [msg, setMsg] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  function load() {
    api
      .getRemote()
      .then((r) => {
        setRemote(r);
        if (r) api.remotePing().then(setPing).catch(() => setPing(null));
      })
      .catch(() => setRemote(null));
  }

  useEffect(load, []);

  async function reconnect() {
    setBusy(true);
    setError(null);
    setMsg(null);
    try {
      const a = await api.remoteConnect();
      setMsg(`Server is now acting as ${a.username}.`);
      api.remotePing().then(setPing).catch(() => {});
      onChange?.();
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setBusy(false);
    }
  }

  async function forget() {
    await api.forgetRemote().catch(() => {});
    setRemote(null);
    setPing(null);
    onChange?.();
  }

  if (remote === undefined) return null;

  return (
    <Card>
      <StepHeader title="Self-hosting" subtitle="A server you run elsewhere to own deletion jobs." />
      {!remote ? (
        <p className="text-sm text-dim">
          No server configured. Open the <span className="text-ink">Server</span> tab to set one up.
        </p>
      ) : (
        <div className="flex flex-col gap-3 text-sm">
          <div className="flex items-center justify-between gap-4 rounded-xl bg-plate/50 px-3.5 py-2.5">
            <span className="text-dim">Address</span>
            <span className="truncate font-mono text-xs text-ink">{remote.address}</span>
          </div>
          <div className="flex items-center justify-between gap-4 rounded-xl bg-plate/50 px-3.5 py-2.5">
            <span className="text-dim">Status</span>
            <span className="truncate text-xs text-ink">
              {ping
                ? ping.actingAs
                  ? `acting as ${ping.actingAs.username} · ${ping.jobs} task(s) · ${ping.monitors} monitor(s)`
                  : "connected, no token sent"
                : "unreachable"}
            </span>
          </div>
          {msg && (
            <Banner tone="info" onDismiss={() => setMsg(null)}>
              {msg}
            </Banner>
          )}
          {error && (
            <Banner tone="error" onDismiss={() => setError(null)}>
              {error}
            </Banner>
          )}
          <div className="flex flex-wrap gap-2 pt-1">
            <Button variant="subtle" onClick={reconnect} disabled={busy}>
              {busy ? "Sending…" : "Re-send token"}
            </Button>
            <Button variant="ghost" onClick={forget}>
              Forget server
            </Button>
          </div>
        </div>
      )}
    </Card>
  );
}
