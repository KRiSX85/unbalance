package domain

// Scheduled Auto Gather configuration and status (scheduler around Stage 3D).

const (
	AutoGatherTriggerManual    = "manual"
	AutoGatherTriggerScheduled = "scheduled"
)

// Schedule frequency values.
const (
	ScheduleFrequencyWeekly  = "weekly"
	ScheduleFrequencyMonthly = "monthly"
)

// Schedule result/kind values for last attempt observability.
const (
	ScheduleResultNone               = ""
	ScheduleResultSkippedDryRun      = "skipped_dry_run"
	ScheduleResultSkippedBusy        = "skipped_busy"
	ScheduleResultSkippedInterrupted = "skipped_interrupted"
	ScheduleResultSkippedDisabled    = "skipped_disabled"
	ScheduleResultStarted            = "started"
	ScheduleResultCompleted          = "completed"
	ScheduleResultFailed             = "failed"
	ScheduleResultStopped            = "stopped"
	ScheduleResultStartFailed        = "start_failed"
)

// AutoGatherScheduleConfig is the operator-configured schedule (persisted).
type AutoGatherScheduleConfig struct {
	Enabled    bool   `json:"enabled"`
	Frequency  string `json:"frequency,omitempty"` // weekly (default) | monthly
	Hour       int    `json:"hour"`                 // 0-23 server local
	Minute     int    `json:"minute"`               // 0-59 server local
	Weekdays   []int  `json:"weekdays"`             // Go time.Weekday: 0=Sunday … 6=Saturday (weekly)
	MonthlyDay int    `json:"monthlyDay,omitempty"` // 1-28 (monthly); ignored for weekly
	MaxShows   int    `json:"maxShows"`
	MaxBytes   uint64 `json:"maxBytes"` // decimal-byte bound (same as Stage 3D)
}

// AutoGatherScheduleState is persisted runtime metadata (not a Stage 3D token).
type AutoGatherScheduleState struct {
	LastAttemptedOccurrence string `json:"lastAttemptedOccurrence,omitempty"` // YYYY-MM-DD@HH:MM local
	LastResult              string `json:"lastResult,omitempty"`
	LastResultDetail        string `json:"lastResultDetail,omitempty"`
	LastAttemptAt           string `json:"lastAttemptAt,omitempty"` // RFC3339
	LastEndedAt             string `json:"lastEndedAt,omitempty"`   // RFC3339
	LastSessionID           string `json:"lastSessionId,omitempty"`
	ConfigError             string `json:"configError,omitempty"`
}

// AutoGatherScheduleFile is the on-disk document under the data directory.
type AutoGatherScheduleFile struct {
	Version int                      `json:"version"`
	Config  AutoGatherScheduleConfig `json:"config"`
	State   AutoGatherScheduleState  `json:"state"`
}

// AutoGatherScheduleSetRequest updates the schedule. Confirm is required when
// enabling or changing an already-enabled real-automation schedule.
type AutoGatherScheduleSetRequest struct {
	Enabled    bool   `json:"enabled"`
	Frequency  string `json:"frequency"`
	Hour       int    `json:"hour"`
	Minute     int    `json:"minute"`
	Weekdays   []int  `json:"weekdays"`
	MonthlyDay int    `json:"monthlyDay"`
	MaxShows   int    `json:"maxShows"`
	MaxBytes   uint64 `json:"maxBytes"`
	Confirm    bool   `json:"confirm"`
}

// AutoGatherScheduleStatus is the server-authoritative schedule view for UI/API.
type AutoGatherScheduleStatus struct {
	Config                  AutoGatherScheduleConfig `json:"config"`
	Enabled                 bool                     `json:"enabled"`
	Timezone                string                   `json:"timezone"`
	TimezoneOffsetMinutes   int                      `json:"timezoneOffsetMinutes"`
	ServerLocalTime         string                   `json:"serverLocalTime"` // RFC3339 in local zone
	NextOccurrence          string                   `json:"nextOccurrence,omitempty"`
	NextOccurrenceAt        string                   `json:"nextOccurrenceAt,omitempty"` // RFC3339
	LastAttemptedOccurrence string                   `json:"lastAttemptedOccurrence,omitempty"`
	LastResult              string                   `json:"lastResult,omitempty"`
	LastResultDetail        string                   `json:"lastResultDetail,omitempty"`
	LastAttemptAt           string                   `json:"lastAttemptAt,omitempty"`
	LastEndedAt             string                   `json:"lastEndedAt,omitempty"`
	LastSessionID           string                   `json:"lastSessionId,omitempty"`
	ConfigError             string                   `json:"configError,omitempty"`
	GlobalDryRun            bool                     `json:"globalDryRun"`
}
