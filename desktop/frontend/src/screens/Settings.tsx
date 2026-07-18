import { useEffect, useState } from "react";
import { api } from "../lib/bridge";
import type { ConfigInfoDTO, UserDTO } from "../lib/types";
import { Avatar } from "../components/Avatar";
import { Banner } from "../components/Banner";
import { Button } from "../components/Button";
import { Card } from "../components/Card";
import { Spinner } from "../components/Spinner";
import { StepHeader } from "../components/StepHeader";
import { TokenField } from "../components/TokenField";
import { Switch } from "../components/ui/switch";
import { SelfHostingCard } from "./remote/SelfHostingCard";

interface Props {
  user: UserDTO | null;
  onReauth: (user: UserDTO) => void;
  onSignOut: () => void;
  skipConfirm: boolean;
  onSkipConfirmChange: (v: boolean) => void;
  preScan: boolean;
  onPreScanChange: (v: boolean) => void;
  onRemoteChange?: () => void;
}

function fmtBytes(n: number): string {
  if (n === 0) return "empty";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

export function Settings({ user, onReauth, onSignOut, skipConfirm, onSkipConfirmChange, preScan, onPreScanChange, onRemoteChange }: Props) {
  const [info, setInfo] = useState<ConfigInfoDTO | null>(null);
  const [token, setToken] = useState("");
  const [botMode, setBotMode] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [clearingLogs, setClearingLogs] = useState(false);
  const [logsCleared, setLogsCleared] = useState(false);

  function loadInfo() {
    api.configInfo().then(setInfo).catch(() => {});
  }

  useEffect(() => {
    loadInfo();
    api.getSavedToken().then((s) => setBotMode(s.botMode)).catch(() => {});
  }, []);

  async function reauth() {
    if (!token.trim() || busy) return;
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      const u = await api.validateToken(token.trim(), botMode);
      onReauth(u);
      setToken("");
      setSaved(true);
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setBusy(false);
    }
  }

  async function clearLogs() {
    if (clearingLogs) return;
    setClearingLogs(true);
    setLogsCleared(false);
    try {
      await api.clearLogs();
      setLogsCleared(true);
      loadInfo();
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setClearingLogs(false);
    }
  }

  async function signOut() {
    try {
      await api.clearSession();
      onSignOut();
    } catch {
      onSignOut();
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-3 px-5 pb-6">
      <Card>
        <StepHeader title="Account" subtitle="Update the Discord token Viaduct uses." />
        {user && (
          <div className="mb-3 flex items-center gap-3 rounded-xl bg-plate/50 p-3.5">
            <Avatar url={user.avatarUrl} name={user.globalName || user.username} size={40} rounded="full" />
            <div className="flex-1">
              <div className="text-sm font-semibold text-ink">{user.globalName || user.username}</div>
              <div className="font-mono text-xs text-dim">{user.id}</div>
            </div>
            <Button variant="subtle" onClick={signOut}>
              Sign out
            </Button>
          </div>
        )}

        {error && (
          <div className="mb-3">
            <Banner tone="error" onDismiss={() => setError(null)}>
              {error}
            </Banner>
          </div>
        )}
        {saved && (
          <div className="mb-3">
            <Banner tone="info" onDismiss={() => setSaved(false)}>
              Token updated.
            </Banner>
          </div>
        )}

        <div className="flex flex-col gap-3">
          <TokenField value={token} onChange={setToken} onSubmit={reauth} invalid={!!error} />
          <label className="flex cursor-pointer items-center gap-2.5 text-sm text-dim">
            <Switch checked={botMode} onCheckedChange={setBotMode} />
            Bot token
          </label>
          <div>
            <Button onClick={reauth} disabled={busy || !token.trim()}>
              {busy ? <Spinner className="border-white/40 border-t-white" /> : "Update token"}
            </Button>
          </div>
        </div>
      </Card>

      <Card>
        <StepHeader title="Deletion" subtitle="Control how much Viaduct asks before deleting." />
        <div className="flex items-center justify-between gap-4 rounded-xl bg-plate/50 px-3.5 py-2.5">
          <span>
            <span className="block text-sm font-medium text-ink">Skip the confirmation step</span>
            <span className="block text-xs text-dim">
              Start deleting straight from the review screen, without the final confirm.
            </span>
          </span>
          <Switch checked={skipConfirm} onCheckedChange={onSkipConfirmChange} className="shrink-0" />
        </div>
        <div className="mt-2 flex items-center justify-between gap-4 rounded-xl bg-plate/50 px-3.5 py-2.5">
          <span>
            <span className="block text-sm font-medium text-ink">Scan everything first</span>
            <span className="block text-xs text-dim">
              Count your full message list before deleting, for an exact total and
              ETA from the start. Adds a short scan up front; deletion speed is the same.
            </span>
          </span>
          <Switch checked={preScan} onCheckedChange={onPreScanChange} className="shrink-0" />
        </div>
      </Card>

      <SelfHostingCard onChange={onRemoteChange} />

      <Card>
        <StepHeader title="Logs & data" subtitle="Everything Viaduct deletes is recorded locally." />
        <div className="flex flex-col gap-3 text-sm">
          <Row label="Config" value={info?.configDir} />
          <Row
            label="Logs"
            value={info ? `${info.logDir}  (${fmtBytes(info.logBytes)})` : undefined}
          />
          {logsCleared && (
            <Banner tone="info" onDismiss={() => setLogsCleared(false)}>
              Logs cleared.
            </Banner>
          )}
          <div className="flex flex-wrap gap-2 pt-1">
            <Button variant="subtle" onClick={() => api.openLogFolder()}>
              Open log folder
            </Button>
            <Button variant="subtle" onClick={clearLogs} disabled={clearingLogs}>
              {clearingLogs ? <Spinner className="border-accent/30 border-t-accent" /> : "Clear logs"}
            </Button>
          </div>
        </div>
      </Card>
    </div>
  );
}

function Row({ label, value }: { label: string; value?: string }) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-xl bg-plate/50 px-3.5 py-2.5">
      <span className="text-dim">{label}</span>
      <span className="truncate font-mono text-xs text-ink">{value ?? "…"}</span>
    </div>
  );
}
