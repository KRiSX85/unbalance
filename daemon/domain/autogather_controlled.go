package domain

// Stage 3D: Controlled Real Auto Gather Orchestration.
// Sequences multiple one-show real Gather moves using the proven Stage 3C
// execution path, with explicit user authorisation and bounded sessions.

// Phase values for the controlled real Auto Gather session (Stage 3D).
const (
	AutoGatherControlledPhaseIdle        = "idle"
	AutoGatherControlledPhaseRunning     = "running"
	AutoGatherControlledPhaseStopping    = "stopping"
	AutoGatherControlledPhaseStopped     = "stopped"
	AutoGatherControlledPhaseFailed      = "failed"
	AutoGatherControlledPhaseCompleted   = "completed"
	AutoGatherControlledPhaseInterrupted = "interrupted"
)

// AutoGatherControlledStartRequest requires explicit confirmation and session bounds.
type AutoGatherControlledStartRequest struct {
	Confirm  bool   `json:"confirm"`
	MaxShows int    `json:"maxShows"`
	MaxBytes uint64 `json:"maxBytes"`
}

// AutoGatherControlledAcknowledgeRequest clears an interrupted session after
// explicit operator confirmation. It never resumes execution.
type AutoGatherControlledAcknowledgeRequest struct {
	Confirm bool `json:"confirm"`
}

// AutoGatherControlledRsyncProbe is a diagnostic snapshot of a recorded rsync
// child PID after daemon restart. It is never used to kill a process.
type AutoGatherControlledRsyncProbe struct {
	PID            int    `json:"pid,omitempty"`
	Alive          bool   `json:"alive"`
	PlausibleRsync bool   `json:"plausibleRsync"`
	Command        string `json:"command,omitempty"`
	Note           string `json:"note,omitempty"`
}

// AutoGatherControlledShowRecord records one show processed during a Stage 3D session.
type AutoGatherControlledShowRecord struct {
	ShowPath           string                            `json:"showPath"`
	ShowName           string                            `json:"showName,omitempty"`
	TargetDisk         string                            `json:"targetDisk,omitempty"`
	MoveBytes          uint64                            `json:"moveBytes,omitempty"`
	PermissionWarnings *AutoGatherRealPermissionWarnings `json:"permissionWarnings,omitempty"`
	Reason             string                            `json:"reason,omitempty"`
	At                 string                            `json:"at,omitempty"`
}

// AutoGatherControlledState is the server-authoritative Stage 3D session status.
type AutoGatherControlledState struct {
	Phase                   string                           `json:"phase"`
	GlobalDryRun            bool                             `json:"globalDryRun"`
	MaxShows                int                              `json:"maxShows"`
	MaxBytes                uint64                           `json:"maxBytes"`
	CurrentShow             string                           `json:"currentShow,omitempty"`
	CurrentShowName         string                           `json:"currentShowName,omitempty"`
	CurrentTarget           string                           `json:"currentTarget,omitempty"`
	OperationPhase          string                           `json:"operationPhase,omitempty"`
	Completed               []AutoGatherControlledShowRecord `json:"completed,omitempty"`
	Skipped                 []AutoGatherControlledShowRecord `json:"skipped,omitempty"`
	CumulativeBytes         uint64                           `json:"cumulativeBytes"`
	FailedShow              string                           `json:"failedShow,omitempty"`
	FailedShowName          string                           `json:"failedShowName,omitempty"`
	FailureReason           string                           `json:"failureReason,omitempty"`
	StartedAt               string                           `json:"startedAt,omitempty"`
	EndedAt                 string                           `json:"endedAt,omitempty"`
	Message                 string                           `json:"message,omitempty"`
	Error                   string                           `json:"error,omitempty"`
	SessionID               string                           `json:"sessionId,omitempty"`
	LastRsyncPID            int                              `json:"lastRsyncPid,omitempty"`
	LastSourceEntry         string                           `json:"lastSourceEntry,omitempty"`
	RsyncProbe              *AutoGatherControlledRsyncProbe  `json:"rsyncProbe,omitempty"`
	RequiresAcknowledgement bool                             `json:"requiresAcknowledgement,omitempty"`
	CanAcknowledge          bool                             `json:"canAcknowledge,omitempty"`
	// LibraryRevision/LibrarySummary are compact presentation hints. The
	// full Stage 1+2 dataset is never included in controlled/status; the
	// browser fetches it separately when the revision changes.
	LibraryRevision uint64                    `json:"libraryRevision,omitempty"`
	LibrarySummary  *AutoGatherLibrarySummary `json:"librarySummary,omitempty"`
}

// AutoGatherLibrarySummary is a lightweight count snapshot for Stage 3D status.
type AutoGatherLibrarySummary struct {
	Revision            uint64 `json:"revision"`
	LibraryPath         string `json:"libraryPath,omitempty"`
	ShowCount           int    `json:"showCount"`
	SplitCount          int    `json:"splitCount"`
	RecommendationCount int    `json:"recommendationCount"`
}

// AutoGatherLibraryView is the already-computed library dataset for presentation.
// GET /auto-gather/library returns this without walking the filesystem.
type AutoGatherLibraryView struct {
	Revision uint64 `json:"revision"`
	AutoGatherScanResult
}
