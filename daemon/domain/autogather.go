package domain

// AutoGatherDiskPresence describes how a show appears on one physical location.
//
// VideoBytes is used only for Split / Waiting-for-Mover / video-presence
// classification. TotalBytes is the all-file size (video + sidecars + other
// regular files) used by Stage 2 Gather-fit recommendations.
type AutoGatherDiskPresence struct {
	DiskName    string `json:"diskName"`
	VideoCount  uint64 `json:"videoCount"`
	VideoBytes  uint64 `json:"videoBytes"`
	TotalBytes  uint64 `json:"totalBytes"`
	EmptyOnly   bool   `json:"emptyOnly"`
	SidecarOnly bool   `json:"sidecarOnly"`
}

// AutoGatherShow is one immediate child of the configured TV library path.
type AutoGatherShow struct {
	Name                string                   `json:"name"`
	Path                string                   `json:"path"`
	Status              string                   `json:"status"`
	Split               bool                     `json:"split"`
	Ready               bool                     `json:"ready"`
	TotalVideoBytes     uint64                   `json:"totalVideoBytes"`
	TotalBytes          uint64                   `json:"totalBytes"`
	VideoDisks          []AutoGatherDiskPresence `json:"videoDisks"`
	SidecarOnlyDisks    []AutoGatherDiskPresence `json:"sidecarOnlyDisks"`
	EmptyOnlyDisks      []AutoGatherDiskPresence `json:"emptyOnlyDisks"`
	CachePoolsWithVideo []string                 `json:"cachePoolsWithVideo"`

	// CleanupCandidateDisks lists physical array disks where this show's folder
	// tree exists but contains no files (empty-folder-only). Display/reporting
	// only in Stage 2 — no directories are removed.
	CleanupCandidateDisks []string `json:"cleanupCandidateDisks,omitempty"`
	CleanupCandidateCount int      `json:"cleanupCandidateCount,omitempty"`

	// Recommendation fields are derived from split-show all-file scan stats and
	// current array free space. Stage 2 is informational only and never creates
	// normal gather pending plans, history entries, or operations.
	RecommendedTargetDisk   string                      `json:"recommendedTargetDisk,omitempty"`
	MoveRequiredBytes       uint64                      `json:"moveRequiredBytes,omitempty"`
	ProjectedFreeBytes      uint64                      `json:"projectedFreeBytes,omitempty"`
	ProjectedFreePercent    float64                     `json:"projectedFreePercent,omitempty"`
	BelowPreferredFreeFloor bool                        `json:"belowPreferredFreeFloor,omitempty"`
	MinMovementAlternative  *AutoGatherTargetCandidate  `json:"minMovementAlternative,omitempty"`
	GatherTargets           []AutoGatherTargetCandidate `json:"gatherTargets,omitempty"`
	NoEligibleReason        string                      `json:"noEligibleReason,omitempty"`
}

// AutoGatherTargetCandidate is one physical array destination evaluation for a
// split show. Eligible targets are those that pass the same reserved-space /
// Greedy fit check used by manual Gather, applied to Stage 1 all-file
// aggregates. Move figures are estimates: per-disk aggregation and Stage 1
// TotalBytes are not guaranteed bit-identical to getItems()/du -bs.
type AutoGatherTargetCandidate struct {
	DiskName                 string  `json:"diskName"`
	Eligible                 bool    `json:"eligible"`
	IneligibleReason         string  `json:"ineligibleReason,omitempty"`
	MoveRequiredBytes        uint64  `json:"moveRequiredBytes"`
	CurrentShowBytesOnTarget uint64  `json:"currentShowBytesOnTarget"`
	FreeBytes                uint64  `json:"freeBytes"`
	DiskSizeBytes            uint64  `json:"diskSizeBytes"`
	ProjectedFreeBytes       uint64  `json:"projectedFreeBytes"`
	ProjectedFreePercent     float64 `json:"projectedFreePercent"`
	MeetsPreferredFreeFloor  bool    `json:"meetsPreferredFreeFloor"`
}

// AutoGatherScanResult is the read-only collection-wide scanner payload.
type AutoGatherScanResult struct {
	LibraryPath string           `json:"libraryPath"`
	Shows       []AutoGatherShow `json:"shows"`
	Warnings    []string         `json:"warnings,omitempty"`
	Error       string           `json:"error,omitempty"`
	// Cancelled is set when a Stage 3B cooperative Stop aborted the scan.
	// It is distinct from Error (planner/filesystem failure).
	Cancelled bool `json:"cancelled,omitempty"`
}

