package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"unbalance/daemon/common"
	"unbalance/daemon/domain"
	"unbalance/daemon/logger"
)

const (
	autoGatherControlledDefaultMaxShows = 3
	autoGatherControlledDefaultMaxBytes = 50 * 1024 * 1024 * 1024 // 50 GB
	autoGatherControlledMessage         = "Stage 3D: controlled real Auto Gather. Each show uses the proven Stage 3C real-move path with fresh planning/revalidation."
	autoGatherControlledMarkerFile      = "autogather_controlled.active"
	autoGatherControlledInterruptedMsg  = "The previous daemon stopped during a real Auto Gather session. An rsync process may still be writing to disk. Do not start another Gather, Scatter, or Auto Gather operation until you have verified the array and explicitly acknowledged this interruption. Acknowledgement does not resume the previous session."
	autoGatherControlledAckLiveRsyncMsg = "the previous rsync still appears active; wait for it to finish or stop before acknowledging. The interrupted lock has not been cleared."
)

// autoGatherControlledMarker is persisted while a Stage 3D session may still
// have an in-flight real command. It is a diagnostic/safety flag, not a
// resumable execution token.
type autoGatherControlledMarker struct {
	SessionID       string `json:"sessionId"`
	StartedAt       string `json:"startedAt,omitempty"`
	DaemonPID       int    `json:"daemonPid,omitempty"`
	MaxShows        int    `json:"maxShows,omitempty"`
	MaxBytes        uint64 `json:"maxBytes,omitempty"`
	CumulativeBytes uint64 `json:"cumulativeBytes,omitempty"`
	CurrentShow     string `json:"currentShow,omitempty"`
	CurrentShowName string `json:"currentShowName,omitempty"`
	CurrentTarget   string `json:"currentTarget,omitempty"`
	OperationPhase  string `json:"operationPhase,omitempty"`
	RsyncPID        int    `json:"rsyncPid,omitempty"`
	SourceEntry     string `json:"sourceEntry,omitempty"`
	WrittenAt       string `json:"writtenAt"`
}

