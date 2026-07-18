import type { UserDTO } from "../lib/types";
import { Avatar } from "./Avatar";

export function ConnectionChip({ user }: { user: UserDTO }) {
  const name = user.globalName || user.username;
  return (
    <div className="glass flex items-center gap-2.5 rounded-full py-1.5 pl-1.5 pr-3.5">
      <Avatar url={user.avatarUrl} name={name} size={28} rounded="full" />
      <div className="leading-tight">
        <div className="text-sm font-semibold text-ink">{name}</div>
      </div>
      <span className="ml-1 h-2 w-2 rounded-full bg-accent shadow-[0_0_0_3px_rgba(255,255,255,0.15)]" />
    </div>
  );
}