// Auto Gather show status values.
const (
	AutoGatherStatusSplit           = "split"
	AutoGatherStatusConsolidated    = "consolidated"
	AutoGatherStatusWaitingForMover = "waiting_for_mover"
	AutoGatherStatusNoVideo         = "no_video"
)

// AutoGatherCanonicalPlanRequest is the Stage 3A read-only verification input.
// ShowPath must be a single /mnt/user-relative path (e.g. data/media/tv/Show).
// Stage2* fields are advisory snapshot values from the last Auto Gather scan.
type AutoGatherCanonicalPlanRequest struct {
	ShowPath                 string `json:"showPath"`
	Stage2RecommendedTarget  string `json:"stage2RecommendedTarget,omitempty"`
	Stage2EstimatedMoveBytes uint64 `json:"stage2EstimatedMoveBytes,omitempty"`
}

// AutoGatherCanonicalTarget is one destination evaluation from canonical Gather
// planning (real getItems/du + Greedy), with Auto Gather array-only eligibility.
type AutoGatherCanonicalTarget struct {
	DiskName                      string  `json:"diskName"`
	DiskPath                      string  `json:"diskPath"`
	IsPhysicalArrayDisk           bool    `json:"isPhysicalArrayDisk"`
	CanonicalEligible             bool    `json:"canonicalEligible"`
	IneligibleReason              string  `json:"ineligibleReason,omitempty"`
	CanonicalBytesToMove          uint64  `json:"canonicalBytesToMove"`
	CanonicalCurrentBytesOnTarget uint64  `json:"canonicalCurrentBytesOnTarget"`
	CanonicalItemCount            int     `json:"canonicalItemCount"`
	FreeBytes                     uint64  `json:"freeBytes"`
	DiskSizeBytes                 uint64  `json:"diskSizeBytes"`
	ProjectedFreeBytes            uint64  `json:"projectedFreeBytes"`
	ProjectedFreePercent          float64 `json:"projectedFreePercent"`
	MeetsPreferredFreeFloor       bool    `json:"meetsPreferredFreeFloor"`
	RawGatherBinPresent           bool    `json:"rawGatherBinPresent"`
}

// AutoGatherCanonicalPlanResult compares Stage 2 advisory recommendation with
// a fresh canonical Gather plan for exactly one show. Stage 3A never executes.
type AutoGatherCanonicalPlanResult struct {
	ShowPath                           string                      `json:"showPath"`
	Stage2RecommendedTarget            string                      `json:"stage2RecommendedTarget,omitempty"`
	Stage2EstimatedMoveBytes           uint64                      `json:"stage2EstimatedMoveBytes,omitempty"`
	Stage2TargetStillCanonicalEligible bool                        `json:"stage2TargetStillCanonicalEligible"`
	CanonicalRecommendedTarget         string                      `json:"canonicalRecommendedTarget,omitempty"`
	CanonicalMoveBytes                 uint64                      `json:"canonicalMoveBytes,omitempty"`
	CanonicalProjectedFreeBytes        uint64                      `json:"canonicalProjectedFreeBytes,omitempty"`
	CanonicalProjectedFreePercent      float64                     `json:"canonicalProjectedFreePercent,omitempty"`
	BelowPreferredFreeFloor            bool                        `json:"belowPreferredFreeFloor,omitempty"`
	CanonicalItemCountTotal            int                         `json:"canonicalItemCountTotal,omitempty"`
	CanonicalTargets                   []AutoGatherCanonicalTarget `json:"canonicalTargets,omitempty"`
	NoEligibleReason                   string                      `json:"noEligibleReason,omitempty"`
	Error                              string                      `json:"error,omitempty"`
	// Cancelled is set when a Stage 3B cooperative Stop aborted canonical planning.
	// It is distinct from Error and NoEligibleReason.
	Cancelled bool `json:"cancelled,omitempty"`
}

// Auto Gather dry-run orchestration (Stage 3B) phase values.
const (
	AutoGatherDryRunPhaseIdle      = "idle"
	AutoGatherDryRunPhaseRunning   = "running"
	AutoGatherDryRunPhaseStopping  = "stopping"
	AutoGatherDryRunPhaseStopped   = "stopped"
	AutoGatherDryRunPhaseFailed    = "failed"
	AutoGatherDryRunPhaseCompleted = "completed"
)

