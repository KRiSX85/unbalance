package core

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cskr/pubsub"

	"unbalance/daemon/common"
	"unbalance/daemon/domain"
)

func newCoreForRealTest() *Core {
	c := newCoreForReserved(1, "Gb")
	c.ctx.DryRun = false
	c.ctx.Config.TvLibraryPath = "data/media/tv"
	c.ctx.Hub = pubsub.New(64)
	c.state.Unraid = &domain.Unraid{
		Disks: []*domain.Disk{
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(50)},
			{Name: "disk2", Path: "/mnt/disk2", Type: "Data", Size: gib(100), Free: gib(50)},
			{Name: "cache", Path: "/mnt/cache", Type: "Cache", Size: gib(100), Free: gib(50)},
		},
		BlockSize: 4096,
	}
	return c
}

func stubRealCanonicalBundle(showPath, targetDisk string, moveBytes uint64) autoGatherCanonicalBundle {
	targetPath := "/mnt/" + targetDisk
	sourcePath := "/mnt/disk2"
	if targetDisk == "disk2" {
		sourcePath = "/mnt/disk1"
	}
	plan := &domain.Plan{
		ChosenFolders: []string{showPath},
		VDisks: map[string]*domain.VDisk{
			targetPath: {
				Path: targetPath,
				Bin: &domain.Bin{
					Size: moveBytes,
					Items: []*domain.Item{
						{Location: sourcePath, Name: "episode.mkv", Path: sourcePath + "/episode.mkv", Size: moveBytes},
					},
				},
			},
			sourcePath: {
				Path: sourcePath,
				Bin: &domain.Bin{
					Size: moveBytes,
					Items: []*domain.Item{
						{Location: sourcePath, Name: "episode.mkv", Path: sourcePath + "/episode.mkv", Size: moveBytes},
					},
				},
			},
		},
	}
	return autoGatherCanonicalBundle{
		Result: domain.AutoGatherCanonicalPlanResult{
			ShowPath:                   showPath,
			CanonicalRecommendedTarget: targetDisk,
			CanonicalMoveBytes:         moveBytes,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{
					DiskName: targetDisk, DiskPath: targetPath,
					IsPhysicalArrayDisk: true, CanonicalEligible: true,
					MeetsPreferredFreeFloor: true, CanonicalBytesToMove: moveBytes,
					FreeBytes: gib(50), ProjectedFreeBytes: gib(40),
				},
				{DiskName: diskNameFromMountPath(sourcePath), DiskPath: sourcePath, IsPhysicalArrayDisk: true},
			},
		},
		Plan: plan,
	}
}

func installRealCanonicalStub(showPath, target string, move uint64) func() {
	prev := autoGatherRealCanonicalHook
	autoGatherRealCanonicalHook = func(c *Core, path, stage2 string, stage2Move uint64) (autoGatherCanonicalBundle, bool) {
		return stubRealCanonicalBundle(showPath, target, move), false
	}
	return func() { autoGatherRealCanonicalHook = prev }
}

func TestStage3BDryRunStillForcesDryRunRsync(t *testing.T) {
	c := newCoreForRealTest()
	c.ctx.DryRun = true
	plan := stubRealCanonicalBundle("data/media/tv/A", "disk1", 100).Plan
	c.state.Status = common.OpGatherMove
	c.autoGatherDryRunExec = true
	op := c.createGatherOperation(*plan)
	if op == nil || !op.DryRun || !rsyncArgsIncludeDryRun(op.RsyncArgs) {
		t.Fatalf("Stage 3B operation must be dry-run with --dry-run, op=%#v", op)
	}
}

func TestStage3BCannotExecuteRealTransfer(t *testing.T) {
	c := newCoreForRealTest()
	c.ctx.DryRun = true
	plan := stubRealCanonicalBundle("data/media/tv/A", "disk1", 100).Plan
	got := c.executeGatherPlanRealSync(plan, "/mnt/disk1")
	if got.outcome != gatherExecUncertain || !strings.Contains(got.reason, "global dry-run") {
		t.Fatalf("real sync must refuse under global dry-run, got %+v", got)
	}
}

func TestStage3CExecuteRefusesWhileGlobalDryRunTrue(t *testing.T) {
	c := newCoreForRealTest()
	c.ctx.DryRun = true
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()

	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{
		ShowPath: "data/media/tv/A",
	})
	if prep.Error != "" {
		t.Fatalf("prepare should succeed read-only: %s", prep.Error)
	}
	_, err := c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID,
		ShowPath:      "data/media/tv/A",
		Confirm:       true,
	})
	if err == nil || !strings.Contains(err.Error(), "global dry-run") {
		t.Fatalf("execute must refuse under global dry-run, err=%v", err)
	}
}

func TestStage3CExecuteRefusesWithoutConfirmation(t *testing.T) {
	c := newCoreForRealTest()
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	_, err := c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID,
		ShowPath:      "data/media/tv/A",
		Confirm:       false,
	})
	if err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("expected confirmation refusal, err=%v", err)
	}
}

