import { useEffect, useRef, useState } from "react";
import { api } from "./lib/bridge";
import { useRemoteStatus } from "./lib/useRemoteStatus";
import type { UserDTO } from "./lib/types";
import { Spinner } from "./components/Spinner";
import { TopBar, type Mode } from "./components/TopBar";
import { TokenGate } from "./screens/TokenGate";
import { LiveWizard } from "./screens/live/LiveWizard";
import { ImportWizard } from "./screens/import/ImportWizard";
import { RemoteSection } from "./screens/remote/RemoteSection";
import { MonitorsScreen } from "./screens/remote/MonitorsScreen";
import { Insights } from "./screens/Insights";
import { Settings } from "./screens/Settings";

export default function App() {
  const [booting, setBooting] = useState(true);
  const [user, setUser] = useState<UserDTO | null>(null);
  const [mode, setMode] = useState<Mode>("live");
  const [skipConfirm, setSkipConfirm] = useState(false);
  const [preScan, setPreScan] = useState(false);
  const mainRef = useRef<HTMLElement>(null);
  const { status: remote, refresh: refreshRemote } = useRemoteStatus();
  const didInitTab = useRef(false);

  useEffect(() => {
    mainRef.current?.scrollTo(0, 0);
  }, [mode]);

  // Try to resume a saved session on launch. The raw token never leaves Go.
  useEffect(() => {
    let alive = true;
    api
      .autoLogin()
      .then((u) => alive && setUser(u))
      .catch(() => {})
      .finally(() => alive && setBooting(false));
    api
      .getPrefs()
      .then((p) => {
        if (!alive) return;
        setSkipConfirm(p.skipConfirm);
        setPreScan(p.preScan);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  // If a self-hosted server is already configured, open straight into its
  // dashboard on first load; the app "becomes the tasker" once set up.
  useEffect(() => {
    if (didInitTab.current) return;
    if (remote.configured) {
      didInitTab.current = true;
      setMode("remote");
    }
  }, [remote.configured]);

  function changeSkipConfirm(v: boolean) {
    setSkipConfirm(v);
    void api.setSkipConfirm(v);
  }

  function changePreScan(v: boolean) {
    setPreScan(v);
    void api.setPreScan(v);
  }

  if (booting) {
    return (
      <div className="grid h-full place-items-center">
        <div className="flex items-center gap-3 text-dim">
          <Spinner /> Starting Viaduct…
        </div>
      </div>
    );
  }

  if (!user) {
    return <TokenGate onAuthed={setUser} />;
  }

  return (
    <div className="flex h-full flex-col">
      <TopBar mode={mode} onMode={setMode} user={user} />
      <main ref={mainRef} className="flex-1 overflow-y-auto pt-2 [overflow-anchor:none]">
        <div className={mode !== "live" ? "hidden" : ""}>
          <LiveWizard
            skipConfirm={skipConfirm}
            remote={remote}
            onDispatched={() => setMode("remote")}
          />
        </div>
        <div className={mode !== "import" ? "hidden" : ""}><ImportWizard skipConfirm={skipConfirm} /></div>
        {mode === "monitors" && <MonitorsScreen remote={remote} user={user} />}
        {mode === "remote" && <RemoteSection onStatusChange={refreshRemote} user={user} />}
        {mode === "insights" && <Insights remote={remote} />}
        {mode === "settings" && (
          <Settings
            user={user}
            onReauth={setUser}
            onSignOut={() => setUser(null)}
            skipConfirm={skipConfirm}
            onSkipConfirmChange={changeSkipConfirm}
            preScan={preScan}
            onPreScanChange={changePreScan}
            onRemoteChange={refreshRemote}
          />
        )}
      </main>
    </div>
  );
}
