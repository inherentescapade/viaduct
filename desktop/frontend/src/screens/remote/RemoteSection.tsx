import { useCallback, useEffect, useState } from "react";
import { api } from "../../lib/bridge";
import type { RemoteDTO, UserDTO } from "../../lib/types";
import { Spinner } from "../../components/Spinner";
import { SelfHostingSetup } from "./SelfHostingSetup";
import { Tasker } from "./Tasker";

// RemoteSection is the "Server" tab. It shows the guided setup wizard until a
// server is configured, then switches to the dashboard. onStatusChange notifies
// the app shell so the deletion wizard learns whether to dispatch remotely.
export function RemoteSection({
  onStatusChange,
  user,
}: {
  onStatusChange?: () => void;
  user: UserDTO;
}) {
  const [remote, setRemote] = useState<RemoteDTO | null | undefined>(undefined);

  const refresh = useCallback(() => {
    api
      .getRemote()
      .then((r) => setRemote(r))
      .catch(() => setRemote(null))
      .finally(() => onStatusChange?.());
  }, [onStatusChange]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  if (remote === undefined) {
    return (
      <div className="grid place-items-center py-20 text-dim">
        <div className="flex items-center gap-3">
          <Spinner /> Checking server…
        </div>
      </div>
    );
  }

  if (!remote) {
    return <SelfHostingSetup onConfigured={refresh} />;
  }

  return <Tasker remote={remote} user={user} onForget={refresh} onStatusChange={onStatusChange} />;
}
