package core

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"unbalance/daemon/autogather"
	"unbalance/daemon/common"
	"unbalance/daemon/domain"
	"unbalance/daemon/logger"
)

const (
	autoGatherRealPrepareTTL   = 5 * time.Minute
	autoGatherRealMessage      = "Stage 3C performs at most one explicitly confirmed real Gather move."
	autoGatherRealExpiredMsg   = "preparation expired; prepare again"
	autoGatherRealCancelledMsg = "preparation cancelled; prepare again to execute a real move"
	autoGatherRealStoppedMsg   = "The move was stopped. The interrupted transfer's source was not deleted. " +
		"Files transferred successfully before the stop may already have had their original source copies " +
		"removed by normal Gather behaviour. The destination may also contain a partial copy of the interrupted transfer."
)

// autoGatherRealExecuteHook allows tests to substitute real execution.
var autoGatherRealExecuteHook func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult

// autoGatherRealCanonicalHook allows tests to substitute canonical bundle building.
var autoGatherRealCanonicalHook func(c *Core, showPath, stage2Target string, stage2Move uint64) (autoGatherCanonicalBundle, bool)

// autoGatherRealFillHook counts Gather fills during EXECUTE for tests.
var autoGatherRealFillHook func()

// autoGatherRealVerifyHook allows tests to substitute post-move verification.
var autoGatherRealVerifyHook func(c *Core, showPath, targetDisk string) domain.AutoGatherRealVerification

func autoGatherRealLog(format string, args ...any) {
	logger.Blue("autoGatherReal: "+format, args...)
}

func (c *Core) PrepareAutoGatherReal(req domain.AutoGatherRealPrepareRequest) domain.AutoGatherRealPrepareResult {
	result := domain.AutoGatherRealPrepareResult{
		GlobalDryRun: c.ctx.DryRun,
	}

	showPath, err := validateAutoGatherCanonicalSelection([]string{strings.TrimSpace(req.ShowPath)})
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if err := c.validateAutoGatherShowUnderLibrary(showPath); err != nil {
		result.Error = err.Error()
		return result
	}

	c.autoGatherMu.Lock()
	if c.isAutoGatherRealExecutionBusyLocked() {
		c.autoGatherMu.Unlock()
		result.Error = "Auto Gather real move is already executing"
		return result
	}
	if c.isAutoGatherDryRunActiveLocked() {
		c.autoGatherMu.Unlock()
		result.Error = "Auto Gather dry-run is active; stop it before preparing a real move"
		return result
	}
	if c.state == nil {
		c.autoGatherMu.Unlock()
		result.Error = "internal state is not available"
		return result
	}
	if c.state.Status != common.OpNeutral && c.state.Status != common.OpAutoGatherReal {
		c.autoGatherMu.Unlock()
		result.Error = fmt.Sprintf("unbalanced is busy: status %d", c.state.Status)
		return result
	}

	c.ensureAutoGatherRealStateLocked()
	c.autoGatherRealState.Phase = domain.AutoGatherRealPhasePreparing
	c.autoGatherRealState.OperationPhase = "planning"
	c.autoGatherRealState.GlobalDryRun = c.ctx.DryRun
	c.autoGatherMu.Unlock()

	c.state.Unraid = c.refreshUnraid()

	bundle, cancelled := c.buildAutoGatherRealCanonicalBundle(showPath, req.Stage2RecommendedTarget, req.Stage2EstimatedMoveBytes)
	if cancelled {
		c.failAutoGatherRealPrepare("preparation cancelled")
		result.Error = "preparation cancelled"
		return result
	}
	canonical := bundle.Result
	if canonical.Error != "" {
		c.failAutoGatherRealPrepare(canonical.Error)
		result.Error = canonical.Error
		return result
	}
	if canonical.NoEligibleReason != "" && canonical.CanonicalRecommendedTarget == "" {
		c.failAutoGatherRealPrepare(canonical.NoEligibleReason)
		result.Error = canonical.NoEligibleReason
		return result
	}

	targetDisk, targetPath, _, resolveErr := resolveAutoGatherExecutionTarget(canonical, req.Stage2RecommendedTarget)
	if resolveErr != nil {
		c.failAutoGatherRealPrepare(resolveErr.Error())
		result.Error = resolveErr.Error()
		return result
	}
	if err := validateAutoGatherGatherTarget(bundle.Plan, targetPath); err != nil {
		c.failAutoGatherRealPrepare(err.Error())
		result.Error = err.Error()
		return result
	}

	sourceDisks := sourceDisksFromGatherPlan(bundle.Plan, targetPath)
	emptyFolders := c.emptyFolderOnlyDisksForShow(showPath)
	issues := gatherPlanIssues(bundle.Plan)
	currentOnTarget := currentBytesOnTarget(canonical, targetDisk)
	targetMeta := lookupCanonicalTargetMeta(canonical, targetDisk)
	fingerprint, fpErr := buildAutoGatherRealPlanFingerprint(showPath, targetDisk, targetPath, bundle.Plan)
	if fpErr != nil {
		c.failAutoGatherRealPrepare(fpErr.Error())
		result.Error = fpErr.Error()
		return result
	}

	stage2Agrees := req.Stage2RecommendedTarget != "" &&
		req.Stage2RecommendedTarget == targetDisk

	now := time.Now().UTC()
	expires := now.Add(autoGatherRealPrepareTTL)
	prepID := c.generateAutoGatherRealPrepID()

	result = domain.AutoGatherRealPrepareResult{
		PreparationID:             prepID,
		ShowPath:                  showPath,
		ShowName:                  filepath.Base(showPath),
		SourceDisks:               sourceDisks,
		CanonicalTargetDisk:       targetDisk,
		Stage2RecommendedTarget:   req.Stage2RecommendedTarget,
		Stage2AgreesWithCanonical: stage2Agrees,
		CurrentBytesOnTarget:      currentOnTarget,
		EstimatedMoveBytes:        bundle.Plan.VDisks[targetPath].Bin.Size,
		TargetFreeBytes:           targetMeta.FreeBytes,
		ProjectedTargetFreeBytes:  targetMeta.ProjectedFreeBytes,
		Executable:                len(issues) == 0 && bundle.Plan.VDisks[targetPath].Bin != nil && len(bundle.Plan.VDisks[targetPath].Bin.Items) > 0,
		Issues:                    issues,
		EmptyFolderOnlyDisks:      emptyFolders,
		ExpiresAt:                 expires.Format(time.RFC3339),
		GlobalDryRun:              c.ctx.DryRun,
		PlanFingerprint:           &fingerprint,
	}

	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()

	c.autoGatherRealPrepared = &domain.AutoGatherRealPreparedMove{
		PreparationID:      prepID,
		ShowPath:           showPath,
		ShowName:           result.ShowName,
		PreparedTargetDisk: targetDisk,
		PreparedTargetPath: targetPath,
		EstimatedMoveBytes: result.EstimatedMoveBytes,
		SourceDisks:        append([]string(nil), sourceDisks...),
		Stage2Target:       req.Stage2RecommendedTarget,
		PreparedAt:         now.Format(time.RFC3339),
		ExpiresAt:          expires.Format(time.RFC3339),
		PlanFingerprint:    fingerprint,
		PrepareSnapshot:    result,
	}
	c.ensureAutoGatherRealStateLocked()
	c.autoGatherRealState.Phase = domain.AutoGatherRealPhasePrepared
	c.autoGatherRealState.OperationPhase = ""
	c.autoGatherRealState.PreparationID = prepID
	c.autoGatherRealState.Prepared = &result
	c.autoGatherRealState.CurrentShow = showPath
	c.autoGatherRealState.CurrentShowName = result.ShowName
	c.autoGatherRealState.CurrentTarget = targetDisk
	c.autoGatherRealState.GlobalDryRun = c.ctx.DryRun
	c.autoGatherRealState.Message = autoGatherRealMessage
	c.autoGatherRealState.Error = ""

	autoGatherRealLog("prepared show=%s target=%s id=%s", showPath, targetDisk, prepID)
	return result
}

