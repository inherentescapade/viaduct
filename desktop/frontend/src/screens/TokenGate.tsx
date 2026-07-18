import { useEffect, useState } from "react";
import { api } from "../lib/bridge";
import type { UserDTO } from "../lib/types";
import { Banner } from "../components/Banner";
import { Button } from "../components/Button";
import { Logo } from "../components/Logo";
import { Spinner } from "../components/Spinner";
import { TokenField } from "../components/TokenField";

interface Props {
  onAuthed: (user: UserDTO) => void;
}

export function TokenGate({ onAuthed }: Props) {
  const [token, setToken] = useState("");
  const [botMode, setBotMode] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasAutoDetect, setHasAutoDetect] = useState(false);
  const [detectBusy, setDetectBusy] = useState(false);

  useEffect(() => {
    api.hasAutoDetect().then(setHasAutoDetect).catch(() => {});
  }, []);

  async function validate() {
    if (!token.trim() || busy) return;
    setBusy(true);
    setError(null);
    try {
      const user = await api.validateToken(token.trim(), botMode);
      onAuthed(user);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function autoDetect() {
    if (detectBusy) return;
    setDetectBusy(true);
    setError(null);
    try {
      const user = await api.autoDetectToken();
      onAuthed(user);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setDetectBusy(false);
    }
  }

  return (
    <div className="mx-auto flex min-h-full max-w-xl flex-col justify-center px-5 py-6">
      <div className="glass animate-fade-up rounded-3xl p-6">
        <div className="mb-4 flex items-center gap-3">
          <div className="grid h-10 w-10 place-items-center rounded-xl bg-primary text-primary-foreground shadow-glow">
            <Logo size={20} />
          </div>
          <div>
            <h1 className="text-lg font-semibold tracking-tight text-ink">Connect to Discord</h1>
            <p className="text-sm text-dim">Paste your account token to get started.</p>
          </div>
        </div>

        {error && (
          <div className="mb-4">
            <Banner tone="error" onDismiss={() => setError(null)}>
              {error}
            </Banner>
          </div>
        )}

        {hasAutoDetect && (
          <div className="mb-4 rounded-xl bg-accent-soft px-3.5 py-3">
            <p className="mb-1 text-sm font-medium text-ink">Detect token automatically</p>
            <p className="mb-3 text-xs text-dim">
              Viaduct will read your Discord token from the app's local storage on this computer.
              It is never sent anywhere except Discord's own servers.
            </p>
            <Button variant="subtle" onClick={autoDetect} disabled={detectBusy}>
              {detectBusy ? <Spinner className="border-accent/30 border-t-accent" /> : "Detect from Discord"}
            </Button>
          </div>
        )}

        <div className="flex flex-col gap-4">
          <TokenField value={token} onChange={setToken} onSubmit={validate} invalid={!!error} />

          <label className="flex items-center gap-2.5 text-sm text-dim">
            <input
              type="checkbox"
              checked={botMode}
              onChange={(e) => setBotMode(e.target.checked)}
              className="h-4 w-4 accent-white"
            />
            This is a bot token
          </label>

          <Button size="lg" onClick={validate} disabled={busy || !token.trim()}>
            {busy ? <Spinner className="border-white/40 border-t-white" /> : "Connect"}
          </Button>
        </div>

        <details className="mt-4 text-sm text-dim">
          <summary className="cursor-pointer select-none font-medium text-ink/80 hover:text-ink">
            Where do I find my token?
          </summary>
          <ol className="mt-3 list-decimal space-y-1 pl-5 leading-relaxed">
            <li>Open Discord in your browser and press F12 to open DevTools.</li>
            <li>Go to the Network tab and reload (Ctrl/Cmd+R).</li>
            <li>Pick any request to discord.com and find the request headers.</li>
            <li>Copy the value of the <span className="font-mono">authorization</span> header.</li>
          </ol>
          <p className="mt-3 rounded-xl bg-warn-soft px-3 py-2 text-xs text-warn">
            Your token is stored only on this computer and is never sent anywhere except Discord.
            Treat it like a password.
          </p>
        </details>
      </div>
    </div>
  );
}
