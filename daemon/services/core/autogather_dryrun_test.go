package core

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cskr/pubsub"

	"unbalance/daemon/autogather"
	"unbalance/daemon/common"
	"unbalance/daemon/domain"
)

func TestStartAutoGatherDryRunRefusesWithoutGlobalDryRun(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	c.ctx.DryRun = false
	_, err := c.StartAutoGatherDryRun()
	if err == nil {
		t.Fatal("expected refusal when global dry-run is disabled")
	}
	if c.state.Status != common.OpNeutral {
		t.Fatalf("status = %d, want neutral", c.state.Status)
	}
}

func TestStartAutoGatherDryRunRefusesWhenBusy(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	c.ctx.DryRun = true
	c.state.Status = common.OpGatherMove
	_, err := c.StartAutoGatherDryRun()
	if err == nil {
		t.Fatal("expected busy refusal")
	}
}

func TestManualGatherScatterBlockedWhileDryRunActive(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	c.ctx.DryRun = true
	c.state.Status = common.OpAutoGatherDryRun
	c.autoGatherRun = &domain.AutoGatherDryRunState{
		Phase:  domain.AutoGatherDryRunPhaseRunning,
		DryRun: true,
	}

	if c.mailboxAllows(common.CommandGatherPlanStart) {
		t.Fatal("gather plan should be blocked during dry-run")
	}
	if c.mailboxAllows(common.CommandScatterPlanStart) {
		t.Fatal("scatter plan should be blocked during dry-run")
	}
	if !c.mailboxAllows(common.CommandStop) {
		t.Fatal("stop should remain available during dry-run")
	}

	c.gatherPlanPrepare(domain.GatherSetup{Selected: []string{"data/media/tv/A"}})
	if c.state.Status != common.OpAutoGatherDryRun {
		t.Fatalf("gatherPlanPrepare must not take over status; got %d", c.state.Status)
	}
}

func TestResolveAutoGatherExecutionTargetPrefersValidStage2(t *testing.T) {
	canonical := domain.AutoGatherCanonicalPlanResult{
		Stage2TargetStillCanonicalEligible: true,
		CanonicalRecommendedTarget:         "disk8",
		CanonicalTargets: []domain.AutoGatherCanonicalTarget{
			{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true},
			{DiskName: "disk8", DiskPath: "/mnt/disk8", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true},
		},
	}
	disk, path, _, err := resolveAutoGatherExecutionTarget(canonical, "disk1")
	if err != nil || disk != "disk1" || path != "/mnt/disk1" {
		t.Fatalf("want stage2 disk1, got %q %q err=%v", disk, path, err)
	}
}

func TestResolveAutoGatherExecutionTargetFallsBackToBalanced(t *testing.T) {
	canonical := domain.AutoGatherCanonicalPlanResult{
		Stage2TargetStillCanonicalEligible: false,
		CanonicalRecommendedTarget:         "disk8",
		CanonicalTargets: []domain.AutoGatherCanonicalTarget{
			{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: false},
			{DiskName: "disk8", DiskPath: "/mnt/disk8", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true},
		},
	}
	disk, _, _, err := resolveAutoGatherExecutionTarget(canonical, "disk1")
	if err != nil || disk != "disk8" {
		t.Fatalf("want balanced disk8, got %q err=%v", disk, err)
	}
}

func TestResolveAutoGatherExecutionTargetRejectsCacheDespiteRawBin(t *testing.T) {
	canonical := domain.AutoGatherCanonicalPlanResult{
		CanonicalRecommendedTarget: "cache",
		CanonicalTargets: []domain.AutoGatherCanonicalTarget{
			{DiskName: "cache", DiskPath: "/mnt/cache", IsPhysicalArrayDisk: false, CanonicalEligible: false, RawGatherBinPresent: true},
		},
	}
	_, _, _, err := resolveAutoGatherExecutionTarget(canonical, "cache")
	if err == nil {
		t.Fatal("cache must not be an execution target")
	}
}

func TestValidateAutoGatherGatherTargetRequiresBinAndArrayDisk(t *testing.T) {
	plan := &domain.Plan{
		VDisks: map[string]*domain.VDisk{
			"/mnt/disk1": {Bin: &domain.Bin{Size: 10}},
			"/mnt/cache": {Bin: &domain.Bin{Size: 10}},
			"/mnt/disk0": {Bin: &domain.Bin{Size: 10}},
		},
	}
	if err := validateAutoGatherGatherTarget(plan, "/mnt/disk8"); err == nil {
		t.Fatal("missing bin should fail")
	}
	if err := validateAutoGatherGatherTarget(plan, "/mnt/cache"); err == nil {
		t.Fatal("cache must be rejected")
	}
	if err := validateAutoGatherGatherTarget(plan, "/mnt/disk0"); err == nil {
		t.Fatal("disk0 must be rejected")
	}
	if err := validateAutoGatherGatherTarget(plan, "/mnt/disk1"); err != nil {
		t.Fatalf("disk1 with bin should pass: %v", err)
	}
}

func TestSelectNextDryRunShowPrefersSmallestAboveFloorMove(t *testing.T) {
	scan := domain.AutoGatherScanResult{
		Shows: []domain.AutoGatherShow{
			{
				Name: "Big", Path: "data/media/tv/Big", Status: domain.AutoGatherStatusSplit,
				RecommendedTargetDisk: "disk8", MoveRequiredBytes: gib(50),
			},
			{
				Name: "Small", Path: "data/media/tv/Small", Status: domain.AutoGatherStatusSplit,
				RecommendedTargetDisk: "disk1", MoveRequiredBytes: gib(5),
			},
		},
	}
	pick, remaining, cancelled := selectNextDryRunShow(scan, map[string]struct{}{}, map[string]struct{}{}, nil)
	if cancelled {
		t.Fatal("unexpected cancellation")
	}
	if remaining != 2 || pick == nil || pick.show.Path != "data/media/tv/Small" {
		t.Fatalf("want smallest Stage 2 move show, got %+v remaining=%d", pick, remaining)
	}
	if pick.stage2Target != "disk1" || pick.stage2Move != gib(5) {
		t.Fatalf("stage2 fields = target %q move %d", pick.stage2Target, pick.stage2Move)
	}
}

func TestSelectNextDryRunShowSkipsNoEligibleWithoutExecute(t *testing.T) {
	scan := domain.AutoGatherScanResult{
		Shows: []domain.AutoGatherShow{
			{
				Name: "NoTarget", Path: "data/media/tv/NoTarget", Status: domain.AutoGatherStatusSplit,
				NoEligibleReason: "insufficient free space on all eligible physical array disks",
			},
		},
	}
	pick, _, cancelled := selectNextDryRunShow(scan, map[string]struct{}{}, map[string]struct{}{}, nil)
	if cancelled {
		t.Fatal("unexpected cancellation")
	}
	if pick == nil || !pick.skipOnly {
		t.Fatalf("expected skip-only pick, got %+v", pick)
	}
}