func (c *Core) ExecuteAutoGatherReal(req domain.AutoGatherRealExecuteRequest) (domain.AutoGatherRealState, error) {
	c.autoGatherMu.Lock()
	state := c.snapshotAutoGatherRealLocked()
	c.autoGatherMu.Unlock()

	if !req.Confirm {
		state.Error = "explicit confirmation is required for a real Auto Gather move"
		return state, fmt.Errorf("%s", state.Error)
	}
	if c.ctx.DryRun {
		state.Error = "Auto Gather real execution requires global dry-run mode to be disabled"
		return state, fmt.Errorf("%s", state.Error)
	}
	if strings.TrimSpace(req.PreparationID) == "" {
		state.Error = "missing preparation token"
		return state, fmt.Errorf("%s", state.Error)
	}

	c.autoGatherMu.Lock()
	prepared := c.autoGatherRealPrepared
	if prepared == nil || prepared.PreparationID != req.PreparationID {
		c.expireAutoGatherRealPreparedIfNeededLocked()
		state = c.snapshotAutoGatherRealLocked()
		c.autoGatherMu.Unlock()
		state.Error = "invalid or missing preparation token"
		return state, fmt.Errorf("%s", state.Error)
	}
	if autoGatherRealPreparationExpired(prepared) {
		c.markAutoGatherRealPreparedExpiredLocked()
		state = c.snapshotAutoGatherRealLocked()
		c.autoGatherMu.Unlock()
		return state, fmt.Errorf("%s", state.Error)
	}
	showPath, err := validateAutoGatherCanonicalSelection([]string{strings.TrimSpace(req.ShowPath)})
	if err != nil {
		c.autoGatherMu.Unlock()
		state.Error = err.Error()
		return state, err
	}
	if showPath != prepared.ShowPath {
		c.autoGatherMu.Unlock()
		state.Error = "selected show does not match prepared show"
		return state, fmt.Errorf("%s", state.Error)
	}
	if c.isAutoGatherDryRunActiveLocked() {
		c.autoGatherMu.Unlock()
		state.Error = "Auto Gather dry-run is active"
		return state, fmt.Errorf("%s", state.Error)
	}
	if c.isAutoGatherRealExecutionBusyLocked() {
		c.autoGatherMu.Unlock()
		state.Error = "Auto Gather real move is already executing"
		return state, fmt.Errorf("%s", state.Error)
	}
	if c.state == nil || c.state.Status != common.OpNeutral {
		c.autoGatherMu.Unlock()
		state.Error = fmt.Sprintf("unbalanced is busy: status %d", c.state.Status)
		return state, fmt.Errorf("%s", state.Error)
	}
	if c.autoGatherRealStopRequested {
		c.autoGatherMu.Unlock()
		state.Error = "stop already requested"
		return state, fmt.Errorf("%s", state.Error)
	}

	c.ensureAutoGatherRealStateLocked()
	c.autoGatherRealState.Phase = domain.AutoGatherRealPhaseExecuting
	c.autoGatherRealState.OperationPhase = "revalidation"
	c.autoGatherRealState.StartedAt = time.Now().UTC().Format(time.RFC3339)
	c.autoGatherRealState.CurrentShow = prepared.ShowPath
	c.autoGatherRealState.CurrentShowName = prepared.ShowName
	c.autoGatherRealState.CurrentTarget = prepared.PreparedTargetDisk
	c.autoGatherRealState.PreparationID = prepared.PreparationID
	c.autoGatherRealState.Prepared = &prepared.PrepareSnapshot
	c.autoGatherRealState.GlobalDryRun = false
	c.autoGatherRealState.Error = ""
	c.autoGatherRealState.Message = autoGatherRealMessage
	c.autoGatherRealStopRequested = false
	c.state.Status = common.OpAutoGatherReal
	snap := c.snapshotAutoGatherRealLocked()
	c.autoGatherMu.Unlock()

	autoGatherRealLog("execute confirmed show=%s target=%s id=%s", prepared.ShowPath, prepared.PreparedTargetDisk, prepared.PreparationID)
	go c.autoGatherRealExecuteAsync(prepared)
	return snap, nil
}

