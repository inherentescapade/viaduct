package main

// Event names emitted from Go to the frontend via the Wails runtime. The
// frontend subscribes with EventsOn(name, cb). Keeping them as constants in one
// place stops the two sides from drifting.
const (
	// EvProgress carries a ProgressDTO during both live and import runs.
	EvProgress = "run:progress"
	// EvMessage carries a MessageDTO for each message seen during a dry-run.
	EvMessage = "run:message"
	// EvEnumDone fires when a dry-run (Enumerate) finishes: {count:int}.
	EvEnumDone = "run:enumDone"
	// EvImportDone carries []FailReasonDTO after an import run completes.
	EvImportDone = "import:done"
	// EvVerifying fires when a live delete enters its verify/mop-up phase.
	EvVerifying = "run:verifying"
	// EvError carries {message:string} when a run fails (not on cancellation).
	EvError = "run:error"
	// EvFinished fires when any run ends. Payload: {logPath, cancelled, and for
	// live deletes verified:bool, remaining:int, deleted:int}.
	EvFinished = "run:finished"
	// EvNotice carries a human-readable status/decision line: {message:string}.
	// Used to surface the engine's adaptive rate-limit decisions live.
	EvNotice = "run:notice"
	// EvExportProgress carries an ExportProgressDTO while a remote export log
	// streams down, so the UI can show a progress bar for large downloads.
	EvExportProgress = "remote:exportProgress"
)