func TestSelectNextDryRunShowDoesNotInvokeCanonicalPlanning(t *testing.T) {
	scan := domain.AutoGatherScanResult{
		Shows: make([]domain.AutoGatherShow, 0, 5),
	}
	for i := 0; i < 5; i++ {
		scan.Shows = append(scan.Shows, domain.AutoGatherShow{
			Name:                  string(rune('A' + i)),
			Path:                  "data/media/tv/" + string(rune('A'+i)),
			Status:                domain.AutoGatherStatusSplit,
			RecommendedTargetDisk: "disk1",
			MoveRequiredBytes:     uint64(100 - i),
		})
	}
	// Stage 2 selection must not require a canonical callback at all.
	pick, remaining, cancelled := selectNextDryRunShow(scan, map[string]struct{}{}, map[string]struct{}{}, nil)
	if cancelled || pick == nil || remaining != 5 {
		t.Fatalf("pick=%+v remaining=%d cancelled=%v", pick, remaining, cancelled)
	}
	if pick.show.Path != "data/media/tv/E" { // smallest MoveRequiredBytes = 96
		t.Fatalf("want E (smallest stage2 move), got %s", pick.show.Path)
	}
}

func TestSelectNextDryRunShowPrefersAboveFloorOverSmallerBelowFloor(t *testing.T) {
	scan := domain.AutoGatherScanResult{
		Shows: []domain.AutoGatherShow{
			{
				Name: "TinyBelow", Path: "data/media/tv/TinyBelow", Status: domain.AutoGatherStatusSplit,
				RecommendedTargetDisk: "disk1", MoveRequiredBytes: 1, BelowPreferredFreeFloor: true,
			},
			{
				Name: "LargerAbove", Path: "data/media/tv/LargerAbove", Status: domain.AutoGatherStatusSplit,
				RecommendedTargetDisk: "disk2", MoveRequiredBytes: 100, BelowPreferredFreeFloor: false,
			},
		},
	}
	pick, _, cancelled := selectNextDryRunShow(scan, map[string]struct{}{}, map[string]struct{}{}, nil)
	if cancelled || pick == nil || pick.show.Path != "data/media/tv/LargerAbove" {
		t.Fatalf("want above-floor candidate, got %+v", pick)
	}
}

func TestClassifyGatherDryRunResultFlagsUncertain(t *testing.T) {
	op := &domain.Operation{
		DryRun: true,
		Commands: []*domain.Command{
			{Status: common.CmdFlagged, Reason: "partial"},
		},
	}
	got := classifyGatherDryRunResult(op, 10, false)
	if got.outcome != gatherExecUncertain {
		t.Fatalf("flagged must stop loop, got %+v", got)
	}
}

func TestDryRunLoopProcessesMultipleShowsServerSide(t *testing.T) {
	prevExec := autoGatherDryRunExecuteHook
	prevScan := autoGatherDryRunScanHook
	prevRefresh := autoGatherDryRunRefreshHook
	prevRescore := autoGatherDryRunRescoreHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunRefreshHook = prevRefresh
		autoGatherDryRunRescoreHook = prevRescore
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
	}()

	var execCount int32
	var scanCalls int32
	autoGatherDryRunExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		atomic.AddInt32(&execCount, 1)
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 100}
	}
	shows := []domain.AutoGatherShow{
		{Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 100},
		{Name: "B", Path: "data/media/tv/B", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 50},
	}
	var refreshCalls int32
	var rescoreCalls int32
	autoGatherDryRunScanHook = func(c *Core) domain.AutoGatherScanResult {
		atomic.AddInt32(&scanCalls, 1)
		return domain.AutoGatherScanResult{Shows: append([]domain.AutoGatherShow(nil), shows...)}
	}
	installStage2RescoreFromScanShows(shows)
	prevRescoreInner := autoGatherDryRunRescoreHook
	autoGatherDryRunRescoreHook = func(c *Core, base domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult {
		atomic.AddInt32(&rescoreCalls, 1)
		return prevRescoreInner(c, base, unraid)
	}
	autoGatherDryRunRefreshHook = func(c *Core) *domain.Unraid {
		atomic.AddInt32(&refreshCalls, 1)
		return c.state.Unraid
	}
	autoGatherDryRunCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		return domain.AutoGatherCanonicalPlanResult{
			Stage2TargetStillCanonicalEligible: true,
			CanonicalRecommendedTarget:         "disk1",
			CanonicalMoveBytes:                 100,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true},
			},
		}
	}
	autoGatherDryRunBuildPlanHook = func(c *Core, showPath string) (*domain.Plan, error) {
		return &domain.Plan{
			VDisks: map[string]*domain.VDisk{
				"/mnt/disk1": {Path: "/mnt/disk1", Bin: &domain.Bin{Size: 100}},
			},
		}, nil
	}

	c := newCoreForDryRunLoopTest()
	c.runAutoGatherDryRunLoopForTest()

	if execCount != 2 {
		t.Fatalf("expected 2 dry-run executes, got %d", execCount)
	}
	if scanCalls != 1 {
		t.Fatalf("expected exactly one Stage 1 scan per run, got %d", scanCalls)
	}
	if refreshCalls < 2 {
		t.Fatalf("expected disk refresh each iteration, got %d", refreshCalls)
	}
	if rescoreCalls < 2 {
		t.Fatalf("expected Stage 2 rescore each iteration, got %d", rescoreCalls)
	}
	state := c.GetAutoGatherDryRunState()
	if state.Phase != domain.AutoGatherDryRunPhaseCompleted {
		t.Fatalf("phase = %q, want completed", state.Phase)
	}
	if len(state.Completed) != 2 {
		t.Fatalf("completed = %d, want 2", len(state.Completed))
	}
}

func TestDryRunLoopStopsOnUncertainExecution(t *testing.T) {
	prevExec := autoGatherDryRunExecuteHook
	prevScan := autoGatherDryRunScanHook
	prevRefresh := autoGatherDryRunRefreshHook
	prevRescore := autoGatherDryRunRescoreHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunRefreshHook = prevRefresh
		autoGatherDryRunRescoreHook = prevRescore
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
	}()

	autoGatherDryRunExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecUncertain, reason: "rsync flagged"}
	}
	installDefaultDryRunLoopHooks()

	c := newCoreForDryRunLoopTest()
	c.runAutoGatherDryRunLoopForTest()

	state := c.GetAutoGatherDryRunState()
	if state.Phase != domain.AutoGatherDryRunPhaseFailed {
		t.Fatalf("phase = %q, want failed", state.Phase)
	}
	if state.FailureReason == "" {
		t.Fatal("expected failure reason")
	}
}

func TestDryRunLoopHonorsStopBeforeNextShow(t *testing.T) {
	prevExec := autoGatherDryRunExecuteHook
	prevScan := autoGatherDryRunScanHook
	prevRefresh := autoGatherDryRunRefreshHook
	prevRescore := autoGatherDryRunRescoreHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunRefreshHook = prevRefresh
		autoGatherDryRunRescoreHook = prevRescore
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
	}()

	var execCount int32
	autoGatherDryRunExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		n := atomic.AddInt32(&execCount, 1)
		if n == 1 {
			c.requestAutoGatherDryRunStop()
		}
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 1}
	}
	installDefaultDryRunLoopHooks()

	c := newCoreForDryRunLoopTest()
	c.runAutoGatherDryRunLoopForTest()

	state := c.GetAutoGatherDryRunState()
	if state.Phase != domain.AutoGatherDryRunPhaseStopped {
		t.Fatalf("phase = %q, want stopped", state.Phase)
	}
	if execCount != 1 {
		t.Fatalf("executed %d shows, want 1 before stop", execCount)
	}
}