func (c *Core) StopAutoGatherReal() domain.AutoGatherRealState {
	c.requestAutoGatherRealStop()
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	return c.snapshotAutoGatherRealLocked()
}

func (c *Core) CancelAutoGatherRealPrepare() domain.AutoGatherRealState {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	c.expireAutoGatherRealPreparedIfNeededLocked()
	c.ensureAutoGatherRealStateLocked()
	switch c.autoGatherRealState.Phase {
	case domain.AutoGatherRealPhasePreparing,
		domain.AutoGatherRealPhaseExecuting,
		domain.AutoGatherRealPhaseStopping:
		snap := c.snapshotAutoGatherRealLocked()
		snap.Error = "cannot cancel: real move is in progress; use Stop"
		return snap
	}
	if c.autoGatherRealPrepared != nil ||
		c.autoGatherRealState.Phase == domain.AutoGatherRealPhasePrepared ||
		c.autoGatherRealState.Phase == domain.AutoGatherRealPhaseExpired {
		c.markAutoGatherRealPreparedCancelledLocked()
	}
	return c.snapshotAutoGatherRealLocked()
}

func (c *Core) GetAutoGatherRealState() domain.AutoGatherRealState {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	c.expireAutoGatherRealPreparedIfNeededLocked()
	return c.snapshotAutoGatherRealLocked()
}

func (c *Core) requestAutoGatherRealStop() {
	c.autoGatherMu.Lock()
	accepted := false
	if c.autoGatherRealState != nil {
		switch c.autoGatherRealState.Phase {
		case domain.AutoGatherRealPhaseExecuting, domain.AutoGatherRealPhasePreparing:
			c.autoGatherRealStopRequested = true
			c.autoGatherRealState.Phase = domain.AutoGatherRealPhaseStopping
			accepted = true
		}
	}
	c.autoGatherMu.Unlock()
	c.stopped = true
	if accepted {
		autoGatherRealLog("stop requested")
	}
}

func (c *Core) autoGatherRealShouldStop() bool {
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	return c.autoGatherRealStopRequested
}

