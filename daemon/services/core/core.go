package core

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/teris-io/shortid"

	"unbalance/daemon/common"
	"unbalance/daemon/domain"
	"unbalance/daemon/lib"
	"unbalance/daemon/logger"
)

const (
	certDir    = "/boot/config/ssl/certs"
	mailCmd    = "/usr/local/emhttp/webGui/scripts/notify"
	timeFormat = "Jan _2, 2006 15:04:05"
)

var (
	reFreeSpace = regexp.MustCompile(`(.*?)\s+(\d+)\s+(\d+)\s+(\d+)\s+(.*?)\s+(.*?)$`)
	reRsync     = regexp.MustCompile(`exit status (\d+)`)
	reProgress  = regexp.MustCompile(`(?s)^([\d,]+).*?\(.*?\)$|^([\d,]+).*?$`)
	reItems     = regexp.MustCompile(`(\d+)\s+(.*?)$`)
	reStat      = regexp.MustCompile(`[-dclpsbD]([-rwxsS]{3})([-rwxsS]{3})([-rwxtT]{3})\|(.*?)\:(.*?)\|(.*?)\|(.*)`)
)

var rsyncErrors = map[int]string{
	0:  "Success",
	1:  "Syntax or usage error",
	2:  "Protocol incompatibility",
	3:  "Errors selecting input/output files, dirs",
	4:  "Requested action not supported: an attempt was made to manipulate 64-bit files on a platform that cannot support them, or an option was specified that is supported by the client and not by the server.",
	5:  "Error starting client-server protocol",
	6:  "Daemon unable to append to log-file",
	10: "Error in socket I/O",
	11: "Error in file I/O",
	12: "Error in rsync protocol data stream",
	13: "Errors with program diagnostics",
	14: "Error in IPC code",
	20: "Received SIGUSR1 or SIGINT",
	21: "Some error returned by waitpid()",
	22: "Error allocating core memory buffers",
	23: "Partial transfer due to error",
	24: "Partial transfer due to vanished source files",
	25: "The --max-delete limit stopped deletions",
	30: "Timeout in data send/receive",
	35: "Timeout waiting for daemon connection",
	99: "Interrupted by the user",
}

type Core struct {
	ctx *domain.Context

	state *domain.State
	sid   *shortid.Shortid

	mailbox chan any

	pendingPlans   map[string]*planTicket
	pendingPlansMu sync.Mutex

	stopped bool

	autoGatherMu            sync.RWMutex
	autoGatherRun           *domain.AutoGatherDryRunState
	autoGatherStopRequested bool
	autoGatherDryRunExec    bool // forces --dry-run on Gather operations during Stage 3B

	autoGatherRealPrepared      *domain.AutoGatherRealPreparedMove
	autoGatherRealState         *domain.AutoGatherRealState
	autoGatherRealStopRequested bool
	autoGatherRealExec          bool // Stage 3C real Gather execution in progress

	autoGatherControlledRun           *domain.AutoGatherControlledState
	autoGatherControlledStopRequested bool
	autoGatherControlledShutdown      bool

	autoGatherLibraryRevision uint64
	autoGatherLibraryScan     *domain.AutoGatherScanResult
	autoGatherLibrarySummary  domain.AutoGatherLibrarySummary

	scheduleMu      sync.RWMutex
	scheduleConfig  domain.AutoGatherScheduleConfig
	scheduleState   domain.AutoGatherScheduleState
	scheduleStop    chan struct{}
	scheduleNow     Clock // injectable; nil means system clock
}

func Create(ctx *domain.Context) *Core {

	return &Core{
		ctx: ctx,
		state: &domain.State{
			Status: common.OpNeutral,
		},
		pendingPlans: make(map[string]*planTicket),
		mailbox: ctx.Hub.Sub(
			common.CommandScatterPlanStart,
			common.CommandScatterMove,
			common.CommandScatterCopy,
			common.CommandGatherPlanStart,
			common.CommandGatherMove,
			common.CommandScatterValidate,
			common.CommandRemoveSource,
			common.CommandReplay,
			common.CommandStop,
		),
	}
}

func (c *Core) Start() error {
	err := c.sanityCheck()
	if err != nil {
		return err
	}

	unraid, err := c.getStatus()
	if err != nil {
		return err
	}

	c.state.Status = common.OpNeutral
	c.state.Unraid = unraid

	history, err := c.historyRead()
	if err != nil {
		logger.Yellow("Unable to read history: %s", err)
	}

	c.state.History = history

	sid, err := shortid.New(1, shortid.DefaultABC, 2342)
	if err != nil {
		return err
	}

	c.sid = sid

	c.RecoverAutoGatherControlledInterrupted()
	c.initAutoGatherSchedule()
	c.startAutoGatherScheduler()

	go c.mailboxHandler()

	return nil
}

