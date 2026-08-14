package core

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"unbalance/daemon/autogather"
	"unbalance/daemon/common"
	"unbalance/daemon/domain"
	"unbalance/daemon/logger"
)

const autoGatherDryRunAdaptationMessage = "Dry-run does not change disk free space or show placement. One Stage 1 library scan is retained for the run; each later iteration refreshes Unraid disk state, re-scores Stage 2 from that inventory, and canonical-plans only the selected show. Free-space adaptation cannot be proven until real moves."

type gatherExecOutcome int

const (
	gatherExecSuccess gatherExecOutcome = iota
	gatherExecUncertain
	gatherExecCancelled
)

type gatherExecResult struct {
	outcome   gatherExecOutcome
	reason    string
	moveBytes uint64
}

type dryRunShowPick struct {
	show           domain.AutoGatherShow
	stage2Target   string
	stage2Move     uint64
	belowFloor     bool // Stage 2 below-floor flag used for selection preference
	canonical      domain.AutoGatherCanonicalPlanResult
	targetDisk     string
	targetPath     string
	canonicalBelow bool // final below-floor after canonical target resolution
	skipOnly       bool
	skipReason     string
}

// autoGatherCanonicalInfeasible reports whether canonical planning found no
// acceptable physical-array destination for a show. These are safe pre-exec skips.
func autoGatherCanonicalInfeasible(canonical domain.AutoGatherCanonicalPlanResult, resolveErr error) bool {
	if canonical.Cancelled || canonical.Error != "" {
		return false
	}
	if canonical.NoEligibleReason != "" {
		return true
	}
	if resolveErr != nil && resolveErr.Error() == "no eligible physical array target" {
		return true
	}
	return false
}

// classifyCanonicalSelectionOutcome distinguishes safe infeasibility skips from
// planner/internal failures during show selection. Cancellation is handled
// separately by the caller.
func classifyCanonicalSelectionOutcome(
	show domain.AutoGatherShow,
	canonical domain.AutoGatherCanonicalPlanResult,
	resolveErr error,
) (skipReason string, fatalErr string) {
	if canonical.Cancelled {
		return "", ""
	}
	if canonical.Error != "" {
		return "", fmt.Sprintf("canonical Gather planner error for %s: %s", show.Path, canonical.Error)
	}
	if resolveErr == nil {
		return "", ""
	}
	if autoGatherCanonicalInfeasible(canonical, resolveErr) {
		reason := canonical.NoEligibleReason
		if reason == "" {
			reason = resolveErr.Error()
		}
		if reason == "" {
			reason = "no eligible physical array target"
		}
		return reason, ""
	}
	return "", fmt.Sprintf("unexpected canonical result for %s: %s", show.Path, resolveErr.Error())
}

// autoGatherDryRunExecuteHook allows tests to substitute dry-run execution.
var autoGatherDryRunExecuteHook func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult

// autoGatherDryRunScanHook allows tests to substitute the one-time Stage 1 scan.
var autoGatherDryRunScanHook func(c *Core) domain.AutoGatherScanResult

// autoGatherDryRunRefreshHook allows tests to substitute Unraid disk refresh.
var autoGatherDryRunRefreshHook func(c *Core) *domain.Unraid

