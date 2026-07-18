import { api } from "./bridge";
import type { MonitorDTO, MonitorReq, PreviewDTO } from "./types";

// MonitorApi abstracts the four monitor operations so the same UI (MonitorPanel,
// NewMonitorForm) drives either local (in-process) or remote (server) monitors.
export interface MonitorApi {
  list: () => Promise<MonitorDTO[]>;
  set: (req: MonitorReq) => Promise<MonitorDTO>;
  remove: (id: string) => Promise<void>;
  preview: (req: MonitorReq) => Promise<PreviewDTO>;
}

export const localMonitorApi: MonitorApi = {
  list: () => api.localMonitors(),
  set: (req) => api.setLocalMonitor(req),
  remove: (id) => api.deleteLocalMonitor(id),
  preview: (req) => api.previewLocalMonitor(req),
};

export const remoteMonitorApi: MonitorApi = {
  list: () => api.remoteMonitors(),
  set: (req) => api.remoteSetMonitor(req),
  remove: (id) => api.remoteDeleteMonitor(id),
  preview: (req) => api.remotePreviewMonitor(req),
};