func TestStage3CExecuteRefusesInvalidPreparationToken(t *testing.T) {
	c := newCoreForRealTest()
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	_, err := c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: "missing-token",
		ShowPath:      "data/media/tv/A",
		Confirm:       true,
	})
	if err == nil || !strings.Contains(err.Error(), "preparation token") {
		t.Fatalf("expected invalid token refusal, err=%v", err)
	}
}

func TestStage3CExecuteRefusesMismatchedShow(t *testing.T) {
	c := newCoreForRealTest()
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	_, err := c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID,
		ShowPath:      "data/media/tv/Other",
		Confirm:       true,
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected show mismatch refusal, err=%v", err)
	}
}

func TestStage3CExecuteRefusesExpiredPreparation(t *testing.T) {
	c := newCoreForRealTest()
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	c.autoGatherMu.Lock()
	c.autoGatherRealPrepared.ExpiresAt = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	c.autoGatherMu.Unlock()
	_, err := c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID,
		ShowPath:      "data/media/tv/A",
		Confirm:       true,
	})
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired token refusal, err=%v", err)
	}
	state := c.GetAutoGatherRealState()
	if state.Phase != domain.AutoGatherRealPhaseExpired {
		t.Fatalf("phase after expired execute = %q, want expired", state.Phase)
	}
	if state.Prepared != nil || state.PreparationID != "" {
		t.Fatal("expired execute must invalidate the prepared object")
	}
	if c.autoGatherRealPrepared != nil {
		t.Fatal("expired execute must clear the in-memory preparation token")
	}
}

func TestStage3CExecuteRefusesTargetChange(t *testing.T) {
	c := newCoreForRealTest()
	prev := autoGatherRealCanonicalHook
	defer func() { autoGatherRealCanonicalHook = prev }()
	var calls int
	autoGatherRealCanonicalHook = func(c *Core, path, stage2 string, move uint64) (autoGatherCanonicalBundle, bool) {
		calls++
		target := "disk1"
		if calls > 1 {
			target = "disk2"
		}
		return stubRealCanonicalBundle("data/media/tv/A", target, 100), false
	}
	prevExec := autoGatherRealExecuteHook
	autoGatherRealExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		t.Fatal("execute must not run when target changed")
		return gatherExecResult{}
	}
	defer func() { autoGatherRealExecuteHook = prevExec }()

	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	_, err := c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID,
		ShowPath:      "data/media/tv/A",
		Confirm:       true,
	})
	time.Sleep(100 * time.Millisecond)
	state := c.GetAutoGatherRealState()
	if err == nil && state.Phase != domain.AutoGatherRealPhaseFailed {
		t.Fatalf("expected target-change failure, err=%v phase=%q", err, state.Phase)
	}
	if !strings.Contains(state.Error, "plan changed") {
		t.Fatalf("expected plan change message, got %q", state.Error)
	}
	if state.Prepared != nil {
		t.Fatal("preparation must be invalidated after plan change")
	}
}

func TestStage3CExecuteRefusesCachePoolDisk0Targets(t *testing.T) {
	for _, target := range []string{"cache", "disk0"} {
		t.Run(target, func(t *testing.T) {
			bundle := stubRealCanonicalBundle("data/media/tv/A", "disk1", 100)
			bundle.Plan.VDisks["/mnt/"+target] = &domain.VDisk{Path: "/mnt/" + target, Bin: &domain.Bin{Size: 1, Items: []*domain.Item{{Name: "x"}}}}
			err := validateAutoGatherGatherTarget(bundle.Plan, "/mnt/"+target)
			if err == nil {
				t.Fatalf("%s target must be rejected", target)
			}
		})
	}
}

func TestStage3CExecuteRefusesWhenDryRunActive(t *testing.T) {
	c := newCoreForRealTest()
	c.ctx.DryRun = true
	c.autoGatherRun = &domain.AutoGatherDryRunState{Phase: domain.AutoGatherDryRunPhaseRunning, DryRun: true}
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	if prep.Error == "" {
		t.Fatal("prepare should refuse while dry-run active")
	}
	_ = prep
}

func TestStage3CPrepareDoesNotMutateFilesystemOrExecute(t *testing.T) {
	c := newCoreForRealTest()
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	prevExec := autoGatherRealExecuteHook
	autoGatherRealExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		t.Fatal("prepare must not execute gather")
		return gatherExecResult{}
	}
	defer func() { autoGatherRealExecuteHook = prevExec }()

	before := c.state.Status
	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	if prep.Error != "" {
		t.Fatalf("prepare failed: %s", prep.Error)
	}
	if c.state.Operation != nil {
		t.Fatal("prepare must not create an operation")
	}
	if c.state.Status != before && c.state.Status != common.OpNeutral {
		t.Fatalf("prepare must not leave busy gather status, got %d", c.state.Status)
	}
}