// autoGatherDryRunRescoreHook allows tests to substitute Stage 2 re-scoring from
// a retained Stage 1 snapshot. nil uses real recommendation enrichment.
var autoGatherDryRunRescoreHook func(c *Core, base domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult

// autoGatherDryRunCanonicalHook allows tests to substitute canonical planning.
var autoGatherDryRunCanonicalHook func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult

// autoGatherDryRunBuildPlanHook allows tests to substitute gather plan building.
var autoGatherDryRunBuildPlanHook func(c *Core, showPath string) (*domain.Plan, error)

// autoGatherDryRunPhaseHook is invoked at named Stage 3B phase boundaries for tests.
var autoGatherDryRunPhaseHook func(phase string)

func autoGatherDryRunLog(format string, args ...any) {
	logger.Blue("autoGatherDryRun: "+format, args...)
}

func (c *Core) notifyAutoGatherPhase(phase string) {
	if autoGatherDryRunPhaseHook != nil {
		autoGatherDryRunPhaseHook(phase)
	}
}

// StartAutoGatherDryRun begins the server-side Stage 3B loop in a background
// goroutine. It refuses to start unless global dry-run mode is enabled.
func (c *Core) StartAutoGatherDryRun() (domain.AutoGatherDryRunState, error) {
	if !c.ctx.DryRun {
		return domain.AutoGatherDryRunState{
			Phase:  domain.AutoGatherDryRunPhaseIdle,
			DryRun: false,
			Error:  "Auto Gather dry-run requires global dry-run mode to be enabled",
		}, fmt.Errorf("Auto Gather dry-run requires global dry-run mode to be enabled")
	}

	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()

	if c.isAutoGatherDryRunActiveLocked() {
		return c.snapshotAutoGatherDryRunLocked(), fmt.Errorf("Auto Gather dry-run is already active")
	}
	if c.state == nil || c.state.Status != common.OpNeutral {
		return domain.AutoGatherDryRunState{
			Phase:  domain.AutoGatherDryRunPhaseIdle,
			DryRun: true,
			Error:  fmt.Sprintf("unbalanced is busy: status %d", c.state.Status),
		}, fmt.Errorf("unbalanced is busy")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	c.autoGatherRun = &domain.AutoGatherDryRunState{
		Phase:     domain.AutoGatherDryRunPhaseRunning,
		DryRun:    true,
		StartedAt: now,
		Message:   autoGatherDryRunAdaptationMessage,
	}
	c.autoGatherStopRequested = false
	c.state.Status = common.OpAutoGatherDryRun

	autoGatherDryRunLog("run started")
	go c.autoGatherDryRunLoop()
	return c.snapshotAutoGatherDryRunLocked(), nil
}

// StopAutoGatherDryRun cooperatively stops the Stage 3B loop.
// The mutex is held only briefly so Stop remains responsive during long scan/planning.
func (c *Core) StopAutoGatherDryRun() domain.AutoGatherDryRunState {
	c.requestAutoGatherDryRunStop()
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	return c.snapshotAutoGatherDryRunLocked()
}

func (c *Core) requestAutoGatherDryRunStop() {
	c.autoGatherMu.Lock()
	accepted := false
	if c.autoGatherRun != nil &&
		(c.autoGatherRun.Phase == domain.AutoGatherDryRunPhaseRunning ||
			c.autoGatherRun.Phase == domain.AutoGatherDryRunPhaseStopping) {
		c.autoGatherStopRequested = true
		if c.autoGatherRun.Phase == domain.AutoGatherDryRunPhaseRunning {
			c.autoGatherRun.Phase = domain.AutoGatherDryRunPhaseStopping
			accepted = true
		}
	}
	c.autoGatherMu.Unlock()

	// Existing rsync cooperative-stop flag. Safe to set even when not in rsync;
	// Stage 3B execution clears it only when a stop is not already pending.
	c.stopped = true

	if accepted {
		autoGatherDryRunLog("stop requested")
		autoGatherDryRunLog("stopping")
	}
}

// GetAutoGatherDryRunState returns the current Stage 3B status snapshot.
func (c *Core) GetAutoGatherDryRunState() domain.AutoGatherDryRunState {
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	return c.snapshotAutoGatherDryRunLocked()
}

func (c *Core) isAutoGatherDryRunActiveLocked() bool {
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

func (c *Core) snapshotAutoGatherDryRunLocked() domain.AutoGatherDryRunState {
	if c.autoGatherRun == nil {
		return domain.AutoGatherDryRunState{
			Phase:   domain.AutoGatherDryRunPhaseIdle,
			DryRun:  true,
			Message: autoGatherDryRunAdaptationMessage,
		}
	}
	snap := *c.autoGatherRun
	if snap.Completed == nil {
		snap.Completed = []domain.AutoGatherDryRunShowRecord{}
	}
	if snap.Skipped == nil {
		snap.Skipped = []domain.AutoGatherDryRunShowRecord{}
	}
	snap.Message = autoGatherDryRunAdaptationMessage
	return snap
}

func (c *Core) autoGatherDryRunLoop() {
	defer func() {
		c.autoGatherMu.Lock()
		if c.autoGatherRun != nil &&
			c.autoGatherRun.Phase != domain.AutoGatherDryRunPhaseFailed &&
			c.autoGatherRun.Phase != domain.AutoGatherDryRunPhaseStopped &&
			c.autoGatherRun.Phase != domain.AutoGatherDryRunPhaseCompleted {
			c.autoGatherRun.Phase = domain.AutoGatherDryRunPhaseStopped
			c.autoGatherRun.EndedAt = time.Now().UTC().Format(time.RFC3339)
		}
		phase := ""
		if c.autoGatherRun != nil {
			phase = c.autoGatherRun.Phase
		}
		c.autoGatherMu.Unlock()
		if c.state != nil && c.state.Status == common.OpAutoGatherDryRun {
			c.state.Status = common.OpNeutral
		}
		switch phase {
		case domain.AutoGatherDryRunPhaseCompleted:
			autoGatherDryRunLog("run completed")
		case domain.AutoGatherDryRunPhaseFailed:
			autoGatherDryRunLog("run failed: %s", c.GetAutoGatherDryRunState().FailureReason)
		default:
			autoGatherDryRunLog("run stopped")
		}
	}()

	completed := map[string]struct{}{}
	skipped := map[string]struct{}{}

	// One Stage 1 library scan for the entire run. Later iterations only refresh
	// disk state and re-score Stage 2 recommendations from this snapshot.
	autoGatherDryRunLog("refresh started")
	c.notifyAutoGatherPhase("refresh")
	c.state.Unraid = c.refreshUnraidForAutoGatherDryRun()
	autoGatherDryRunLog("refresh completed")
	if c.autoGatherShouldStop() {
		c.finalizeAutoGatherDryRun(domain.AutoGatherDryRunPhaseStopped, "", "")
		return
	}
	if c.state.Unraid == nil {
		c.finalizeAutoGatherDryRunFailed(domain.AutoGatherShow{}, "unable to read array disks")
		return
	}

	autoGatherDryRunLog("library scan started")
	c.notifyAutoGatherPhase("scan")
	stage1 := c.scanForAutoGatherDryRunStage1()
	if stage1.Cancelled || c.autoGatherShouldStop() {
		autoGatherDryRunLog("library scan cancelled")
		c.finalizeAutoGatherDryRun(domain.AutoGatherDryRunPhaseStopped, "", "")
		return
	}
	if stage1.Error != "" {
		c.finalizeAutoGatherDryRunFailed(domain.AutoGatherShow{}, "scan failed: "+stage1.Error)
		return
	}
	baseInventory := cloneAutoGatherDiscoverySnapshot(stage1)
	autoGatherDryRunLog("library scan completed; shows=%d retained for run", len(baseInventory.Shows))

	for {
		if c.autoGatherShouldStop() {
			c.finalizeAutoGatherDryRun(domain.AutoGatherDryRunPhaseStopped, "", "")
			return
		}

		autoGatherDryRunLog("iteration started")
		autoGatherDryRunLog("refresh started")
		c.notifyAutoGatherPhase("refresh")
		c.state.Unraid = c.refreshUnraidForAutoGatherDryRun()
		autoGatherDryRunLog("refresh completed")
		if c.autoGatherShouldStop() {
			c.finalizeAutoGatherDryRun(domain.AutoGatherDryRunPhaseStopped, "", "")
			return
		}
		if c.state.Unraid == nil {
			c.finalizeAutoGatherDryRunFailed(domain.AutoGatherShow{}, "unable to read array disks")
			return
		}

		c.notifyAutoGatherPhase("rescore")
		scan := c.rescoreAutoGatherDryRun(baseInventory, c.state.Unraid)
		if scan.Cancelled || c.autoGatherShouldStop() {
			c.finalizeAutoGatherDryRun(domain.AutoGatherDryRunPhaseStopped, "", "")
			return
		}

		c.notifyAutoGatherPhase("select")
		pick, remaining, cancelled := selectNextDryRunShow(scan, completed, skipped, c.autoGatherShouldStop)
		if cancelled || c.autoGatherShouldStop() {
			c.finalizeAutoGatherDryRun(domain.AutoGatherDryRunPhaseStopped, "", "")
			return
		}
		c.autoGatherMu.Lock()
		if c.autoGatherRun != nil {
			c.autoGatherRun.IterationsConsidered++
			c.autoGatherRun.SplitRemaining = remaining
			c.autoGatherRun.CurrentShow = ""
			c.autoGatherRun.CurrentShowName = ""
			c.autoGatherRun.CurrentTarget = ""
		}
		c.autoGatherMu.Unlock()

		autoGatherDryRunLog("Stage 2 rescore complete; split candidates=%d", remaining)

		if pick == nil {
			c.finalizeAutoGatherDryRun(domain.AutoGatherDryRunPhaseCompleted, "", "")
			return
		}

		if pick.skipOnly {
			c.recordAutoGatherSkipped(pick.show, pick.skipReason)
			skipped[pick.show.Path] = struct{}{}
			continue
		}

		if c.autoGatherShouldStop() {
			c.finalizeAutoGatherDryRun(domain.AutoGatherDryRunPhaseStopped, "", "")
			return
		}

		autoGatherDryRunLog(
			"selected %s; stage2Target=%s; estimatedMove=%d",
			pick.show.Path, pick.stage2Target, pick.stage2Move,
		)
		c.setAutoGatherCurrent(pick.show, pick.stage2Target)

		c.notifyAutoGatherPhase("canonical")
		autoGatherDryRunLog("canonical planning started for %s", pick.show.Path)
		canonical := c.canonicalPlanForShow(pick.show)
		if canonical.Cancelled || c.autoGatherShouldStop() {
			autoGatherDryRunLog("canonical planning cancelled: %s", pick.show.Path)
			c.finalizeAutoGatherDryRun(domain.AutoGatherDryRunPhaseStopped, "", "")
			return
		}
		pick.canonical = canonical

		targetDisk, targetPath, belowFloor, err := resolveAutoGatherExecutionTarget(
			canonical,
			pick.show.RecommendedTargetDisk,
		)
		skipReason, fatalErr := classifyCanonicalSelectionOutcome(pick.show, canonical, err)
		if fatalErr != "" {
			c.finalizeAutoGatherDryRunFailed(pick.show, fatalErr)
			return
		}
		if skipReason != "" {
			c.recordAutoGatherSkipped(pick.show, skipReason)
			skipped[pick.show.Path] = struct{}{}
			continue
		}
		if err != nil {
			// classifyCanonicalSelectionOutcome should have handled this; belt-and-braces.
			c.finalizeAutoGatherDryRunFailed(
				pick.show,
				fmt.Sprintf("canonical target resolution failed for %s: %s", pick.show.Path, err.Error()),
			)
			return
		}

		pick.targetDisk = targetDisk
		pick.targetPath = targetPath
		pick.canonicalBelow = belowFloor
		c.setAutoGatherCurrent(pick.show, targetDisk)
		autoGatherDryRunLog(
			"canonical planning completed for %s",
			pick.show.Path,
		)
		autoGatherDryRunLog(
			"canonical target=%s; canonicalMove=%d",
			targetDisk, canonical.CanonicalMoveBytes,
		)

		if c.autoGatherShouldStop() {
			c.finalizeAutoGatherDryRun(domain.AutoGatherDryRunPhaseStopped, "", "")
			return
		}

		c.notifyAutoGatherPhase("build-plan")
		plan, err := c.buildGatherPlanForShow(pick.show.Path)
		if err != nil {
			if isAutoGatherCancelledErr(err) || c.autoGatherShouldStop() {
				c.finalizeAutoGatherDryRun(domain.AutoGatherDryRunPhaseStopped, "", "")
				return
			}
			c.finalizeAutoGatherDryRunFailed(
				pick.show,
				fmt.Sprintf("gather plan build failed for %s: %s", pick.show.Path, err.Error()),
			)
			return
		}

		if c.autoGatherShouldStop() {
			c.finalizeAutoGatherDryRun(domain.AutoGatherDryRunPhaseStopped, "", "")
			return
		}

		if err := validateAutoGatherGatherTarget(plan, targetPath); err != nil {
			c.finalizeAutoGatherDryRunFailed(
				pick.show,
				fmt.Sprintf("canonical target validation failed for %s: %s", pick.show.Path, err.Error()),
			)
			return
		}

		if c.autoGatherShouldStop() {
			c.finalizeAutoGatherDryRun(domain.AutoGatherDryRunPhaseStopped, "", "")
			return
		}

		autoGatherDryRunLog("dry-run execution started: %s -> %s", pick.show.Path, targetDisk)
		c.notifyAutoGatherPhase("execute")
		result := c.executeAutoGatherDryRunGather(plan, targetPath)
		switch result.outcome {
		case gatherExecSuccess:
			autoGatherDryRunLog("dry-run execution completed: %s", pick.show.Path)
			c.recordAutoGatherCompleted(pick.show, pick.targetDisk, result.moveBytes, pick.canonicalBelow)
			completed[pick.show.Path] = struct{}{}
		case gatherExecCancelled:
			c.finalizeAutoGatherDryRun(domain.AutoGatherDryRunPhaseStopped, "", "")
			return
		case gatherExecUncertain:
			c.finalizeAutoGatherDryRunFailed(pick.show, result.reason)
			return
		}

		if c.state.Status != common.OpAutoGatherDryRun {
			c.state.Status = common.OpAutoGatherDryRun
		}
	}
}

func (c *Core) autoGatherShouldStop() bool {
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	return c.autoGatherStopRequested
}

func isAutoGatherCancelledErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "cancelled")
}

func (c *Core) finalizeAutoGatherDryRunFailed(show domain.AutoGatherShow, reason string) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	if c.autoGatherRun == nil {
		return
	}
	c.autoGatherRun.Phase = domain.AutoGatherDryRunPhaseFailed
	c.autoGatherRun.EndedAt = time.Now().UTC().Format(time.RFC3339)
	c.autoGatherRun.FailedShow = show.Path
	c.autoGatherRun.FailedShowName = show.Name
	c.autoGatherRun.FailureReason = reason
	c.autoGatherRun.CurrentShow = ""
	c.autoGatherRun.CurrentShowName = ""
	c.autoGatherRun.CurrentTarget = ""
}

