package common

const (
	PluginName      = "unbalanced"
	APIEndpoint     = "/api"
	MailCmd         = "/usr/local/emhttp/webGui/scripts/notify" // MailCmd - location of notify command
	DefaultDataDir  = "/boot/config/plugins/unbalanced"         // DefaultDataDir - official plugin config path
	DefaultLogsDir  = "/var/log"
	PluginLocation  = DefaultDataDir // retained alias for compatibility
	ChanCapacity    = 3
	HistoryCapacity = 25
	EnvFilename     = "unbalanced.env"
	HistoryFilename = "unbalanced.hist"
	HistoryVersion  = 2
	SessionFilename = "unbalanced.sessions"
	LogFilename     = "unbalanced.log"
	RsyncArgs       = "-avPR"
)

const ReservedSpace uint64 = 1024 * 1024 * 1024 // 1Gb

const (
	OpNeutral         = 0
	OpScatterPlan     = 1
	OpScatterMove     = 2
	OpScatterCopy     = 3
	OpScatterValidate = 4
	OpGatherPlan      = 5
	OpGatherMove      = 6
	OpAutoGatherDryRun = 7
	OpAutoGatherReal   = 8
)

const (
	CommandScatterPlanStart  = "scatter:plan:start"
	EventScatterPlanStarted  = "scatter:plan:started"
	EventScatterPlanProgress = "scatter:plan:progress"
	EventScatterPlanEnded    = "scatter:plan:ended"
	CommandScatterMove       = "scatter:move"
	CommandScatterCopy       = "scatter:copy"
	CommandScatterValidate   = "scatter:validate"

	CommandGatherPlanStart  = "gather:plan:start"
	EventGatherPlanStarted  = "gather:plan:started"
	EventGatherPlanProgress = "gather:plan:progress"
	EventGatherPlanEnded    = "gather:plan:ended"
	CommandGatherMove       = "gather:move"

	EventTransferStarted  = "transfer:started"
	EventTransferProgress = "transfer:progress"
	EventTransferEnded    = "transfer:ended"

	EventOperationError = "operation:error"

	CommandRemoveSource = "remove:source"
	CommandReplay       = "replay"
	CommandStop         = "stop"
)

const (
	CmdCompleted = iota
	CmdPending
	CmdFlagged
	CmdStopped
	CmdSourceRemoval
	CmdInProgress
)
