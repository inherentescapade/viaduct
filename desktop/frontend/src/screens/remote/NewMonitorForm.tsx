import { useEffect, useMemo, useState } from "react";
import { api } from "../../lib/bridge";
import { usePoll } from "../../lib/usePoll";
import { useVisibleCounts } from "../../lib/useCounts";
import type { ChannelDTO, GuildDTO } from "../../lib/types";
import type { MonitorApi } from "../../lib/monitorApi";
import type { MonitorDTO, MonitorReq, PreviewDTO } from "../../lib/types";
import { Avatar } from "../../components/Avatar";
import { Banner } from "../../components/Banner";
import { Button } from "../../components/Button";
import { ChannelList } from "../../components/ChannelList";
import { SelectRow } from "../../components/SelectRow";
import { Spinner } from "../../components/Spinner";

const inputCls =
  "h-9 w-full rounded-xl border border-line bg-plate/70 px-3.5 text-sm text-ink placeholder:text-dim focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30";

// The retention window units, in ascending order. Values match the server's
// MonitorAgeUnit strings.
const AGE_UNITS = ["minutes", "hours", "days", "weeks"] as const;

// How often to quietly re-fetch the server / DM / group listing the form is
// showing, so servers joined or left (and DMs / groups opened or closed) while
// the form is open are reflected without reopening it.
const LISTING_REFRESH_MS = 60000;

interface Props {
  mapi: MonitorApi;
  onSaved: () => void;
  onCancel: () => void;
  // initial switches the form into edit mode: fields are pre-filled from the
  // existing monitor and saving updates it in place (same ID) instead of
  // creating a new one.
  initial?: MonitorDTO | null;
}