func TestSelectNextDryRunShowExcludesCompletedShowsDespiteFreshSplitScan(t *testing.T) {
	scan := domain.AutoGatherScanResult{
		Shows: []domain.AutoGatherShow{
			{Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 100},
			{Name: "B", Path: "data/media/tv/B", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 50},
		},
	}

	completed := map[string]struct{}{"data/media/tv/A": {}}
	pick, remaining, cancelled := selectNextDryRunShow(scan, completed, map[string]struct{}{}, nil)
	if cancelled {
		t.Fatal("unexpected cancellation")
	}
	if remaining != 1 || pick == nil || pick.show.Path != "data/media/tv/B" {
		t.Fatalf("want show B after A completed, got %+v remaining=%d", pick, remaining)
	}

	skipped := map[string]struct{}{"data/media/tv/B": {}}
	pick, remaining, cancelled = selectNextDryRunShow(scan, map[string]struct{}{}, skipped, nil)
	if cancelled {
		t.Fatal("unexpected cancellation")
	}
	if remaining != 1 || pick == nil || pick.show.Path != "data/media/tv/A" {
		t.Fatalf("want show A when B skipped, got %+v remaining=%d", pick, remaining)
	}
}

func TestDryRunLoopCanonicalPlansOnlySelectedShow(t *testing.T) {
	prevExec := autoGatherDryRunExecuteHook
	prevScan := autoGatherDryRunScanHook
	prevRefresh := autoGatherDryRunRefreshHook
	prevRescore := autoGatherDryRunRescoreHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunRefreshHook = prevRefresh
		autoGatherDryRunRescoreHook = prevRescore
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
	}()

	var canonicalPaths []string
	autoGatherDryRunExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 1}
	}
	autoGatherDryRunScanHook = func(c *Core) domain.AutoGatherScanResult {
		return domain.AutoGatherScanResult{
			Shows: []domain.AutoGatherShow{
				{Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 100},
				{Name: "B", Path: "data/media/tv/B", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 50},
				{Name: "C", Path: "data/media/tv/C", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 75},
			},
		}
	}
	installStage2RescoreFromScanShows([]domain.AutoGatherShow{
		{Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 100},
		{Name: "B", Path: "data/media/tv/B", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 50},
		{Name: "C", Path: "data/media/tv/C", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 75},
	})
	autoGatherDryRunCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		canonicalPaths = append(canonicalPaths, show.Path)
		return domain.AutoGatherCanonicalPlanResult{
			Stage2TargetStillCanonicalEligible: true,
			CanonicalRecommendedTarget:         "disk1",
			CanonicalMoveBytes:                 show.MoveRequiredBytes,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true},
			},
		}
	}
	autoGatherDryRunBuildPlanHook = func(c *Core, showPath string) (*domain.Plan, error) {
		return &domain.Plan{
			VDisks: map[string]*domain.VDisk{
				"/mnt/disk1": {Path: "/mnt/disk1", Bin: &domain.Bin{Size: 1}},
			},
		}, nil
	}

	c := newCoreForDryRunLoopTest()
	// Run only until first show completes by stopping after first successful execute.
	autoGatherDryRunExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		c.requestAutoGatherDryRunStop()
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 1}
	}
	c.runAutoGatherDryRunLoopForTest()

	if len(canonicalPaths) != 1 {
		t.Fatalf("canonical plans = %v, want exactly 1 for the selected show", canonicalPaths)
	}
	if canonicalPaths[0] != "data/media/tv/B" {
		t.Fatalf("selected/canonical show = %s, want B (smallest Stage 2 move)", canonicalPaths[0])
	}
}

func TestDryRunLoopCanonicalErrorFailsRun(t *testing.T) {
	prevExec := autoGatherDryRunExecuteHook
	prevScan := autoGatherDryRunScanHook
	prevRefresh := autoGatherDryRunRefreshHook
	prevRescore := autoGatherDryRunRescoreHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunRefreshHook = prevRefresh
		autoGatherDryRunRescoreHook = prevRescore
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
	}()

	installDefaultDryRunLoopHooks()
	autoGatherDryRunCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		return domain.AutoGatherCanonicalPlanResult{Error: "unable to read array disks"}
	}

	c := newCoreForDryRunLoopTest()
	c.runAutoGatherDryRunLoopForTest()

	state := c.GetAutoGatherDryRunState()
	if state.Phase != domain.AutoGatherDryRunPhaseFailed {
		t.Fatalf("phase = %q, want failed", state.Phase)
	}
	if !strings.Contains(state.FailureReason, "canonical Gather planner error") {
		t.Fatalf("failure reason = %q", state.FailureReason)
	}
}

func TestDryRunLoopCanonicalNoEligibleSkipsOnlySelectedShow(t *testing.T) {
	prevExec := autoGatherDryRunExecuteHook
	prevScan := autoGatherDryRunScanHook
	prevRefresh := autoGatherDryRunRefreshHook
	prevRescore := autoGatherDryRunRescoreHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunRefreshHook = prevRefresh
		autoGatherDryRunRescoreHook = prevRescore
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
	}()

	var canonicalPaths []string
	autoGatherDryRunExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 1}
	}
	autoGatherDryRunScanHook = func(c *Core) domain.AutoGatherScanResult {
		return domain.AutoGatherScanResult{
			Shows: []domain.AutoGatherShow{
				{Name: "Hard", Path: "data/media/tv/Hard", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 10},
				{Name: "Easy", Path: "data/media/tv/Easy", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 20},
			},
		}
	}
	installStage2RescoreFromScanShows([]domain.AutoGatherShow{
		{Name: "Hard", Path: "data/media/tv/Hard", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 10},
		{Name: "Easy", Path: "data/media/tv/Easy", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 20},
	})
	autoGatherDryRunCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		canonicalPaths = append(canonicalPaths, show.Path)
		if show.Path == "data/media/tv/Hard" {
			return domain.AutoGatherCanonicalPlanResult{
				NoEligibleReason: "insufficient free space on all eligible physical array disks",
			}
		}
		return domain.AutoGatherCanonicalPlanResult{
			CanonicalRecommendedTarget: "disk1",
			CanonicalMoveBytes:         20,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true},
			},
		}
	}
	autoGatherDryRunBuildPlanHook = func(c *Core, showPath string) (*domain.Plan, error) {
		return &domain.Plan{
			VDisks: map[string]*domain.VDisk{
				"/mnt/disk1": {Path: "/mnt/disk1", Bin: &domain.Bin{Size: 1}},
			},
		}, nil
	}

	c := newCoreForDryRunLoopTest()
	c.runAutoGatherDryRunLoopForTest()

	state := c.GetAutoGatherDryRunState()
	if state.Phase != domain.AutoGatherDryRunPhaseCompleted {
		t.Fatalf("phase = %q, want completed", state.Phase)
	}
	if len(canonicalPaths) != 2 {
		t.Fatalf("canonical paths = %v, want Hard then Easy (one per iteration)", canonicalPaths)
	}
	if canonicalPaths[0] != "data/media/tv/Hard" || canonicalPaths[1] != "data/media/tv/Easy" {
		t.Fatalf("canonical order = %v", canonicalPaths)
	}
	if len(state.Skipped) != 1 || state.Skipped[0].ShowPath != "data/media/tv/Hard" {
		t.Fatalf("skipped = %+v", state.Skipped)
	}
	if len(state.Completed) != 1 || state.Completed[0].ShowPath != "data/media/tv/Easy" {
		t.Fatalf("completed = %+v", state.Completed)
	}
}