// Test hooks for Stage 3D orchestration.
var autoGatherControlledScanHook func(c *Core) domain.AutoGatherScanResult
var autoGatherControlledRefreshHook func(c *Core) *domain.Unraid
var autoGatherControlledRescoreHook func(c *Core, base domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult
var autoGatherControlledCanonicalHook func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult
var autoGatherControlledBuildPlanHook func(c *Core, showPath string) (*domain.Plan, error)
var autoGatherControlledExecuteHook func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult
var autoGatherControlledVerifyHook func(c *Core, showPath, targetDisk string) domain.AutoGatherRealVerification
var autoGatherControlledRsyncProbeHook func(pid int) domain.AutoGatherControlledRsyncProbe

func autoGatherControlledLog(format string, args ...any) {
	logger.Blue("autoGatherControlled: "+format, args...)
}

// StartAutoGatherControlled begins a bounded multi-show real Auto Gather session.
func (c *Core) StartAutoGatherControlled(req domain.AutoGatherControlledStartRequest) (domain.AutoGatherControlledState, error) {
	if !req.Confirm {
		return domain.AutoGatherControlledState{
			Phase:        domain.AutoGatherControlledPhaseIdle,
			GlobalDryRun: c.ctx.DryRun,
			Error:        "explicit confirmation is required to start a real Auto Gather session",
		}, fmt.Errorf("explicit confirmation required")
	}
	if c.ctx.DryRun {
		return domain.AutoGatherControlledState{
			Phase:        domain.AutoGatherControlledPhaseIdle,
			GlobalDryRun: true,
			Error:        "controlled real Auto Gather requires global dry-run mode to be disabled",
		}, fmt.Errorf("global dry-run mode is enabled")
	}
	if req.MaxShows <= 0 {
		req.MaxShows = autoGatherControlledDefaultMaxShows
	}
	if req.MaxBytes <= 0 {
		req.MaxBytes = autoGatherControlledDefaultMaxBytes
	}

	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()

	if c.isAutoGatherControlledInterruptedLocked() {
		snap := c.snapshotAutoGatherControlledLocked()
		snap.Error = "a previous controlled Auto Gather session was interrupted; acknowledge it before starting a new real session"
		return snap, fmt.Errorf("controlled Auto Gather session was interrupted")
	}
	if c.isAutoGatherControlledActiveLocked() {
		return c.snapshotAutoGatherControlledLocked(), fmt.Errorf("controlled Auto Gather session is already active")
	}
	if c.isAutoGatherDryRunActiveLocked() {
		return domain.AutoGatherControlledState{
			Phase:        domain.AutoGatherControlledPhaseIdle,
			GlobalDryRun: false,
			Error:        "Auto Gather dry-run is active",
		}, fmt.Errorf("Auto Gather dry-run is active")
	}
	if c.isAutoGatherRealSessionActiveLocked() {
		return domain.AutoGatherControlledState{
			Phase:        domain.AutoGatherControlledPhaseIdle,
			GlobalDryRun: false,
			Error:        "Stage 3C one-show real move session is active",
		}, fmt.Errorf("Stage 3C real move session is active")
	}
	if c.state == nil || c.state.Status != common.OpNeutral {
		return domain.AutoGatherControlledState{
			Phase:        domain.AutoGatherControlledPhaseIdle,
			GlobalDryRun: false,
			Error:        fmt.Sprintf("unbalanced is busy: status %d", c.state.Status),
		}, fmt.Errorf("unbalanced is busy")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	c.autoGatherControlledRun = &domain.AutoGatherControlledState{
		Phase:     domain.AutoGatherControlledPhaseRunning,
		MaxShows:  req.MaxShows,
		MaxBytes:  req.MaxBytes,
		StartedAt: now,
		Message:   autoGatherControlledMessage,
		SessionID: c.newControlledSessionID(),
	}
	c.autoGatherControlledStopRequested = false
	c.autoGatherControlledShutdown = false
	c.state.Status = common.OpAutoGatherReal

	autoGatherControlledLog("session started id=%s maxShows=%d maxBytes=%d", c.autoGatherControlledRun.SessionID, req.MaxShows, req.MaxBytes)
	c.writeAutoGatherControlledMarkerLocked()
	go c.autoGatherControlledLoop(req.MaxShows, req.MaxBytes)
	return c.snapshotAutoGatherControlledLocked(), nil
}

// StopAutoGatherControlled cooperatively stops the Stage 3D session.
func (c *Core) StopAutoGatherControlled() domain.AutoGatherControlledState {
	c.autoGatherMu.Lock()
	accepted := false
	if c.autoGatherControlledRun != nil {
		switch c.autoGatherControlledRun.Phase {
		case domain.AutoGatherControlledPhaseRunning:
			c.autoGatherControlledStopRequested = true
			c.autoGatherControlledRun.Phase = domain.AutoGatherControlledPhaseStopping
			accepted = true
		case domain.AutoGatherControlledPhaseStopping:
			// already stopping
		}
	}
	c.autoGatherMu.Unlock()
	c.stopped = true
	if accepted {
		autoGatherControlledLog("stop requested")
	}
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	return c.snapshotAutoGatherControlledLocked()
}

// GetAutoGatherControlledState returns the current Stage 3D session status.
func (c *Core) GetAutoGatherControlledState() domain.AutoGatherControlledState {
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	return c.snapshotAutoGatherControlledLocked()
}

// ResetAutoGatherControlledSession clears a clean terminal Stage 3D session
// (completed, stopped, or failed) back to idle so a new explicitly confirmed
// Start can be configured. It never resumes or starts execution, and it
// refuses interrupted sessions (use Acknowledge instead).
func (c *Core) ResetAutoGatherControlledSession(confirm bool) (domain.AutoGatherControlledState, error) {
	if !confirm {
		c.autoGatherMu.RLock()
		defer c.autoGatherMu.RUnlock()
		snap := c.snapshotAutoGatherControlledLocked()
		snap.Error = "explicit confirmation is required to clear a finished Auto Gather session"
		return snap, fmt.Errorf("explicit confirmation required")
	}

	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()

	if c.autoGatherControlledRun == nil {
		return c.snapshotAutoGatherControlledLocked(), nil
	}

	phase := c.autoGatherControlledRun.Phase
	switch phase {
	case domain.AutoGatherControlledPhaseCompleted,
		domain.AutoGatherControlledPhaseStopped,
		domain.AutoGatherControlledPhaseFailed:
		// clean terminal: safe to clear for a new explicit Start
	case domain.AutoGatherControlledPhaseInterrupted:
		snap := c.snapshotAutoGatherControlledLocked()
		snap.Error = "interrupted Auto Gather sessions must be acknowledged; they cannot use the normal new-session reset"
		return snap, fmt.Errorf("interrupted session requires acknowledgement")
	case domain.AutoGatherControlledPhaseRunning, domain.AutoGatherControlledPhaseStopping:
		snap := c.snapshotAutoGatherControlledLocked()
		snap.Error = "cannot clear an active Auto Gather session; stop it first"
		return snap, fmt.Errorf("controlled Auto Gather session is still active")
	default:
		snap := c.snapshotAutoGatherControlledLocked()
		snap.Error = fmt.Sprintf("cannot clear Auto Gather session in phase %q", phase)
		return snap, fmt.Errorf("unsupported controlled session phase")
	}

	autoGatherControlledLog("clean terminal session cleared for new start previousPhase=%s id=%s", phase, c.autoGatherControlledRun.SessionID)
	c.autoGatherControlledRun = nil
	c.autoGatherControlledStopRequested = false
	c.removeAutoGatherControlledMarkerLocked(true)
	return c.snapshotAutoGatherControlledLocked(), nil
}

// AcknowledgeAutoGatherControlledInterrupted clears a persisted interrupted
// session after explicit operator confirmation. It never resumes execution.
func (c *Core) AcknowledgeAutoGatherControlledInterrupted(confirm bool) (domain.AutoGatherControlledState, error) {
	if !confirm {
		c.autoGatherMu.RLock()
		defer c.autoGatherMu.RUnlock()
		snap := c.snapshotAutoGatherControlledLocked()
		snap.Error = "explicit confirmation is required to acknowledge an interrupted Auto Gather session"
		return snap, fmt.Errorf("explicit confirmation required")
	}

	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()

	if !c.isAutoGatherControlledInterruptedLocked() {
		snap := c.snapshotAutoGatherControlledLocked()
		snap.Error = "no interrupted Auto Gather session to acknowledge"
		return snap, fmt.Errorf("no interrupted Auto Gather session to acknowledge")
	}

	probe := probeControlledRsyncPID(c.autoGatherControlledRun.LastRsyncPID)
	if controlledRsyncProbeBlocksAck(probe) {
		snap := c.snapshotAutoGatherControlledLocked()
		snap.RsyncProbe = &probe
		snap.CanAcknowledge = false
		snap.RequiresAcknowledgement = true
		snap.Error = autoGatherControlledAckLiveRsyncMsg
		autoGatherControlledLog("acknowledgement refused: recorded rsync pid=%d still appears active", probe.PID)
		return snap, fmt.Errorf("%s", autoGatherControlledAckLiveRsyncMsg)
	}

	autoGatherControlledLog("interrupted session acknowledged id=%s", c.autoGatherControlledRun.SessionID)
	c.autoGatherControlledRun = nil
	c.autoGatherControlledStopRequested = false
	c.removeAutoGatherControlledMarkerLocked(true)
	return c.snapshotAutoGatherControlledLocked(), nil
}

func (c *Core) isAutoGatherControlledActiveLocked() bool {
	if c.autoGatherControlledRun == nil {
		return false
	}
	switch c.autoGatherControlledRun.Phase {
	case domain.AutoGatherControlledPhaseRunning, domain.AutoGatherControlledPhaseStopping:
		return true
	}
	return false
}

func (c *Core) isAutoGatherControlledActive() bool {
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	return c.isAutoGatherControlledActiveLocked()
}

func (c *Core) isAutoGatherControlledInterruptedLocked() bool {
	return c.autoGatherControlledRun != nil &&
		c.autoGatherControlledRun.Phase == domain.AutoGatherControlledPhaseInterrupted
}

func (c *Core) isAutoGatherControlledInterrupted() bool {
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	return c.isAutoGatherControlledInterruptedLocked()
}

func (c *Core) autoGatherControlledBlocksRealOpsLocked() bool {
	return c.isAutoGatherControlledActiveLocked() || c.isAutoGatherControlledInterruptedLocked()
}

func (c *Core) autoGatherControlledBlocksRealOps() bool {
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	return c.autoGatherControlledBlocksRealOpsLocked()
}

func (c *Core) autoGatherControlledShouldStop() bool {
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	return c.autoGatherControlledStopRequested || c.autoGatherControlledShutdown
}

func (c *Core) snapshotAutoGatherControlledLocked() domain.AutoGatherControlledState {
	if c.autoGatherControlledRun == nil {
		return domain.AutoGatherControlledState{
			Phase:        domain.AutoGatherControlledPhaseIdle,
			GlobalDryRun: c.ctx.DryRun,
			Message:      autoGatherControlledMessage,
		}
	}
	snap := *c.autoGatherControlledRun
	snap.GlobalDryRun = c.ctx.DryRun
	if snap.Completed == nil {
		snap.Completed = []domain.AutoGatherControlledShowRecord{}
	}
	if snap.Skipped == nil {
		snap.Skipped = []domain.AutoGatherControlledShowRecord{}
	}
	if snap.Phase == domain.AutoGatherControlledPhaseInterrupted {
		snap.RequiresAcknowledgement = true
		snap.Message = autoGatherControlledInterruptedMsg
		probe := probeControlledRsyncPID(snap.LastRsyncPID)
		snap.RsyncProbe = &probe
		snap.CanAcknowledge = !controlledRsyncProbeBlocksAck(probe)
	}
	if c.autoGatherLibraryRevision > 0 {
		snap.LibraryRevision = c.autoGatherLibraryRevision
		sum := c.autoGatherLibrarySummary
		snap.LibrarySummary = &sum
	}
	return snap
}

func (c *Core) autoGatherControlledLoop(maxShows int, maxBytes uint64) {
	defer func() {
		c.autoGatherMu.Lock()
		shutdown := c.autoGatherControlledShutdown
		phase := ""
		if c.autoGatherControlledRun != nil {
			phase = c.autoGatherControlledRun.Phase
		}
		if !shutdown && c.state != nil && c.state.Status == common.OpAutoGatherReal {
			c.state.Status = common.OpNeutral
		}
		if shutdown {
			c.writeAutoGatherControlledMarkerLocked()
			c.autoGatherMu.Unlock()
			autoGatherControlledLog("session interrupted by daemon shutdown; marker retained")
			return
		}
		switch phase {
		case domain.AutoGatherControlledPhaseCompleted,
			domain.AutoGatherControlledPhaseStopped,
			domain.AutoGatherControlledPhaseFailed:
			c.removeAutoGatherControlledMarkerLocked(true)
		default:
			c.writeAutoGatherControlledMarkerLocked()
		}
		c.autoGatherMu.Unlock()
		autoGatherControlledLog("session ended phase=%s", c.GetAutoGatherControlledState().Phase)
	}()

	completed := map[string]struct{}{}
	skipped := map[string]struct{}{}
	var cumulativeBytes uint64
	showsCompleted := 0

	for {
		if c.autoGatherControlledShouldStop() {
			c.finalizeAutoGatherControlled(domain.AutoGatherControlledPhaseStopped, "", "", "")
			return
		}

		// Check session bounds.
		if showsCompleted >= maxShows {
			autoGatherControlledLog("max shows reached (%d)", maxShows)
			c.finalizeAutoGatherControlled(domain.AutoGatherControlledPhaseCompleted, "", "", "")
			return
		}
		if cumulativeBytes >= maxBytes {
			autoGatherControlledLog("max bytes reached (%d)", maxBytes)
			c.finalizeAutoGatherControlled(domain.AutoGatherControlledPhaseCompleted, "", "", "")
			return
		}

		// Fresh Unraid refresh before each iteration.
		autoGatherControlledLog("refresh started")
		c.setAutoGatherControlledOperationPhase("refresh")
		freshUnraid := c.refreshUnraidForAutoGatherControlled()
		if freshUnraid == nil {
			c.failAutoGatherControlled("", "", "unable to read array disks")
			return
		}
		c.state.Unraid = freshUnraid
		if c.autoGatherControlledShouldStop() {
			c.finalizeAutoGatherControlled(domain.AutoGatherControlledPhaseStopped, "", "", "")
			return
		}

		// Fresh full library scan for real moves (actual placement changed).
		autoGatherControlledLog("library scan started")
		c.setAutoGatherControlledOperationPhase("scan")
		scan := c.scanForAutoGatherControlled()
		if scan.Cancelled || c.autoGatherControlledShouldStop() {
			c.finalizeAutoGatherControlled(domain.AutoGatherControlledPhaseStopped, "", "", "")
			return
		}
		if scan.Error != "" {
			c.failAutoGatherControlled("", "", "scan failed: "+scan.Error)
			return
		}
		autoGatherControlledLog("library scan completed; shows=%d", len(scan.Shows))

		// Stage 2 recommendations using fresh disk state and fresh scan.
		c.setAutoGatherControlledOperationPhase("rescore")
		scored := c.rescoreAutoGatherControlled(scan, freshUnraid)
		if scored.Cancelled || c.autoGatherControlledShouldStop() {
			c.finalizeAutoGatherControlled(domain.AutoGatherControlledPhaseStopped, "", "", "")
			return
		}
		c.publishAutoGatherControlledLibraryScan(scored)

		// Select next show (same selection logic as dry-run).
		c.setAutoGatherControlledOperationPhase("select")
		pick, _, cancelled := selectNextDryRunShow(scored, completed, skipped, c.autoGatherControlledShouldStop)
		if cancelled || c.autoGatherControlledShouldStop() {
			c.finalizeAutoGatherControlled(domain.AutoGatherControlledPhaseStopped, "", "", "")
			return
		}
		if pick == nil {
			c.finalizeAutoGatherControlled(domain.AutoGatherControlledPhaseCompleted, "", "", "")
			return
		}

		if pick.skipOnly {
			c.recordAutoGatherControlledSkipped(pick.show, pick.skipReason)
			skipped[pick.show.Path] = struct{}{}
			continue
		}

		if c.autoGatherControlledShouldStop() {
			c.finalizeAutoGatherControlled(domain.AutoGatherControlledPhaseStopped, "", "", "")
			return
		}

		showPath := pick.show.Path
		showName := pick.show.Name
		stage2Target := pick.stage2Target
		stage2Move := pick.stage2Move

		autoGatherControlledLog("selected %s target=%s move=%d", showPath, stage2Target, stage2Move)
		c.setAutoGatherControlledCurrent(showPath, showName, stage2Target)

		// Canonical planning (fresh, per-show).
		c.setAutoGatherControlledOperationPhase("planning")
		bundle, bundleCancelled := c.buildAutoGatherControlledCanonicalBundle(showPath, stage2Target, stage2Move)
		if bundleCancelled || c.autoGatherControlledShouldStop() {
			c.finalizeAutoGatherControlled(domain.AutoGatherControlledPhaseStopped, "", "", "")
			return
		}
		canonical := bundle.Result
		if canonical.Error != "" {
			c.failAutoGatherControlled(showPath, showName, "canonical plan error: "+canonical.Error)
			return
		}

		// Resolve target.
		targetDisk, targetPath, _, resolveErr := resolveAutoGatherExecutionTarget(canonical, stage2Target)
		skipReason, fatalErr := classifyCanonicalSelectionOutcome(pick.show, canonical, resolveErr)
		if fatalErr != "" {
			c.failAutoGatherControlled(showPath, showName, fatalErr)
			return
		}
		if skipReason != "" {
			// Safe pre-exec skip: no filesystem mutation happened.
			c.recordAutoGatherControlledSkipped(pick.show, skipReason)
			skipped[showPath] = struct{}{}
			continue
		}
		if resolveErr != nil {
			c.failAutoGatherControlled(showPath, showName, resolveErr.Error())
			return
		}

		// Validate target.
		if err := validateAutoGatherGatherTarget(bundle.Plan, targetPath); err != nil {
			c.failAutoGatherControlled(showPath, showName, err.Error())
			return
		}

		// Structural executability check.
		if !isAutoGatherRealPlanStructurallyExecutable(bundle.Plan, targetPath) {
			c.failAutoGatherControlled(showPath, showName, "canonical Gather target has no executable items")
			return
		}

		// Build fingerprint.
		fingerprint, fpErr := buildAutoGatherRealPlanFingerprint(showPath, targetDisk, targetPath, bundle.Plan)
		if fpErr != nil {
			c.failAutoGatherControlled(showPath, showName, fpErr.Error())
			return
		}

		// Check byte bounds won't be exceeded (strict hard limit).
		moveBytes := bundle.Plan.VDisks[targetPath].Bin.Size
		if cumulativeBytes+moveBytes > maxBytes {
			autoGatherControlledLog("candidate %s (%d bytes) exceeds remaining byte budget; skipping", showPath, moveBytes)
			c.recordAutoGatherControlledSkipped(pick.show, fmt.Sprintf("move %d bytes exceeds remaining byte allowance (%d remaining of %d)", moveBytes, maxBytes-cumulativeBytes, maxBytes))
			skipped[showPath] = struct{}{}
			continue
		}

		if c.autoGatherControlledShouldStop() {
			c.finalizeAutoGatherControlled(domain.AutoGatherControlledPhaseStopped, "", "", "")
			return
		}

		// --- EXECUTE: fresh revalidation (Stage 3C revalidation pattern). ---
		c.setAutoGatherControlledOperationPhase("revalidation")
		c.state.Unraid = c.refreshUnraidForAutoGatherControlled()
		if c.state.Unraid == nil {
			c.failAutoGatherControlled(showPath, showName, "unable to read array disks during revalidation")
			return
		}

		freshBundle, freshCancelled := c.buildAutoGatherControlledCanonicalBundle(showPath, stage2Target, stage2Move)
		if freshCancelled || c.autoGatherControlledShouldStop() {
			c.finalizeAutoGatherControlled(domain.AutoGatherControlledPhaseStopped, "", "", "")
			return
		}
		if freshBundle.Result.Error != "" {
			c.failAutoGatherControlled(showPath, showName, "revalidation canonical error: "+freshBundle.Result.Error)
			return
		}

		freshTarget, freshPath, _, freshErr := resolveAutoGatherExecutionTarget(freshBundle.Result, stage2Target)
		if freshErr != nil {
			c.failAutoGatherControlled(showPath, showName, "revalidation target: "+freshErr.Error())
			return
		}
		if freshTarget != targetDisk || freshPath != targetPath {
			c.failAutoGatherControlled(showPath, showName, fmt.Sprintf("canonical target changed from %s to %s", targetDisk, freshTarget))
			return
		}
		freshFingerprint, fpErr2 := buildAutoGatherRealPlanFingerprint(showPath, freshTarget, freshPath, freshBundle.Plan)
		if fpErr2 != nil {
			c.failAutoGatherControlled(showPath, showName, fpErr2.Error())
			return
		}
		if diff := diffAutoGatherRealPlanFingerprints(fingerprint, freshFingerprint); diff != "" {
			c.failAutoGatherControlled(showPath, showName, "plan fingerprint changed: "+diff)
			return
		}

		// Stage 3C execution gates.
		if c.ctx.DryRun {
			c.failAutoGatherControlled(showPath, showName, "global dry-run mode is enabled")
			return
		}
		if c.autoGatherControlledShouldStop() {
			c.finalizeAutoGatherControlled(domain.AutoGatherControlledPhaseStopped, "", "", "")
			return
		}

		// Execute real Gather (Stage 3C path).
		autoGatherControlledLog("executing %s -> %s", showPath, targetDisk)
		c.setAutoGatherControlledOperationPhase("rsync")
		c.setAutoGatherControlledCurrent(showPath, showName, targetDisk)
		execResult := c.executeAutoGatherControlledGather(freshBundle.Plan, targetPath)
		if c.autoGatherControlledShutdownActive() {
			return
		}
		if c.autoGatherControlledShouldStop() || execResult.outcome == gatherExecCancelled {
			c.failAutoGatherControlled(showPath, showName,
				"The move was stopped. The interrupted transfer's source was not deleted. "+
					"Files transferred successfully before the stop may already have had their original source copies "+
					"removed by normal Gather behaviour.")
			c.autoGatherMu.Lock()
			if c.autoGatherControlledRun != nil {
				c.autoGatherControlledRun.Phase = domain.AutoGatherControlledPhaseStopped
			}
			c.autoGatherMu.Unlock()
			return
		}
		if execResult.outcome != gatherExecSuccess {
			c.failAutoGatherControlled(showPath, showName, "execution failed: "+execResult.reason)
			return
		}

		// Post-move verification (fresh refresh).
		c.setAutoGatherControlledOperationPhase("verification")
		c.state.Unraid = c.refreshUnraidForAutoGatherControlled()
		verification := c.verifyAutoGatherControlledMove(showPath, targetDisk)
		if !verification.Passed {
			msg := "post-move verification failed"
			if verification.Message != "" {
				msg = verification.Message
			}
			c.failAutoGatherControlled(showPath, showName, msg)
			return
		}

		// Record success.
		permWarnings := gatherPlanPermissionWarnings(freshBundle.Plan)
		c.recordAutoGatherControlledCompleted(pick.show, targetDisk, moveBytes, permWarnings)
		completed[showPath] = struct{}{}
		cumulativeBytes += moveBytes
		showsCompleted++
		autoGatherControlledLog("completed %s -> %s (%d bytes); total=%d/%d shows=%d/%d",
			showPath, targetDisk, moveBytes, cumulativeBytes, maxBytes, showsCompleted, maxShows)
	}
}

// --- Helpers for controlled orchestration state management ---

func (c *Core) setAutoGatherControlledOperationPhase(phase string) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	if c.autoGatherControlledRun != nil {
		c.autoGatherControlledRun.OperationPhase = phase
		c.writeAutoGatherControlledMarkerLocked()
	}
}

func (c *Core) setAutoGatherControlledCurrent(showPath, showName, target string) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	if c.autoGatherControlledRun != nil {
		c.autoGatherControlledRun.CurrentShow = showPath
		c.autoGatherControlledRun.CurrentShowName = showName
		c.autoGatherControlledRun.CurrentTarget = target
		c.writeAutoGatherControlledMarkerLocked()
	}
}

