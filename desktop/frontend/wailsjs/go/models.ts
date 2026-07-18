export namespace logstats {
	
	export class Bucket {
	    label: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new Bucket(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.count = source["count"];
	    }
	}
	export class ChannelStat {
	    id: string;
	    label: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new ChannelStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.count = source["count"];
	    }
	}
	export class FailReason {
	    reason: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new FailReason(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.reason = source["reason"];
	        this.count = source["count"];
	    }
	}
	export class RunStat {
	    file: string;
	    startedAt: string;
	    deleted: number;
	    failed: number;
	    topChannel: string;
	    topChannelLabel: string;
	
	    static createFrom(source: any = {}) {
	        return new RunStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file = source["file"];
	        this.startedAt = source["startedAt"];
	        this.deleted = source["deleted"];
	        this.failed = source["failed"];
	        this.topChannel = source["topChannel"];
	        this.topChannelLabel = source["topChannelLabel"];
	    }
	}
	export class SearchHit {
	    file: string;
	    runAt: string;
	    kind: string;
	    id: string;
	    channelId: string;
	    channelLabel: string;
	    content: string;
	    timestamp: string;
	    attachments: number;
	    error: string;
	    authorName: string;
	    authorAvatarUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new SearchHit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file = source["file"];
	        this.runAt = source["runAt"];
	        this.kind = source["kind"];
	        this.id = source["id"];
	        this.channelId = source["channelId"];
	        this.channelLabel = source["channelLabel"];
	        this.content = source["content"];
	        this.timestamp = source["timestamp"];
	        this.attachments = source["attachments"];
	        this.error = source["error"];
	        this.authorName = source["authorName"];
	        this.authorAvatarUrl = source["authorAvatarUrl"];
	    }
	}
	export class SearchResult {
	    hits: SearchHit[];
	    total: number;
	    offset: number;
	    limit: number;
	    scanned: number;
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hits = this.convertValues(source["hits"], SearchHit);
	        this.total = source["total"];
	        this.offset = source["offset"];
	        this.limit = source["limit"];
	        this.scanned = source["scanned"];
	        this.truncated = source["truncated"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Stats {
	    source: string;
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
	    topChannels: ChannelStat[];
	    byMonth: Bucket[];
	    byHour: number[];
	    recent: RunStat[];
	    failures: FailReason[];
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.runs = source["runs"];
	        this.totalDeleted = source["totalDeleted"];
	        this.totalFailed = source["totalFailed"];
	        this.withAttachments = source["withAttachments"];
	        this.attachments = source["attachments"];
	        this.totalChars = source["totalChars"];
	        this.channels = source["channels"];
	        this.firstPostedAt = source["firstPostedAt"];
	        this.lastPostedAt = source["lastPostedAt"];
	        this.firstRunAt = source["firstRunAt"];
	        this.lastRunAt = source["lastRunAt"];
	        this.topChannels = this.convertValues(source["topChannels"], ChannelStat);
	        this.byMonth = this.convertValues(source["byMonth"], Bucket);
	        this.byHour = source["byHour"];
	        this.recent = this.convertValues(source["recent"], RunStat);
	        this.failures = this.convertValues(source["failures"], FailReason);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class ActingAsDTO {
	    username: string;
	    id: string;
	
	    static createFrom(source: any = {}) {
	        return new ActingAsDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.username = source["username"];
	        this.id = source["id"];
	    }
	}
	export class AttachmentDTO {
	    url: string;
	    contentType: string;
	    isImage: boolean;
	    width: number;
	    height: number;
	    filename: string;
	
	    static createFrom(source: any = {}) {
	        return new AttachmentDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.contentType = source["contentType"];
	        this.isImage = source["isImage"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.filename = source["filename"];
	    }
	}
	export class ChannelDTO {
	    id: string;
	    name: string;
	    type: number;
	    nsfw: boolean;
	    avatarUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new ChannelDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.nsfw = source["nsfw"];
	        this.avatarUrl = source["avatarUrl"];
	    }
	}
	export class ChannelExportDTO {
	    id: string;
	    name: string;
	    type: string;
	    messageCount: number;
	    isDm: boolean;
	    isForgotten: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ChannelExportDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.messageCount = source["messageCount"];
	        this.isDm = source["isDm"];
	        this.isForgotten = source["isForgotten"];
	    }
	}
	export class ConfigInfoDTO {
	    configDir: string;
	    logDir: string;
	    logBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new ConfigInfoDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configDir = source["configDir"];
	        this.logDir = source["logDir"];
	        this.logBytes = source["logBytes"];
	    }
	}
	export class CountDTO {
	    messages: number;
	    channels: number;
	
	    static createFrom(source: any = {}) {
	        return new CountDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.messages = source["messages"];
	        this.channels = source["channels"];
	    }
	}
	export class DeleteRequest {
	    guildId: string;
	    guildName: string;
	    channelIds: string[];
	    before: string;
	    after: string;
	    maxId: string;
	    minId: string;
	    includePinned: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DeleteRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.guildId = source["guildId"];
	        this.guildName = source["guildName"];
	        this.channelIds = source["channelIds"];
	        this.before = source["before"];
	        this.after = source["after"];
	        this.maxId = source["maxId"];
	        this.minId = source["minId"];
	        this.includePinned = source["includePinned"];
	    }
	}
	export class ExportSummaryDTO {
	    root: string;
	    totalMessages: number;
	    channels: ChannelExportDTO[];
	
	    static createFrom(source: any = {}) {
	        return new ExportSummaryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.totalMessages = source["totalMessages"];
	        this.channels = this.convertValues(source["channels"], ChannelExportDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FeedMessageDTO {
	    content: string;
	    channel: string;
	    timestamp: string;
	    authorName: string;
	    authorAvatarUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new FeedMessageDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.channel = source["channel"];
	        this.timestamp = source["timestamp"];
	        this.authorName = source["authorName"];
	        this.authorAvatarUrl = source["authorAvatarUrl"];
	    }
	}
	export class GuildDTO {
	    id: string;
	    name: string;
	    iconUrl: string;
	    owner: boolean;
	    isDm: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GuildDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.iconUrl = source["iconUrl"];
	        this.owner = source["owner"];
	        this.isDm = source["isDm"];
	    }
	}
	export class IdentityDTO {
	    publicKey: string;
	    fingerprint: string;
	    created: boolean;
	
	    static createFrom(source: any = {}) {
	        return new IdentityDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.publicKey = source["publicKey"];
	        this.fingerprint = source["fingerprint"];
	        this.created = source["created"];
	    }
	}
	export class ImportRequest {
	    include: string[];
	    exclude: string[];
	    forgotten: boolean;
	    noDms: boolean;
	    before: string;
	    after: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.include = source["include"];
	        this.exclude = source["exclude"];
	        this.forgotten = source["forgotten"];
	        this.noDms = source["noDms"];
	        this.before = source["before"];
	        this.after = source["after"];
	    }
	}
	export class JobDTO {
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
	
	    static createFrom(source: any = {}) {
	        return new JobDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.description = source["description"];
	        this.state = source["state"];
	        this.total = source["total"];
	        this.deleted = source["deleted"];
	        this.failed = source["failed"];
	        this.skipped = source["skipped"];
	        this.ignored = source["ignored"];
	        this.residual = source["residual"];
	        this.error = source["error"];
	        this.hasExport = source["hasExport"];
	        this.created = source["created"];
	        this.recent = this.convertValues(source["recent"], FeedMessageDTO);
	        this.ratePerSec = source["ratePerSec"];
	        this.etaMs = source["etaMs"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LogSearchRequest {
	    text: string;
	    channelId: string;
	    kind: string;
	    withAttachments: boolean;
	    before: string;
	    after: string;
	    limit: number;
	    offset: number;
	
	    static createFrom(source: any = {}) {
	        return new LogSearchRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.channelId = source["channelId"];
	        this.kind = source["kind"];
	        this.withAttachments = source["withAttachments"];
	        this.before = source["before"];
	        this.after = source["after"];
	        this.limit = source["limit"];
	        this.offset = source["offset"];
	    }
	}
	export class MessageDTO {
	    id: string;
	    channelId: string;
	    channelName: string;
	    content: string;
	    timestamp: string;
	    authorName: string;
	    authorAvatarUrl: string;
	    attachments: AttachmentDTO[];
	
	    static createFrom(source: any = {}) {
	        return new MessageDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.channelId = source["channelId"];
	        this.channelName = source["channelName"];
	        this.content = source["content"];
	        this.timestamp = source["timestamp"];
	        this.authorName = source["authorName"];
	        this.authorAvatarUrl = source["authorAvatarUrl"];
	        this.attachments = this.convertValues(source["attachments"], AttachmentDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MonitorDTO {
	    id: string;
	    name: string;
	    enabled: boolean;
	    scope: string;
	    mode: string;
	    channels: string[];
	    maxAgeAmount: number;
	    maxAgeUnit: string;
	    intervalHrs: number;
	    includePinned: boolean;
	    lastRun: string;
	    nextRun: string;
	    lastDeleted: number;
	    total: number;
	    running: boolean;
	    recent: FeedMessageDTO[];
	
	    static createFrom(source: any = {}) {
	        return new MonitorDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	        this.scope = source["scope"];
	        this.mode = source["mode"];
	        this.channels = source["channels"];
	        this.maxAgeAmount = source["maxAgeAmount"];
	        this.maxAgeUnit = source["maxAgeUnit"];
	        this.intervalHrs = source["intervalHrs"];
	        this.includePinned = source["includePinned"];
	        this.lastRun = source["lastRun"];
	        this.nextRun = source["nextRun"];
	        this.lastDeleted = source["lastDeleted"];
	        this.total = source["total"];
	        this.running = source["running"];
	        this.recent = this.convertValues(source["recent"], FeedMessageDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MonitorReq {
	    id: string;
	    name: string;
	    enabled: boolean;
	    scope: string;
	    mode: string;
	    channels: string[];
	    maxAgeAmount: number;
	    maxAgeUnit: string;
	    intervalHrs: number;
	    includePinned: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MonitorReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	        this.scope = source["scope"];
	        this.mode = source["mode"];
	        this.channels = source["channels"];
	        this.maxAgeAmount = source["maxAgeAmount"];
	        this.maxAgeUnit = source["maxAgeUnit"];
	        this.intervalHrs = source["intervalHrs"];
	        this.includePinned = source["includePinned"];
	    }
	}
	export class PingDTO {
	    version: string;
	    hasToken: boolean;
	    actingAs?: ActingAsDTO;
	    jobs: number;
	    monitors: number;
	
	    static createFrom(source: any = {}) {
	        return new PingDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.hasToken = source["hasToken"];
	        this.actingAs = this.convertValues(source["actingAs"], ActingAsDTO);
	        this.jobs = source["jobs"];
	        this.monitors = source["monitors"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PrefsDTO {
	    skipConfirm: boolean;
	    preScan: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PrefsDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skipConfirm = source["skipConfirm"];
	        this.preScan = source["preScan"];
	    }
	}
	export class PreviewDTO {
	    target: string;
	    total: number;
	    actingAs?: ActingAsDTO;
	
	    static createFrom(source: any = {}) {
	        return new PreviewDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.target = source["target"];
	        this.total = source["total"];
	        this.actingAs = this.convertValues(source["actingAs"], ActingAsDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RemoteDTO {
	    name: string;
	    address: string;
	    hasKey: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.address = source["address"];
	        this.hasKey = source["hasKey"];
	    }
	}
	export class RemoteJobRequest {
	    kind: string;
	    guild: string;
	    channels: string[];
	    exclude: string[];
	    user: string;
	    before: string;
	    after: string;
	    maxId: string;
	    minId: string;
	    verify: boolean;
	    includePinned: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteJobRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.guild = source["guild"];
	        this.channels = source["channels"];
	        this.exclude = source["exclude"];
	        this.user = source["user"];
	        this.before = source["before"];
	        this.after = source["after"];
	        this.maxId = source["maxId"];
	        this.minId = source["minId"];
	        this.verify = source["verify"];
	        this.includePinned = source["includePinned"];
	    }
	}
	export class TokenStateDTO {
	    hasToken: boolean;
	    botMode: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TokenStateDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hasToken = source["hasToken"];
	        this.botMode = source["botMode"];
	    }
	}
	export class UserDTO {
	    id: string;
	    username: string;
	    globalName: string;
	    avatarUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new UserDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.username = source["username"];
	        this.globalName = source["globalName"];
	        this.avatarUrl = source["avatarUrl"];
	    }
	}

}