func (c *Core) Stop() error {
	c.stopAutoGatherScheduler()
	c.persistAutoGatherControlledForShutdown()
	return nil
}

func (c *Core) mailboxHandler() {
	for p := range c.mailbox {
		packet := p.(domain.Packet)

		if !c.mailboxAllows(packet.Topic) {
			logger.Yellow("unbalance is busy: %d", c.state.Status)
			continue
		}

		switch packet.Topic {
		case common.CommandScatterPlanStart:
			var setup domain.ScatterSetup
			err := lib.Bind(packet.Payload, &setup)
			if err != nil {
				logger.Red("unable to unmarshal %s packet: %s", packet.Topic, err)
				continue
			}
			go c.scatterPlanPrepare(setup)
		case common.CommandScatterMove:
			var ref planRef
			err := lib.Bind(packet.Payload, &ref)
			if err != nil {
				logger.Red("unable to unmarshal %s packet: %s", packet.Topic, err)
				continue
			}
			go c.scatterMove(ref.PlanID)
		case common.CommandScatterCopy:
			var ref planRef
			err := lib.Bind(packet.Payload, &ref)
			if err != nil {
				logger.Red("unable to unmarshal %s packet: %s", packet.Topic, err)
				continue
			}
			go c.scatterCopy(ref.PlanID)

		case common.CommandGatherPlanStart:
			var setup domain.GatherSetup
			err := lib.Bind(packet.Payload, &setup)
			if err != nil {
				logger.Red("unable to unmarshal %s packet: %s", packet.Topic, err)
				continue
			}
			go c.gatherPlanPrepare(setup)

		case common.CommandGatherMove:
			var ref planRef
			err := lib.Bind(packet.Payload, &ref)
			if err != nil {
				logger.Red("unable to unmarshal %s packet: %s", packet.Topic, err)
				continue
			}
			go c.gatherMove(ref.PlanID, ref.Target)

		case common.CommandScatterValidate:
			var ref operationRef
			err := lib.Bind(packet.Payload, &ref)
			if err != nil {
				logger.Red("unable to unmarshal %s packet: %s", packet.Topic, err)
				continue
			}
			go c.scatterValidate(ref.OperationID)

		case common.CommandRemoveSource:
			var ref commandRef
			err := lib.Bind(packet.Payload, &ref)
			if err != nil {
				logger.Red("unable to unmarshal %s packet: %s", packet.Topic, err)
				continue
			}
			go c.removeSourceByID(ref.OperationID, ref.CommandID)

		case common.CommandReplay:
			var ref operationRef
			err := lib.Bind(packet.Payload, &ref)
			if err != nil {
				logger.Red("unable to unmarshal %s packet: %s", packet.Topic, err)
				continue
			}

			go c.replay(ref.OperationID)

		case common.CommandStop:
			c.stopped = true
			c.requestAutoGatherDryRunStop()
			c.requestAutoGatherRealStop()
			c.StopAutoGatherControlled()
		}
	}
}

// mailboxAllows gates manual Gather/Scatter commands while Stage 3B/3C/3D
// execution is active, or while a previous Stage 3D session was interrupted
// and has not been acknowledged. Stop remains available during Auto Gather
// dry-run/real and in-flight Gather moves.
func (c *Core) mailboxAllows(topic string) bool {
	if topic == common.CommandStop {
		return c.state.Status == common.OpGatherMove ||
			c.state.Status == common.OpAutoGatherDryRun ||
			c.state.Status == common.OpAutoGatherReal ||
			c.isAutoGatherDryRunActive() ||
			c.isAutoGatherRealExecutionBusy()
	}
	if c.isAutoGatherDryRunActive() || c.isAutoGatherRealExecutionBusy() || c.autoGatherControlledBlocksRealOps() {
		return false
	}
	if c.state.Status == common.OpNeutral {
		return true
	}
	return false
}

func (c *Core) isAutoGatherDryRunActive() bool {
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	if c.autoGatherRun == nil {
		return false
	}
	switch c.autoGatherRun.Phase {
	case domain.AutoGatherDryRunPhaseRunning, domain.AutoGatherDryRunPhaseStopping:
		return true
	default:
		return false
	}
}