func (c *Core) autoGatherRealExecuteAsync(prepared *domain.AutoGatherRealPreparedMove) {
	defer func() {
		c.autoGatherMu.Lock()
		if c.state != nil && c.state.Status == common.OpAutoGatherReal {
			c.state.Status = common.OpNeutral
		}
		c.autoGatherRealExec = false
		c.clearAutoGatherRealPreparedLocked()
		c.autoGatherMu.Unlock()
	}()

	if c.autoGatherRealShouldStop() {
		c.finalizeAutoGatherRealStopped("stop requested before execution")
		return
	}

	c.setAutoGatherRealOperationPhase("revalidation")
	c.state.Unraid = c.refreshUnraid()

	bundle, cancelled := c.buildAutoGatherRealCanonicalBundle(prepared.ShowPath, prepared.Stage2Target, prepared.EstimatedMoveBytes)
	if cancelled || c.autoGatherRealShouldStop() {
		c.finalizeAutoGatherRealStopped("stop requested during revalidation")
		return
	}
	if bundle.Result.Error != "" {
		c.finalizeAutoGatherRealFailed(bundle.Result.Error)
		return
	}

	targetDisk, targetPath, _, err := resolveAutoGatherExecutionTarget(bundle.Result, prepared.Stage2Target)
	if err != nil {
		c.finalizeAutoGatherRealFailed(err.Error())
		return
	}
	if targetDisk != prepared.PreparedTargetDisk || targetPath != prepared.PreparedTargetPath {
		c.finalizeAutoGatherRealPlanChanged(
			fmt.Sprintf("canonical target changed from %s to %s", prepared.PreparedTargetDisk, targetDisk),
		)
		return
	}
	freshFingerprint, fpErr := buildAutoGatherRealPlanFingerprint(prepared.ShowPath, targetDisk, targetPath, bundle.Plan)
	if fpErr != nil {
		c.finalizeAutoGatherRealFailed(fpErr.Error())
		return
	}
	if diff := diffAutoGatherRealPlanFingerprints(prepared.PlanFingerprint, freshFingerprint); diff != "" {
		c.finalizeAutoGatherRealPlanChanged(diff)
		return
	}
	if err := validateAutoGatherGatherTarget(bundle.Plan, targetPath); err != nil {
		c.finalizeAutoGatherRealFailed(err.Error())
		return
	}
	if issues := gatherPlanIssues(bundle.Plan); len(issues) > 0 {
		c.finalizeAutoGatherRealFailed(strings.Join(issues, "; "))
		return
	}
	if err := c.validateAutoGatherRealExecutionGates(prepared, bundle.Plan, targetPath); err != nil {
		c.finalizeAutoGatherRealFailed(err.Error())
		return
	}

	c.setAutoGatherRealOperationPhase("rsync")
	execResult := c.executeAutoGatherRealGather(bundle.Plan, targetPath)
	if c.autoGatherRealShouldStop() || execResult.outcome == gatherExecCancelled {
		c.finalizeAutoGatherRealStopped(autoGatherRealStoppedMsg)
		return
	}
	if execResult.outcome != gatherExecSuccess {
		c.finalizeAutoGatherRealFailed(execResult.reason)
		return
	}

	c.setAutoGatherRealOperationPhase("verification")
	c.state.Unraid = c.refreshUnraid()
	verification := c.verifyAutoGatherRealMove(prepared.ShowPath, targetDisk)
	c.finalizeAutoGatherRealCompleted(verification)
}

func (c *Core) validateAutoGatherRealExecutionGates(prepared *domain.AutoGatherRealPreparedMove, plan *domain.Plan, targetPath string) error {
	if c.ctx.DryRun {
		return fmt.Errorf("global dry-run mode is enabled")
	}
	if c.autoGatherRealShouldStop() {
		return fmt.Errorf("stop requested")
	}
	if c.isAutoGatherDryRunActive() {
		return fmt.Errorf("Auto Gather dry-run is active")
	}
	if err := c.validateAutoGatherShowUnderLibrary(prepared.ShowPath); err != nil {
		return err
	}
	vdisk := plan.VDisks[targetPath]
	if vdisk == nil || vdisk.Bin == nil || len(vdisk.Bin.Items) == 0 {
		return fmt.Errorf("canonical Gather target has no executable items")
	}
	name := diskNameFromMountPath(targetPath)
	if !autogather.IsArrayDiskName(name) {
		return fmt.Errorf("target %q is not a physical array disk", name)
	}
	return nil
}

func (c *Core) executeAutoGatherRealGather(plan *domain.Plan, targetPath string) gatherExecResult {
	if autoGatherRealExecuteHook != nil {
		if c.autoGatherRealShouldStop() {
			return gatherExecResult{outcome: gatherExecCancelled, reason: "stop requested before execution"}
		}
		return autoGatherRealExecuteHook(c, plan, targetPath)
	}
	return c.executeGatherPlanRealSync(plan, targetPath)
}

func (c *Core) executeGatherPlanRealSync(plan *domain.Plan, targetPath string) gatherExecResult {
	if c.autoGatherRealShouldStop() {
		return gatherExecResult{outcome: gatherExecCancelled, reason: "stop requested before execution"}
	}
	if c.ctx.DryRun {
		return gatherExecResult{outcome: gatherExecUncertain, reason: "Auto Gather real execution refused: global dry-run mode is enabled"}
	}
	if err := validateAutoGatherGatherTarget(plan, targetPath); err != nil {
		return gatherExecResult{outcome: gatherExecUncertain, reason: err.Error()}
	}

	plan.Target = targetPath
	moveBytes := plan.VDisks[targetPath].Bin.Size

	prevStatus := c.state.Status
	c.state.Status = common.OpGatherMove
	c.autoGatherRealExec = true
	if !c.autoGatherRealShouldStop() {
		c.stopped = false
	}
	defer func() {
		c.autoGatherRealExec = false
		if c.isAutoGatherRealExecutionBusy() {
			c.state.Status = common.OpAutoGatherReal
		} else if c.state.Status == common.OpGatherMove {
			c.state.Status = prevStatus
		}
	}()

	if c.autoGatherRealShouldStop() {
		return gatherExecResult{outcome: gatherExecCancelled, reason: "stop requested before execution"}
	}

	c.state.Operation = c.createGatherOperation(*plan)
	op := c.state.Operation
	if op == nil {
		return gatherExecResult{outcome: gatherExecUncertain, reason: "gather operation was not created"}
	}
	if op.DryRun {
		return gatherExecResult{outcome: gatherExecUncertain, reason: "gather operation must not be dry-run"}
	}
	if rsyncArgsIncludeDryRun(op.RsyncArgs) {
		return gatherExecResult{outcome: gatherExecUncertain, reason: "gather operation must not include --dry-run"}
	}
	if len(op.Commands) == 0 {
		return gatherExecResult{outcome: gatherExecUncertain, reason: "gather operation has no commands"}
	}

	c.runOperation("Move")
	return classifyGatherRealResult(op, moveBytes, c.autoGatherRealShouldStop())
}