func (c *Core) recordAutoGatherControlledCompleted(show domain.AutoGatherShow, target string, moveBytes uint64, warnings *domain.AutoGatherRealPermissionWarnings) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	if c.autoGatherControlledRun == nil {
		return
	}
	c.autoGatherControlledRun.Completed = append(c.autoGatherControlledRun.Completed, domain.AutoGatherControlledShowRecord{
		ShowPath:           show.Path,
		ShowName:           show.Name,
		TargetDisk:         target,
		MoveBytes:          moveBytes,
		PermissionWarnings: warnings,
		At:                 time.Now().UTC().Format(time.RFC3339),
	})
	c.autoGatherControlledRun.CumulativeBytes += moveBytes
	c.autoGatherControlledRun.CurrentShow = ""
	c.autoGatherControlledRun.CurrentShowName = ""
	c.autoGatherControlledRun.CurrentTarget = ""
	c.autoGatherControlledRun.OperationPhase = ""
	c.markCompletedShowInPresentedLibraryLocked(show.Path)
}

func (c *Core) recordAutoGatherControlledSkipped(show domain.AutoGatherShow, reason string) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	if c.autoGatherControlledRun == nil {
		return
	}
	c.autoGatherControlledRun.Skipped = append(c.autoGatherControlledRun.Skipped, domain.AutoGatherControlledShowRecord{
		ShowPath: show.Path,
		ShowName: show.Name,
		Reason:   reason,
		At:       time.Now().UTC().Format(time.RFC3339),
	})
	autoGatherControlledLog("skipped: %s: %s", show.Path, reason)
}