func (c *Core) GetConfig() *domain.Config {
	return &c.ctx.Config
}

func (c *Core) GetState() *domain.State {
	return c.state
}

func (c *Core) GetStorage() *domain.Unraid {
	unraid, err := c.getStatus()
	if err != nil {
		logger.Yellow("unable to get storage: %s", err)
	} else {
		c.state.Unraid = unraid
	}

	return c.state.Unraid
}

func (c *Core) GetOperation() *domain.Operation {
	return c.state.Operation
}

func (c *Core) GetHistory() *domain.History {
	c.state.History.LastChecked = time.Now()

	go func() {
		err := c.historyWrite(c.state.History)
		if err != nil {
			logger.Yellow("Unable to write history: %s", err)
		}
	}()

	return c.state.History
}

// configMutationBlocked returns an error when persisted config must not change.
// Interrupted Stage 3D sessions block all normal config mutations until
// acknowledged; this never clears interrupted state or resumes work.
func (c *Core) configMutationBlocked() error {
	if c.autoGatherControlledBlocksRealOps() {
		return fmt.Errorf("config changes are blocked while a controlled Auto Gather session is active or awaiting interruption acknowledgement")
	}
	return nil
}

// dryRunChangeBlocked returns an error when DRY_RUN must not change because an
// operation is in flight. Changing DRY_RUN never alters in-progress semantics.
func (c *Core) dryRunChangeBlocked() error {
	if err := c.configMutationBlocked(); err != nil {
		return err
	}
	if c.state != nil && c.state.Status != common.OpNeutral {
		return fmt.Errorf("cannot change dry-run while unbalanced is busy (status %d)", c.state.Status)
	}
	if c.isAutoGatherDryRunActive() {
		return fmt.Errorf("cannot change dry-run while Auto Gather dry-run is active")
	}
	if c.isAutoGatherRealSessionActive() {
		return fmt.Errorf("cannot change dry-run while a real Auto Gather preparation or move is active")
	}
	if c.isAutoGatherControlledActive() {
		return fmt.Errorf("cannot change dry-run while controlled Auto Gather is active")
	}
	return nil
}

// SetDryRun updates the effective runtime dry-run flag and persists it to the
// configured data-dir unbalanced.env. No service restart is required.
//
// Setting dryRun=false (enabling real transfers/deletes) requires confirm=true.
// The change is refused while Gather/Scatter/Auto Gather work is active or while
// a Stage 3D interruption awaits acknowledgement. It never executes transfers,
// clears interrupted state, resumes work, or substitutes for Stage 3C/3D confirmations.
func (c *Core) SetDryRun(dryRun bool, confirm bool) (*domain.Config, error) {
	if err := c.dryRunChangeBlocked(); err != nil {
		return &c.ctx.Config, err
	}

	if c.ctx.DryRun == dryRun {
		return &c.ctx.Config, nil
	}

	if !dryRun && !confirm {
		return &c.ctx.Config, fmt.Errorf("explicit confirmation is required to disable global dry-run (real transfers and source deletion become possible)")
	}

	prev := c.ctx.DryRun
	c.ctx.DryRun = dryRun
	if err := c.saveSettings(); err != nil {
		c.ctx.DryRun = prev
		logger.Yellow("setDryRun: unable to save settings: %s", err)
		return &c.ctx.Config, fmt.Errorf("unable to persist dry-run setting: %w", err)
	}
	return &c.ctx.Config, nil
}

func (c *Core) SetNotifyPlan(value int) *domain.Config {
	if err := c.configMutationBlocked(); err != nil {
		logger.Yellow("setNotifyPlan: %s", err)
		return &c.ctx.Config
	}
	c.ctx.Config.NotifyPlan = value
	if err := c.saveSettings(); err != nil {
		logger.Yellow("setNotifyPlan: unable to save settings: %s", err)
	}
	return &c.ctx.Config
}

func (c *Core) SetNotifyTransfer(value int) *domain.Config {
	if err := c.configMutationBlocked(); err != nil {
		logger.Yellow("setNotifyTransfer: %s", err)
		return &c.ctx.Config
	}
	c.ctx.Config.NotifyTransfer = value
	if err := c.saveSettings(); err != nil {
		logger.Yellow("setNotifyTransfer: unable to save settings: %s", err)
	}
	return &c.ctx.Config
}