func classifyGatherRealResult(op *domain.Operation, moveBytes uint64, stopRequested bool) gatherExecResult {
	if op == nil {
		return gatherExecResult{outcome: gatherExecUncertain, reason: "missing operation result"}
	}
	for _, cmd := range op.Commands {
		if cmd == nil {
			continue
		}
		switch cmd.Status {
		case common.CmdFlagged:
			return gatherExecResult{outcome: gatherExecUncertain, reason: cmd.Reason}
		case common.CmdStopped:
			return gatherExecResult{outcome: gatherExecCancelled, reason: "stopped by the user"}
		}
	}
	if stopRequested {
		return gatherExecResult{outcome: gatherExecCancelled, reason: "stop requested during execution"}
	}
	if op.BytesTransferred >= moveBytes || op.Completed >= 99.9 {
		return gatherExecResult{outcome: gatherExecSuccess}
	}
	return gatherExecResult{outcome: gatherExecUncertain, reason: "gather operation did not complete successfully"}
}

func (c *Core) verifyAutoGatherRealMove(showPath, targetDisk string) domain.AutoGatherRealVerification {
	if autoGatherRealVerifyHook != nil {
		return autoGatherRealVerifyHook(c, showPath, targetDisk)
	}
	if c.state.Unraid == nil {
		return domain.AutoGatherRealVerification{
			Passed:  false,
			Message: "unable to verify placement: missing disk state",
		}
	}
	arrayDisks, cacheDisks := autogather.PartitionDisks(c.state.Unraid.Disks)
	show := autogather.ScanSingleShow(showPath, arrayDisks, cacheDisks)

	substantive := make([]string, 0)
	for _, disk := range show.VideoDisks {
		if disk.VideoBytes > 0 && disk.DiskName != targetDisk {
			substantive = append(substantive, disk.DiskName)
		}
	}
	for _, disk := range show.SidecarOnlyDisks {
		if disk.TotalBytes > 0 && disk.DiskName != targetDisk {
			substantive = append(substantive, disk.DiskName)
		}
	}
	sort.Strings(substantive)
	substantive = uniqueStrings(substantive)

	emptyRemnants := append([]string(nil), show.CleanupCandidateDisks...)
	sort.Strings(emptyRemnants)

	targetHasData := false
	for _, disk := range show.VideoDisks {
		if disk.DiskName == targetDisk && disk.VideoBytes > 0 {
			targetHasData = true
			break
		}
	}

	passed := targetHasData && len(substantive) == 0
	message := "Show data consolidated on target disk"
	if !passed {
		if !targetHasData {
			message = "verification failed: target disk has no substantive show video data"
		} else if len(substantive) > 0 {
			message = fmt.Sprintf("verification warning: substantive show data remains on %s", strings.Join(substantive, ", "))
		}
	}

	return domain.AutoGatherRealVerification{
		Passed:              passed,
		TargetDisk:          targetDisk,
		SubstantiveDisks:    substantive,
		EmptyFolderRemnants: emptyRemnants,
		Message:             message,
	}
}

func (c *Core) buildAutoGatherRealCanonicalBundle(showPath, stage2Target string, stage2Move uint64) (autoGatherCanonicalBundle, bool) {
	if autoGatherRealCanonicalHook != nil {
		if autoGatherRealFillHook != nil {
			autoGatherRealFillHook()
		}
		return autoGatherRealCanonicalHook(c, showPath, stage2Target, stage2Move)
	}
	prevStatus := c.state.Status
	c.state.Status = common.OpGatherPlan
	defer func() {
		if c.state.Status == common.OpGatherPlan {
			c.state.Status = prevStatus
		}
	}()
	if autoGatherRealFillHook != nil {
		autoGatherRealFillHook()
	}
	return c.buildAutoGatherCanonicalBundleCancellable(showPath, stage2Target, stage2Move, c.autoGatherRealShouldStop)
}