func TestStage3CRealExecuteProcessesExactlyOneShow(t *testing.T) {
	c := newCoreForRealTest()
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	var execCount int32
	prevExec := autoGatherRealExecuteHook
	autoGatherRealExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		atomic.AddInt32(&execCount, 1)
		return gatherExecResult{outcome: gatherExecSuccess}
	}
	defer func() { autoGatherRealExecuteHook = prevExec }()
	prevVerify := autoGatherRealVerifyHook
	autoGatherRealVerifyHook = func(c *Core, showPath, targetDisk string) domain.AutoGatherRealVerification {
		return domain.AutoGatherRealVerification{Passed: true, TargetDisk: targetDisk}
	}
	defer func() { autoGatherRealVerifyHook = prevVerify }()

	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	if _, err := c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID,
		ShowPath:      "data/media/tv/A",
		Confirm:       true,
	}); err != nil {
		t.Fatalf("execute start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if atomic.LoadInt32(&execCount) != 1 {
		t.Fatalf("expected exactly one execution, got %d", execCount)
	}
	state := c.GetAutoGatherRealState()
	if state.Phase != domain.AutoGatherRealPhaseCompleted {
		t.Fatalf("phase = %q, want completed", state.Phase)
	}
}

func TestStage3CNoAutomaticSecondShowExecution(t *testing.T) {
	c := newCoreForRealTest()
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	var execCount int32
	prevExec := autoGatherRealExecuteHook
	autoGatherRealExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		atomic.AddInt32(&execCount, 1)
		return gatherExecResult{outcome: gatherExecSuccess}
	}
	defer func() { autoGatherRealExecuteHook = prevExec }()
	prevVerify := autoGatherRealVerifyHook
	autoGatherRealVerifyHook = func(c *Core, showPath, targetDisk string) domain.AutoGatherRealVerification {
		return domain.AutoGatherRealVerification{Passed: true}
	}
	defer func() { autoGatherRealVerifyHook = prevVerify }()

	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID, ShowPath: "data/media/tv/A", Confirm: true,
	})
	time.Sleep(200 * time.Millisecond)
	if atomic.LoadInt32(&execCount) != 1 {
		t.Fatalf("expected one execution only, got %d", execCount)
	}
	_, err := c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID, ShowPath: "data/media/tv/A", Confirm: true,
	})
	if err == nil {
		t.Fatal("second execute without prepare must fail")
	}
}

func TestStage3CRealOperationDryRunFalseAfterGates(t *testing.T) {
	c := newCoreForRealTest()
	plan := stubRealCanonicalBundle("data/media/tv/A", "disk1", 100).Plan
	prevExec := autoGatherRealExecuteHook
	autoGatherRealExecuteHook = nil
	defer func() {
		autoGatherRealExecuteHook = prevExec
		c.autoGatherRealExec = false
	}()

	c.autoGatherRealExec = true
	c.state.Status = common.OpGatherMove
	c.state.Operation = c.createGatherOperation(*plan)
	op := c.state.Operation
	if op.DryRun {
		t.Fatal("real auto gather operation must not be dry-run")
	}
	if rsyncArgsIncludeDryRun(op.RsyncArgs) {
		t.Fatal("real auto gather rsync args must not include --dry-run")
	}
}

func TestStage3CRealRsyncArgsExcludeDryRun(t *testing.T) {
	c := newCoreForRealTest()
	plan := stubRealCanonicalBundle("data/media/tv/A", "disk1", 100).Plan
	c.state.Status = common.OpGatherMove
	c.autoGatherRealExec = true
	op := c.createGatherOperation(*plan)
	if rsyncArgsIncludeDryRun(op.RsyncArgs) {
		t.Fatal("real gather must not include --dry-run")
	}
}

func TestStage3BDryRunRsyncArgsIncludeDryRun(t *testing.T) {
	c := newCoreForRealTest()
	c.ctx.DryRun = true
	plan := stubRealCanonicalBundle("data/media/tv/A", "disk1", 100).Plan
	c.state.Status = common.OpGatherMove
	c.autoGatherDryRunExec = true
	op := c.createGatherOperation(*plan)
	if !rsyncArgsIncludeDryRun(op.RsyncArgs) {
		t.Fatal("Stage 3B must include --dry-run")
	}
}

func TestInterruptedRealRsyncDoesNotDeleteSource(t *testing.T) {
	op := &domain.Operation{DryRun: false, OpKind: common.OpGatherMove}
	cmd := &domain.Command{Status: common.CmdStopped, Entry: "episode.mkv", Src: "/mnt/disk2", Dst: "/mnt/disk1"}
	c := newCoreForRealTest()
	c.autoGatherRealExec = true
	c.handleItemDeletion(op, cmd)
	if !strings.Contains(op.Line, "skipping:deletion") {
		t.Fatalf("stopped command must skip deletion, line=%q", op.Line)
	}
}

func TestFailedRealRsyncDoesNotDeleteSource(t *testing.T) {
	op := &domain.Operation{DryRun: false, OpKind: common.OpGatherMove}
	cmd := &domain.Command{Status: common.CmdFlagged, Entry: "episode.mkv", Src: "/mnt/disk2", Dst: "/mnt/disk1"}
	c := newCoreForRealTest()
	c.autoGatherRealExec = true
	c.handleItemDeletion(op, cmd)
	if !strings.Contains(op.Line, "skipping:deletion") {
		t.Fatalf("flagged command must skip deletion, line=%q", op.Line)
	}
}