func TestClassifyCanonicalSelectionOutcomeDistinguishesSkipFromFail(t *testing.T) {
	show := domain.AutoGatherShow{Path: "data/media/tv/X"}
	skip, fatal := classifyCanonicalSelectionOutcome(show, domain.AutoGatherCanonicalPlanResult{
		NoEligibleReason: "insufficient free space on all eligible physical array disks",
	}, fmt.Errorf("no eligible physical array target"))
	if skip == "" || fatal != "" {
		t.Fatalf("infeasible show should skip, got skip=%q fatal=%q", skip, fatal)
	}

	skip, fatal = classifyCanonicalSelectionOutcome(show, domain.AutoGatherCanonicalPlanResult{
		Error: "unable to read array disks",
	}, nil)
	if skip != "" || fatal == "" {
		t.Fatalf("planner error should fail, got skip=%q fatal=%q", skip, fatal)
	}
}

func TestMailboxAllowsBlocksDuringOpNeutralWhileAutoGatherActive(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	c.ctx.DryRun = true
	c.state.Status = common.OpNeutral
	c.autoGatherRun = &domain.AutoGatherDryRunState{
		Phase:  domain.AutoGatherDryRunPhaseRunning,
		DryRun: true,
	}

	if c.mailboxAllows(common.CommandGatherPlanStart) {
		t.Fatal("gather must be blocked when Auto Gather active even if status is OpNeutral")
	}
	if c.mailboxAllows(common.CommandScatterPlanStart) {
		t.Fatal("scatter must be blocked when Auto Gather active even if status is OpNeutral")
	}
	if !c.mailboxAllows(common.CommandStop) {
		t.Fatal("stop must remain available")
	}
}

func TestStage3BDryRunDoesNotWriteNormalHistory(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	c.state.History = &domain.History{
		Items: map[string]*domain.Operation{},
		Order: []string{},
	}
	op := &domain.Operation{ID: "stage3b-op", DryRun: true}

	c.autoGatherDryRunExec = true
	c.updateHistory(c.state.History, op)
	if len(c.state.History.Order) != 0 {
		t.Fatalf("Stage 3B dry-run must not write history, got order=%v", c.state.History.Order)
	}

	c.autoGatherDryRunExec = false
	c.updateHistory(c.state.History, op)
	if len(c.state.History.Order) != 1 || c.state.History.Order[0] != "stage3b-op" {
		t.Fatalf("manual dry-run history unchanged, got order=%v", c.state.History.Order)
	}
}

func TestStage3BDoesNotBroadcastTransferSocketEvents(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	ch := c.ctx.Hub.Sub("socket:broadcast")
	t.Cleanup(func() { c.ctx.Hub.Unsub(ch) })

	c.autoGatherDryRunExec = true
	c.publishOperationSocket(&domain.Packet{Topic: common.EventTransferEnded, Payload: c.state})
	select {
	case msg := <-ch:
		t.Fatalf("Stage 3B must not broadcast transfer events, got %#v", msg)
	case <-time.After(50 * time.Millisecond):
	}

	c.autoGatherDryRunExec = false
	c.publishOperationSocket(&domain.Packet{Topic: common.EventTransferEnded, Payload: c.state})
	select {
	case msg := <-ch:
		packet, ok := msg.(*domain.Packet)
		if !ok || packet.Topic != common.EventTransferEnded {
			t.Fatalf("expected transfer:ended for manual ops, got %#v", msg)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected transfer:ended when Stage 3B exec is not active")
	}
}

func TestAutoGatherDryRunStatusMessageDescribesScanOnceArchitecture(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	c.ctx.DryRun = true
	state := c.GetAutoGatherDryRunState()
	if strings.Contains(strings.ToLower(state.Message), "re-scan") {
		t.Fatalf("stale rescan wording still present: %q", state.Message)
	}
	if !strings.Contains(state.Message, "One Stage 1 library scan") {
		t.Fatalf("message should describe scan-once architecture: %q", state.Message)
	}
	if !strings.Contains(state.Message, "re-scores Stage 2") {
		t.Fatalf("message should describe Stage 2 rescore: %q", state.Message)
	}
}

func TestManualGatherScatterStillBlockedAfterUiRoutingChange(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	c.ctx.DryRun = true
	c.autoGatherRun = &domain.AutoGatherDryRunState{
		Phase:  domain.AutoGatherDryRunPhaseRunning,
		DryRun: true,
	}
	c.state.Status = common.OpGatherMove
	if c.mailboxAllows(common.CommandGatherPlanStart) || c.mailboxAllows(common.CommandScatterPlanStart) {
		t.Fatal("manual Gather/Scatter must stay blocked while Auto Gather is active")
	}
	if !c.mailboxAllows(common.CommandStop) {
		t.Fatal("Stop must remain available")
	}
}

func TestDryRunLoopFailsOnScanError(t *testing.T) {
	prevScan := autoGatherDryRunScanHook
	defer func() { autoGatherDryRunScanHook = prevScan }()

	autoGatherDryRunScanHook = func(c *Core) domain.AutoGatherScanResult {
		return domain.AutoGatherScanResult{Error: "unable to read array disks"}
	}

	c := newCoreForDryRunLoopTest()
	c.runAutoGatherDryRunLoopForTest()

	state := c.GetAutoGatherDryRunState()
	if state.Phase != domain.AutoGatherDryRunPhaseFailed {
		t.Fatalf("phase = %q, want failed", state.Phase)
	}
	if !strings.Contains(state.FailureReason, "scan failed") {
		t.Fatalf("failure reason = %q", state.FailureReason)
	}
}

func TestDryRunLoopFailsOnPlanBuildError(t *testing.T) {
	prevExec := autoGatherDryRunExecuteHook
	prevScan := autoGatherDryRunScanHook
	prevRefresh := autoGatherDryRunRefreshHook
	prevRescore := autoGatherDryRunRescoreHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunRefreshHook = prevRefresh
		autoGatherDryRunRescoreHook = prevRescore
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
	}()

	installDefaultDryRunLoopHooks()
	autoGatherDryRunBuildPlanHook = func(c *Core, showPath string) (*domain.Plan, error) {
		return nil, fmt.Errorf("getItems failed")
	}

	c := newCoreForDryRunLoopTest()
	c.runAutoGatherDryRunLoopForTest()

	state := c.GetAutoGatherDryRunState()
	if state.Phase != domain.AutoGatherDryRunPhaseFailed {
		t.Fatalf("phase = %q, want failed", state.Phase)
	}
	if !strings.Contains(state.FailureReason, "gather plan build failed") {
		t.Fatalf("failure reason = %q", state.FailureReason)
	}
	if len(state.Skipped) != 0 {
		t.Fatal("plan build error must not be recorded as skip")
	}
}

