import { BarChart3, Eye, Package, Server, Settings, Trash2 } from "lucide-react";
import type { UserDTO } from "../lib/types";
import { Tabs, TabsList, TabsTrigger } from "./ui/tabs";
import { ConnectionChip } from "./ConnectionChip";
import { Logo } from "./Logo";

export type Mode = "live" | "import" | "monitors" | "remote" | "insights" | "settings";

interface Props {
  mode: Mode;
  onMode: (m: Mode) => void;
  user: UserDTO | null;
  disabled?: boolean;
}

const tabs: { key: Mode; label: string; Icon: typeof Trash2 }[] = [
  { key: "live", label: "Live deletion", Icon: Trash2 },
  { key: "import", label: "Data package", Icon: Package },
  { key: "monitors", label: "Monitors", Icon: Eye },
  { key: "remote", label: "Server", Icon: Server },
  { key: "insights", label: "Insights", Icon: BarChart3 },
  { key: "settings", label: "Settings", Icon: Settings },
];

export function TopBar({ mode, onMode, user, disabled }: Props) {
  return (
    <header className="flex items-center justify-between gap-4 px-5 py-3">
      <div className="flex items-center gap-2.5">
        <div className="edge-top grid h-8 w-8 place-items-center rounded-xl bg-primary text-primary-foreground shadow-glow">
          <Logo size={16} />
        </div>
        <div className="leading-tight">
          <div className="text-sm font-semibold tracking-tight text-foreground">Viaduct</div>
          <div className="text-xs text-muted-foreground">Discord message cleanup</div>
        </div>
      </div>

      <Tabs value={mode} onValueChange={(v) => onMode(v as Mode)}>
        <TabsList className="glass h-auto gap-0.5 rounded-full border-white/10 p-1">
          {tabs.map(({ key, label, Icon }) => (
            <TabsTrigger
              key={key}
              value={key}
              disabled={disabled}
              className="gap-1.5 rounded-full px-3 py-1.5 text-muted-foreground transition-colors hover:text-foreground data-[state=active]:bg-foreground/15 data-[state=active]:text-foreground data-[state=active]:shadow-none"
            >
              <Icon className="h-3.5 w-3.5" aria-hidden />
              <span className="hidden md:inline">{label}</span>
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      <div className="flex min-w-[120px] justify-end">
        {user && <ConnectionChip user={user} />}
      </div>
    </header>
  );
}
