import { cn } from "../lib/cn";
import { formatCount } from "../lib/format";
import { Spinner } from "./Spinner";

interface Props {
  title: string;
  subtitle?: string;
  badge?: string;
  // count, when set, shows how many of the user's messages are here.
  count?: number;
  // loading, when true, shows a spinner in place of the count while its query
  // is in flight.
  loading?: boolean;
  selected?: boolean;
  onClick?: () => void;
  leading?: React.ReactNode;
  // rowRef attaches the row element to a visibility observer (lazy count loading).
  rowRef?: (el: HTMLElement | null) => void;
}

// A single selectable row used for guild/target pickers. A left accent bar grows
// in on hover and stays for the selected row, so the list feels responsive.
export function SelectRow({ title, subtitle, badge, count, loading, selected, onClick, leading, rowRef }: Props) {
  return (
    <button
      ref={rowRef}
      onClick={onClick}
      className={cn(
        "group relative flex w-full items-center gap-2.5 overflow-hidden rounded-xl border px-3 py-2 text-left transition-all duration-150",
        selected
          ? "border-white/15 bg-white/[0.07] shadow-[inset_0_1px_0_rgba(255,255,255,0.08)]"
          : "border-transparent hover:border-white/10 hover:bg-white/[0.045]",
      )}
    >
      {/* left accent bar */}
      <span
        className={cn(
          "absolute left-0 top-1/2 -translate-y-1/2 rounded-r-full bg-foreground transition-all duration-200",
          selected ? "h-6 w-[3px] opacity-100" : "h-4 w-[3px] opacity-0 group-hover:opacity-40",
        )}
      />
      {leading}
      <span className="flex-1 truncate">
        <span className={cn("block truncate text-sm font-medium", selected ? "text-foreground" : "text-foreground/90")}>
          {title}
        </span>
        {subtitle && <span className="block truncate text-xs text-muted-foreground">{subtitle}</span>}
      </span>
      {count !== undefined ? (
        <span className="shrink-0 text-xs tabular-nums text-muted-foreground transition-colors group-hover:text-foreground/80">
          {formatCount(count)} msg{count === 1 ? "" : "s"}
        </span>
      ) : loading ? (
        <Spinner className="h-3.5 w-3.5 shrink-0" />
      ) : null}
      {badge && (
        <span className="rounded-md bg-foreground/10 px-1.5 py-0.5 text-xs font-semibold uppercase text-muted-foreground">
          {badge}
        </span>
      )}
    </button>
  );
}