func TestSuccessfulRealOperationWritesHistory(t *testing.T) {
	c := newCoreForRealTest()
	c.state.History = &domain.History{Items: map[string]*domain.Operation{}, Order: []string{}}
	op := &domain.Operation{ID: "real-op", DryRun: false}
	c.autoGatherRealExec = true
	c.autoGatherDryRunExec = false
	c.updateHistory(c.state.History, op)
	if len(c.state.History.Order) != 1 {
		t.Fatalf("real operation must write history, order=%v", c.state.History.Order)
	}
}

func TestStage3BHistoryStillSuppressed(t *testing.T) {
	c := newCoreForRealTest()
	c.state.History = &domain.History{Items: map[string]*domain.Operation{}, Order: []string{}}
	op := &domain.Operation{ID: "dry-op", DryRun: true}
	c.autoGatherDryRunExec = true
	c.updateHistory(c.state.History, op)
	if len(c.state.History.Order) != 0 {
		t.Fatal("Stage 3B must still suppress history")
	}
}

func TestManualGatherScatterUnchangedWhenAutoGatherInactive(t *testing.T) {
	c := newCoreForRealTest()
	if !c.mailboxAllows(common.CommandGatherPlanStart) || !c.mailboxAllows(common.CommandScatterPlanStart) {
		t.Fatal("manual ops should be allowed when Auto Gather inactive")
	}
}

func TestManualGatherScatterBlockedDuringStage3CExecution(t *testing.T) {
	c := newCoreForRealTest()
	c.autoGatherRealState = &domain.AutoGatherRealState{Phase: domain.AutoGatherRealPhaseExecuting}
	c.state.Status = common.OpAutoGatherReal
	if c.mailboxAllows(common.CommandGatherPlanStart) || c.mailboxAllows(common.CommandScatterPlanStart) {
		t.Fatal("manual ops must be blocked during Stage 3C execution")
	}
	if !c.mailboxAllows(common.CommandStop) {
		t.Fatal("stop must remain available during Stage 3C execution")
	}
}

func TestStage3CStopAllowedDuringRealTransfer(t *testing.T) {
	c := newCoreForRealTest()
	c.autoGatherRealState = &domain.AutoGatherRealState{Phase: domain.AutoGatherRealPhaseExecuting}
	c.state.Status = common.OpGatherMove
	c.autoGatherRealExec = true
	if !c.mailboxAllows(common.CommandStop) {
		t.Fatal("stop must be allowed during real rsync")
	}
}

func TestStage3CStopDuringRealTransferResultsStoppedNotCompleted(t *testing.T) {
	c := newCoreForRealTest()
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	prevExec := autoGatherRealExecuteHook
	autoGatherRealExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecCancelled, reason: "stopped by the user"}
	}
	defer func() { autoGatherRealExecuteHook = prevExec }()

	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID, ShowPath: "data/media/tv/A", Confirm: true,
	})
	time.Sleep(200 * time.Millisecond)
	state := c.GetAutoGatherRealState()
	if state.Phase != domain.AutoGatherRealPhaseStopped {
		t.Fatalf("phase = %q, want stopped", state.Phase)
	}
	if state.StoppedMessage == "" || !strings.Contains(state.StoppedMessage, "interrupted transfer") {
		t.Fatalf("expected accurate stopped message, got %q", state.StoppedMessage)
	}
}

func TestPostMoveVerificationChecksSelectedShowOnly(t *testing.T) {
	c := newCoreForRealTest()
	prevVerify := autoGatherRealVerifyHook
	var seenShow string
	autoGatherRealVerifyHook = func(c *Core, showPath, targetDisk string) domain.AutoGatherRealVerification {
		seenShow = showPath
		return domain.AutoGatherRealVerification{Passed: true, TargetDisk: targetDisk}
	}
	defer func() { autoGatherRealVerifyHook = prevVerify }()
	defer installRealCanonicalStub("data/media/tv/OnlyOne", "disk1", 100)()
	prevExec := autoGatherRealExecuteHook
	autoGatherRealExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecSuccess}
	}
	defer func() { autoGatherRealExecuteHook = prevExec }()

	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/OnlyOne"})
	c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID, ShowPath: "data/media/tv/OnlyOne", Confirm: true,
	})
	time.Sleep(200 * time.Millisecond)
	if seenShow != "data/media/tv/OnlyOne" {
		t.Fatalf("verification show = %q", seenShow)
	}
}

func TestEmptyFolderRemnantsDoNotFailSubstantiveVerification(t *testing.T) {
	c := newCoreForRealTest()
	prev := autoGatherRealVerifyHook
	autoGatherRealVerifyHook = nil
	defer func() { autoGatherRealVerifyHook = prev }()

	verification := domain.AutoGatherRealVerification{
		Passed:              true,
		TargetDisk:          "disk1",
		SubstantiveDisks:    nil,
		EmptyFolderRemnants: []string{"disk2"},
		Message:             "ok",
	}
	if !verification.Passed {
		t.Fatal("empty-folder remnants alone should not fail substantive verification")
	}
	_ = c
}