func TestDryRunLoopSkipsInfeasibleShowAndContinues(t *testing.T) {
	prevExec := autoGatherDryRunExecuteHook
	prevScan := autoGatherDryRunScanHook
	prevRefresh := autoGatherDryRunRefreshHook
	prevRescore := autoGatherDryRunRescoreHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunRefreshHook = prevRefresh
		autoGatherDryRunRescoreHook = prevRescore
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
	}()

	var execPaths []string
	var canonicalPaths []string
	autoGatherDryRunExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 1}
	}
	autoGatherDryRunScanHook = func(c *Core) domain.AutoGatherScanResult {
		return domain.AutoGatherScanResult{
			Shows: []domain.AutoGatherShow{
				{
					Name: "NoTarget", Path: "data/media/tv/NoTarget", Status: domain.AutoGatherStatusSplit,
					NoEligibleReason: "insufficient free space on all eligible physical array disks",
				},
				{
					Name: "OK", Path: "data/media/tv/OK", Status: domain.AutoGatherStatusSplit,
					RecommendedTargetDisk: "disk1", MoveRequiredBytes: 1,
				},
			},
		}
	}
	installStage2RescoreFromScanShows([]domain.AutoGatherShow{
		{
			Name: "NoTarget", Path: "data/media/tv/NoTarget", Status: domain.AutoGatherStatusSplit,
			NoEligibleReason: "insufficient free space on all eligible physical array disks",
		},
		{
			Name: "OK", Path: "data/media/tv/OK", Status: domain.AutoGatherStatusSplit,
			RecommendedTargetDisk: "disk1", MoveRequiredBytes: 1,
		},
	})
	autoGatherDryRunCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		canonicalPaths = append(canonicalPaths, show.Path)
		return domain.AutoGatherCanonicalPlanResult{
			CanonicalRecommendedTarget: "disk1",
			CanonicalMoveBytes:         1,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true},
			},
		}
	}
	autoGatherDryRunBuildPlanHook = func(c *Core, showPath string) (*domain.Plan, error) {
		execPaths = append(execPaths, showPath)
		return &domain.Plan{
			VDisks: map[string]*domain.VDisk{
				"/mnt/disk1": {Path: "/mnt/disk1", Bin: &domain.Bin{Size: 1}},
			},
		}, nil
	}

	c := newCoreForDryRunLoopTest()
	c.runAutoGatherDryRunLoopForTest()

	state := c.GetAutoGatherDryRunState()
	if state.Phase != domain.AutoGatherDryRunPhaseCompleted {
		t.Fatalf("phase = %q, want completed", state.Phase)
	}
	if len(state.Skipped) != 1 || state.Skipped[0].ShowPath != "data/media/tv/NoTarget" {
		t.Fatalf("skipped = %+v", state.Skipped)
	}
	if len(state.Completed) != 1 || state.Completed[0].ShowPath != "data/media/tv/OK" {
		t.Fatalf("completed = %+v", state.Completed)
	}
	if len(execPaths) != 1 || execPaths[0] != "data/media/tv/OK" {
		t.Fatalf("executed paths = %v", execPaths)
	}
	// Stage 2 skip for NoTarget must not trigger canonical planning.
	if len(canonicalPaths) != 1 || canonicalPaths[0] != "data/media/tv/OK" {
		t.Fatalf("canonical paths = %v, want only OK", canonicalPaths)
	}
}

func TestDryRunLoopDoesNotReExecuteCompletedShow(t *testing.T) {
	prevExec := autoGatherDryRunExecuteHook
	prevScan := autoGatherDryRunScanHook
	prevRefresh := autoGatherDryRunRefreshHook
	prevRescore := autoGatherDryRunRescoreHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunRefreshHook = prevRefresh
		autoGatherDryRunRescoreHook = prevRescore
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
	}()

	var execPaths []string
	var canonicalPaths []string
	autoGatherDryRunExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 1}
	}
	autoGatherDryRunScanHook = func(c *Core) domain.AutoGatherScanResult {
		// Retained inventory still reports both shows as split (dry-run does not change layout).
		return domain.AutoGatherScanResult{
			Shows: []domain.AutoGatherShow{
				{Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 10},
				{Name: "B", Path: "data/media/tv/B", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 5},
			},
		}
	}
	installStage2RescoreFromScanShows([]domain.AutoGatherShow{
		{Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 10},
		{Name: "B", Path: "data/media/tv/B", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 5},
	})
	autoGatherDryRunCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		canonicalPaths = append(canonicalPaths, show.Path)
		return domain.AutoGatherCanonicalPlanResult{
			CanonicalRecommendedTarget: "disk1",
			CanonicalMoveBytes:         show.MoveRequiredBytes,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true},
			},
		}
	}
	autoGatherDryRunBuildPlanHook = func(c *Core, showPath string) (*domain.Plan, error) {
		execPaths = append(execPaths, showPath)
		return &domain.Plan{
			VDisks: map[string]*domain.VDisk{
				"/mnt/disk1": {Path: "/mnt/disk1", Bin: &domain.Bin{Size: 1}},
			},
		}, nil
	}

	c := newCoreForDryRunLoopTest()
	c.runAutoGatherDryRunLoopForTest()

	if len(execPaths) != 2 {
		t.Fatalf("executed %d shows, want 2 distinct", len(execPaths))
	}
	if execPaths[0] == execPaths[1] {
		t.Fatalf("same show executed twice: %v", execPaths)
	}
	if execPaths[0] != "data/media/tv/B" || execPaths[1] != "data/media/tv/A" {
		t.Fatalf("expected B then A (smallest Stage 2 first), got %v", execPaths)
	}
	if len(canonicalPaths) != 2 || canonicalPaths[0] != "data/media/tv/B" || canonicalPaths[1] != "data/media/tv/A" {
		t.Fatalf("canonical must be one show per iteration: %v", canonicalPaths)
	}
}

func TestExecuteGatherPlanDryRunSyncForcesDryRunFlags(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	c.ctx.DryRun = true
	c.state.Status = common.OpAutoGatherDryRun
	c.state.Unraid = &domain.Unraid{
		Disks: []*domain.Disk{{Name: "disk1", Path: "/mnt/disk1"}},
	}

	plan := &domain.Plan{
		VDisks: map[string]*domain.VDisk{
			"/mnt/disk1": {Path: "/mnt/disk1", Bin: &domain.Bin{Size: 10, Items: []*domain.Item{{Path: "data/media/tv/A", Location: "/mnt/disk8", Size: 10}}}},
		},
	}

	c.autoGatherDryRunExec = true
	op := c.createGatherOperation(*plan)
	c.autoGatherDryRunExec = false
	if !op.DryRun {
		t.Fatal("operation must be dry-run")
	}
	if !rsyncArgsIncludeDryRun(op.RsyncArgs) {
		t.Fatalf("missing --dry-run in %v", op.RsyncArgs)
	}
}

