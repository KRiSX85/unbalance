package domain

type Config struct {
	Version        string   `json:"version"`
	DryRun         bool     `json:"dryRun"`
	NotifyPlan     int      `json:"notifyPlan"`
	NotifyTransfer int      `json:"notifyTransfer"`
	ReservedAmount uint64   `json:"reservedAmount"`
	ReservedUnit   string   `json:"reservedUnit"`
	RsyncArgs      []string `json:"rsyncArgs"`
	Verbosity      int      `json:"verbosity"`
	RefreshRate    int      `json:"refreshRate"`
	LogLines       int      `json:"logLines"`
	SpeedWindow    string   `json:"speedWindow"`
	TvLibraryPath  string   `json:"tvLibraryPath"`
	AuthEnabled    bool     `json:"authEnabled"`
	AuthUsername   string   `json:"authUsername"`
	AuthPassword   string   `json:"-"`
}

// SetDryRunRequest updates the persisted/runtime global dry-run switch.
// Setting DryRun=false (enabling real transfers) requires Confirm=true.
type SetDryRunRequest struct {
	DryRun  bool `json:"dryRun"`
	Confirm bool `json:"confirm"`
}
