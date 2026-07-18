import { localMonitorApi, remoteMonitorApi } from "../../lib/monitorApi";
import type { RemoteStatus } from "../../lib/useRemoteStatus";
import type { UserDTO } from "../../lib/types";
import { Banner } from "../../components/Banner";
import { Card } from "../../components/Card";
import { StepHeader } from "../../components/StepHeader";
import { MonitorPanel } from "./MonitorPanel";

// MonitorsScreen manages retention-policy monitors. Like the Live deletion and
// Data package tabs, it dispatches to a connected server automatically: when a
// server is active, monitors are created on it (always-on, 24/7); otherwise they
// run locally, in-process, while the app is open. Either way everything happens
// on this tab — the Server tab is only for connection setup.
export function MonitorsScreen({ remote, user }: { remote: RemoteStatus; user: UserDTO }) {
  // active === the same condition that makes the Live wizard dispatch remotely:
  // configured, reachable, and holding a token.
  const onServer = remote.active;
  const mapi = onServer ? remoteMonitorApi : localMonitorApi;

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-3 px-5 pb-6">
      <Card>
        <StepHeader
          eyebrow="Automatic cleanups"
          title="Monitors"
          subtitle={
            onServer
              ? "Keep messages trimmed to a maximum age. These run on your server, 24/7 — even when Viaduct is closed."
              : "Keep messages trimmed to a maximum age. These run on this machine while Viaduct is open."
          }
        />
        {onServer ? (
          <Banner tone="info">
            Connected to your server{remote.actingAs ? ` (acting as ${remote.actingAs})` : ""}. Monitors you
            create here are <span className="font-semibold">dispatched to run there</span>, so they keep
            cleaning up around the clock. The same list appears on the Server tab.
          </Banner>
        ) : (
          <Banner tone="info">
            These local monitors run only while Viaduct is open.{" "}
            {remote.configured
              ? "For 24/7 cleanups, finish connecting your server on the Server tab — new monitors will then run there automatically."
              : "To run them 24/7, set up a server (Server tab) or run `viaduct monitor run` on an always-on machine."}
          </Banner>
        )}
        <div className="mt-3">
          <MonitorPanel key={onServer ? "server" : "local"} mapi={mapi} self={user} />
        </div>
      </Card>
    </div>
  );
}