func TestExecuteGatherPlanDryRunSyncRestoresAutoGatherStatus(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	c.ctx.DryRun = true
	c.autoGatherRun = &domain.AutoGatherDryRunState{
		Phase:  domain.AutoGatherDryRunPhaseRunning,
		DryRun: true,
	}
	c.state.Status = common.OpAutoGatherDryRun

	func() {
		prevStatus := c.state.Status
		c.state.Status = common.OpGatherMove
		c.autoGatherDryRunExec = true
		defer func() {
			c.autoGatherDryRunExec = false
			if c.isAutoGatherDryRunActive() {
				c.state.Status = common.OpAutoGatherDryRun
			} else if c.state.Status == common.OpGatherMove {
				c.state.Status = prevStatus
			}
		}()
		c.state.Status = common.OpNeutral
	}()

	if c.state.Status != common.OpAutoGatherDryRun {
		t.Fatalf("status = %d, want OpAutoGatherDryRun after simulated endOperation", c.state.Status)
	}
}

func TestIsArrayDiskNameRejectsPoolsUsedByAutoGather(t *testing.T) {
	for _, name := range []string{"cache", "data_cache", "disk0", "flash"} {
		if autogather.IsArrayDiskName(name) {
			t.Fatalf("%q must not be array disk", name)
		}
	}
}

func TestStopRequestSetsStoppingImmediatelyAndRemainsResponsive(t *testing.T) {
	c := newCoreForDryRunLoopTest()
	c.ctx.DryRun = true
	c.state.Status = common.OpNeutral

	started := make(chan struct{})
	unblock := make(chan struct{})
	prevScan := autoGatherDryRunScanHook
	prevExec := autoGatherDryRunExecuteHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
		select {
		case <-unblock:
		default:
			close(unblock)
		}
	}()

	autoGatherDryRunScanHook = func(c *Core) domain.AutoGatherScanResult {
		select {
		case <-started:
		default:
			close(started)
		}
		<-unblock
		if c.autoGatherShouldStop() {
			return domain.AutoGatherScanResult{Cancelled: true}
		}
		return domain.AutoGatherScanResult{
			Shows: []domain.AutoGatherShow{
				{Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1"},
			},
		}
	}
	autoGatherDryRunExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		t.Fatal("must not execute after stop during scan")
		return gatherExecResult{}
	}

	if _, err := c.StartAutoGatherDryRun(); err != nil {
		t.Fatalf("start: %v", err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("scan did not start")
	}

	done := make(chan domain.AutoGatherDryRunState, 1)
	go func() {
		done <- c.StopAutoGatherDryRun()
	}()

	select {
	case stopState := <-done:
		if stopState.Phase != domain.AutoGatherDryRunPhaseStopping {
			t.Fatalf("phase after stop = %q, want stopping", stopState.Phase)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StopAutoGatherDryRun blocked while Stage 3B scan was active")
	}

	close(unblock)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.GetAutoGatherDryRunState().Phase == domain.AutoGatherDryRunPhaseStopped {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("phase = %q, want stopped", c.GetAutoGatherDryRunState().Phase)
}

func TestStopDuringLibraryScanDoesNotSelectOrExecute(t *testing.T) {
	prevExec := autoGatherDryRunExecuteHook
	prevScan := autoGatherDryRunScanHook
	prevRefresh := autoGatherDryRunRefreshHook
	prevRescore := autoGatherDryRunRescoreHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunRefreshHook = prevRefresh
		autoGatherDryRunRescoreHook = prevRescore
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
	}()

	var execCount int32
	autoGatherDryRunExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		atomic.AddInt32(&execCount, 1)
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 1}
	}
	autoGatherDryRunScanHook = func(c *Core) domain.AutoGatherScanResult {
		c.requestAutoGatherDryRunStop()
		return domain.AutoGatherScanResult{Cancelled: true}
	}
	autoGatherDryRunCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		t.Fatal("canonical planning must not run after cancelled scan")
		return domain.AutoGatherCanonicalPlanResult{}
	}

	c := newCoreForDryRunLoopTest()
	c.runAutoGatherDryRunLoopForTest()

	state := c.GetAutoGatherDryRunState()
	if state.Phase != domain.AutoGatherDryRunPhaseStopped {
		t.Fatalf("phase = %q, want stopped", state.Phase)
	}
	if execCount != 0 {
		t.Fatalf("executed %d shows, want 0", execCount)
	}
	if len(state.Skipped) != 0 || len(state.Completed) != 0 {
		t.Fatalf("stop must not record skip/complete; skipped=%v completed=%v", state.Skipped, state.Completed)
	}
	if state.FailureReason != "" {
		t.Fatalf("stop must not be recorded as failure: %s", state.FailureReason)
	}
}

func TestStopDuringCanonicalPlanningPreventsExecution(t *testing.T) {
	prevExec := autoGatherDryRunExecuteHook
	prevScan := autoGatherDryRunScanHook
	prevRefresh := autoGatherDryRunRefreshHook
	prevRescore := autoGatherDryRunRescoreHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunRefreshHook = prevRefresh
		autoGatherDryRunRescoreHook = prevRescore
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
	}()

	var execCount int32
	autoGatherDryRunExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		atomic.AddInt32(&execCount, 1)
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 1}
	}
	installDefaultDryRunLoopHooks()
	autoGatherDryRunCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		c.requestAutoGatherDryRunStop()
		return domain.AutoGatherCanonicalPlanResult{ShowPath: show.Path, Cancelled: true}
	}

	c := newCoreForDryRunLoopTest()
	c.runAutoGatherDryRunLoopForTest()

	state := c.GetAutoGatherDryRunState()
	if state.Phase != domain.AutoGatherDryRunPhaseStopped {
		t.Fatalf("phase = %q, want stopped", state.Phase)
	}
	if execCount != 0 {
		t.Fatalf("executed %d shows after cancelled canonical plan", execCount)
	}
}

func TestStopAfterPlanningBeforeExecutionPreventsExecution(t *testing.T) {
	prevExec := autoGatherDryRunExecuteHook
	prevScan := autoGatherDryRunScanHook
	prevRefresh := autoGatherDryRunRefreshHook
	prevRescore := autoGatherDryRunRescoreHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunRefreshHook = prevRefresh
		autoGatherDryRunRescoreHook = prevRescore
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
	}()

	var execEntered int32
	installDefaultDryRunLoopHooks()
	autoGatherDryRunBuildPlanHook = func(c *Core, showPath string) (*domain.Plan, error) {
		c.requestAutoGatherDryRunStop()
		return &domain.Plan{
			VDisks: map[string]*domain.VDisk{
				"/mnt/disk1": {Path: "/mnt/disk1", Bin: &domain.Bin{Size: 100}},
			},
		}, nil
	}
	autoGatherDryRunExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		atomic.AddInt32(&execEntered, 1)
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 1}
	}

	c := newCoreForDryRunLoopTest()
	c.runAutoGatherDryRunLoopForTest()

	state := c.GetAutoGatherDryRunState()
	if state.Phase != domain.AutoGatherDryRunPhaseStopped {
		t.Fatalf("phase = %q, want stopped", state.Phase)
	}
	if execEntered != 0 {
		t.Fatalf("execute hook entered %d times; stop before execution must prevent it", execEntered)
	}
}

