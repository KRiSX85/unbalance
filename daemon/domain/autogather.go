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
	RecommendedTargetDisk string                     `json:"recommendedTargetDisk,omitempty"`
	MoveRequiredBytes     uint64                     `json:"moveRequiredBytes,omitempty"`
	GatherTargets         []AutoGatherTargetCandidate `json:"gatherTargets,omitempty"`
	NoEligibleReason      string                     `json:"noEligibleReason,omitempty"`
}

// AutoGatherTargetCandidate is one physical array destination evaluation for a
// split show. Eligible targets are those that pass the same reserved-space /
// Greedy fit check used by manual Gather, applied to Stage 1 all-file
// aggregates. Move figures are estimates: per-disk aggregation and Stage 1
// TotalBytes are not guaranteed bit-identical to getItems()/du -bs.
type AutoGatherTargetCandidate struct {
	DiskName                 string `json:"diskName"`
	Eligible                 bool   `json:"eligible"`
	IneligibleReason         string `json:"ineligibleReason,omitempty"`
	MoveRequiredBytes        uint64 `json:"moveRequiredBytes"`
	CurrentShowBytesOnTarget uint64 `json:"currentShowBytesOnTarget"`
	FreeBytes                uint64 `json:"freeBytes"`
	ProjectedFreeBytes       uint64 `json:"projectedFreeBytes"`
}

// AutoGatherScanResult is the read-only collection-wide scanner payload.
type AutoGatherScanResult struct {
	LibraryPath string           `json:"libraryPath"`
	Shows       []AutoGatherShow `json:"shows"`
	Warnings    []string         `json:"warnings,omitempty"`
	Error       string           `json:"error,omitempty"`
}

// Auto Gather show status values.
const (
	AutoGatherStatusSplit           = "split"
	AutoGatherStatusConsolidated    = "consolidated"
	AutoGatherStatusWaitingForMover = "waiting_for_mover"
	AutoGatherStatusNoVideo         = "no_video"
)