func (c *Core) finalizeAutoGatherControlled(phase, failedShow, failedName, reason string) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	if c.autoGatherControlledShutdown {
		return
	}
	if c.autoGatherControlledRun == nil {
		return
	}
	c.autoGatherControlledRun.Phase = phase
	c.autoGatherControlledRun.EndedAt = time.Now().UTC().Format(time.RFC3339)
	c.autoGatherControlledRun.CurrentShow = ""
	c.autoGatherControlledRun.CurrentShowName = ""
	c.autoGatherControlledRun.CurrentTarget = ""
	c.autoGatherControlledRun.OperationPhase = ""
	if phase == domain.AutoGatherControlledPhaseFailed {
		c.autoGatherControlledRun.FailedShow = failedShow
		c.autoGatherControlledRun.FailedShowName = failedName
		c.autoGatherControlledRun.FailureReason = reason
	}
}

func (c *Core) failAutoGatherControlled(showPath, showName, reason string) {
	autoGatherControlledLog("failed: %s: %s", showPath, reason)
	c.finalizeAutoGatherControlled(domain.AutoGatherControlledPhaseFailed, showPath, showName, reason)
}

func (c *Core) publishAutoGatherControlledLibraryScan(scan domain.AutoGatherScanResult) {
	if scan.Cancelled || scan.Error != "" {
		return
	}
	c.storePresentedAutoGatherLibrary(scan, true)
}

