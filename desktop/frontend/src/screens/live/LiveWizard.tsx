import { useEffect, useMemo, useState } from "react";
import { api } from "../../lib/bridge";
import { cn } from "../../lib/cn";
import { dateValid } from "../../lib/datecheck";
import { formatCount } from "../../lib/format";
import { usePoll } from "../../lib/usePoll";
import { useRun } from "../../lib/useRun";
import { useVisibleCounts } from "../../lib/useCounts";
import type { ChannelDTO, DeleteRequest, GuildDTO, MessageDTO, RemoteJobRequest } from "../../lib/types";
import type { RemoteStatus } from "../../lib/useRemoteStatus";
import { Avatar } from "../../components/Avatar";
import { Banner } from "../../components/Banner";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { ChannelList } from "../../components/ChannelList";
import { ConfirmGate } from "../../components/ConfirmGate";
import { DateField } from "../../components/DateField";
import { MessagePreview } from "../../components/MessagePreview";
import { MessageRain } from "../../components/MessageRain";
import { ProgressMeter } from "../../components/ProgressMeter";
import { SelectRow } from "../../components/SelectRow";
import { Spinner } from "../../components/Spinner";
import { StepHeader } from "../../components/StepHeader";
import { WizardShell } from "../../components/WizardShell";

type Step = "target" | "scope" | "filters" | "review" | "confirm" | "progress" | "done" | "dispatched";

// toRemoteJob maps the wizard's local DeleteRequest to the server job spec. The
// wizard already covers DMs via the @me guild + channel selection, so a single
// delete_guild kind is sufficient. Verify is on to match the local engine, which
// always runs verification passes.
function toRemoteJob(req: DeleteRequest): RemoteJobRequest {
  return {
    kind: "delete_guild",
    guild: req.guildId,
    channels: req.channelIds,
    user: "",
    before: req.before,
    after: req.after,
    maxId: req.maxId,
    minId: req.minId,
    verify: true,
    includePinned: req.includePinned,
  };
}

const STEPS = [
  { key: "target", label: "Target" },
  { key: "scope", label: "Scope" },
  { key: "filters", label: "Filters" },
  { key: "review", label: "Review" },
  { key: "confirm", label: "Confirm" },
];

function stepIndex(step: Step): number {
  const i = STEPS.findIndex((s) => s.key === step);
  return i === -1 ? STEPS.length : i;
}

// How often to quietly re-fetch the server / DM / group listings the user is
// browsing. Servers can be joined or left, and DMs and group chats can open or
// close, while the app is left running, so the visible listing is refreshed on
// this cadence to stay current without the user having to back out and re-enter.
const LISTING_REFRESH_MS = 60000;

