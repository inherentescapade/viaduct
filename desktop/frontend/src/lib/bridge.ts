// The single seam between the React app and the Go backend. At runtime Wails
// injects window.go.main.App (the bound methods) and window.runtime (the event
// bus). We wrap both in typed helpers rather than depending on generated code.

import type {
  ActingAsDTO,
  ChannelDTO,
  ConfigInfoDTO,
  CountDTO,
  DeleteRequest,
  ExportSummaryDTO,
  GuildDTO,
  IdentityDTO,
  ImportRequest,
  JobDTO,
  LogSearchRequest,
  LogStatsDTO,
  MessageDTO,
  MonitorDTO,
  MonitorReq,
  PingDTO,
  PrefsDTO,
  PreviewDTO,
  RemoteDTO,
  RemoteJobRequest,
  RunStatDTO,
  SearchResultDTO,
  TokenStateDTO,
  UserDTO,
} from "./types";

interface GoApp {
  ValidateToken(token: string, botMode: boolean): Promise<UserDTO>;
  AutoLogin(): Promise<UserDTO | null>;
  HasAutoDetect(): Promise<boolean>;
  AutoDetectToken(): Promise<UserDTO>;
  GetSavedToken(): Promise<TokenStateDTO>;
  ListGuilds(): Promise<GuildDTO[]>;
  ListChannels(guildId: string): Promise<ChannelDTO[]>;
  GuildMessageCount(guildId: string): Promise<number>;
  DMMessageCount(channelId: string): Promise<number>;
  Preview(req: DeleteRequest): Promise<number>;
  SampleMessages(req: DeleteRequest): Promise<MessageDTO[]>;
  StartDelete(req: DeleteRequest): Promise<void>;
  Enumerate(req: DeleteRequest): Promise<void>;
  Cancel(): Promise<void>;
  PickExportFolder(): Promise<string>;
  LoadExport(path: string): Promise<ExportSummaryDTO>;
  CountImport(req: ImportRequest): Promise<CountDTO>;
  StartImport(req: ImportRequest): Promise<void>;
  LogPath(): Promise<string>;
  OpenLogFolder(): Promise<void>;
  OpenPath(p: string): Promise<void>;
  DownloadLog(file: string): Promise<string>;
  DeleteLog(file: string): Promise<void>;
  ConfigInfo(): Promise<ConfigInfoDTO>;
  GetPrefs(): Promise<PrefsDTO>;
  SetSkipConfirm(v: boolean): Promise<void>;
  SetPreScan(v: boolean): Promise<void>;
  ClearLogs(): Promise<void>;
  ClearSession(): Promise<void>;
  // Insights (parsed deletion logs).
  LogStats(): Promise<LogStatsDTO>;
  RemoteLogStats(): Promise<LogStatsDTO>;
  SearchLogs(req: LogSearchRequest): Promise<SearchResultDTO>;
  RemoteSearchLogs(req: LogSearchRequest): Promise<SearchResultDTO>;
  ExportSearch(req: LogSearchRequest): Promise<string>;
  RemoteExportSearch(req: LogSearchRequest): Promise<string>;
  PurgeLogs(req: LogSearchRequest): Promise<number>;
  RemotePurgeLogs(req: LogSearchRequest): Promise<number>;
  ListRuns(): Promise<RunStatDTO[]>;
  RemoteListRuns(): Promise<RunStatDTO[]>;
  RemoteDownloadRun(file: string): Promise<string>;
  RemoteDeleteRun(file: string): Promise<void>;
  // Self-hosting ("Server" tab).
  EnsureIdentity(): Promise<IdentityDTO>;
  GetRemote(): Promise<RemoteDTO | null>;
  RemotePairRequest(address: string): Promise<void>;
  RemotePairComplete(address: string, code: string): Promise<ActingAsDTO | null>;
  ForgetRemote(): Promise<void>;
  RemotePing(): Promise<PingDTO>;
  RemoteConnect(): Promise<ActingAsDTO>;
  RemotePreview(req: RemoteJobRequest): Promise<PreviewDTO>;
  RemoteSubmit(req: RemoteJobRequest): Promise<JobDTO>;
  RemoteJobs(): Promise<JobDTO[]>;
  RemoteJob(id: string): Promise<JobDTO>;
  RemoteCancel(id: string): Promise<JobDTO>;
  RemoteRemoveJob(id: string): Promise<void>;
  RemoteRetryJob(id: string): Promise<JobDTO>;
  RemoteExportJob(id: string): Promise<string>;
  RemoteMonitors(): Promise<MonitorDTO[]>;
  RemoteSetMonitor(req: MonitorReq): Promise<MonitorDTO>;
  RemotePreviewMonitor(req: MonitorReq): Promise<PreviewDTO>;
  RemoteDeleteMonitor(id: string): Promise<void>;
  // Local (in-process) monitors: run while the app is open, no server.
  LocalMonitors(): Promise<MonitorDTO[]>;
  SetLocalMonitor(req: MonitorReq): Promise<MonitorDTO>;
  PreviewLocalMonitor(req: MonitorReq): Promise<PreviewDTO>;
  DeleteLocalMonitor(id: string): Promise<void>;
}

interface WailsRuntime {
  EventsOn(name: string, cb: (...data: unknown[]) => void): (() => void) | void;
  EventsOff(name: string, ...additional: string[]): void;
}

declare global {
  interface Window {
    go?: { main: { App: GoApp } };
    runtime?: WailsRuntime;
  }
}