func (c *Core) storePresentedAutoGatherLibrary(scan domain.AutoGatherScanResult, applyCompleted bool) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	c.storePresentedAutoGatherLibraryLocked(scan, applyCompleted)
}

func (c *Core) storePresentedAutoGatherLibraryLocked(scan domain.AutoGatherScanResult, applyCompleted bool) {
	cloned := cloneAutoGatherScanResult(scan)
	if applyCompleted && c.autoGatherControlledRun != nil {
		for _, rec := range c.autoGatherControlledRun.Completed {
			markShowConsolidatedInLibraryScan(&cloned, rec.ShowPath)
		}
	}
	c.autoGatherLibraryRevision++
	c.autoGatherLibraryScan = &cloned
	c.autoGatherLibrarySummary = summarizeAutoGatherLibrary(cloned, c.autoGatherLibraryRevision)
	if c.autoGatherControlledRun != nil {
		c.autoGatherControlledRun.LibraryRevision = c.autoGatherLibrarySummary.Revision
		sum := c.autoGatherLibrarySummary
		c.autoGatherControlledRun.LibrarySummary = &sum
	}
}

func (c *Core) markCompletedShowInPresentedLibraryLocked(showPath string) {
	if c.autoGatherLibraryScan == nil || showPath == "" {
		return
	}
	cloned := cloneAutoGatherScanResult(*c.autoGatherLibraryScan)
	markShowConsolidatedInLibraryScan(&cloned, showPath)
	c.autoGatherLibraryRevision++
	c.autoGatherLibraryScan = &cloned
	c.autoGatherLibrarySummary = summarizeAutoGatherLibrary(cloned, c.autoGatherLibraryRevision)
	if c.autoGatherControlledRun != nil {
		c.autoGatherControlledRun.LibraryRevision = c.autoGatherLibrarySummary.Revision
		sum := c.autoGatherLibrarySummary
		c.autoGatherControlledRun.LibrarySummary = &sum
	}
}