// NewMonitorForm creates or edits a standing retention policy: keep messages no
// older than the chosen window in the chosen scope. The target and channel
// pickers are the same ones the Live deletion wizard uses. It previews the
// immediate impact before enabling.
export function NewMonitorForm({ mapi, onSaved, onCancel, initial }: Props) {
  const [name, setName] = useState(initial?.name ?? "");

  // The stored scope of the monitor being edited (the server accepts "" as an
  // alias for the "@me" DM scope, so normalize it for the picker).
  const initialScope = initial ? (initial.scope === "" ? "@me" : initial.scope) : null;

  // Target: a real server, or the synthetic "@me" DM entry.
  const [guilds, setGuilds] = useState<GuildDTO[]>([]);
  const [loadingGuilds, setLoadingGuilds] = useState(true);
  const [guildQuery, setGuildQuery] = useState("");
  const [guild, setGuild] = useState<GuildDTO | null>(null);
  const [picking, setPicking] = useState(true);

  // Channel selection: for a server, the channels to skip (exclude) or target
  // (include); for DMs, the conversations to skip or target.
  const [channels, setChannels] = useState<ChannelDTO[]>([]);
  const [loadingChannels, setLoadingChannels] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(() => new Set(initial?.channels ?? []));
  const [mode, setMode] = useState<"exclude" | "include">(initial?.mode === "include" ? "include" : "exclude");

  const [maxAgeAmount, setMaxAgeAmount] = useState(initial?.maxAgeAmount ?? 7);
  const [maxAgeUnit, setMaxAgeUnit] = useState<string>(initial?.maxAgeUnit ?? "days");
  const [intervalHrs, setIntervalHrs] = useState(initial?.intervalHrs ?? 6);
  const [includePinned, setIncludePinned] = useState(initial?.includePinned ?? false);
  const [enabled, setEnabled] = useState(initial?.enabled ?? true);

  const [preview, setPreview] = useState<PreviewDTO | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isDM = guild?.isDm ?? false;
  const itemNoun = isDM ? "conversations" : "channels";

  useEffect(() => {
    let alive = true;
    setLoadingGuilds(true);
    api
      .listGuilds()
      .then((g) => {
        if (!alive) return;
        setGuilds(g);
        // In edit mode, re-select the monitor's stored target so the form opens
        // already filled in. If it's gone (e.g. the user left that server), stay
        // on the picker so they can choose a new one.
        if (initialScope) {
          const match = g.find((x) => x.id === initialScope);
          if (match) {
            setGuild(match);
            setPicking(false);
            setLoadingChannels(true);
            api
              .listChannels(match.id)
              .then((cs) => alive && setChannels(cs))
              .catch((e) => alive && setError(String(e instanceof Error ? e.message : e)))
              .finally(() => alive && setLoadingChannels(false));
          } else {
            setError("The server this monitor targets is no longer in your list — pick a new target.");
          }
        }
      })
      .catch((e) => alive && setError(String(e instanceof Error ? e.message : e)))
      .finally(() => alive && setLoadingGuilds(false));
    return () => {
      alive = false;
    };
  }, []);

  // Keep the visible listing fresh while the form is open: refresh the guild
  // list while the target picker is showing (servers joined/left appear), and the
  // conversation list while a DM target is selected (DMs / groups opened/closed
  // appear). usePoll pauses while the window is hidden and swallows transient
  // failures, so the current list stays put until the next successful tick.
  usePoll(
    async () => {
      if (picking) {
        setGuilds(await api.listGuilds());
      } else if (isDM && guild) {
        setChannels(await api.listChannels(guild.id));
      }
    },
    { baseMs: LISTING_REFRESH_MS },
  );

  // Lazy per-row message counts, exactly as the Live wizard shows them.
  const guildIds = useMemo(() => guilds.filter((g) => !g.isDm).map((g) => g.id), [guilds]);
  const { counts: guildCounts, loading: guildLoading, register: registerGuild } = useVisibleCounts(
    guildIds,
    api.guildMessageCount,
  );
  const dmIds = useMemo(() => (isDM ? channels.map((c) => c.id) : []), [isDM, channels]);
  const { counts: dmCounts, loading: dmLoading, register: registerDM } = useVisibleCounts(dmIds, api.dmMessageCount);

  const filteredGuilds = useMemo(() => {
    const q = guildQuery.trim().toLowerCase();
    if (!q) return guilds;
    return guilds.filter((g) => g.name.toLowerCase().includes(q));
  }, [guilds, guildQuery]);

  function invalidate() {
    setPreview(null);
  }

  async function pickGuild(g: GuildDTO) {
    setGuild(g);
    setPicking(false);
    setMode("exclude");
    setSelected(new Set());
    setChannels([]);
    invalidate();
    setLoadingChannels(true);
    try {
      setChannels(await api.listChannels(g.id));
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setLoadingChannels(false);
    }
  }

  function toggleChannel(id: string) {
    setSelected((s) => {
      const n = new Set(s);
      n.has(id) ? n.delete(id) : n.add(id);
      return n;
    });
    invalidate();
  }
  const selectAllChannels = () => {
    setSelected(new Set(channels.map((c) => c.id)));
    invalidate();
  };
  const clearChannels = () => {
    setSelected(new Set());
    invalidate();
  };

  function buildReq(): MonitorReq {
    return {
      id: initial?.id ?? "",
      name: name.trim(),
      enabled,
      scope: guild?.id || "@me",
      mode,
      channels: Array.from(selected),
      maxAgeAmount,
      maxAgeUnit,
      intervalHrs,
      includePinned,
    };
  }

  async function doPreview() {
    setBusy(true);
    setError(null);
    try {
      setPreview(await mapi.preview(buildReq()));
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setBusy(false);
    }
  }

  async function doSave() {
    if (!name.trim()) {
      setError("Give the monitor a name.");
      return;
    }
    if (!guild) {
      setError("Pick a server or your DMs to monitor.");
      return;
    }
    if (maxAgeAmount <= 0) {
      setError("Age must be at least 1.");
      return;
    }
    if (mode === "include" && selected.size === 0) {
      setError(`Pick at least one ${isDM ? "conversation" : "channel"} to include.`);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await mapi.set(buildReq());
      onSaved();
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-3">
      {error && <Banner tone="error">{error}</Banner>}

      <div>
        <label className="mb-1 block text-xs font-medium text-dim">Name</label>
        <input className={inputCls} placeholder="tidy-dms" value={name} onChange={(e) => setName(e.target.value)} />
      </div>

      {/* Target picker — same list the Live deletion tab uses. */}
      <div>
        <label className="mb-1 block text-xs font-medium text-dim">Monitor</label>
        {guild && !picking ? (
          <div className="flex items-center gap-2.5 rounded-xl border border-line bg-plate/50 p-2.5">
            {isDM ? (
              <span className="grid h-8 w-8 shrink-0 place-items-center rounded-xl bg-accent-soft text-lg text-accent-strong">
                ✉
              </span>
            ) : (
              <Avatar url={guild.iconUrl} name={guild.name} size={32} rounded="2xl" />
            )}
            <span className="min-w-0 flex-1 truncate text-sm font-medium text-ink">
              {isDM ? "Direct Messages" : guild.name}
            </span>
            <Button
              variant="subtle"
              onClick={() => {
                setPicking(true);
                invalidate();
              }}
            >
              Change
            </Button>
          </div>
        ) : (
          <>
            <input
              value={guildQuery}
              onChange={(e) => setGuildQuery(e.target.value)}
              placeholder="Search servers…"
              className={`mb-2 ${inputCls}`}
            />
            {loadingGuilds ? (
              <div className="flex items-center gap-2 px-1 py-6 text-sm text-dim">
                <Spinner /> Loading your servers…
              </div>
            ) : (
              <div className="max-h-[240px] space-y-1 overflow-y-auto pr-1">
                {filteredGuilds.map((g) => (
                  <SelectRow
                    key={g.id}
                    title={g.isDm ? "Direct Messages" : g.name}
                    subtitle={g.isDm ? "Your DMs and group chats" : g.owner ? "You own this server" : undefined}
                    badge={g.isDm ? "DM" : undefined}
                    count={g.isDm ? undefined : guildCounts[g.id]}
                    loading={g.isDm ? undefined : guildLoading[g.id]}
                    rowRef={g.isDm ? undefined : registerGuild(g.id)}
                    selected={guild?.id === g.id}
                    onClick={() => pickGuild(g)}
                    leading={
                      g.isDm ? (
                        <span className="grid h-8 w-8 shrink-0 place-items-center rounded-xl bg-accent-soft text-lg text-accent-strong">
                          ✉
                        </span>
                      ) : (
                        <Avatar url={g.iconUrl} name={g.name} size={32} rounded="2xl" />
                      )
                    }
                  />
                ))}
              </div>
            )}
          </>
        )}
      </div>

      {/* Mode + channel selection appear once a target is chosen. */}
      {guild && !picking && (
        <>
          <div>
            <span className="mb-1 block text-xs font-medium text-dim">What should this monitor clean?</span>
            <div className="flex gap-2">
              <button
                onClick={() => {
                  setMode("exclude");
                  invalidate();
                }}
                className={`flex-1 rounded-xl border px-3 py-2 text-xs ${mode === "exclude" ? "border-accent bg-accent-soft text-accent-strong" : "border-line text-dim hover:text-ink"}`}
              >
                Every {isDM ? "conversation" : "channel"}, including new ones
              </button>
              <button
                onClick={() => {
                  setMode("include");
                  invalidate();
                }}
                className={`flex-1 rounded-xl border px-3 py-2 text-xs ${mode === "include" ? "border-accent bg-accent-soft text-accent-strong" : "border-line text-dim hover:text-ink"}`}
              >
                Only {itemNoun} I pick
              </button>
            </div>
          </div>

          <div>
            <p className="mb-1.5 text-xs text-dim">
              {mode === "exclude"
                ? `Trims every ${isDM ? "conversation" : "channel"} in ${isDM ? "your DMs" : "this server"} — even ones that appear later. Check any below to protect them: checked ${itemNoun} are never deleted from.`
                : `Only the ${itemNoun} you check below are cleaned. Everything else is left alone.`}
            </p>
            {loadingChannels ? (
              <div className="flex items-center gap-2 py-6 text-sm text-dim">
                <Spinner /> Loading {itemNoun}…
              </div>
            ) : (
              <ChannelList
                avatars={isDM}
                channels={channels}
                selected={selected}
                onToggle={toggleChannel}
                onAll={selectAllChannels}
                onNone={clearChannels}
                counts={isDM ? dmCounts : undefined}
                loading={isDM ? dmLoading : undefined}
                register={isDM ? registerDM : undefined}
              />
            )}
          </div>
        </>
      )}

      <div className="grid grid-cols-2 gap-2">
        <div>
          <label className="mb-1 block text-xs font-medium text-dim">Delete older than</label>
          <div className="flex gap-2">
            <input
              type="number"
              min={1}
              className={`${inputCls} flex-1`}
              value={maxAgeAmount}
              onChange={(e) => {
                setMaxAgeAmount(Number(e.target.value));
                invalidate();
              }}
            />
            <select
              className={`${inputCls} shrink-0 grow-0 basis-28`}
              value={maxAgeUnit}
              onChange={(e) => {
                setMaxAgeUnit(e.target.value);
                invalidate();
              }}
            >
              {AGE_UNITS.map((u) => (
                <option key={u} value={u}>
                  {u}
                </option>
              ))}
            </select>
          </div>
        </div>
        <div>
          <label className="mb-1 block text-xs font-medium text-dim">Run every (hours)</label>
          <input
            type="number"
            min={1}
            className={inputCls}
            value={intervalHrs}
            onChange={(e) => setIntervalHrs(Number(e.target.value))}
          />
        </div>
      </div>

      <label className="flex items-start gap-2 text-sm text-dim">
        <input
          type="checkbox"
          className="mt-0.5"
          checked={includePinned}
          onChange={(e) => {
            setIncludePinned(e.target.checked);
            invalidate();
          }}
        />
        <span>
          Also delete pinned messages
          <span className="block text-xs text-dim/80">Pinned messages are kept by default.</span>
        </span>
      </label>

      <label className="flex items-center gap-2 text-sm text-dim">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        {initial ? "Enabled (runs on the schedule)" : "Enable immediately (start running on the schedule)"}
      </label>

      {preview && (
        <Banner tone={preview.total > 0 ? "warn" : "info"}>
          Right now this would delete {preview.total.toLocaleString()} message(s) older than {maxAgeAmount}{" "}
          {maxAgeUnit}
          {includePinned ? ", including pinned messages" : " (pinned messages are kept)"}.
        </Banner>
      )}

      <div className="flex justify-between pt-1">
        <Button variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <div className="flex gap-2">
          <Button variant="subtle" disabled={busy || !guild} onClick={doPreview}>
            {busy ? "Checking…" : "Preview impact"}
          </Button>
          <Button disabled={busy || !guild} onClick={doSave}>
            {busy ? "Saving…" : initial ? "Save changes" : "Save monitor"}
          </Button>
        </div>
      </div>
    </div>
  );
}