func (c *Core) finalizeAutoGatherDryRun(phase, failedShow, reason string) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	if c.autoGatherRun == nil {
		return
	}
	c.autoGatherRun.Phase = phase
	c.autoGatherRun.EndedAt = time.Now().UTC().Format(time.RFC3339)
	c.autoGatherRun.CurrentShow = ""
	c.autoGatherRun.CurrentShowName = ""
	c.autoGatherRun.CurrentTarget = ""
	if phase == domain.AutoGatherDryRunPhaseFailed {
		c.autoGatherRun.FailedShow = failedShow
		c.autoGatherRun.FailureReason = reason
		for _, show := range c.autoGatherRun.Completed {
			if show.ShowPath == failedShow {
				c.autoGatherRun.FailedShowName = show.ShowName
				break
			}
		}
	}
}

func (c *Core) setAutoGatherCurrent(show domain.AutoGatherShow, targetDisk string) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	if c.autoGatherRun == nil {
		return
	}
	c.autoGatherRun.CurrentShow = show.Path
	c.autoGatherRun.CurrentShowName = show.Name
	c.autoGatherRun.CurrentTarget = targetDisk
}

func (c *Core) recordAutoGatherCompleted(show domain.AutoGatherShow, target string, moveBytes uint64, belowFloor bool) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	if c.autoGatherRun == nil {
		return
	}
	c.autoGatherRun.Completed = append(c.autoGatherRun.Completed, domain.AutoGatherDryRunShowRecord{
		ShowPath:                show.Path,
		ShowName:                show.Name,
		TargetDisk:              target,
		MoveBytes:               moveBytes,
		BelowPreferredFreeFloor: belowFloor,
		At:                      time.Now().UTC().Format(time.RFC3339),
	})
}