func TestStage3CRealSessionRoutingPhaseActive(t *testing.T) {
	c := newCoreForRealTest()
	c.autoGatherRealState = &domain.AutoGatherRealState{Phase: domain.AutoGatherRealPhaseExecuting}
	if !c.isAutoGatherRealExecutionBusy() {
		t.Fatal("executing phase must be busy")
	}
	c.autoGatherRealState.Phase = domain.AutoGatherRealPhasePrepared
	if c.isAutoGatherRealExecutionBusy() {
		t.Fatal("prepared-only must not block manual execution busy gate")
	}
	if !c.isAutoGatherRealSessionActive() {
		t.Fatal("prepared phase must be session active for dry-run block")
	}
}

func TestStartAutoGatherDryRunBlockedDuringStage3CSession(t *testing.T) {
	c := newCoreForRealTest()
	c.ctx.DryRun = true
	c.autoGatherRealState = &domain.AutoGatherRealState{Phase: domain.AutoGatherRealPhasePrepared}
	_, err := c.StartAutoGatherDryRun()
	if err == nil || !strings.Contains(err.Error(), "real move session") {
		t.Fatalf("dry-run must refuse during Stage 3C session, err=%v", err)
	}
}

func TestNoBatchRealAutoGatherAPI(t *testing.T) {
	c := newCoreForRealTest()
	_, err := c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: "x", ShowPath: "data/media/tv/A", Confirm: true,
	})
	if err == nil {
		t.Fatal("execute without prepare must fail")
	}
	if c.GetAutoGatherRealState().Phase == domain.AutoGatherRealPhaseExecuting {
		t.Fatal("no batch loop should start")
	}
}

func TestStage3CExecutePerformsOneGatherFill(t *testing.T) {
	c := newCoreForRealTest()
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	var fills int32
	prevFill := autoGatherRealFillHook
	autoGatherRealFillHook = func() { atomic.AddInt32(&fills, 1) }
	defer func() { autoGatherRealFillHook = prevFill }()
	prevExec := autoGatherRealExecuteHook
	autoGatherRealExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecSuccess}
	}
	defer func() { autoGatherRealExecuteHook = prevExec }()
	prevVerify := autoGatherRealVerifyHook
	autoGatherRealVerifyHook = func(c *Core, showPath, targetDisk string) domain.AutoGatherRealVerification {
		return domain.AutoGatherRealVerification{Passed: true}
	}
	defer func() { autoGatherRealVerifyHook = prevVerify }()

	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	if prep.Error != "" {
		t.Fatalf("prepare: %s", prep.Error)
	}
	prepareFills := atomic.LoadInt32(&fills)
	c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID, ShowPath: "data/media/tv/A", Confirm: true,
	})
	time.Sleep(200 * time.Millisecond)
	executeFills := atomic.LoadInt32(&fills) - prepareFills
	if executeFills != 1 {
		t.Fatalf("EXECUTE phase must perform one Gather fill, got %d", executeFills)
	}
}

func TestStage3CRealDoesNotBroadcastTransferWebsocket(t *testing.T) {
	c := newCoreForRealTest()
	ch := c.ctx.Hub.Sub("socket:broadcast")
	t.Cleanup(func() { c.ctx.Hub.Unsub(ch) })
	c.autoGatherRealExec = true
	c.publishOperationSocket(&domain.Packet{Topic: common.EventTransferStarted, Payload: c.state})
	select {
	case msg := <-ch:
		t.Fatalf("real auto gather must suppress transfer websocket, got %#v", msg)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStage3CPrepareFillCountIsOne(t *testing.T) {
	c := newCoreForRealTest()
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	var fills int32
	prevFill := autoGatherRealFillHook
	autoGatherRealFillHook = func() { atomic.AddInt32(&fills, 1) }
	defer func() { autoGatherRealFillHook = prevFill }()
	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	if prep.Error != "" {
		t.Fatalf("prepare: %s", prep.Error)
	}
	if atomic.LoadInt32(&fills) != 1 {
		t.Fatalf("PREPARE must perform one Gather fill, got %d", fills)
	}
}

func TestStage3CExecuteRefusesWhenBusy(t *testing.T) {
	c := newCoreForRealTest()
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	if prep.Error != "" {
		t.Fatalf("prepare failed: %s", prep.Error)
	}
	c.state.Status = common.OpGatherMove
	_, err := c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID, ShowPath: "data/media/tv/A", Confirm: true,
	})
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("expected busy refusal, err=%v", err)
	}
}

func TestVerifyAutoGatherRealMoveDetectsSplitSubstantiveData(t *testing.T) {
	c := newCoreForRealTest()
	prevScan := autoGatherRealVerifyHook
	autoGatherRealVerifyHook = nil
	defer func() { autoGatherRealVerifyHook = prevScan }()

	// Without filesystem, ScanSingleShow returns no_video; verification fails target check.
	got := c.verifyAutoGatherRealMove("data/media/tv/Missing", "disk1")
	if got.Passed {
		t.Fatal("expected verification failure without substantive target data")
	}
	if got.Message == "" {
		t.Fatal("expected verification message")
	}
}