func TestStopDuringSelectionDoesNotBeginLaterShow(t *testing.T) {
	stop := false
	seen := 0
	scan := domain.AutoGatherScanResult{
		Shows: []domain.AutoGatherShow{
			{Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 10},
			{Name: "B", Path: "data/media/tv/B", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 20},
		},
	}
	pick, _, cancelled := selectNextDryRunShow(
		scan, map[string]struct{}{}, map[string]struct{}{},
		func() bool {
			seen++
			// Cancel after the first remaining split show is considered.
			if seen > 1 {
				stop = true
			}
			return stop
		},
	)
	if !cancelled || pick != nil {
		t.Fatalf("want cancelled selection, pick=%+v cancelled=%v", pick, cancelled)
	}
}

func TestStopDuringStage2SelectionCancelsLoop(t *testing.T) {
	prevExec := autoGatherDryRunExecuteHook
	prevScan := autoGatherDryRunScanHook
	prevRefresh := autoGatherDryRunRefreshHook
	prevRescore := autoGatherDryRunRescoreHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunRefreshHook = prevRefresh
		autoGatherDryRunRescoreHook = prevRescore
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
	}()

	var canonicalCount int32
	autoGatherDryRunScanHook = func(c *Core) domain.AutoGatherScanResult {
		c.requestAutoGatherDryRunStop()
		return domain.AutoGatherScanResult{
			Shows: []domain.AutoGatherShow{
				{Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 1},
			},
		}
	}
	autoGatherDryRunCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		atomic.AddInt32(&canonicalCount, 1)
		return domain.AutoGatherCanonicalPlanResult{}
	}
	autoGatherDryRunExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		t.Fatal("must not execute")
		return gatherExecResult{}
	}

	c := newCoreForDryRunLoopTest()
	c.runAutoGatherDryRunLoopForTest()
	if c.GetAutoGatherDryRunState().Phase != domain.AutoGatherDryRunPhaseStopped {
		t.Fatalf("phase = %q", c.GetAutoGatherDryRunState().Phase)
	}
	if canonicalCount != 0 {
		t.Fatalf("canonical count = %d", canonicalCount)
	}
}

func TestClassifyGatherDryRunResultUserStopIsCancelledNotFailed(t *testing.T) {
	op := &domain.Operation{
		DryRun: true,
		Commands: []*domain.Command{
			{Status: common.CmdStopped, Reason: "stopped by the user"},
		},
	}
	got := classifyGatherDryRunResult(op, 10, true)
	if got.outcome != gatherExecCancelled {
		t.Fatalf("user stop during rsync must be cancelled, got %+v", got)
	}
	got = classifyGatherDryRunResult(op, 10, false)
	if got.outcome != gatherExecUncertain {
		t.Fatalf("unexpected stop without Stage 3B flag must be uncertain, got %+v", got)
	}
}

func TestManualGatherUnchangedWhenStage3BInactive(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	c.state.Status = common.OpNeutral
	c.autoGatherRun = nil
	if !c.mailboxAllows(common.CommandGatherPlanStart) {
		t.Fatal("manual gather must be allowed when Stage 3B inactive")
	}
	if !c.mailboxAllows(common.CommandScatterPlanStart) {
		t.Fatal("manual scatter must be allowed when Stage 3B inactive")
	}
}

func TestDryRunLoopScansLibraryOnceAndRescoresFromDiskState(t *testing.T) {
	prevExec := autoGatherDryRunExecuteHook
	prevScan := autoGatherDryRunScanHook
	prevRefresh := autoGatherDryRunRefreshHook
	prevRescore := autoGatherDryRunRescoreHook
	prevCanon := autoGatherDryRunCanonicalHook
	prevPlan := autoGatherDryRunBuildPlanHook
	defer func() {
		autoGatherDryRunExecuteHook = prevExec
		autoGatherDryRunScanHook = prevScan
		autoGatherDryRunRefreshHook = prevRefresh
		autoGatherDryRunRescoreHook = prevRescore
		autoGatherDryRunCanonicalHook = prevCanon
		autoGatherDryRunBuildPlanHook = prevPlan
	}()

	var scanCalls, refreshCalls, rescoreCalls int32
	var seenTargets []string
	shows := []domain.AutoGatherShow{
		{Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit},
		{Name: "B", Path: "data/media/tv/B", Status: domain.AutoGatherStatusSplit},
	}
	autoGatherDryRunScanHook = func(c *Core) domain.AutoGatherScanResult {
		atomic.AddInt32(&scanCalls, 1)
		return domain.AutoGatherScanResult{Shows: append([]domain.AutoGatherShow(nil), shows...)}
	}
	autoGatherDryRunRefreshHook = func(c *Core) *domain.Unraid {
		n := atomic.AddInt32(&refreshCalls, 1)
		// After the first show completes, flip free space so rescoring prefers B->disk2.
		if n >= 3 { // startup refresh + iter1 refresh + post-first-show refresh
			return &domain.Unraid{
				Disks: []*domain.Disk{
					{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(5)},
					{Name: "disk2", Path: "/mnt/disk2", Type: "Data", Size: gib(100), Free: gib(80)},
				},
			}
		}
		return &domain.Unraid{
			Disks: []*domain.Disk{
				{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(80)},
				{Name: "disk2", Path: "/mnt/disk2", Type: "Data", Size: gib(100), Free: gib(5)},
			},
		}
	}
	autoGatherDryRunRescoreHook = func(c *Core, base domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult {
		atomic.AddInt32(&rescoreCalls, 1)
		out := cloneAutoGatherDiscoverySnapshot(base)
		preferDisk2 := false
		for _, d := range unraid.Disks {
			if d.Name == "disk2" && d.Free > gib(50) {
				preferDisk2 = true
			}
		}
		for i := range out.Shows {
			switch out.Shows[i].Path {
			case "data/media/tv/A":
				out.Shows[i].RecommendedTargetDisk = "disk1"
				out.Shows[i].MoveRequiredBytes = 10
				if preferDisk2 {
					// After free-space change, A becomes infeasible in Stage 2.
					out.Shows[i].RecommendedTargetDisk = ""
					out.Shows[i].NoEligibleReason = "insufficient free space on all eligible physical array disks"
					out.Shows[i].MoveRequiredBytes = 0
				}
			case "data/media/tv/B":
				out.Shows[i].RecommendedTargetDisk = "disk1"
				out.Shows[i].MoveRequiredBytes = 20
				if preferDisk2 {
					out.Shows[i].RecommendedTargetDisk = "disk2"
					out.Shows[i].MoveRequiredBytes = 20
				}
			}
		}
		return out
	}
	autoGatherDryRunCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		target := show.RecommendedTargetDisk
		path := "/mnt/" + target
		seenTargets = append(seenTargets, target)
		return domain.AutoGatherCanonicalPlanResult{
			Stage2TargetStillCanonicalEligible: true,
			CanonicalRecommendedTarget:         target,
			CanonicalMoveBytes:                 show.MoveRequiredBytes,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: target, DiskPath: path, IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true},
			},
		}
	}
	autoGatherDryRunBuildPlanHook = func(c *Core, showPath string) (*domain.Plan, error) {
		target := "disk1"
		if len(seenTargets) > 0 {
			target = seenTargets[len(seenTargets)-1]
		}
		path := "/mnt/" + target
		return &domain.Plan{
			VDisks: map[string]*domain.VDisk{
				path: {Path: path, Bin: &domain.Bin{Size: 1}},
			},
		}, nil
	}
	autoGatherDryRunExecuteHook = func(c *Core, plan *domain.Plan, targetPath string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 1}
	}

	c := newCoreForDryRunLoopTest()
	c.runAutoGatherDryRunLoopForTest()

	if scanCalls != 1 {
		t.Fatalf("Stage 1 scans = %d, want 1", scanCalls)
	}
	if refreshCalls < 3 {
		t.Fatalf("refreshCalls = %d, want at least startup + per iteration", refreshCalls)
	}
	if rescoreCalls < 2 {
		t.Fatalf("rescoreCalls = %d, want >= 2", rescoreCalls)
	}
	state := c.GetAutoGatherDryRunState()
	if state.Phase != domain.AutoGatherDryRunPhaseCompleted {
		t.Fatalf("phase = %q", state.Phase)
	}
	if len(state.Completed) != 2 {
		t.Fatalf("completed = %+v", state.Completed)
	}
	if state.Completed[0].ShowPath != "data/media/tv/A" || state.Completed[0].TargetDisk != "disk1" {
		t.Fatalf("first completed = %+v", state.Completed[0])
	}
	if state.Completed[1].ShowPath != "data/media/tv/B" || state.Completed[1].TargetDisk != "disk2" {
		t.Fatalf("second completed should use rescored disk2 target, got %+v", state.Completed[1])
	}
	if len(seenTargets) != 2 || seenTargets[0] != "disk1" || seenTargets[1] != "disk2" {
		t.Fatalf("canonical targets = %v", seenTargets)
	}
}