func (c *Core) validateAutoGatherShowUnderLibrary(showPath string) error {
	libPath := c.ctx.Config.TvLibraryPath
	if libPath == "" {
		libPath = autogather.DefaultTvLibraryPath
	}
	cleanLib, err := autogather.NormalizeTvLibraryPath(libPath)
	if err != nil {
		return fmt.Errorf("invalid configured library path")
	}
	cleanShow, err := autogather.NormalizeTvLibraryPath(showPath)
	if err != nil {
		return fmt.Errorf("invalid show path")
	}
	if cleanShow != cleanLib && !strings.HasPrefix(cleanShow, cleanLib+"/") {
		return fmt.Errorf("show path is not under the configured Auto Gather library path")
	}
	return nil
}

func sourceDisksFromGatherPlan(plan *domain.Plan, targetPath string) []string {
	if plan == nil {
		return nil
	}
	set := map[string]struct{}{}
	for _, vdisk := range plan.VDisks {
		if vdisk == nil || vdisk.Path == targetPath || vdisk.Bin == nil {
			continue
		}
		for _, item := range vdisk.Bin.Items {
			if item == nil {
				continue
			}
			name := diskNameFromMountPath(item.Location)
			if autogather.IsArrayDiskName(name) {
				set[name] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// buildAutoGatherRealPlanFingerprint derives a deterministic identity for the
// executable Gather operation the user reviewed at PREPARE.
//
// Fields and rationale:
//   - ShowPath: normalized library-relative show path (exactly one show).
//   - TargetDisk / TargetPath: canonical physical array destination.
//   - MoveBytes: target Bin.Size (total bytes Gather plans to move).
//   - SourceDisks: sorted unique source array disk names for items whose
//     Location is not the target mount (disks contributing off-target data).
//   - Items: sorted list of each planned rsync transfer (SourceDisk, Entry,
//     Size) using the same entry normalization as createGatherOperation.
//     This captures per-file/folder composition; target+bytes alone cannot
//     detect swaps when totals coincide.
func buildAutoGatherRealPlanFingerprint(showPath, targetDisk, targetPath string, plan *domain.Plan) (domain.AutoGatherRealPlanFingerprint, error) {
	cleanShow, err := autogather.NormalizeTvLibraryPath(showPath)
	if err != nil {
		return domain.AutoGatherRealPlanFingerprint{}, fmt.Errorf("invalid show path for fingerprint")
	}
	vdisk := plan.VDisks[targetPath]
	if vdisk == nil || vdisk.Bin == nil {
		return domain.AutoGatherRealPlanFingerprint{}, fmt.Errorf("missing target Bin for fingerprint")
	}

	items := gatherTransferItemsFromTargetBin(vdisk.Bin, targetPath)
	sourceSet := map[string]struct{}{}
	for _, it := range items {
		if it.SourceDisk != targetDisk && autogather.IsArrayDiskName(it.SourceDisk) {
			sourceSet[it.SourceDisk] = struct{}{}
		}
	}
	sourceDisks := make([]string, 0, len(sourceSet))
	for name := range sourceSet {
		sourceDisks = append(sourceDisks, name)
	}
	sort.Strings(sourceDisks)

	return domain.AutoGatherRealPlanFingerprint{
		ShowPath:    cleanShow,
		TargetDisk:  targetDisk,
		TargetPath:  targetPath,
		MoveBytes:   vdisk.Bin.Size,
		SourceDisks: sourceDisks,
		Items:       items,
	}, nil
}

func gatherTransferItemsFromTargetBin(bin *domain.Bin, targetPath string) []domain.AutoGatherRealPlanTransferItem {
	if bin == nil {
		return nil
	}
	items := make([]domain.AutoGatherRealPlanTransferItem, 0, len(bin.Items))
	for _, item := range bin.Items {
		if item == nil {
			continue
		}
		items = append(items, domain.AutoGatherRealPlanTransferItem{
			SourceDisk: diskNameFromMountPath(item.Location),
			Entry:      normalizeGatherTransferEntry(item.Path),
			Size:       item.Size,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.SourceDisk != b.SourceDisk {
			return a.SourceDisk < b.SourceDisk
		}
		if a.Entry != b.Entry {
			return a.Entry < b.Entry
		}
		return a.Size < b.Size
	})
	return items
}

func normalizeGatherTransferEntry(path string) string {
	entry := filepath.ToSlash(strings.TrimSpace(path))
	entry = strings.TrimPrefix(entry, "/")
	return entry
}

func diffAutoGatherRealPlanFingerprints(prepared, fresh domain.AutoGatherRealPlanFingerprint) string {
	if prepared.ShowPath != fresh.ShowPath {
		return fmt.Sprintf("show path changed from %q to %q", prepared.ShowPath, fresh.ShowPath)
	}
	if prepared.TargetDisk != fresh.TargetDisk || prepared.TargetPath != fresh.TargetPath {
		return fmt.Sprintf("target changed from %s (%s) to %s (%s)",
			prepared.TargetDisk, prepared.TargetPath, fresh.TargetDisk, fresh.TargetPath)
	}
	if prepared.MoveBytes != fresh.MoveBytes {
		return fmt.Sprintf("move bytes changed from %d to %d", prepared.MoveBytes, fresh.MoveBytes)
	}
	if !stringSlicesEqual(prepared.SourceDisks, fresh.SourceDisks) {
		return fmt.Sprintf("source disks changed from [%s] to [%s]",
			strings.Join(prepared.SourceDisks, ", "), strings.Join(fresh.SourceDisks, ", "))
	}
	if !transferItemsEqual(prepared.Items, fresh.Items) {
		return "planned transfer item composition changed"
	}
	return ""
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func transferItemsEqual(a, b []domain.AutoGatherRealPlanTransferItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func gatherPlanIssues(plan *domain.Plan) []string {
	if plan == nil {
		return []string{"missing gather plan"}
	}
	issues := make([]string, 0)
	if plan.OwnerIssue > 0 {
		issues = append(issues, fmt.Sprintf("%d ownership issue(s)", plan.OwnerIssue))
	}
	if plan.GroupIssue > 0 {
		issues = append(issues, fmt.Sprintf("%d group issue(s)", plan.GroupIssue))
	}
	if plan.FolderIssue > 0 {
		issues = append(issues, fmt.Sprintf("%d folder issue(s)", plan.FolderIssue))
	}
	if plan.FileIssue > 0 {
		issues = append(issues, fmt.Sprintf("%d file issue(s)", plan.FileIssue))
	}
	return issues
}

func currentBytesOnTarget(canonical domain.AutoGatherCanonicalPlanResult, targetDisk string) uint64 {
	for _, t := range canonical.CanonicalTargets {
		if t.DiskName == targetDisk {
			return t.CanonicalCurrentBytesOnTarget
		}
	}
	return 0
}

type autoGatherTargetDiskMeta struct {
	FreeBytes          uint64
	ProjectedFreeBytes uint64
}

func lookupCanonicalTargetMeta(canonical domain.AutoGatherCanonicalPlanResult, targetDisk string) autoGatherTargetDiskMeta {
	for _, t := range canonical.CanonicalTargets {
		if t.DiskName == targetDisk {
			return autoGatherTargetDiskMeta{
				FreeBytes:          t.FreeBytes,
				ProjectedFreeBytes: t.ProjectedFreeBytes,
			}
		}
	}
	return autoGatherTargetDiskMeta{}
}

func (c *Core) emptyFolderOnlyDisksForShow(showPath string) []string {
	if c.state.Unraid == nil {
		return nil
	}
	arrayDisks, cacheDisks := autogather.PartitionDisks(c.state.Unraid.Disks)
	show := autogather.ScanSingleShow(showPath, arrayDisks, cacheDisks)
	return append([]string(nil), show.CleanupCandidateDisks...)
}

func (c *Core) generateAutoGatherRealPrepID() string {
	if c.sid != nil {
		return c.sid.MustGenerate()
	}
	return fmt.Sprintf("prep-%d", time.Now().UnixNano())
}

func parseAutoGatherTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func autoGatherRealPreparationExpired(prepared *domain.AutoGatherRealPreparedMove) bool {
	if prepared == nil {
		return true
	}
	exp := parseAutoGatherTime(prepared.ExpiresAt)
	return exp.IsZero() || time.Now().UTC().After(exp)
}

func (c *Core) expireAutoGatherRealPreparedIfNeededLocked() {
	if c.autoGatherRealState != nil {
		switch c.autoGatherRealState.Phase {
		case domain.AutoGatherRealPhasePreparing,
			domain.AutoGatherRealPhaseExecuting,
			domain.AutoGatherRealPhaseStopping:
			return
		}
	}
	expired := false
	if c.autoGatherRealPrepared != nil {
		expired = autoGatherRealPreparationExpired(c.autoGatherRealPrepared)
	} else if c.autoGatherRealState != nil && c.autoGatherRealState.Phase == domain.AutoGatherRealPhasePrepared {
		expired = true
	}
	if !expired {
		return
	}
	c.markAutoGatherRealPreparedExpiredLocked()
}

func (c *Core) markAutoGatherRealPreparedExpiredLocked() {
	c.clearAutoGatherRealPreparedSessionLocked(domain.AutoGatherRealPhaseExpired, autoGatherRealExpiredMsg)
	autoGatherRealLog("preparation expired")
}

func (c *Core) markAutoGatherRealPreparedCancelledLocked() {
	c.clearAutoGatherRealPreparedSessionLocked(domain.AutoGatherRealPhaseIdle, autoGatherRealCancelledMsg)
	autoGatherRealLog("preparation cancelled")
}

func (c *Core) clearAutoGatherRealPreparedSessionLocked(phase, message string) {
	c.clearAutoGatherRealPreparedLocked()
	c.ensureAutoGatherRealStateLocked()
	c.autoGatherRealState.Phase = phase
	c.autoGatherRealState.OperationPhase = ""
	c.autoGatherRealState.PreparationID = ""
	c.autoGatherRealState.Prepared = nil
	c.autoGatherRealState.CurrentShow = ""
	c.autoGatherRealState.CurrentShowName = ""
	c.autoGatherRealState.CurrentTarget = ""
	c.autoGatherRealState.Error = message
	c.autoGatherRealState.Message = autoGatherRealMessage
}

func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	set := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := set[s]; ok {
			continue
		}
		set[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func (c *Core) isAutoGatherRealSessionActive() bool {
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	return c.isAutoGatherRealSessionActiveLocked()
}

func (c *Core) isAutoGatherRealSessionActiveLocked() bool {
	if c.autoGatherRealState == nil {
		return false
	}
	switch c.autoGatherRealState.Phase {
	case domain.AutoGatherRealPhasePreparing,
		domain.AutoGatherRealPhasePrepared,
		domain.AutoGatherRealPhaseExecuting,
		domain.AutoGatherRealPhaseStopping:
		return true
	default:
		return false
	}
}

func (c *Core) isAutoGatherRealExecutionBusy() bool {
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	return c.isAutoGatherRealExecutionBusyLocked()
}

func (c *Core) isAutoGatherRealExecutionBusyLocked() bool {
	if c.autoGatherRealState == nil {
		return false
	}
	switch c.autoGatherRealState.Phase {
	case domain.AutoGatherRealPhaseExecuting, domain.AutoGatherRealPhaseStopping:
		return true
	default:
		return false
	}
}

func (c *Core) ensureAutoGatherRealStateLocked() {
	if c.autoGatherRealState == nil {
		c.autoGatherRealState = &domain.AutoGatherRealState{
			Phase:    domain.AutoGatherRealPhaseIdle,
			Message:  autoGatherRealMessage,
			GlobalDryRun: c.ctx.DryRun,
		}
	}
}

func (c *Core) snapshotAutoGatherRealLocked() domain.AutoGatherRealState {
	c.ensureAutoGatherRealStateLocked()
	snap := *c.autoGatherRealState
	snap.GlobalDryRun = c.ctx.DryRun
	if snap.Prepared != nil {
		prepared := *snap.Prepared
		prepared.GlobalDryRun = c.ctx.DryRun
		snap.Prepared = &prepared
	}
	return snap
}

func (c *Core) clearAutoGatherRealPreparedLocked() {
	c.autoGatherRealPrepared = nil
}

func (c *Core) failAutoGatherRealPrepare(reason string) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	c.ensureAutoGatherRealStateLocked()
	c.autoGatherRealState.Phase = domain.AutoGatherRealPhaseIdle
	c.autoGatherRealState.OperationPhase = ""
	c.autoGatherRealState.Error = reason
	c.clearAutoGatherRealPreparedLocked()
}

func (c *Core) setAutoGatherRealOperationPhase(phase string) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	if c.autoGatherRealState != nil {
		c.autoGatherRealState.OperationPhase = phase
	}
}

func (c *Core) finalizeAutoGatherRealStopped(message string) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	c.ensureAutoGatherRealStateLocked()
	c.autoGatherRealState.Phase = domain.AutoGatherRealPhaseStopped
	c.autoGatherRealState.EndedAt = time.Now().UTC().Format(time.RFC3339)
	c.autoGatherRealState.OperationPhase = ""
	c.autoGatherRealState.StoppedMessage = message
	c.autoGatherRealState.Message = autoGatherRealMessage
	autoGatherRealLog("stopped: %s", message)
}

func (c *Core) finalizeAutoGatherRealPlanChanged(details string) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	c.clearAutoGatherRealPreparedLocked()
	c.ensureAutoGatherRealStateLocked()
	c.autoGatherRealState.Phase = domain.AutoGatherRealPhaseFailed
	c.autoGatherRealState.EndedAt = time.Now().UTC().Format(time.RFC3339)
	c.autoGatherRealState.OperationPhase = ""
	c.autoGatherRealState.Prepared = nil
	c.autoGatherRealState.PreparationID = ""
	c.autoGatherRealState.Error = "plan changed; prepare again: " + details
	c.autoGatherRealState.Message = autoGatherRealMessage
	autoGatherRealLog("plan changed: %s", details)
}

func (c *Core) finalizeAutoGatherRealFailed(reason string) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	c.ensureAutoGatherRealStateLocked()
	c.autoGatherRealState.Phase = domain.AutoGatherRealPhaseFailed
	c.autoGatherRealState.EndedAt = time.Now().UTC().Format(time.RFC3339)
	c.autoGatherRealState.OperationPhase = ""
	c.autoGatherRealState.Error = reason
	c.autoGatherRealState.Message = autoGatherRealMessage
	autoGatherRealLog("failed: %s", reason)
}

func (c *Core) finalizeAutoGatherRealCompleted(verification domain.AutoGatherRealVerification) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	c.ensureAutoGatherRealStateLocked()
	if verification.Passed {
		c.autoGatherRealState.Phase = domain.AutoGatherRealPhaseCompleted
	} else {
		c.autoGatherRealState.Phase = domain.AutoGatherRealPhaseVerificationWarning
	}
	c.autoGatherRealState.EndedAt = time.Now().UTC().Format(time.RFC3339)
	c.autoGatherRealState.OperationPhase = ""
	c.autoGatherRealState.Verification = &verification
	c.autoGatherRealState.Message = autoGatherRealMessage
	autoGatherRealLog("completed verification passed=%v", verification.Passed)
}