// AutoGatherDryRunShowRecord records one show handled during a Stage 3B run.
type AutoGatherDryRunShowRecord struct {
	ShowPath                string `json:"showPath"`
	ShowName                string `json:"showName,omitempty"`
	TargetDisk              string `json:"targetDisk,omitempty"`
	MoveBytes               uint64 `json:"moveBytes,omitempty"`
	BelowPreferredFreeFloor bool   `json:"belowPreferredFreeFloor,omitempty"`
	Reason                  string `json:"reason,omitempty"`
	At                      string `json:"at,omitempty"`
}

// AutoGatherDryRunState is the in-memory Stage 3B orchestration status.
// One Stage 1 library scan is retained for the run; later iterations refresh
// Unraid disk state and re-score Stage 2 without rescanning the TV library.
type AutoGatherDryRunState struct {
	Phase                string                       `json:"phase"`
	DryRun               bool                         `json:"dryRun"`
	CurrentShow          string                       `json:"currentShow,omitempty"`
	CurrentShowName      string                       `json:"currentShowName,omitempty"`
	CurrentTarget        string                       `json:"currentTarget,omitempty"`
	Completed            []AutoGatherDryRunShowRecord `json:"completed,omitempty"`
	Skipped              []AutoGatherDryRunShowRecord `json:"skipped,omitempty"`
	FailedShow           string                       `json:"failedShow,omitempty"`
	FailedShowName       string                       `json:"failedShowName,omitempty"`
	FailureReason        string                       `json:"failureReason,omitempty"`
	StartedAt            string                       `json:"startedAt,omitempty"`
	EndedAt              string                       `json:"endedAt,omitempty"`
	IterationsConsidered int                          `json:"iterationsConsidered"`
	SplitRemaining       int                          `json:"splitRemaining,omitempty"`
	Message              string                       `json:"message,omitempty"`
	Error                string                       `json:"error,omitempty"`
}

// Auto Gather one-show real execution (Stage 3C) phase values.
const (
	AutoGatherRealPhaseIdle                = "idle"
	AutoGatherRealPhasePreparing           = "preparing"
	AutoGatherRealPhasePrepared            = "prepared"
	AutoGatherRealPhaseExpired             = "expired"
	AutoGatherRealPhaseExecuting           = "executing"
	AutoGatherRealPhaseStopping            = "stopping"
	AutoGatherRealPhaseStopped             = "stopped"
	AutoGatherRealPhaseCompleted           = "completed"
	AutoGatherRealPhaseFailed              = "failed"
	AutoGatherRealPhaseVerificationWarning = "verification_warning"
)

// AutoGatherRealPrepareRequest selects one split show for read-only real-move
// preparation. Preparation never mutates the filesystem.
type AutoGatherRealPrepareRequest struct {
	ShowPath                 string `json:"showPath"`
	Stage2RecommendedTarget  string `json:"stage2RecommendedTarget,omitempty"`
	Stage2EstimatedMoveBytes uint64 `json:"stage2EstimatedMoveBytes,omitempty"`
}

// AutoGatherRealPrepareResult is returned by PREPARE and embedded in session
// state while a preparation remains valid (~5 minutes).
type AutoGatherRealPrepareResult struct {
	PreparationID             string   `json:"preparationId"`
	ShowPath                  string   `json:"showPath"`
	ShowName                  string   `json:"showName,omitempty"`
	SourceDisks               []string `json:"sourceDisks,omitempty"`
	CanonicalTargetDisk       string   `json:"canonicalTargetDisk,omitempty"`
	Stage2RecommendedTarget   string   `json:"stage2RecommendedTarget,omitempty"`
	Stage2AgreesWithCanonical bool     `json:"stage2AgreesWithCanonical"`
	CurrentBytesOnTarget      uint64   `json:"currentBytesOnTarget,omitempty"`
	EstimatedMoveBytes        uint64   `json:"estimatedMoveBytes,omitempty"`
	TargetFreeBytes           uint64   `json:"targetFreeBytes,omitempty"`
	ProjectedTargetFreeBytes  uint64   `json:"projectedTargetFreeBytes,omitempty"`
	Executable                bool     `json:"executable"`
	Issues                    []string `json:"issues,omitempty"`
	PermissionWarnings        *AutoGatherRealPermissionWarnings `json:"permissionWarnings,omitempty"`
	EmptyFolderOnlyDisks      []string `json:"emptyFolderOnlyDisks,omitempty"`
	ExpiresAt                 string   `json:"expiresAt,omitempty"`
	GlobalDryRun              bool     `json:"globalDryRun"`
	Error                     string   `json:"error,omitempty"`
	PlanFingerprint           *AutoGatherRealPlanFingerprint `json:"planFingerprint,omitempty"`
}