func summarizeAutoGatherLibrary(scan domain.AutoGatherScanResult, revision uint64) domain.AutoGatherLibrarySummary {
	split := 0
	recs := 0
	for _, show := range scan.Shows {
		if show.Status == domain.AutoGatherStatusSplit || show.Split {
			split++
			if show.RecommendedTargetDisk != "" {
				recs++
			}
		}
	}
	return domain.AutoGatherLibrarySummary{
		Revision:            revision,
		LibraryPath:         scan.LibraryPath,
		ShowCount:           len(scan.Shows),
		SplitCount:          split,
		RecommendationCount: recs,
	}
}

func markShowConsolidatedInLibraryScan(scan *domain.AutoGatherScanResult, showPath string) {
	if scan == nil || showPath == "" {
		return
	}
	for i := range scan.Shows {
		if scan.Shows[i].Path != showPath {
			continue
		}
		scan.Shows[i].Split = false
		scan.Shows[i].Ready = false
		scan.Shows[i].Status = domain.AutoGatherStatusConsolidated
		clearAutoGatherDerivedRecommendationFields(&scan.Shows[i])
		return
	}
}

// --- Execution adapters (reuse Stage 3C real Gather path) ---

func (c *Core) refreshUnraidForAutoGatherControlled() *domain.Unraid {
	if autoGatherControlledRefreshHook != nil {
		return autoGatherControlledRefreshHook(c)
	}
	return c.refreshUnraid()
}