function app(): GoApp {
  const a = window.go?.main?.App;
  if (!a) {
    throw new Error(
      "The Viaduct backend isn't available. Run the app with `wails dev` or the built binary.",
    );
  }
  return a;
}

export const api = {
  validateToken: (token: string, botMode: boolean) => app().ValidateToken(token, botMode),
  autoLogin: () => app().AutoLogin(),
  hasAutoDetect: () => app().HasAutoDetect(),
  autoDetectToken: () => app().AutoDetectToken(),
  getSavedToken: () => app().GetSavedToken(),
  listGuilds: () => app().ListGuilds(),
  listChannels: (guildId: string) => app().ListChannels(guildId),
  guildMessageCount: (guildId: string) => app().GuildMessageCount(guildId),
  dmMessageCount: (channelId: string) => app().DMMessageCount(channelId),
  preview: (req: DeleteRequest) => app().Preview(req),
  sampleMessages: (req: DeleteRequest) => app().SampleMessages(req),
  startDelete: (req: DeleteRequest) => app().StartDelete(req),
  enumerate: (req: DeleteRequest) => app().Enumerate(req),
  cancel: () => app().Cancel(),
  pickExportFolder: () => app().PickExportFolder(),
  loadExport: (path: string) => app().LoadExport(path),
  countImport: (req: ImportRequest) => app().CountImport(req),
  startImport: (req: ImportRequest) => app().StartImport(req),
  logPath: () => app().LogPath(),
  openLogFolder: () => app().OpenLogFolder(),
  openPath: (p: string) => app().OpenPath(p),
  downloadLog: (file: string) => app().DownloadLog(file),
  deleteLog: (file: string) => app().DeleteLog(file),
  configInfo: () => app().ConfigInfo(),
  getPrefs: () => app().GetPrefs(),
  setSkipConfirm: (v: boolean) => app().SetSkipConfirm(v),
  setPreScan: (v: boolean) => app().SetPreScan(v),
  clearLogs: () => app().ClearLogs(),
  clearSession: () => app().ClearSession(),
  // Insights (parsed deletion logs).
  logStats: () => app().LogStats(),
  remoteLogStats: () => app().RemoteLogStats(),
  searchLogs: (req: LogSearchRequest) => app().SearchLogs(req),
  remoteSearchLogs: (req: LogSearchRequest) => app().RemoteSearchLogs(req),
  exportSearch: (req: LogSearchRequest) => app().ExportSearch(req),
  remoteExportSearch: (req: LogSearchRequest) => app().RemoteExportSearch(req),
  purgeLogs: (req: LogSearchRequest) => app().PurgeLogs(req),
  remotePurgeLogs: (req: LogSearchRequest) => app().RemotePurgeLogs(req),
  listRuns: () => app().ListRuns(),
  remoteListRuns: () => app().RemoteListRuns(),
  remoteDownloadRun: (file: string) => app().RemoteDownloadRun(file),
  remoteDeleteRun: (file: string) => app().RemoteDeleteRun(file),
  // Self-hosting ("Server" tab).
  ensureIdentity: () => app().EnsureIdentity(),
  getRemote: () => app().GetRemote(),
  remotePairRequest: (address: string) => app().RemotePairRequest(address),
  remotePairComplete: (address: string, code: string) =>
    app().RemotePairComplete(address, code),
  forgetRemote: () => app().ForgetRemote(),
  remotePing: () => app().RemotePing(),
  remoteConnect: () => app().RemoteConnect(),
  remotePreview: (req: RemoteJobRequest) => app().RemotePreview(req),
  remoteSubmit: (req: RemoteJobRequest) => app().RemoteSubmit(req),
  remoteJobs: () => app().RemoteJobs(),
  remoteJob: (id: string) => app().RemoteJob(id),
  remoteCancel: (id: string) => app().RemoteCancel(id),
  remoteRemoveJob: (id: string) => app().RemoteRemoveJob(id),
  remoteRetryJob: (id: string) => app().RemoteRetryJob(id),
  remoteExportJob: (id: string) => app().RemoteExportJob(id),
  remoteMonitors: () => app().RemoteMonitors(),
  remoteSetMonitor: (req: MonitorReq) => app().RemoteSetMonitor(req),
  remotePreviewMonitor: (req: MonitorReq) => app().RemotePreviewMonitor(req),
  remoteDeleteMonitor: (id: string) => app().RemoteDeleteMonitor(id),
  localMonitors: () => app().LocalMonitors(),
  setLocalMonitor: (req: MonitorReq) => app().SetLocalMonitor(req),
  previewLocalMonitor: (req: MonitorReq) => app().PreviewLocalMonitor(req),
  deleteLocalMonitor: (id: string) => app().DeleteLocalMonitor(id),
};

// Event names: must match desktop/events.go.
export const EV = {
  progress: "run:progress",
  message: "run:message",
  enumDone: "run:enumDone",
  importDone: "import:done",
  verifying: "run:verifying",
  error: "run:error",
  finished: "run:finished",
  notice: "run:notice",
  exportProgress: "remote:exportProgress",
} as const;

// on subscribes to a backend event and returns an unsubscribe function. Wails'
// EventsOn returns an unsubscribe in recent runtimes; we fall back to EventsOff.
export function on<T>(event: string, cb: (data: T) => void): () => void {
  const rt = window.runtime;
  if (!rt) return () => {};
  const off = rt.EventsOn(event, (...args: unknown[]) => cb(args[0] as T));
  return typeof off === "function" ? off : () => rt.EventsOff(event);
}