export function LiveWizard({
  skipConfirm,
  remote,
  onDispatched,
}: {
  skipConfirm: boolean;
  remote: RemoteStatus;
  onDispatched: () => void;
}) {
  const run = useRun();
  // When a server is connected, the wizard dispatches the job there instead of
  // running it locally; same flow, the final action just changes.
  const dispatchMode = remote.active;
  const [step, setStep] = useState<Step>("target");
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    document.querySelector("main")?.scrollTo(0, 0);
  }, [step]);

  // target
  const [guilds, setGuilds] = useState<GuildDTO[]>([]);
  const [loadingGuilds, setLoadingGuilds] = useState(true);
  const [guildQuery, setGuildQuery] = useState("");
  const [guild, setGuild] = useState<GuildDTO | null>(null);

  // scope
  const [channels, setChannels] = useState<ChannelDTO[]>([]);
  const [loadingChannels, setLoadingChannels] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  // For servers: "all" deletes account-wide via Discord's guild search;
  // "channels" restricts the deletion to the channels selected below. DMs are
  // always per-conversation and ignore this.
  const [scopeMode, setScopeMode] = useState<"all" | "channels">("all");

  // server-scope message preview
  const [sample, setSample] = useState<MessageDTO[]>([]);
  const [loadingSample, setLoadingSample] = useState(false);

  // filters
  const [before, setBefore] = useState("");
  const [after, setAfter] = useState("");
  const [maxId, setMaxId] = useState("");
  const [minId, setMinId] = useState("");
  const [includePinned, setIncludePinned] = useState(false);

  // review
  const [previewCount, setPreviewCount] = useState<number | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [dryRunning, setDryRunning] = useState(false);

  // progress
  const [stopping, setStopping] = useState(false);

  const isDM = guild?.isDm ?? false;
  // Whether the request carries an explicit channel selection: always for DMs,
  // and for servers when the user chose to filter by channel.
  const useChannelFilter = isDM || scopeMode === "channels";

  useEffect(() => {
    let alive = true;
    setLoadingGuilds(true);
    api
      .listGuilds()
      .then((g) => alive && setGuilds(g))
      .catch((e) => alive && setError(String(e instanceof Error ? e.message : e)))
      .finally(() => alive && setLoadingGuilds(false));
    return () => {
      alive = false;
    };
  }, []);

  // When a run finishes, advance to the done screen. run.finished is the
  // primary signal (carries verification result); run.progress.done is a
  // fallback for edge cases where the finished event is delayed.
  useEffect(() => {
    if (step !== "progress") return;
    if (run.finished) {
      setStep("done");
      setStopping(false);
    }
  }, [run.finished, step]);

  useEffect(() => {
    if (step !== "progress") return;
    if (run.progress?.done && !run.verifying && !run.finished) {
      // Engine signalled completion but finished event hasn't arrived yet.
      // Give it 2 s then transition anyway so the done screen is not lost.
      const t = setTimeout(() => {
        setStep((s) => (s === "progress" ? "done" : s));
        setStopping(false);
      }, 2000);
      return () => clearTimeout(t);
    }
  }, [run.progress?.done, run.verifying, run.finished, step]);

  // Keep the listings the user is currently looking at fresh, since they can
  // change while the app is left running: servers can be joined or left (the
  // guild list on the target step), and DMs / group chats can open or close (the
  // conversation list on the DM scope step). usePoll re-fetches on an interval
  // and pauses while the window is hidden; each refresh replaces the relevant
  // list silently, without flipping its loading spinner. Transient failures are
  // swallowed by usePoll, so the existing list simply stays put until the next
  // successful tick.
  usePoll(
    async () => {
      if (step === "target") {
        setGuilds(await api.listGuilds());
      } else if (isDM && step === "scope" && guild) {
        setChannels(await api.listChannels(guild.id));
      }
    },
    { baseMs: LISTING_REFRESH_MS },
  );

  const filtersValid = dateValid(before) && dateValid(after);

  const request = (): DeleteRequest => ({
    guildId: guild!.id,
    guildName: guild!.name,
    channelIds: useChannelFilter ? Array.from(selected) : [],
    before: before.trim(),
    after: after.trim(),
    maxId: maxId.trim(),
    minId: minId.trim(),
    includePinned,
  });

  async function loadChannels(guildId: string) {
    setLoadingChannels(true);
    try {
      return await api.listChannels(guildId);
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
      return null;
    } finally {
      setLoadingChannels(false);
    }
  }

  // Switch a server between account-wide and channel-filtered deletion. The
  // channel list is fetched on first use of "channels" mode.
  async function selectGuildScope(mode: "all" | "channels") {
    setScopeMode(mode);
    if (mode === "channels" && channels.length === 0 && guild) {
      const ch = await loadChannels(guild.id);
      if (ch) setChannels(ch);
    }
  }

  function toggleChannel(id: string) {
    setSelected((s) => {
      const n = new Set(s);
      n.has(id) ? n.delete(id) : n.add(id);
      return n;
    });
  }
  const selectAllChannels = () => setSelected(new Set(channels.map((c) => c.id)));
  const clearChannels = () => setSelected(new Set());

  async function pickGuild(g: GuildDTO) {
    setGuild(g);
    setSelected(new Set());
    setChannels([]);
    setSample([]);
    setScopeMode("all");
    setStep("scope");
    if (g.isDm) {
      const ch = await loadChannels(g.id);
      if (ch) {
        setChannels(ch);
        // Start with nothing selected; the user picks which conversations to
        // clear rather than having every DM checked by default.
        setSelected(new Set());
      }
    } else {
      // Preview a few recent messages we'll remove from this server.
      setLoadingSample(true);
      try {
        setSample(await api.sampleMessages({
          guildId: g.id,
          guildName: g.name,
          channelIds: [],
          before: "",
          after: "",
          maxId: "",
          minId: "",
          includePinned: false,
        }));
      } catch {
        setSample([]);
      } finally {
        setLoadingSample(false);
      }
    }
  }

  async function goReview() {
    setStep("review");
    setPreviewCount(null);
    setPreviewing(true);
    setError(null);
    try {
      setPreviewCount(await api.preview(request()));
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setPreviewing(false);
    }
  }

  async function dryRun() {
    run.reset();
    setDryRunning(true);
    setError(null);
    try {
      await api.enumerate(request());
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
      setDryRunning(false);
    }
  }
  // stop the dry-run spinner when the enumeration completes
  useEffect(() => {
    if (run.enumCount !== null) setDryRunning(false);
  }, [run.enumCount]);

  async function startDelete() {
    // Remote mode: dispatch to the server and land on a "dispatched" card.
    if (dispatchMode) {
      setError(null);
      try {
        await api.remoteSubmit(toRemoteJob(request()));
        setStep("dispatched");
      } catch (e) {
        setError(String(e instanceof Error ? e.message : e));
        setStep("confirm");
      }
      return;
    }
    run.reset();
    setStopping(false);
    setError(null);
    setStep("progress");
    try {
      await api.startDelete(request());
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
      setStep("confirm");
    }
  }

  async function cancel() {
    setStopping(true);
    await api.cancel();
  }

  function restart() {
    run.reset();
    setStep("target");
    setGuild(null);
    setChannels([]);
    setSelected(new Set());
    setScopeMode("all");
    setBefore("");
    setAfter("");
    setMaxId("");
    setMinId("");
    setPreviewCount(null);
  }

  const filteredGuilds = useMemo(() => {
    const q = guildQuery.trim().toLowerCase();
    if (!q) return guilds;
    return guilds.filter((g) => g.name.toLowerCase().includes(q));
  }, [guilds, guildQuery]);

  // How many of the user's messages live in each server / DM conversation.
  // Counts load lazily, only for rows scrolled into view, so we don't fire a
  // search for every item in a long list.
  const guildIds = useMemo(() => guilds.filter((g) => !g.isDm).map((g) => g.id), [guilds]);
  const { counts: guildCounts, loading: guildLoading, register: registerGuild } = useVisibleCounts(
    guildIds,
    api.guildMessageCount,
  );
  const dmIds = useMemo(() => (isDM ? channels.map((c) => c.id) : []), [isDM, channels]);
  const { counts: dmCounts, loading: dmLoading, register: registerDM } = useVisibleCounts(dmIds, api.dmMessageCount);

  const scopeLabel = isDM
    ? `${selected.size} conversation${selected.size === 1 ? "" : "s"}`
    : scopeMode === "channels"
      ? `${selected.size} channel${selected.size === 1 ? "" : "s"} in ${guild?.name ?? ""}`
      : guild?.name ?? "";

  return (
    <WizardShell
      steps={STEPS}
      currentIndex={stepIndex(step)}
      aside={
        guild && (
          <div className="glass rounded-3xl p-3.5 text-sm">
            <div className="text-xs font-semibold uppercase tracking-wide text-dim">Target</div>
            <div className="mt-1 truncate font-medium text-ink">{guild.name}</div>
            {isDM ? (
              <div className="mt-0.5 text-xs text-dim">{scopeLabel}</div>
            ) : scopeMode === "channels" ? (
              <div className="mt-0.5 text-xs text-dim">
                {selected.size} channel{selected.size === 1 ? "" : "s"} selected
              </div>
            ) : (
              <div className="mt-0.5 text-xs text-dim">Server-wide</div>
            )}
          </div>
        )
      }
    >
      {error && (
        <div className="mb-4">
          <Banner tone="error" onDismiss={() => setError(null)}>
            {error}
          </Banner>
        </div>
      )}

      {dispatchMode && step !== "dispatched" && (
        <div className="mb-4">
          <Banner tone="info">
            Connected to your server{remote.actingAs ? ` (acting as ${remote.actingAs})` : ""}. This
            deletion will be <span className="font-semibold">dispatched to run there</span>, so it keeps
            going even if you close the app.
          </Banner>
        </div>
      )}

      {step === "target" && (
        <Card>
          <StepHeader
            eyebrow="Step 1"
            title="Choose what to clean up"
            subtitle="Pick a server, or your direct messages."
          />
          <input
            value={guildQuery}
            onChange={(e) => setGuildQuery(e.target.value)}
            placeholder="Search servers…"
            className="mb-3 h-9 w-full rounded-xl border border-line bg-plate/70 px-3.5 text-sm text-ink placeholder:text-dim focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30"
          />
          {loadingGuilds ? (
            <div className="flex items-center gap-2 px-1 py-6 text-sm text-dim">
              <Spinner /> Loading your servers…
            </div>
          ) : (
            <div className="max-h-[min(52vh,460px)] space-y-1 overflow-y-auto pr-1">
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
        </Card>
      )}

      {step === "scope" && (
        <Card>
          <StepHeader
            eyebrow="Step 2"
            title={isDM ? "Pick conversations" : "Scope"}
            subtitle={
              isDM
                ? "Choose which direct messages to delete from. At least one is required."
                : "Clean up every channel in this server, or target specific ones."
            }
          />
          {isDM ? (
            loadingChannels ? (
              <div className="flex items-center gap-2 py-6 text-sm text-dim">
                <Spinner /> Loading conversations…
              </div>
            ) : (
              <ChannelList
                avatars
                channels={channels}
                selected={selected}
                onToggle={toggleChannel}
                onAll={selectAllChannels}
                onNone={clearChannels}
                counts={dmCounts}
                loading={dmLoading}
                register={registerDM}
              />
            )
          ) : (
            <div className="flex flex-col gap-4">
              {/* Choose between an account-wide sweep and a channel-scoped one. */}
              <div className="flex gap-1 rounded-xl border border-line bg-plate/40 p-1">
                <button
                  onClick={() => selectGuildScope("all")}
                  className={cn(
                    "flex-1 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors",
                    scopeMode === "all" ? "bg-plate text-ink shadow-sm" : "text-dim hover:text-ink",
                  )}
                >
                  All channels
                </button>
                <button
                  onClick={() => selectGuildScope("channels")}
                  className={cn(
                    "flex-1 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors",
                    scopeMode === "channels" ? "bg-plate text-ink shadow-sm" : "text-dim hover:text-ink",
                  )}
                >
                  Specific channels
                </button>
              </div>

              {scopeMode === "all" ? (
                <>
                  <div className="flex items-center gap-3 rounded-xl border border-line bg-plate/50 p-3.5">
                    <Avatar url={guild?.iconUrl} name={guild?.name ?? "?"} size={40} rounded="2xl" />
                    <div className="text-sm leading-relaxed text-dim">
                      Viaduct finds <span className="font-medium text-ink">your messages across every channel</span> in{" "}
                      <span className="font-medium text-ink">{guild?.name}</span>. Use the date filters next to narrow
                      the range.
                    </div>
                  </div>

                  <div>
                    <div className="mb-1 flex items-center gap-2 text-sm font-semibold text-ink">
                      Your recent messages in this server {loadingSample && <Spinner />}
                    </div>
                    {!loadingSample && sample.length === 0 ? (
                      <div className="rounded-xl border border-line bg-plate/40 px-3.5 py-5 text-center text-xs text-dim">
                        No recent messages found here.
                      </div>
                    ) : (
                      <div className="rounded-xl border border-line bg-plate/40 px-3.5 py-1">
                        <MessagePreview messages={sample} max={6} />
                      </div>
                    )}
                  </div>
                </>
              ) : loadingChannels ? (
                <div className="flex items-center gap-2 py-6 text-sm text-dim">
                  <Spinner /> Loading channels…
                </div>
              ) : (
                <ChannelList
                  channels={channels}
                  selected={selected}
                  onToggle={toggleChannel}
                  onAll={selectAllChannels}
                  onNone={clearChannels}
                />
              )}
            </div>
          )}
          <div className="mt-5 flex items-center justify-between">
            <Button variant="ghost" onClick={() => setStep("target")}>
              ← Back
            </Button>
            <Button onClick={() => setStep("filters")} disabled={useChannelFilter && selected.size === 0}>
              Continue →
            </Button>
          </div>
        </Card>
      )}

      {step === "filters" && (
        <Card>
          <StepHeader
            eyebrow="Step 3"
            title="Narrow the range"
            subtitle="All optional. Leave blank to target everything."
          />
          <div className="grid grid-cols-2 gap-3">
            <DateField label="Before" value={before} onChange={setBefore} invalid={!dateValid(before)} hint="Only messages older than this" />
            <DateField label="After" value={after} onChange={setAfter} invalid={!dateValid(after)} hint="Only messages newer than this" />
          </div>
          <details className="mt-3 text-sm text-dim">
            <summary className="cursor-pointer select-none font-medium text-ink/80 hover:text-ink">
              Advanced: message ID range
            </summary>
            <div className="mt-3 grid grid-cols-2 gap-3">
              <label className="flex flex-col gap-1">
                <span className="text-sm font-medium text-ink">Max ID</span>
                <input
                  value={maxId}
                  onChange={(e) => setMaxId(e.target.value)}
                  placeholder="snowflake"
                  className="h-9 rounded-xl border border-line bg-plate/70 px-3.5 font-mono text-sm text-ink placeholder:font-sans placeholder:text-dim focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30"
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-sm font-medium text-ink">Min ID</span>
                <input
                  value={minId}
                  onChange={(e) => setMinId(e.target.value)}
                  placeholder="snowflake"
                  className="h-9 rounded-xl border border-line bg-plate/70 px-3.5 font-mono text-sm text-ink placeholder:font-sans placeholder:text-dim focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30"
                />
              </label>
            </div>
          </details>
          <label className="mt-4 flex items-start gap-2 text-sm text-dim">
            <input
              type="checkbox"
              className="mt-0.5"
              checked={includePinned}
              onChange={(e) => setIncludePinned(e.target.checked)}
            />
            <span>
              Also delete pinned messages
              <span className="block text-xs text-dim/80">Pinned messages are kept by default.</span>
            </span>
          </label>
          <div className="mt-5 flex items-center justify-between">
            <Button variant="ghost" onClick={() => setStep("scope")}>
              ← Back
            </Button>
            <Button onClick={goReview} disabled={!filtersValid}>
              Review →
            </Button>
          </div>
        </Card>
      )}

      {step === "review" && (
        <Card>
          <StepHeader eyebrow="Step 4" title="Review" subtitle="Confirm the scope before anything is deleted." />
          <div className="mb-4 flex items-center gap-3.5 rounded-xl bg-plate/50 p-3.5">
            <div className="grid h-12 w-12 place-items-center rounded-xl bg-accent-soft text-accent-strong">
              <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden>
                <path d="M4 7h16M4 12h16M4 17h10" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
              </svg>
            </div>
            <div>
              <div className="text-sm text-dim">Messages found in {scopeLabel || guild?.name}</div>
              <div className="font-mono text-2xl font-semibold tabular-nums text-ink">
                {previewing ? <Spinner className="h-6 w-6" /> : previewCount === null ? "—" : formatCount(previewCount)}
              </div>
            </div>
          </div>

          {previewCount === 0 && !previewing && (
            <Banner tone="warn">No messages match these settings. Try widening your filters.</Banner>
          )}

          {(run.messages.length > 0 || dryRunning || run.enumCount !== null) && (
            <div className="mb-4">
              <div className="mb-2 flex items-center gap-2 text-sm font-medium text-ink">
                Dry run {dryRunning && <Spinner />}
                {run.enumCount !== null && (
                  <span className="text-dim">{formatCount(run.enumCount)} messages found, nothing deleted</span>
                )}
              </div>
              <div className="max-h-64 overflow-y-auto rounded-xl border border-line bg-plate/50 px-3.5">
                <MessagePreview messages={[...run.messages].reverse()} max={100} />
              </div>
            </div>
          )}

          <div className="flex items-center justify-between">
            <Button variant="ghost" onClick={() => {
              if (dryRunning) {
                void api.cancel();
                setDryRunning(false);
                run.reset();
              }
              setStep("filters");
            }}>
              ← Back
            </Button>
            <div className="flex items-center gap-2">
              <Button variant="subtle" onClick={dryRun} disabled={dryRunning || previewing}>
                {dryRunning ? "Listing…" : "Dry run"}
              </Button>
              {dryRunning && (
                <Button variant="subtle" onClick={() => { void api.cancel(); setDryRunning(false); run.reset(); }}>
                  Stop
                </Button>
              )}
              <Button
                variant="danger"
                onClick={() => (skipConfirm ? startDelete() : setStep("confirm"))}
                // Never block on the preview count — it loads asynchronously and
                // is purely informational. Only stop a run the completed preview
                // proved is empty.
                disabled={!previewing && previewCount === 0}
              >
                {skipConfirm ? (dispatchMode ? "Dispatch →" : "Delete →") : "Continue →"}
              </Button>
            </div>
          </div>
        </Card>
      )}

      {step === "confirm" && (
        <Card>
          <StepHeader
            eyebrow="Step 5"
            title={dispatchMode ? "Confirm dispatch" : "Confirm deletion"}
            subtitle="This can't be undone."
          />
          <ConfirmGate
            count={previewCount}
            scopeLabel={scopeLabel || guild?.name || ""}
            onBack={() => setStep("review")}
            onConfirm={startDelete}
            remoteName={dispatchMode ? (remote.actingAs ?? "your server") : null}
            pinsKept={!includePinned}
          />
        </Card>
      )}

      {step === "progress" && (
        <Card>
          <StepHeader
            title={run.verifying ? "Verifying" : "Deleting"}
            subtitle={
              run.verifying
                ? "Double-checking Discord's index for any stragglers…"
                : "You can stop at any time. Messages already deleted will stay gone."
            }
          />
          <ProgressMeter progress={run.progress} variant="live" stopping={stopping} verifying={run.verifying} />
          {run.notices.length > 0 && (
            <div className="mt-5">
              <div className="mb-1.5 text-xs font-medium uppercase tracking-wide text-dim">
                Activity
              </div>
              <div className="max-h-28 overflow-y-auto rounded-lg bg-black/20 px-3 py-2 font-mono text-xs leading-relaxed text-dim">
                {run.notices.slice(-8).reverse().map((n, i) => (
                  <div key={run.notices.length - i} className="truncate">
                    {n}
                  </div>
                ))}
              </div>
            </div>
          )}
          <div className="mt-5">
            <MessageRain messages={run.messages} />
          </div>
          <div className="mt-5 flex justify-end">
            <Button variant="subtle" onClick={cancel} disabled={stopping}>
              {stopping ? "Stopping…" : "Stop"}
            </Button>
          </div>
        </Card>
      )}

      {step === "done" && (
        <DoneCard
          deleted={run.finished?.deleted ?? run.progress?.deleted ?? 0}
          failed={run.progress?.failed ?? 0}
          cancelled={run.finished?.cancelled ?? false}
          verified={run.finished?.verified ?? false}
          remaining={run.finished?.remaining ?? 0}
          logPath={run.finished?.logPath ?? ""}
          onRestart={restart}
        />
      )}

      {step === "dispatched" && (
        <Card>
          <div className="flex flex-col items-center py-2 text-center">
            <div className="mb-3 grid h-14 w-14 place-items-center rounded-full bg-accent-soft text-xl">
              🚀
            </div>
            <h2 className="text-base font-semibold tracking-tight text-ink">Dispatched to your server</h2>
            <p className="mt-1 max-w-sm text-sm text-dim">
              The job is running on your server{remote.actingAs ? ` (acting as ${remote.actingAs})` : ""} and
              will continue even if you close this app. Track its progress under the Server tab.
            </p>
            <div className="mt-5 flex items-center gap-2">
              <Button onClick={onDispatched}>View on server →</Button>
              <Button variant="ghost" onClick={restart}>
                New task
              </Button>
            </div>
          </div>
        </Card>
      )}
    </WizardShell>
  );
}

function DoneCard({
  deleted,
  failed,
  cancelled,
  verified,
  remaining,
  logPath,
  onRestart,
}: {
  deleted: number;
  failed: number;
  cancelled: boolean;
  verified: boolean;
  remaining: number;
  logPath: string;
  onRestart: () => void;
}) {
  const hasResidual = !cancelled && remaining > 0;
  const icon = cancelled ? "✋" : verified ? "✓" : "⚠️";
  const title = cancelled ? "Stopped" : verified ? "Deletion complete" : "Finished with leftovers";
  const ringTone = cancelled ? "bg-ink/5" : verified ? "bg-accent-soft" : "bg-warn-soft";

  return (
    <Card>
      <div className="flex flex-col items-center py-2 text-center">
        <div className={cn("mb-3 grid h-14 w-14 place-items-center rounded-full text-xl", ringTone)}>
          {icon}
        </div>
        <h2 className="text-base font-semibold tracking-tight text-ink">{title}</h2>
        <p className="mt-1 text-sm text-dim">
          {formatCount(deleted)} message{deleted === 1 ? "" : "s"} deleted
          {failed > 0 && <span className="text-danger-strong"> · {formatCount(failed)} failed</span>}.
        </p>

        {!cancelled && (
          <div
            className={cn(
              "mt-4 rounded-full px-3.5 py-1.5 text-sm font-medium",
              verified ? "bg-accent-soft text-accent-strong" : "bg-warn-soft text-warn",
            )}
          >
            {verified ? "Verified: 0 messages remaining" : `${formatCount(remaining)} could not be removed`}
          </div>
        )}
        {hasResidual && (
          <p className="mt-2 max-w-sm text-xs text-dim">
            These are usually messages you no longer have permission to delete. The log lists what was attempted.
          </p>
        )}

        <div className="mt-5 flex items-center gap-2">
          {logPath && (
            <Button onClick={() => api.openPath(logPath)}>Show log</Button>
          )}
          <Button variant="subtle" onClick={() => api.openLogFolder()}>
            Open folder
          </Button>
          <Button variant="ghost" onClick={onRestart}>
            Start over
          </Button>
        </div>
        {logPath && <div className="mt-4 max-w-full truncate font-mono text-xs text-dim">{logPath}</div>}
      </div>
    </Card>
  );
}