func (c *Core) scanForAutoGatherControlled() domain.AutoGatherScanResult {
	if autoGatherControlledScanHook != nil {
		return autoGatherControlledScanHook(c)
	}
	scan, _ := c.scanAutoGatherStage1WithCancel(c.autoGatherControlledShouldStop)
	return scan
}

func (c *Core) rescoreAutoGatherControlled(base domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult {
	if autoGatherControlledRescoreHook != nil {
		return autoGatherControlledRescoreHook(c, base, unraid)
	}
	scored := cloneAutoGatherDiscoverySnapshot(base)
	return c.addAutoGatherRecommendationsCancellable(scored, unraid, c.autoGatherControlledShouldStop)
}

func (c *Core) buildAutoGatherControlledCanonicalBundle(showPath, stage2Target string, stage2Move uint64) (autoGatherCanonicalBundle, bool) {
	if autoGatherControlledCanonicalHook != nil {
		if c.autoGatherControlledShouldStop() {
			return autoGatherCanonicalBundle{}, true
		}
		show := domain.AutoGatherShow{Path: showPath, RecommendedTargetDisk: stage2Target, MoveRequiredBytes: stage2Move}
		canonical := autoGatherControlledCanonicalHook(c, show)
		if canonical.Cancelled {
			return autoGatherCanonicalBundle{}, true
		}
		var plan *domain.Plan
		if autoGatherControlledBuildPlanHook != nil {
			var err error
			plan, err = autoGatherControlledBuildPlanHook(c, showPath)
			if err != nil {
				return autoGatherCanonicalBundle{Result: domain.AutoGatherCanonicalPlanResult{ShowPath: showPath, Error: err.Error()}}, false
			}
		}
		return autoGatherCanonicalBundle{Result: canonical, Plan: plan}, false
	}
	prevStatus := c.state.Status
	c.state.Status = common.OpGatherPlan
	defer func() {
		if c.state.Status == common.OpGatherPlan {
			c.state.Status = prevStatus
		}
	}()
	return c.buildAutoGatherCanonicalBundleCancellable(showPath, stage2Target, stage2Move, c.autoGatherControlledShouldStop)
}

func (c *Core) executeAutoGatherControlledGather(plan *domain.Plan, targetPath string) gatherExecResult {
	if autoGatherControlledExecuteHook != nil {
		if c.autoGatherControlledShouldStop() {
			return gatherExecResult{outcome: gatherExecCancelled, reason: "stop requested before execution"}
		}
		return autoGatherControlledExecuteHook(c, plan, targetPath)
	}
	return c.executeGatherPlanRealSync(plan, targetPath)
}

func (c *Core) verifyAutoGatherControlledMove(showPath, targetDisk string) domain.AutoGatherRealVerification {
	if autoGatherControlledVerifyHook != nil {
		return autoGatherControlledVerifyHook(c, showPath, targetDisk)
	}
	return c.verifyAutoGatherRealMove(showPath, targetDisk)
}

func (c *Core) newControlledSessionID() string {
	if c.sid != nil {
		if id, err := c.sid.Generate(); err == nil && id != "" {
			return id
		}
	}
	return fmt.Sprintf("ag3d-%d", time.Now().UTC().UnixNano())
}

func (c *Core) autoGatherControlledShutdownActive() bool {
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	return c.autoGatherControlledShutdown
}

// persistAutoGatherControlledForShutdown is invoked on SIGTERM/SIGINT.
// It leaves the session marker in place so restart reports interrupted, and
// never resumes or kills a child rsync.
func (c *Core) persistAutoGatherControlledForShutdown() {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	c.autoGatherControlledShutdown = true
	if c.autoGatherControlledRun == nil {
		return
	}
	switch c.autoGatherControlledRun.Phase {
	case domain.AutoGatherControlledPhaseRunning, domain.AutoGatherControlledPhaseStopping:
		c.autoGatherControlledRun.Phase = domain.AutoGatherControlledPhaseInterrupted
		c.autoGatherControlledRun.EndedAt = time.Now().UTC().Format(time.RFC3339)
		c.autoGatherControlledRun.Message = autoGatherControlledInterruptedMsg
		c.autoGatherControlledRun.RequiresAcknowledgement = true
		c.writeAutoGatherControlledMarkerLocked()
		autoGatherControlledLog("daemon shutdown during active session; retaining interrupted marker")
	}
}

func (c *Core) noteControlledRsyncChild(pid int, entry string) {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	if !c.isAutoGatherControlledActiveLocked() && !c.isAutoGatherControlledInterruptedLocked() {
		return
	}
	if c.autoGatherControlledRun == nil {
		return
	}
	c.autoGatherControlledRun.LastRsyncPID = pid
	c.autoGatherControlledRun.LastSourceEntry = entry
	c.writeAutoGatherControlledMarkerLocked()
}

func (c *Core) clearControlledRsyncChild() {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	if c.autoGatherControlledShutdown {
		return
	}
	if c.autoGatherControlledRun == nil {
		return
	}
	c.autoGatherControlledRun.LastRsyncPID = 0
	c.autoGatherControlledRun.LastSourceEntry = ""
	if c.isAutoGatherControlledActiveLocked() {
		c.writeAutoGatherControlledMarkerLocked()
	}
}

// --- Session marker persistence (data-dir) ---

func (c *Core) autoGatherControlledMarkerPath() string {
	dir := c.ctx.DataDir
	if dir == "" {
		dir = common.DefaultDataDir
	}
	return filepath.Join(dir, autoGatherControlledMarkerFile)
}

func (c *Core) writeAutoGatherControlledMarkerLocked() {
	if c.autoGatherControlledRun == nil {
		return
	}
	path := c.autoGatherControlledMarkerPath()
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	marker := autoGatherControlledMarker{
		SessionID:       c.autoGatherControlledRun.SessionID,
		StartedAt:       c.autoGatherControlledRun.StartedAt,
		DaemonPID:       os.Getpid(),
		MaxShows:        c.autoGatherControlledRun.MaxShows,
		MaxBytes:        c.autoGatherControlledRun.MaxBytes,
		CumulativeBytes: c.autoGatherControlledRun.CumulativeBytes,
		CurrentShow:     c.autoGatherControlledRun.CurrentShow,
		CurrentShowName: c.autoGatherControlledRun.CurrentShowName,
		CurrentTarget:   c.autoGatherControlledRun.CurrentTarget,
		OperationPhase:  c.autoGatherControlledRun.OperationPhase,
		RsyncPID:        c.autoGatherControlledRun.LastRsyncPID,
		SourceEntry:     c.autoGatherControlledRun.LastSourceEntry,
		WrittenAt:       time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, append(data, '\n'), 0644)
}

func (c *Core) writeAutoGatherControlledMarker() {
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	c.writeAutoGatherControlledMarkerLocked()
}

func (c *Core) removeAutoGatherControlledMarkerLocked(force bool) {
	if !force && c.autoGatherControlledShutdown {
		return
	}
	_ = os.Remove(c.autoGatherControlledMarkerPath())
}

func (c *Core) readAutoGatherControlledMarker() (autoGatherControlledMarker, bool) {
	path := c.autoGatherControlledMarkerPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return autoGatherControlledMarker{}, false
	}
	var marker autoGatherControlledMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		// Legacy timestamp-only marker from earlier Stage 3D builds.
		text := strings.TrimSpace(string(data))
		return autoGatherControlledMarker{StartedAt: text, WrittenAt: text}, true
	}
	return marker, true
}

