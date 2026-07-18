import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "./bridge";

// RemoteStatus summarises whether a self-hosted server is set up and ready to
// receive jobs. "active" means: configured, reachable, and holding a token, so
// the deletion wizard should dispatch to it instead of running locally.
export interface RemoteStatus {
  configured: boolean;
  active: boolean;
  address: string;
  actingAs: string | null;
}

const EMPTY: RemoteStatus = { configured: false, active: false, address: "", actingAs: null };

// Re-check the connection on this cadence. The status is global: it decides
// whether the Live wizard dispatches remotely, so it has to keep tracking
// reality after launch. Without a periodic probe, a connection that dropped
// mid-session would leave the app believing the server is still active (and
// dispatching deletions into the void), and a recovered connection would never
// clear the stale state on its own.
const POLL_MS = 5000;

export function useRemoteStatus(): { status: RemoteStatus; refresh: () => void } {
  const [status, setStatus] = useState<RemoteStatus>(EMPTY);

  // Monotonic token guarding against out-of-order async results. Probes can
  // overlap (the poll loop plus a manual refresh, or a flapping connection
  // firing several in quick succession), and ping latency means an older probe
  // can resolve after a newer one. Only the most recently started probe is
  // allowed to commit, so a slow result from before a reconnect can't clobber a
  // fresher one — the source of the "inconsistent after reconnect" state.
  const seqRef = useRef(0);
  const aliveRef = useRef(true);

  const probe = useCallback(async () => {
    const seq = ++seqRef.current;
    const commit = (next: RemoteStatus) => {
      if (aliveRef.current && seq === seqRef.current) setStatus(next);
    };
    try {
      const r = await api.getRemote();
      if (!r) {
        commit(EMPTY);
        return;
      }
      try {
        const p = await api.remotePing();
        commit({
          configured: true,
          active: p.hasToken,
          address: r.address,
          actingAs: p.actingAs?.username ?? null,
        });
      } catch {
        // Configured but unreachable: stay configured (so the UI doesn't bounce
        // back to setup), but mark inactive so nothing dispatches to it.
        commit({ configured: true, active: false, address: r.address, actingAs: null });
      }
    } catch {
      commit(EMPTY);
    }
  }, []);

  const refresh = useCallback(() => {
    void probe();
  }, [probe]);

  // Probe on mount and on an interval, pausing while the window is hidden so a
  // backgrounded app isn't polling the server needlessly.
  useEffect(() => {
    aliveRef.current = true;
    let timer: number | undefined;
    const loop = async () => {
      if (!document.hidden) await probe();
      if (aliveRef.current) timer = window.setTimeout(loop, POLL_MS);
    };
    void loop();
    return () => {
      aliveRef.current = false;
      if (timer) window.clearTimeout(timer);
    };
  }, [probe]);

  return { status, refresh };
}
