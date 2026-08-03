package domain

// AutoGatherDiskPresence describes how a show appears on one physical location.
type AutoGatherDiskPresence struct {
	DiskName   string `json:"diskName"`
	VideoCount uint64 `json:"videoCount"`
	VideoBytes uint64 `json:"videoBytes"`
	TotalBytes uint64 `json:"totalBytes"`
	EmptyOnly  bool   `json:"emptyOnly"`
	SidecarOnly bool  `json:"sidecarOnly"`
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
	SidecarOnlyDisks    []string                 `json:"sidecarOnlyDisks"`
	EmptyOnlyDisks      []string                 `json:"emptyOnlyDisks"`
	CachePoolsWithVideo []string                 `json:"cachePoolsWithVideo"`
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