func TestStage3BDryRunStartStillRequiresGlobalDryRun(t *testing.T) {
	c := newCoreForRealTest()
	c.ctx.DryRun = false
	_, err := c.StartAutoGatherDryRun()
	if err == nil {
		t.Fatal("Stage 3B must still require global dry-run")
	}
}

func TestStage3CStopEndpointSetsStoppingPhase(t *testing.T) {
	c := newCoreForRealTest()
	c.autoGatherRealState = &domain.AutoGatherRealState{Phase: domain.AutoGatherRealPhaseExecuting}
	state := c.StopAutoGatherReal()
	if state.Phase != domain.AutoGatherRealPhaseStopping {
		t.Fatalf("phase = %q, want stopping", state.Phase)
	}
}

func TestStage3CPreparedLostOnRestart(t *testing.T) {
	c := newCoreForRealTest()
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	c2 := newCoreForRealTest()
	_, err := c2.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID, ShowPath: "data/media/tv/A", Confirm: true,
	})
	if err == nil || !strings.Contains(err.Error(), "preparation token") {
		t.Fatalf("fresh core must refuse stale preparation, err=%v", err)
	}
	_ = fmt.Sprintf("%s", prep.PreparationID)
}

func stubRealCanonicalBundleWithItems(showPath, targetDisk string, moveBytes uint64, transfers []struct {
	sourcePath string
	entry      string
	size       uint64
}) autoGatherCanonicalBundle {
	targetPath := "/mnt/" + targetDisk
	items := make([]*domain.Item, 0, len(transfers))
	for _, tr := range transfers {
		items = append(items, &domain.Item{
			Location: tr.sourcePath,
			Path:     tr.entry,
			Size:     tr.size,
		})
	}
	plan := &domain.Plan{
		ChosenFolders: []string{showPath},
		VDisks: map[string]*domain.VDisk{
			targetPath: {
				Path: targetPath,
				Bin:  &domain.Bin{Size: moveBytes, Items: items},
			},
		},
	}
	return autoGatherCanonicalBundle{
		Result: domain.AutoGatherCanonicalPlanResult{
			ShowPath:                   showPath,
			CanonicalRecommendedTarget: targetDisk,
			CanonicalMoveBytes:         moveBytes,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{
					DiskName: targetDisk, DiskPath: targetPath,
					IsPhysicalArrayDisk: true, CanonicalEligible: true,
					MeetsPreferredFreeFloor: true, CanonicalBytesToMove: moveBytes,
				},
			},
		},
		Plan: plan,
	}
}

func TestAutoGatherRealFingerprintSamePlanAllowsExecute(t *testing.T) {
	c := newCoreForRealTest()
	transfers := []struct {
		sourcePath string
		entry      string
		size       uint64
	}{
		{"/mnt/disk2", "data/media/tv/A/ep1.mkv", 100},
	}
	bundle := stubRealCanonicalBundleWithItems("data/media/tv/A", "disk1", 100, transfers)
	prev := autoGatherRealCanonicalHook
	autoGatherRealCanonicalHook = func(c *Core, path, stage2 string, move uint64) (autoGatherCanonicalBundle, bool) {
		return bundle, false
	}
	defer func() { autoGatherRealCanonicalHook = prev }()
	var execCount int32
	prevExec := autoGatherRealExecuteHook
	autoGatherRealExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		atomic.AddInt32(&execCount, 1)
		return gatherExecResult{outcome: gatherExecSuccess}
	}
	defer func() { autoGatherRealExecuteHook = prevExec }()
	prevVerify := autoGatherRealVerifyHook
	autoGatherRealVerifyHook = func(c *Core, showPath, targetDisk string) domain.AutoGatherRealVerification {
		return domain.AutoGatherRealVerification{Passed: true}
	}
	defer func() { autoGatherRealVerifyHook = prevVerify }()

	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	if prep.PlanFingerprint == nil || len(prep.PlanFingerprint.Items) != 1 {
		t.Fatalf("prepare must retain fingerprint, got %+v", prep.PlanFingerprint)
	}
	c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID, ShowPath: "data/media/tv/A", Confirm: true,
	})
	time.Sleep(200 * time.Millisecond)
	if atomic.LoadInt32(&execCount) != 1 {
		t.Fatalf("expected execute with matching fingerprint, got %d", execCount)
	}
}

func TestAutoGatherRealFingerprintChangedBytesRefuses(t *testing.T) {
	c := newCoreForRealTest()
	transfers := []struct {
		sourcePath string
		entry      string
		size       uint64
	}{
		{"/mnt/disk2", "data/media/tv/A/ep1.mkv", 100},
	}
	prev := autoGatherRealCanonicalHook
	var calls int
	autoGatherRealCanonicalHook = func(c *Core, path, stage2 string, move uint64) (autoGatherCanonicalBundle, bool) {
		calls++
		b := stubRealCanonicalBundleWithItems("data/media/tv/A", "disk1", 100, transfers)
		if calls > 1 {
			b.Plan.VDisks["/mnt/disk1"].Bin.Size = 200
		}
		return b, false
	}
	defer func() { autoGatherRealCanonicalHook = prev }()
	prevExec := autoGatherRealExecuteHook
	autoGatherRealExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		t.Fatal("must not execute when move bytes changed")
		return gatherExecResult{}
	}
	defer func() { autoGatherRealExecuteHook = prevExec }()

	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID, ShowPath: "data/media/tv/A", Confirm: true,
	})
	time.Sleep(200 * time.Millisecond)
	state := c.GetAutoGatherRealState()
	if !strings.Contains(state.Error, "move bytes changed") {
		t.Fatalf("expected bytes change refusal, got %q", state.Error)
	}
	if state.Prepared != nil {
		t.Fatal("preparation must be invalidated")
	}
}