// AutoGatherRealPermissionWarnings surfaces legacy Gather planner permission
// diagnostics (owner/group/folder/file). Upstream unbalanced treats these as
// warnings during planning; they do not by themselves block Gather execution.
type AutoGatherRealPermissionWarnings struct {
	OwnerIssues  int64 `json:"ownerIssues,omitempty"`
	GroupIssues  int64 `json:"groupIssues,omitempty"`
	FolderIssues int64 `json:"folderIssues,omitempty"`
	FileIssues   int64 `json:"fileIssues,omitempty"`
}

// AutoGatherRealPlanTransferItem identifies one executable Gather rsync command
// derived from the target Bin (same normalization as createGatherOperation).
type AutoGatherRealPlanTransferItem struct {
	SourceDisk string `json:"sourceDisk"`
	Entry      string `json:"entry"`
	Size       uint64 `json:"size"`
}

// AutoGatherRealPlanFingerprint captures the substantive operation reviewed at
// PREPARE time. EXECUTE compares a fresh fingerprint; any mismatch refuses
// without rsync or deletion.
type AutoGatherRealPlanFingerprint struct {
	ShowPath    string                           `json:"showPath"`
	TargetDisk  string                           `json:"targetDisk"`
	TargetPath  string                           `json:"targetPath"`
	MoveBytes   uint64                           `json:"moveBytes"`
	SourceDisks []string                         `json:"sourceDisks,omitempty"`
	Items       []AutoGatherRealPlanTransferItem `json:"items,omitempty"`
}

// AutoGatherRealPreparedMove is the in-memory preparation record validated at
// EXECUTE time. Lost on daemon restart.
type AutoGatherRealPreparedMove struct {
	PreparationID       string
	ShowPath            string
	ShowName            string
	PreparedTargetDisk  string
	PreparedTargetPath  string
	EstimatedMoveBytes  uint64
	SourceDisks         []string
	Stage2Target        string
	PreparedAt          string
	ExpiresAt           string
	PlanFingerprint     AutoGatherRealPlanFingerprint
	PrepareSnapshot     AutoGatherRealPrepareResult
}

// AutoGatherRealExecuteRequest requires an explicit confirmation flag and must
// match the active preparation token and show path.
type AutoGatherRealExecuteRequest struct {
	PreparationID string `json:"preparationId"`
	ShowPath      string `json:"showPath"`
	Confirm       bool   `json:"confirm"`
}

// AutoGatherRealVerification summarizes post-move placement for the selected show.
type AutoGatherRealVerification struct {
	Passed              bool     `json:"passed"`
	TargetDisk          string   `json:"targetDisk,omitempty"`
	SubstantiveDisks    []string `json:"substantiveDisks,omitempty"`
	EmptyFolderRemnants []string `json:"emptyFolderRemnants,omitempty"`
	Message             string   `json:"message,omitempty"`
}

// AutoGatherRealState is the in-memory Stage 3C one-show real execution status.
type AutoGatherRealState struct {
	Phase            string                      `json:"phase"`
	GlobalDryRun     bool                        `json:"globalDryRun"`
	CurrentShow      string                      `json:"currentShow,omitempty"`
	CurrentShowName  string                      `json:"currentShowName,omitempty"`
	CurrentTarget    string                      `json:"currentTarget,omitempty"`
	OperationPhase   string                      `json:"operationPhase,omitempty"`
	PreparationID    string                      `json:"preparationId,omitempty"`
	Prepared         *AutoGatherRealPrepareResult `json:"prepared,omitempty"`
	Verification     *AutoGatherRealVerification `json:"verification,omitempty"`
	StartedAt        string                      `json:"startedAt,omitempty"`
	EndedAt          string                      `json:"endedAt,omitempty"`
	Error            string                      `json:"error,omitempty"`
	Message          string                      `json:"message,omitempty"`
	StoppedMessage   string                      `json:"stoppedMessage,omitempty"`
}