func (c *Core) recordAutoGatherSkipped(show domain.AutoGatherShow, reason string) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	if c.autoGatherRun == nil {
		return
	}
	c.autoGatherRun.Skipped = append(c.autoGatherRun.Skipped, domain.AutoGatherDryRunShowRecord{
		ShowPath: show.Path,
		ShowName: show.Name,
		Reason:   reason,
		At:       time.Now().UTC().Format(time.RFC3339),
	})
	autoGatherDryRunLog("show skipped: %s: %s", show.Path, reason)
}

func (c *Core) scanForAutoGatherDryRunStage1() domain.AutoGatherScanResult {
	if autoGatherDryRunScanHook != nil {
		return autoGatherDryRunScanHook(c)
	}
	scan, _ := c.scanAutoGatherStage1WithCancel(c.autoGatherShouldStop)
	return scan
}

func (c *Core) refreshUnraidForAutoGatherDryRun() *domain.Unraid {
	if autoGatherDryRunRefreshHook != nil {
		return autoGatherDryRunRefreshHook(c)
	}
	return c.refreshUnraid()
}

// rescoreAutoGatherDryRun recalculates Stage 2 recommendations from a retained
// Stage 1 discovery snapshot using the current Unraid disk state. It never
// rescans the TV library filesystem.
func (c *Core) rescoreAutoGatherDryRun(base domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult {
	if autoGatherDryRunRescoreHook != nil {
		return autoGatherDryRunRescoreHook(c, base, unraid)
	}
	scored := cloneAutoGatherDiscoverySnapshot(base)
	return c.addAutoGatherRecommendationsCancellable(scored, unraid, c.autoGatherShouldStop)
}

func (c *Core) buildGatherPlanForShow(showPath string) (*domain.Plan, error) {
	if autoGatherDryRunBuildPlanHook != nil {
		if c.autoGatherShouldStop() {
			return nil, fmt.Errorf("cancelled")
		}
		return autoGatherDryRunBuildPlanHook(c, showPath)
	}
	if c.state.Unraid == nil {
		return nil, fmt.Errorf("unable to read array disks")
	}
	if c.autoGatherShouldStop() {
		return nil, fmt.Errorf("cancelled")
	}

	prevStatus := c.state.Status
	c.state.Status = common.OpGatherPlan
	defer func() {
		if c.state.Status == common.OpGatherPlan {
			c.state.Status = prevStatus
		}
	}()

	plan := &domain.Plan{
		Started:       time.Now(),
		ChosenFolders: []string{showPath},
		VDisks:        make(map[string]*domain.VDisk),
	}
	for _, disk := range c.state.Unraid.Disks {
		if disk == nil {
			continue
		}
		plan.VDisks[disk.Path] = &domain.VDisk{
			Path:        disk.Path,
			CurrentFree: disk.Free,
			PlannedFree: disk.Free,
			Bin:         nil,
		}
	}
	_, cancelled := c.fillGatherPlanCancellable(plan, c.state.Unraid.Disks, c.state.Unraid.BlockSize, c.autoGatherShouldStop)
	if cancelled {
		return nil, fmt.Errorf("cancelled")
	}
	return plan, nil
}

func (c *Core) canonicalPlanForShow(show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
	if autoGatherDryRunCanonicalHook != nil {
		if c.autoGatherShouldStop() {
			return domain.AutoGatherCanonicalPlanResult{ShowPath: show.Path, Cancelled: true}
		}
		return autoGatherDryRunCanonicalHook(c, show)
	}
	prevStatus := c.state.Status
	c.state.Status = common.OpGatherPlan
	defer func() {
		if c.state.Status == common.OpGatherPlan {
			c.state.Status = prevStatus
		}
	}()
	return c.buildCanonicalPlanResultCancellable(
		show.Path,
		show.RecommendedTargetDisk,
		show.MoveRequiredBytes,
		c.autoGatherShouldStop,
	)
}

// selectNextDryRunShow chooses the next show using Stage 2 recommendation data
// only. Canonical Gather planning is deliberately NOT invoked here — it runs
// once for the selected show after this returns.
//
// Preference order:
//  1. remaining split shows with a Stage 2 recommended target that meets the
//     preferred projected-free floor, smallest MoveRequiredBytes first
//  2. otherwise remaining Stage 2 recommended targets below the floor, smallest
//     MoveRequiredBytes first
//  3. otherwise a Stage 2 infeasible/no-target show as skip-only
func selectNextDryRunShow(
	scan domain.AutoGatherScanResult,
	completed, skipped map[string]struct{},
	shouldStop func() bool,
) (*dryRunShowPick, int, bool) {
	remaining := 0
	executableAbove := make([]dryRunShowPick, 0)
	executableBelow := make([]dryRunShowPick, 0)
	var firstSkip *dryRunShowPick

	for _, show := range scan.Shows {
		if shouldStop != nil && shouldStop() {
			return nil, remaining, true
		}
		if show.Status != domain.AutoGatherStatusSplit {
			continue
		}
		if _, ok := completed[show.Path]; ok {
			continue
		}
		if _, ok := skipped[show.Path]; ok {
			continue
		}
		remaining++

		if show.RecommendedTargetDisk == "" {
			reason := show.NoEligibleReason
			if reason == "" {
				reason = "no Stage 2 recommended physical array target"
			}
			skip := dryRunShowPick{
				show:       show,
				skipOnly:   true,
				skipReason: reason,
			}
			if firstSkip == nil {
				firstSkip = &skip
			}
			continue
		}

		pick := dryRunShowPick{
			show:         show,
			stage2Target: show.RecommendedTargetDisk,
			stage2Move:   show.MoveRequiredBytes,
			belowFloor:   show.BelowPreferredFreeFloor,
		}
		if show.BelowPreferredFreeFloor {
			executableBelow = append(executableBelow, pick)
		} else {
			executableAbove = append(executableAbove, pick)
		}
	}

	sort.SliceStable(executableAbove, func(i, j int) bool {
		if executableAbove[i].stage2Move != executableAbove[j].stage2Move {
			return executableAbove[i].stage2Move < executableAbove[j].stage2Move
		}
		if executableAbove[i].show.Path != executableAbove[j].show.Path {
			return executableAbove[i].show.Path < executableAbove[j].show.Path
		}
		return executableAbove[i].show.Name < executableAbove[j].show.Name
	})
	sort.SliceStable(executableBelow, func(i, j int) bool {
		if executableBelow[i].stage2Move != executableBelow[j].stage2Move {
			return executableBelow[i].stage2Move < executableBelow[j].stage2Move
		}
		if executableBelow[i].show.Path != executableBelow[j].show.Path {
			return executableBelow[i].show.Path < executableBelow[j].show.Path
		}
		return executableBelow[i].show.Name < executableBelow[j].show.Name
	})

	if len(executableAbove) > 0 {
		return &executableAbove[0], remaining, false
	}
	if len(executableBelow) > 0 {
		return &executableBelow[0], remaining, false
	}
	if firstSkip != nil {
		return firstSkip, remaining, false
	}
	return nil, 0, false
}

func resolveAutoGatherExecutionTarget(
	canonical domain.AutoGatherCanonicalPlanResult,
	stage2Target string,
) (diskName, diskPath string, belowFloor bool, err error) {
	prefer := stage2Target
	if !canonical.Stage2TargetStillCanonicalEligible || prefer == "" {
		prefer = canonical.CanonicalRecommendedTarget
	}
	if prefer == "" {
		return "", "", false, fmt.Errorf("no eligible physical array target")
	}
	for _, t := range canonical.CanonicalTargets {
		if t.DiskName != prefer {
			continue
		}
		if !t.IsPhysicalArrayDisk {
			return "", "", false, fmt.Errorf("target %q is not a physical array disk", prefer)
		}
		if !t.CanonicalEligible {
			return "", "", false, fmt.Errorf("target %q is not canonically eligible", prefer)
		}
		return t.DiskName, t.DiskPath, !t.MeetsPreferredFreeFloor, nil
	}
	return "", "", false, fmt.Errorf("target %q is not canonically eligible", prefer)
}

func validateAutoGatherGatherTarget(plan *domain.Plan, targetPath string) error {
	if plan == nil {
		return fmt.Errorf("missing gather plan")
	}
	vdisk := plan.VDisks[targetPath]
	if vdisk == nil || vdisk.Bin == nil {
		return fmt.Errorf("canonical Gather target has no Bin")
	}
	name := diskNameFromMountPath(targetPath)
	if !autogather.IsArrayDiskName(name) {
		return fmt.Errorf("target %q is not a physical array disk", name)
	}
	return nil
}

func diskNameFromMountPath(mountPath string) string {
	parts := strings.Split(strings.Trim(mountPath, "/"), "/")
	if len(parts) == 0 {
		return mountPath
	}
	return parts[len(parts)-1]
}

func (c *Core) executeAutoGatherDryRunGather(plan *domain.Plan, targetPath string) gatherExecResult {
	if autoGatherDryRunExecuteHook != nil {
		if c.autoGatherShouldStop() {
			return gatherExecResult{outcome: gatherExecCancelled, reason: "stop requested before execution"}
		}
		return autoGatherDryRunExecuteHook(c, plan, targetPath)
	}
	return c.executeGatherPlanDryRunSync(plan, targetPath)
}

func (c *Core) executeGatherPlanDryRunSync(plan *domain.Plan, targetPath string) gatherExecResult {
	if c.autoGatherShouldStop() {
		return gatherExecResult{outcome: gatherExecCancelled, reason: "stop requested before execution"}
	}
	if err := validateAutoGatherGatherTarget(plan, targetPath); err != nil {
		return gatherExecResult{outcome: gatherExecUncertain, reason: err.Error()}
	}
	if !c.ctx.DryRun {
		return gatherExecResult{
			outcome: gatherExecUncertain,
			reason:  "Auto Gather dry-run refused: global dry-run mode is disabled",
		}
	}

	plan.Target = targetPath
	moveBytes := plan.VDisks[targetPath].Bin.Size

	prevStatus := c.state.Status
	c.state.Status = common.OpGatherMove
	c.autoGatherDryRunExec = true
	// Only clear the rsync stop latch when Stage 3B stop is not already pending.
	if !c.autoGatherShouldStop() {
		c.stopped = false
	}
	defer func() {
		c.autoGatherDryRunExec = false
		if c.isAutoGatherDryRunActive() {
			c.state.Status = common.OpAutoGatherDryRun
		} else if c.state.Status == common.OpGatherMove {
			c.state.Status = prevStatus
		}
	}()

	if c.autoGatherShouldStop() {
		return gatherExecResult{outcome: gatherExecCancelled, reason: "stop requested before execution"}
	}

	c.state.Operation = c.createGatherOperation(*plan)
	op := c.state.Operation
	if op == nil {
		return gatherExecResult{outcome: gatherExecUncertain, reason: "gather operation was not created"}
	}
	if !op.DryRun {
		return gatherExecResult{outcome: gatherExecUncertain, reason: "gather operation is not dry-run"}
	}
	if !rsyncArgsIncludeDryRun(op.RsyncArgs) {
		return gatherExecResult{outcome: gatherExecUncertain, reason: "gather operation missing --dry-run rsync flag"}
	}
	if len(op.Commands) == 0 {
		return gatherExecResult{outcome: gatherExecUncertain, reason: "gather operation has no commands"}
	}

	c.runOperation("Move")
	return classifyGatherDryRunResult(op, moveBytes, c.autoGatherShouldStop())
}

func rsyncArgsIncludeDryRun(args []string) bool {
	for _, arg := range args {
		if arg == "--dry-run" {
			return true
		}
	}
	return false
}

func classifyGatherDryRunResult(op *domain.Operation, moveBytes uint64, stopRequested bool) gatherExecResult {
	if op == nil {
		return gatherExecResult{outcome: gatherExecUncertain, reason: "missing operation result"}
	}
	for _, cmd := range op.Commands {
		if cmd == nil {
			continue
		}
		switch cmd.Status {
		case common.CmdFlagged:
			return gatherExecResult{
				outcome: gatherExecUncertain,
				reason:  fmt.Sprintf("rsync command flagged: %s", cmd.Reason),
			}
		case common.CmdStopped:
			if stopRequested {
				return gatherExecResult{
					outcome: gatherExecCancelled,
					reason:  "stop requested during dry-run rsync",
				}
			}
			return gatherExecResult{
				outcome: gatherExecUncertain,
				reason:  fmt.Sprintf("rsync command stopped: %s", cmd.Reason),
			}
		}
	}
	if stopRequested {
		return gatherExecResult{outcome: gatherExecCancelled, reason: "stop requested"}
	}
	if !op.DryRun {
		return gatherExecResult{outcome: gatherExecUncertain, reason: "operation was not dry-run"}
	}
	return gatherExecResult{outcome: gatherExecSuccess, moveBytes: moveBytes}
}