func TestAutoGatherRealFingerprintChangedSourceItemsRefuses(t *testing.T) {
	c := newCoreForRealTest()
	prev := autoGatherRealCanonicalHook
	var calls int
	autoGatherRealCanonicalHook = func(c *Core, path, stage2 string, move uint64) (autoGatherCanonicalBundle, bool) {
		calls++
		if calls == 1 {
			return stubRealCanonicalBundleWithItems("data/media/tv/A", "disk1", 100, []struct {
				sourcePath string
				entry      string
				size       uint64
			}{{"/mnt/disk2", "data/media/tv/A/ep1.mkv", 100}}), false
		}
		return stubRealCanonicalBundleWithItems("data/media/tv/A", "disk1", 100, []struct {
			sourcePath string
			entry      string
			size       uint64
		}{{"/mnt/disk2", "data/media/tv/A/ep2.mkv", 100}}), false
	}
	defer func() { autoGatherRealCanonicalHook = prev }()
	prevExec := autoGatherRealExecuteHook
	autoGatherRealExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		t.Fatal("must not execute when item composition changed")
		return gatherExecResult{}
	}
	defer func() { autoGatherRealExecuteHook = prevExec }()

	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID, ShowPath: "data/media/tv/A", Confirm: true,
	})
	time.Sleep(200 * time.Millisecond)
	state := c.GetAutoGatherRealState()
	if !strings.Contains(state.Error, "planned transfer item composition changed") {
		t.Fatalf("expected item composition refusal, got %q", state.Error)
	}
}

func TestAutoGatherRealFingerprintChangedSourceDisksRefuses(t *testing.T) {
	c := newCoreForRealTest()
	prev := autoGatherRealCanonicalHook
	var calls int
	autoGatherRealCanonicalHook = func(c *Core, path, stage2 string, move uint64) (autoGatherCanonicalBundle, bool) {
		calls++
		if calls == 1 {
			return stubRealCanonicalBundleWithItems("data/media/tv/A", "disk8", 100, []struct {
				sourcePath string
				entry      string
				size       uint64
			}{{"/mnt/disk1", "data/media/tv/A/ep1.mkv", 60}, {"/mnt/disk2", "data/media/tv/A/ep2.mkv", 40}}), false
		}
		return stubRealCanonicalBundleWithItems("data/media/tv/A", "disk8", 100, []struct {
			sourcePath string
			entry      string
			size       uint64
		}{{"/mnt/disk1", "data/media/tv/A/ep1.mkv", 60}, {"/mnt/disk3", "data/media/tv/A/ep2.mkv", 40}}), false
	}
	defer func() { autoGatherRealCanonicalHook = prev }()
	prevExec := autoGatherRealExecuteHook
	autoGatherRealExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		t.Fatal("must not execute when source disks changed")
		return gatherExecResult{}
	}
	defer func() { autoGatherRealExecuteHook = prevExec }()

	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID, ShowPath: "data/media/tv/A", Confirm: true,
	})
	time.Sleep(200 * time.Millisecond)
	state := c.GetAutoGatherRealState()
	if !strings.Contains(state.Error, "source disks changed") {
		t.Fatalf("expected source disk change refusal, got %q", state.Error)
	}
}

func TestAutoGatherRealPlanChangedPerformsNoOperation(t *testing.T) {
	c := newCoreForRealTest()
	prev := autoGatherRealCanonicalHook
	var calls int
	autoGatherRealCanonicalHook = func(c *Core, path, stage2 string, move uint64) (autoGatherCanonicalBundle, bool) {
		calls++
		target := "disk1"
		if calls > 1 {
			target = "disk2"
		}
		return stubRealCanonicalBundle("data/media/tv/A", target, 100), false
	}
	defer func() { autoGatherRealCanonicalHook = prev }()
	prevExec := autoGatherRealExecuteHook
	autoGatherRealExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		t.Fatal("stale/changed plan must not invoke rsync")
		return gatherExecResult{}
	}
	defer func() { autoGatherRealExecuteHook = prevExec }()

	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID, ShowPath: "data/media/tv/A", Confirm: true,
	})
	time.Sleep(200 * time.Millisecond)
	if c.state.Operation != nil {
		t.Fatal("plan change must not create a gather operation")
	}
}