func probeControlledRsyncPID(pid int) domain.AutoGatherControlledRsyncProbe {
	if autoGatherControlledRsyncProbeHook != nil {
		return autoGatherControlledRsyncProbeHook(pid)
	}
	if pid <= 0 {
		return domain.AutoGatherControlledRsyncProbe{
			Note: "no rsync child PID was recorded for the interrupted session",
		}
	}
	probe := domain.AutoGatherControlledRsyncProbe{PID: pid}
	comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		probe.Note = "recorded PID is not currently running (or /proc is unavailable); not killed because of PID reuse risk"
		return probe
	}
	command := strings.TrimSpace(string(comm))
	probe.Alive = true
	probe.Command = command
	probe.PlausibleRsync = command == "rsync" || strings.HasPrefix(command, "rsync")
	if probe.PlausibleRsync {
		probe.Note = "a process with the recorded PID still appears to be rsync. Acknowledgement is blocked until that process finishes or is stopped. It was not killed."
	} else {
		probe.Note = "PID exists but command is not rsync (possible PID reuse); it was not killed. Verify disk activity before acknowledging."
	}
	return probe
}

func controlledRsyncProbeBlocksAck(probe domain.AutoGatherControlledRsyncProbe) bool {
	return probe.Alive && probe.PlausibleRsync
}

// RecoverAutoGatherControlledInterrupted checks on startup whether a previous
// Stage 3D session was active when the daemon terminated. The marker is kept
// until the operator explicitly acknowledges it. Execution is never resumed.
func (c *Core) RecoverAutoGatherControlledInterrupted() {
	marker, ok := c.readAutoGatherControlledMarker()
	if !ok {
		return
	}
	c.autoGatherMu.Lock()
	defer c.autoGatherMu.Unlock()
	c.autoGatherControlledRun = &domain.AutoGatherControlledState{
		Phase:                   domain.AutoGatherControlledPhaseInterrupted,
		MaxShows:                marker.MaxShows,
		MaxBytes:                marker.MaxBytes,
		CurrentShow:             marker.CurrentShow,
		CurrentShowName:         marker.CurrentShowName,
		CurrentTarget:           marker.CurrentTarget,
		OperationPhase:          marker.OperationPhase,
		CumulativeBytes:         marker.CumulativeBytes,
		StartedAt:               marker.StartedAt,
		EndedAt:                 time.Now().UTC().Format(time.RFC3339),
		Message:                 autoGatherControlledInterruptedMsg,
		SessionID:               marker.SessionID,
		LastRsyncPID:            marker.RsyncPID,
		LastSourceEntry:         marker.SourceEntry,
		RequiresAcknowledgement: true,
	}
	autoGatherControlledLog("recovered interrupted session marker id=%s rsyncPid=%d", marker.SessionID, marker.RsyncPID)
}