func (c *Core) SetReservedSpace(amount uint64, unit string) *domain.Config {
	if err := c.configMutationBlocked(); err != nil {
		logger.Yellow("setReservedSpace: %s", err)
		return &c.ctx.Config
	}
	c.ctx.Config.ReservedAmount = amount
	c.ctx.Config.ReservedUnit = unit
	if err := c.saveSettings(); err != nil {
		logger.Yellow("setReservedSpace: unable to save settings: %s", err)
	}
	return &c.ctx.Config
}

func (c *Core) SetRsyncArgs(value []string) (*domain.Config, error) {
	if err := c.configMutationBlocked(); err != nil {
		return &c.ctx.Config, err
	}
	value = cleanRsyncArgs(value)
	if err := validateRsyncArgs(value); err != nil {
		logger.Yellow("setRsyncArgs: rejected args %v: %s", value, err)
		return &c.ctx.Config, err
	}

	c.ctx.Config.RsyncArgs = value
	if err := c.saveSettings(); err != nil {
		logger.Yellow("setRsyncArgs: unable to save settings: %s", err)
		return &c.ctx.Config, err
	}
	return &c.ctx.Config, nil
}

func (c *Core) SetVerbosity(value int) *domain.Config {
	if err := c.configMutationBlocked(); err != nil {
		logger.Yellow("setVerbosity: %s", err)
		return &c.ctx.Config
	}
	c.ctx.Config.Verbosity = value
	if err := c.saveSettings(); err != nil {
		logger.Yellow("setVerbosity: unable to save settings: %s", err)
	}
	return &c.ctx.Config
}

func (c *Core) SetRefreshRate(value int) *domain.Config {
	if err := c.configMutationBlocked(); err != nil {
		logger.Yellow("setRefreshRate: %s", err)
		return &c.ctx.Config
	}
	c.ctx.Config.RefreshRate = value
	c.resetSamples(c.state.Operation)
	if err := c.saveSettings(); err != nil {
		logger.Yellow("setRefreshRate: unable to save settings: %s", err)
	}
	return &c.ctx.Config
}

func (c *Core) SetLogLines(value int) *domain.Config {
	if err := c.configMutationBlocked(); err != nil {
		logger.Yellow("setLogLines: %s", err)
		return &c.ctx.Config
	}
	c.ctx.Config.LogLines = clampLogLines(value)
	if err := c.saveSettings(); err != nil {
		logger.Yellow("setLogLines: unable to save settings: %s", err)
	}
	return &c.ctx.Config
}

func (c *Core) SetAuth(passwordHash string) error {
	if err := c.configMutationBlocked(); err != nil {
		return err
	}
	c.ctx.Config.AuthPassword = passwordHash
	return c.saveSettings()
}

func (c *Core) saveSettings() error {
	return lib.SaveEnv(c.ctx.Paths.EnvFile, c.ctx.Config)
}

// HISTORY HANDLERS
func (c *Core) historyRead() (*domain.History, error) {
	var history domain.History

	fileName := c.ctx.Paths.HistoryFile

	file, err := os.Open(fileName)
	if err != nil {
		empty := &domain.History{
			Version: common.HistoryVersion,
			Items:   make(map[string]*domain.Operation),
			Order:   make([]string, 0),
		}

		return empty, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&history)
	if err != nil {
		empty := &domain.History{
			Version: common.HistoryVersion,
			Items:   make(map[string]*domain.Operation),
			Order:   make([]string, 0),
		}

		return empty, err
	}

	return &history, nil
}

func (c *Core) historyWrite(history *domain.History) error {
	tmpName := c.ctx.Paths.HistoryFile + "." + shortid.MustGenerate()

	file, err := os.Create(tmpName)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	err = encoder.Encode(history)
	if err != nil {
		return err
	}

	return os.Rename(tmpName, c.ctx.Paths.HistoryFile)
}

func (c *Core) updateHistory(history *domain.History, operation *domain.Operation) {
	// Stage 3B dry-run orchestration must not churn normal Gather/Scatter history.
	if c.autoGatherDryRunExec {
		return
	}
	count := len(history.Order)
	if count == common.HistoryCapacity {
		delete(history.Items, history.Order[count-1])
		// prepend item, remove oldest item
		history.Order = append([]string{operation.ID}, history.Order[:count-1]...)
	} else {
		// prepend item
		history.Order = append([]string{operation.ID}, history.Order...)
	}

	history.Items[operation.ID] = operation

	go func() {
		err := c.historyWrite(history)
		if err != nil {
			logger.Yellow("Unable to write history: %s", err)
		}
	}()
}