func TestRecommendationEnrichmentClearsStaleDerivedFields(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	c.state.Unraid = &domain.Unraid{
		Disks: []*domain.Disk{
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(1)}, // too small
		},
	}
	scan := domain.AutoGatherScanResult{
		Shows: []domain.AutoGatherShow{
			{
				Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit,
				RecommendedTargetDisk: "disk9", // stale derived value
				MoveRequiredBytes:     999,
				VideoDisks: []domain.AutoGatherDiskPresence{
					{DiskName: "disk8", VideoCount: 1, VideoBytes: gib(10), TotalBytes: gib(10)},
				},
			},
		},
	}
	got := c.addAutoGatherRecommendations(cloneAutoGatherDiscoverySnapshot(scan), c.state.Unraid)
	if got.Shows[0].RecommendedTargetDisk != "" {
		t.Fatalf("stale recommended target retained: %q", got.Shows[0].RecommendedTargetDisk)
	}
	if got.Shows[0].MoveRequiredBytes != 0 {
		t.Fatalf("stale move bytes retained: %d", got.Shows[0].MoveRequiredBytes)
	}
	if got.Shows[0].NoEligibleReason == "" {
		t.Fatal("expected no-eligible reason after rescoring against tiny disk")
	}
}

func TestCloneDiscoverySnapshotDoesNotShareRecommendationState(t *testing.T) {
	src := domain.AutoGatherScanResult{
		Shows: []domain.AutoGatherShow{
			{
				Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit,
				RecommendedTargetDisk: "disk1", MoveRequiredBytes: 10,
				VideoDisks: []domain.AutoGatherDiskPresence{{DiskName: "disk8", TotalBytes: 10}},
			},
		},
	}
	cloned := cloneAutoGatherDiscoverySnapshot(src)
	if cloned.Shows[0].RecommendedTargetDisk != "" || cloned.Shows[0].MoveRequiredBytes != 0 {
		t.Fatalf("clone must strip recommendations: %+v", cloned.Shows[0])
	}
	cloned.Shows[0].VideoDisks[0].TotalBytes = 99
	if src.Shows[0].VideoDisks[0].TotalBytes != 10 {
		t.Fatal("clone must deep-copy discovery slices")
	}
}

func installStage2RescoreFromScanShows(shows []domain.AutoGatherShow) {
	byPath := make(map[string]domain.AutoGatherShow, len(shows))
	for _, s := range shows {
		byPath[s.Path] = s
	}
	autoGatherDryRunRescoreHook = func(c *Core, base domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult {
		out := cloneAutoGatherDiscoverySnapshot(base)
		for i := range out.Shows {
			src, ok := byPath[out.Shows[i].Path]
			if !ok {
				continue
			}
			out.Shows[i].RecommendedTargetDisk = src.RecommendedTargetDisk
			out.Shows[i].MoveRequiredBytes = src.MoveRequiredBytes
			out.Shows[i].BelowPreferredFreeFloor = src.BelowPreferredFreeFloor
			out.Shows[i].NoEligibleReason = src.NoEligibleReason
			out.Shows[i].ProjectedFreeBytes = src.ProjectedFreeBytes
			out.Shows[i].ProjectedFreePercent = src.ProjectedFreePercent
		}
		return out
	}
}

func installDefaultDryRunLoopHooks() {
	shows := []domain.AutoGatherShow{
		{Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 100},
		{Name: "B", Path: "data/media/tv/B", Status: domain.AutoGatherStatusSplit, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 50},
	}
	autoGatherDryRunScanHook = func(c *Core) domain.AutoGatherScanResult {
		return domain.AutoGatherScanResult{Shows: append([]domain.AutoGatherShow(nil), shows...)}
	}
	installStage2RescoreFromScanShows(shows)
	autoGatherDryRunCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		return domain.AutoGatherCanonicalPlanResult{
			Stage2TargetStillCanonicalEligible: true,
			CanonicalRecommendedTarget:         "disk1",
			CanonicalMoveBytes:                 100,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true},
			},
		}
	}
	autoGatherDryRunBuildPlanHook = func(c *Core, showPath string) (*domain.Plan, error) {
		return &domain.Plan{
			VDisks: map[string]*domain.VDisk{
				"/mnt/disk1": {Path: "/mnt/disk1", Bin: &domain.Bin{Size: 100}},
			},
		}, nil
	}
}

func (c *Core) runAutoGatherDryRunLoopForTest() {
	c.autoGatherMu.Lock()
	c.autoGatherRun = &domain.AutoGatherDryRunState{
		Phase:     domain.AutoGatherDryRunPhaseRunning,
		DryRun:    true,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Message:   autoGatherDryRunAdaptationMessage,
	}
	c.autoGatherStopRequested = false
	c.state.Status = common.OpAutoGatherDryRun
	c.autoGatherMu.Unlock()
	c.autoGatherDryRunLoop()
}

func newCoreForDryRunLoopTest() *Core {
	return &Core{
		ctx: &domain.Context{
			Config: domain.Config{ReservedAmount: 1, ReservedUnit: "Gb", DryRun: true},
			Hub:    pubsub.New(8),
		},
		state: &domain.State{
			Status: common.OpNeutral,
			Unraid: &domain.Unraid{
				Disks: []*domain.Disk{
					{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(50)},
				},
			},
		},
		pendingPlans: map[string]*planTicket{},
	}
}
