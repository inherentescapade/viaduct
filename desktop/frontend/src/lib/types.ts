// Mirrors the Go DTOs in desktop/dto.go. Kept hand-written (rather than relying
// on Wails binding generation) so the frontend builds standalone.

export interface UserDTO {
  id: string;
  username: string;
  globalName: string;
  avatarUrl: string;
}

export interface TokenStateDTO {
  hasToken: boolean;
  botMode: boolean;
}

export interface GuildDTO {
  id: string;
  name: string;
  iconUrl: string;
  owner: boolean;
  isDm: boolean;
}

export interface ChannelDTO {
  id: string;
  name: string;
  type: number;
  nsfw: boolean;
  avatarUrl: string;
}

export interface AttachmentDTO {
  url: string;
  contentType: string;
  isImage: boolean;
  width: number;
  height: number;
  filename: string;
}

export interface ChannelExportDTO {
  id: string;
  name: string;
  type: string;
  messageCount: number;
  isDm: boolean;
  isForgotten: boolean;
}

export interface ExportSummaryDTO {
  root: string;
  totalMessages: number;
  channels: ChannelExportDTO[];
}

export interface CountDTO {
  messages: number;
  channels: number;
}

export interface FailReasonDTO {
  reason: string;
  count: number;
}

// ---- Insights (parsed deletion logs): mirrors viaduct/logstats.Stats ----

export interface ChannelStatDTO {
  id: string;
  label: string;
  count: number;
}

export interface BucketDTO {
  label: string;
  count: number;
}

export interface RunStatDTO {
  file: string;
  startedAt: string;
  deleted: number;
  failed: number;
  topChannel: string;
  topChannelLabel?: string;
  // source is set client-side when merging local + server runs into one list,
  // so a per-row action (download/delete) knows which backend to call. Not
  // sent by the Go bridge itself.
  source?: "local" | "server";
}

export interface LogStatsDTO {
  source: string; // "local" | "server" | "combined"
  runs: number;
  totalDeleted: number;
  totalFailed: number;
  withAttachments: number;
  attachments: number;
  totalChars: number;
  channels: number;
  firstPostedAt: string;
  lastPostedAt: string;
  firstRunAt: string;
  lastRunAt: string;
  topChannels: ChannelStatDTO[];
  byMonth: BucketDTO[];
  byHour: number[];
  recent: RunStatDTO[];
  failures: FailReasonDTO[];
}

// ---- Deletion-log search: mirrors viaduct/logstats.Search* ----

export interface LogSearchRequest {
  text: string;
  channelId: string;
  kind: string; // "" | "deleted" | "failed"
  withAttachments: boolean;
  before: string; // date expression ("2024-01-01", "30d", ...)
  after: string;
  limit: number;
  offset: number;
}

export interface SearchHitDTO {
  file: string;
  runAt: string;
  kind: string; // "deleted" | "failed"
  id: string;
  channelId: string;
  channelLabel: string;
  content: string;
  timestamp: string;
  attachments: number;
  error: string;
  authorName: string;
  authorAvatarUrl: string;
  // source is set client-side when merging local + server search results, so
  // a per-row action (download/delete) knows which backend to call. Not sent
  // by the Go bridge itself.
  source?: "local" | "server";
}

export interface SearchResultDTO {
  hits: SearchHitDTO[];
  total: number;
  offset: number;
  limit: number;
  scanned: number;
  truncated: boolean;
}

export interface MessageDTO {
  id: string;
  channelId: string;
  channelName: string;
  content: string;
  timestamp: string;
  authorName: string;
  authorAvatarUrl: string;
  attachments: AttachmentDTO[];
}

export interface ConfigInfoDTO {
  configDir: string;
  logDir: string;
  logBytes: number;
}

export interface PrefsDTO {
  skipConfirm: boolean;
  preScan: boolean;
}

export interface ProgressDTO {
  guildName: string;
  channel: string;
  total: number;
  deleted: number;
  failed: number;
  skipped: number;
  ignored: number;
  rateLimited: number;
  done: boolean;
  error: string;
  elapsedMs: number;
  ratePerSec: number;
  etaMs: number;
  starting: boolean;
}

export interface DeleteRequest {
  guildId: string;
  guildName: string;
  channelIds: string[];
  before: string;
  after: string;
  maxId: string;
  minId: string;
}

export interface ImportRequest {
  include: string[];
  exclude: string[];
  forgotten: boolean;
  noDms: boolean;
  before: string;
  after: string;
}

export interface FinishedPayload {
  logPath: string;
  cancelled: boolean;
  // Present for live deletions:
  verified?: boolean;
  remaining?: number;
  deleted?: number;
}

// ---- Self-hosting ("Server" tab): mirrors desktop/remote_dto.go ----

export interface IdentityDTO {
  publicKey: string;
  fingerprint: string;
  created: boolean;
}

export interface RemoteDTO {
  name: string;
  address: string;
  hasKey: boolean;
}

export interface ActingAsDTO {
  username: string;
  id: string;
}

export interface PingDTO {
  version: string;
  hasToken: boolean;
  actingAs: ActingAsDTO | null;
  jobs: number;
  monitors: number;
}

export interface PreviewDTO {
  target: string;
  total: number;
  actingAs: ActingAsDTO | null;
}

export interface FeedMessageDTO {
  content: string;
  channel: string;
  timestamp: string;
  authorName: string;
  authorAvatarUrl: string;
}

export interface JobDTO {
  id: string;
  kind: string;
  description: string;
  state: string;
  total: number;
  deleted: number;
  failed: number;
  skipped: number;
  ignored: number;
  residual: number;
  error: string;
  hasExport: boolean;
  created: string;
  recent: FeedMessageDTO[];
  ratePerSec: number;
  etaMs: number;
}

// ExportProgressDTO is emitted on EV.exportProgress while a remote export log
// streams down, carrying byte counts so the UI can show a download progress bar.
export interface ExportProgressDTO {
  jobId: string;
  received: number;
  total: number;
}

export interface MonitorDTO {
  id: string;
  name: string;
  enabled: boolean;
  scope: string;
  mode: string;
  channels: string[];
  maxAgeAmount: number;
  maxAgeUnit: string;
  intervalHrs: number;
  lastRun: string;
  nextRun: string;
  lastDeleted: number;
  total: number;
  running: boolean;
  recent: FeedMessageDTO[];
}

export interface RemoteJobRequest {
  kind: string; // "delete_guild" | "delete_dm"
  guild: string;
  channels: string[];
  exclude?: string[]; // delete everywhere in scope EXCEPT these channels/DMs
  user: string;
  before: string;
  after: string;
  maxId: string;
  minId: string;
  verify: boolean;
}

export interface MonitorReq {
  id: string;
  name: string;
  enabled: boolean;
  scope: string;
  mode: string; // "exclude" | "include"
  channels: string[];
  maxAgeAmount: number;
  maxAgeUnit: string; // "minutes" | "hours" | "days" | "weeks"
  intervalHrs: number;
}