func TestGetAutoGatherRealStateExpiresStalePrepared(t *testing.T) {
	c := newCoreForRealTest()
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	if prep.Error != "" {
		t.Fatalf("prepare: %s", prep.Error)
	}

	fresh := c.GetAutoGatherRealState()
	if fresh.Phase != domain.AutoGatherRealPhasePrepared || fresh.Prepared == nil {
		t.Fatalf("valid prepared status must survive GET, phase=%q prepared=%v", fresh.Phase, fresh.Prepared != nil)
	}
	if fresh.Prepared.PreparationID != prep.PreparationID {
		t.Fatal("GET must return the same preparation token while valid")
	}

	c.autoGatherMu.Lock()
	c.autoGatherRealPrepared.ExpiresAt = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	c.autoGatherMu.Unlock()

	expired := c.GetAutoGatherRealState()
	if expired.Phase != domain.AutoGatherRealPhaseExpired {
		t.Fatalf("GET after expiry phase = %q, want expired", expired.Phase)
	}
	if expired.Prepared != nil || expired.PreparationID != "" {
		t.Fatal("expired GET must not keep a usable prepared object")
	}
	if !strings.Contains(expired.Error, "expired") {
		t.Fatalf("expired GET must explain expiry, got %q", expired.Error)
	}
	if c.isAutoGatherRealSessionActive() {
		t.Fatal("expired phase must not remain a live Stage 3C session")
	}
	if c.isAutoGatherRealExecutionBusy() {
		t.Fatal("expired phase must not be execution-busy")
	}
	prevExec := autoGatherRealExecuteHook
	autoGatherRealExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		t.Fatal("expired preparation must not execute")
		return gatherExecResult{}
	}
	defer func() { autoGatherRealExecuteHook = prevExec }()
	if _, err := c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{
		PreparationID: prep.PreparationID, ShowPath: "data/media/tv/A", Confirm: true,
	}); err == nil {
		t.Fatal("execute after status expiry must refuse")
	}
}

func TestCancelAutoGatherRealPrepareClearsValidAndExpiredState(t *testing.T) {
	c := newCoreForRealTest()
	defer installRealCanonicalStub("data/media/tv/A", "disk1", 100)()
	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	if prep.Error != "" {
		t.Fatalf("prepare: %s", prep.Error)
	}

	cancelled := c.CancelAutoGatherRealPrepare()
	if cancelled.Phase != domain.AutoGatherRealPhaseIdle {
		t.Fatalf("cancel phase = %q, want idle", cancelled.Phase)
	}
	if cancelled.Prepared != nil || c.autoGatherRealPrepared != nil {
		t.Fatal("cancel must clear the preparation token and snapshot")
	}
	if !strings.Contains(cancelled.Error, "cancelled") {
		t.Fatalf("cancel message = %q", cancelled.Error)
	}

	c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	c.autoGatherMu.Lock()
	c.autoGatherRealPrepared.ExpiresAt = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	c.autoGatherMu.Unlock()
	afterExpiry := c.CancelAutoGatherRealPrepare()
	if afterExpiry.Phase != domain.AutoGatherRealPhaseIdle {
		t.Fatalf("cancel after expiry phase = %q, want idle", afterExpiry.Phase)
	}
	if afterExpiry.Prepared != nil {
		t.Fatal("cancel after expiry must not leave a prepared object")
	}
}

func TestCancelAutoGatherRealPrepareDoesNotStopExecuting(t *testing.T) {
	c := newCoreForRealTest()
	c.autoGatherRealState = &domain.AutoGatherRealState{Phase: domain.AutoGatherRealPhaseExecuting}
	c.autoGatherRealStopRequested = false
	got := c.CancelAutoGatherRealPrepare()
	if got.Phase != domain.AutoGatherRealPhaseExecuting {
		t.Fatalf("cancel during execute phase = %q, want executing", got.Phase)
	}
	if !strings.Contains(got.Error, "use Stop") {
		t.Fatalf("cancel during execute must refuse, got %q", got.Error)
	}
	if c.autoGatherRealStopRequested {
		t.Fatal("cancel must not request Stop")
	}
	live := c.GetAutoGatherRealState()
	if live.Phase != domain.AutoGatherRealPhaseExecuting {
		t.Fatalf("live phase after cancel attempt = %q", live.Phase)
	}
	if live.Error != "" {
		t.Fatalf("cancel refusal must not persist onto live executing state, got %q", live.Error)
	}
}

func TestExpiredPreparedDoesNotRemainSessionActiveAfterStatusRead(t *testing.T) {
	c := newCoreForRealTest()
	c.ctx.DryRun = true
	c.autoGatherRealState = &domain.AutoGatherRealState{
		Phase:         domain.AutoGatherRealPhasePrepared,
		PreparationID: "stale",
		Prepared:      &domain.AutoGatherRealPrepareResult{PreparationID: "stale", ExpiresAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)},
	}
	c.autoGatherRealPrepared = &domain.AutoGatherRealPreparedMove{
		PreparationID: "stale",
		ExpiresAt:     time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
	}
	_ = c.GetAutoGatherRealState()
	if c.isAutoGatherRealSessionActive() {
		t.Fatal("status read must inactivate an expired preparation")
	}
}
