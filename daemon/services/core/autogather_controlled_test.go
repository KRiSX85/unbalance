package core

import (
	"encoding/json"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"unbalance/daemon/common"
	"unbalance/daemon/domain"
)

func newCoreForControlledTest() *Core {
	c := newCoreForRealTest()
	c.ctx.DryRun = false
	return c
}

func newCoreForControlledMarkerTest(t *testing.T) *Core {
	t.Helper()
	c := newCoreForControlledTest()
	c.ctx.DataDir = t.TempDir()
	return c
}

func controlledMarkerExists(c *Core) bool {
	_, err := os.Stat(c.autoGatherControlledMarkerPath())
	return err == nil
}

func installControlledHooksDefault(showPath, target string, move uint64) func() {
	prevScan := autoGatherControlledScanHook
	prevRefresh := autoGatherControlledRefreshHook
	prevRescore := autoGatherControlledRescoreHook
	prevCanonical := autoGatherControlledCanonicalHook
	prevBuild := autoGatherControlledBuildPlanHook
	prevExec := autoGatherControlledExecuteHook
	prevVerify := autoGatherControlledVerifyHook

	autoGatherControlledRefreshHook = func(c *Core) *domain.Unraid { return c.state.Unraid }
	autoGatherControlledScanHook = func(c *Core) domain.AutoGatherScanResult {
		return domain.AutoGatherScanResult{
			LibraryPath: "data/media/tv",
			Shows: []domain.AutoGatherShow{
				{
					Name: "Show A", Path: showPath, Status: domain.AutoGatherStatusSplit,
					Split: true, Ready: true,
					RecommendedTargetDisk: target,
					MoveRequiredBytes:     move,
					TotalVideoBytes:       move,
					TotalBytes:            move,
				},
			},
		}
	}
	autoGatherControlledRescoreHook = func(c *Core, base domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult {
		return base
	}
	targetPath := "/mnt/" + target
	autoGatherControlledCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		return domain.AutoGatherCanonicalPlanResult{
			ShowPath:                           show.Path,
			CanonicalRecommendedTarget:         target,
			Stage2TargetStillCanonicalEligible: true,
			CanonicalMoveBytes:                 move,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: target, DiskPath: targetPath, IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true, CanonicalBytesToMove: move, FreeBytes: gib(50), ProjectedFreeBytes: gib(40)},
			},
		}
	}
	autoGatherControlledBuildPlanHook = func(c *Core, path string) (*domain.Plan, error) {
		return stubRealCanonicalBundle(path, target, move).Plan, nil
	}
	autoGatherControlledExecuteHook = func(c *Core, plan *domain.Plan, tp string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: move}
	}
	autoGatherControlledVerifyHook = func(c *Core, sp, td string) domain.AutoGatherRealVerification {
		return domain.AutoGatherRealVerification{Passed: true, TargetDisk: td, Message: "consolidated"}
	}

	return func() {
		autoGatherControlledScanHook = prevScan
		autoGatherControlledRefreshHook = prevRefresh
		autoGatherControlledRescoreHook = prevRescore
		autoGatherControlledCanonicalHook = prevCanonical
		autoGatherControlledBuildPlanHook = prevBuild
		autoGatherControlledExecuteHook = prevExec
		autoGatherControlledVerifyHook = prevVerify
	}
}

func TestControlledRefusesDryRunTrue(t *testing.T) {
	c := newCoreForControlledTest()
	c.ctx.DryRun = true
	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 3, MaxBytes: gib(50)})
	if err == nil || !strings.Contains(err.Error(), "dry-run") {
		t.Fatalf("expected dry-run refusal, err=%v", err)
	}
}

func TestControlledRefusesWithoutConfirmation(t *testing.T) {
	c := newCoreForControlledTest()
	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: false, MaxShows: 3, MaxBytes: gib(50)})
	if err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("expected confirmation refusal, err=%v", err)
	}
}

func TestControlledCompletesOneShow(t *testing.T) {
	c := newCoreForControlledMarkerTest(t)
	defer installControlledHooksDefault("data/media/tv/A", "disk1", 100)()

	state, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if state.Phase != domain.AutoGatherControlledPhaseRunning {
		t.Fatalf("phase=%q", state.Phase)
	}
	if !controlledMarkerExists(c) {
		t.Fatal("active session must persist a marker")
	}
	time.Sleep(300 * time.Millisecond)
	final := c.GetAutoGatherControlledState()
	if final.Phase != domain.AutoGatherControlledPhaseCompleted {
		t.Fatalf("final phase=%q error=%q", final.Phase, final.FailureReason)
	}
	if len(final.Completed) != 1 || final.Completed[0].ShowPath != "data/media/tv/A" {
		t.Fatalf("completed=%#v", final.Completed)
	}
	if final.LibraryRevision == 0 || final.LibrarySummary == nil {
		t.Fatalf("expected compact library summary, got rev=%d summary=%#v", final.LibraryRevision, final.LibrarySummary)
	}
	if final.LibrarySummary.SplitCount != 0 || final.LibrarySummary.RecommendationCount != 0 {
		t.Fatalf("completed show should drop split/recommendation counts: %+v", final.LibrarySummary)
	}
	raw, err := json.Marshal(final)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if strings.Contains(string(raw), `"libraryScan"`) {
		t.Fatal("controlled/status must not include full libraryScan")
	}
	lib, ok := c.GetAutoGatherLibrary()
	if !ok || len(lib.Shows) != 1 {
		t.Fatalf("expected published library snapshot, got ok=%v %#v", ok, lib)
	}
	got := lib.Shows[0]
	if got.Split || got.Status != domain.AutoGatherStatusConsolidated || got.RecommendedTargetDisk != "" {
		t.Fatalf("completed show should be consolidated in published scan: %+v", got)
	}
	if controlledMarkerExists(c) {
		t.Fatal("completed session must remove the marker")
	}
}

func TestControlledMaxShowsEnforced(t *testing.T) {
	c := newCoreForControlledTest()
	showCount := 0
	prevScan := autoGatherControlledScanHook
	prevRefresh := autoGatherControlledRefreshHook
	prevRescore := autoGatherControlledRescoreHook
	prevCanonical := autoGatherControlledCanonicalHook
	prevBuild := autoGatherControlledBuildPlanHook
	prevExec := autoGatherControlledExecuteHook
	prevVerify := autoGatherControlledVerifyHook
	defer func() {
		autoGatherControlledScanHook = prevScan
		autoGatherControlledRefreshHook = prevRefresh
		autoGatherControlledRescoreHook = prevRescore
		autoGatherControlledCanonicalHook = prevCanonical
		autoGatherControlledBuildPlanHook = prevBuild
		autoGatherControlledExecuteHook = prevExec
		autoGatherControlledVerifyHook = prevVerify
	}()

	autoGatherControlledRefreshHook = func(c *Core) *domain.Unraid { return c.state.Unraid }
	autoGatherControlledScanHook = func(c *Core) domain.AutoGatherScanResult {
		showCount++
		shows := []domain.AutoGatherShow{}
		for i := 0; i < 10; i++ {
			shows = append(shows, domain.AutoGatherShow{
				Name: "Show", Path: "data/media/tv/Show" + string(rune('A'+i)), Status: domain.AutoGatherStatusSplit,
				Split: true, Ready: true, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 100,
				TotalVideoBytes: 100, TotalBytes: 100,
			})
		}
		return domain.AutoGatherScanResult{LibraryPath: "data/media/tv", Shows: shows}
	}
	autoGatherControlledRescoreHook = func(c *Core, base domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult {
		return base
	}
	autoGatherControlledCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		return domain.AutoGatherCanonicalPlanResult{
			ShowPath: show.Path, CanonicalRecommendedTarget: "disk1",
			Stage2TargetStillCanonicalEligible: true, CanonicalMoveBytes: 100,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true, CanonicalBytesToMove: 100, FreeBytes: gib(50), ProjectedFreeBytes: gib(40)},
			},
		}
	}
	autoGatherControlledBuildPlanHook = func(c *Core, path string) (*domain.Plan, error) {
		return stubRealCanonicalBundle(path, "disk1", 100).Plan, nil
	}
	autoGatherControlledExecuteHook = func(c *Core, plan *domain.Plan, tp string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 100}
	}
	autoGatherControlledVerifyHook = func(c *Core, sp, td string) domain.AutoGatherRealVerification {
		return domain.AutoGatherRealVerification{Passed: true, TargetDisk: td}
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 2, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	final := c.GetAutoGatherControlledState()
	if final.Phase != domain.AutoGatherControlledPhaseCompleted {
		t.Fatalf("phase=%q reason=%q", final.Phase, final.FailureReason)
	}
	if len(final.Completed) != 2 {
		t.Fatalf("expected 2 completed, got %d", len(final.Completed))
	}
	if final.LibrarySummary == nil || final.LibrarySummary.SplitCount != 8 || final.LibrarySummary.RecommendationCount != 8 {
		t.Fatalf("library summary after two moves = %+v, want split/recs 8/8", final.LibrarySummary)
	}
	lib, ok := c.GetAutoGatherLibrary()
	if !ok {
		t.Fatal("expected published library snapshot after real moves")
	}
	completed := map[string]struct{}{}
	for _, rec := range final.Completed {
		completed[rec.ShowPath] = struct{}{}
	}
	split := 0
	recs := 0
	for _, show := range lib.Shows {
		if _, ok := completed[show.Path]; ok {
			if show.Split || show.Status != domain.AutoGatherStatusConsolidated || show.RecommendedTargetDisk != "" {
				t.Fatalf("completed show %s still looks split in published scan: %+v", show.Path, show)
			}
			continue
		}
		if show.Status == domain.AutoGatherStatusSplit {
			split++
			if show.RecommendedTargetDisk != "" {
				recs++
			}
		}
	}
	if split != 8 || recs != 8 {
		t.Fatalf("published scan split=%d recs=%d, want 8/8 after two completed moves", split, recs)
	}
}

func TestControlledMaxBytesEnforced(t *testing.T) {
	c := newCoreForControlledTest()
	defer installControlledHooksDefault("data/media/tv/A", "disk1", gib(30))()
	// maxBytes=25 GB, candidate requires 30 GB → must NOT execute.
	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 10, MaxBytes: gib(25)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	final := c.GetAutoGatherControlledState()
	if final.Phase != domain.AutoGatherControlledPhaseCompleted {
		t.Fatalf("phase=%q reason=%q", final.Phase, final.FailureReason)
	}
	if len(final.Completed) != 0 {
		t.Fatalf("no show should be completed when it exceeds byte limit; got %d", len(final.Completed))
	}
	if len(final.Skipped) != 1 {
		t.Fatalf("over-budget show should be skipped; got %d", len(final.Skipped))
	}
	if !strings.Contains(final.Skipped[0].Reason, "exceeds remaining byte allowance") {
		t.Fatalf("skip reason should mention byte allowance: %q", final.Skipped[0].Reason)
	}
}

func TestControlledMaxBytesCumulativeNeverExceeded(t *testing.T) {
	c := newCoreForControlledTest()
	defer func() {
		autoGatherControlledScanHook = nil
		autoGatherControlledRefreshHook = nil
		autoGatherControlledRescoreHook = nil
		autoGatherControlledCanonicalHook = nil
		autoGatherControlledBuildPlanHook = nil
		autoGatherControlledExecuteHook = nil
		autoGatherControlledVerifyHook = nil
	}()

	autoGatherControlledRefreshHook = func(c *Core) *domain.Unraid { return c.state.Unraid }
	autoGatherControlledScanHook = func(c *Core) domain.AutoGatherScanResult {
		return domain.AutoGatherScanResult{
			LibraryPath: "data/media/tv",
			Shows: []domain.AutoGatherShow{
				{Name: "Small", Path: "data/media/tv/Small", Status: domain.AutoGatherStatusSplit, Split: true, Ready: true, RecommendedTargetDisk: "disk1", MoveRequiredBytes: gib(10), TotalVideoBytes: gib(10), TotalBytes: gib(10)},
				{Name: "Big", Path: "data/media/tv/Big", Status: domain.AutoGatherStatusSplit, Split: true, Ready: true, RecommendedTargetDisk: "disk1", MoveRequiredBytes: gib(30), TotalVideoBytes: gib(30), TotalBytes: gib(30)},
			},
		}
	}
	autoGatherControlledRescoreHook = func(c *Core, base domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult {
		return base
	}
	autoGatherControlledCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		return domain.AutoGatherCanonicalPlanResult{
			ShowPath: show.Path, CanonicalRecommendedTarget: "disk1",
			Stage2TargetStillCanonicalEligible: true, CanonicalMoveBytes: show.MoveRequiredBytes,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true, CanonicalBytesToMove: show.MoveRequiredBytes, FreeBytes: gib(100), ProjectedFreeBytes: gib(80)},
			},
		}
	}
	autoGatherControlledBuildPlanHook = func(c *Core, path string) (*domain.Plan, error) {
		move := gib(10)
		if strings.Contains(path, "Big") {
			move = gib(30)
		}
		return stubRealCanonicalBundle(path, "disk1", move).Plan, nil
	}
	autoGatherControlledExecuteHook = func(c *Core, plan *domain.Plan, tp string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: plan.VDisks[tp].Bin.Size}
	}
	autoGatherControlledVerifyHook = func(c *Core, sp, td string) domain.AutoGatherRealVerification {
		return domain.AutoGatherRealVerification{Passed: true, TargetDisk: td}
	}

	// maxBytes=20 GB: Small (10GB) fits; Big (30GB) cannot.
	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 10, MaxBytes: gib(20)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	final := c.GetAutoGatherControlledState()
	if final.Phase != domain.AutoGatherControlledPhaseCompleted {
		t.Fatalf("phase=%q reason=%q", final.Phase, final.FailureReason)
	}
	if len(final.Completed) != 1 {
		t.Fatalf("expected 1 completed (Small), got %d", len(final.Completed))
	}
	if final.Completed[0].ShowName != "Small" {
		t.Fatalf("expected Small to be completed, got %q", final.Completed[0].ShowName)
	}
	if final.CumulativeBytes > gib(20) {
		t.Fatalf("cumulative %d exceeds maxBytes %d", final.CumulativeBytes, gib(20))
	}
}

func TestControlledSessionCompletesWhenNoCandidateFits(t *testing.T) {
	c := newCoreForControlledTest()
	defer installControlledHooksDefault("data/media/tv/A", "disk1", gib(60))()
	// maxBytes=50 GB, only candidate is 60 GB → session completes cleanly.
	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 10, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	final := c.GetAutoGatherControlledState()
	if final.Phase != domain.AutoGatherControlledPhaseCompleted {
		t.Fatalf("phase=%q reason=%q", final.Phase, final.FailureReason)
	}
	if len(final.Completed) != 0 {
		t.Fatalf("no shows should complete, got %d", len(final.Completed))
	}
}

func TestControlledStopBetweenShows(t *testing.T) {
	c := newCoreForControlledTest()
	iteration := int32(0)
	prevScan := autoGatherControlledScanHook
	prevRefresh := autoGatherControlledRefreshHook
	prevRescore := autoGatherControlledRescoreHook
	prevCanonical := autoGatherControlledCanonicalHook
	prevBuild := autoGatherControlledBuildPlanHook
	prevExec := autoGatherControlledExecuteHook
	prevVerify := autoGatherControlledVerifyHook
	defer func() {
		autoGatherControlledScanHook = prevScan
		autoGatherControlledRefreshHook = prevRefresh
		autoGatherControlledRescoreHook = prevRescore
		autoGatherControlledCanonicalHook = prevCanonical
		autoGatherControlledBuildPlanHook = prevBuild
		autoGatherControlledExecuteHook = prevExec
		autoGatherControlledVerifyHook = prevVerify
	}()

	autoGatherControlledRefreshHook = func(c *Core) *domain.Unraid { return c.state.Unraid }
	autoGatherControlledScanHook = func(c *Core) domain.AutoGatherScanResult {
		n := atomic.AddInt32(&iteration, 1)
		if n == 2 {
			// After first show completed, request stop before second scan returns.
			c.StopAutoGatherControlled()
		}
		return domain.AutoGatherScanResult{
			LibraryPath: "data/media/tv",
			Shows: []domain.AutoGatherShow{
				{Name: "Show A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit, Split: true, Ready: true, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 100, TotalVideoBytes: 100, TotalBytes: 100},
				{Name: "Show B", Path: "data/media/tv/B", Status: domain.AutoGatherStatusSplit, Split: true, Ready: true, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 200, TotalVideoBytes: 200, TotalBytes: 200},
			},
		}
	}
	autoGatherControlledRescoreHook = func(c *Core, base domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult {
		return base
	}
	autoGatherControlledCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		return domain.AutoGatherCanonicalPlanResult{
			ShowPath: show.Path, CanonicalRecommendedTarget: "disk1",
			Stage2TargetStillCanonicalEligible: true, CanonicalMoveBytes: show.MoveRequiredBytes,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true, CanonicalBytesToMove: show.MoveRequiredBytes, FreeBytes: gib(50), ProjectedFreeBytes: gib(40)},
			},
		}
	}
	autoGatherControlledBuildPlanHook = func(c *Core, path string) (*domain.Plan, error) {
		return stubRealCanonicalBundle(path, "disk1", 100).Plan, nil
	}
	autoGatherControlledExecuteHook = func(c *Core, plan *domain.Plan, tp string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 100}
	}
	autoGatherControlledVerifyHook = func(c *Core, sp, td string) domain.AutoGatherRealVerification {
		return domain.AutoGatherRealVerification{Passed: true, TargetDisk: td}
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 10, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	final := c.GetAutoGatherControlledState()
	if final.Phase != domain.AutoGatherControlledPhaseStopped {
		t.Fatalf("expected stopped, got phase=%q", final.Phase)
	}
	if len(final.Completed) != 1 {
		t.Fatalf("expected 1 completed before stop, got %d", len(final.Completed))
	}
}

func TestControlledStopDuringExecution(t *testing.T) {
	c := newCoreForControlledMarkerTest(t)
	defer func() {
		autoGatherControlledScanHook = nil
		autoGatherControlledRefreshHook = nil
		autoGatherControlledRescoreHook = nil
		autoGatherControlledCanonicalHook = nil
		autoGatherControlledBuildPlanHook = nil
		autoGatherControlledExecuteHook = nil
		autoGatherControlledVerifyHook = nil
	}()

	autoGatherControlledRefreshHook = func(c *Core) *domain.Unraid { return c.state.Unraid }
	autoGatherControlledScanHook = func(c *Core) domain.AutoGatherScanResult {
		return domain.AutoGatherScanResult{
			LibraryPath: "data/media/tv",
			Shows:       []domain.AutoGatherShow{{Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit, Split: true, Ready: true, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 100, TotalVideoBytes: 100, TotalBytes: 100}},
		}
	}
	autoGatherControlledRescoreHook = func(c *Core, base domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult {
		return base
	}
	autoGatherControlledCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		return domain.AutoGatherCanonicalPlanResult{
			ShowPath: show.Path, CanonicalRecommendedTarget: "disk1",
			Stage2TargetStillCanonicalEligible: true, CanonicalMoveBytes: 100,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true, CanonicalBytesToMove: 100, FreeBytes: gib(50), ProjectedFreeBytes: gib(40)},
			},
		}
	}
	autoGatherControlledBuildPlanHook = func(c *Core, path string) (*domain.Plan, error) {
		return stubRealCanonicalBundle(path, "disk1", 100).Plan, nil
	}
	autoGatherControlledExecuteHook = func(c *Core, plan *domain.Plan, tp string) gatherExecResult {
		c.StopAutoGatherControlled()
		return gatherExecResult{outcome: gatherExecCancelled, reason: "stopped"}
	}
	autoGatherControlledVerifyHook = func(c *Core, sp, td string) domain.AutoGatherRealVerification {
		return domain.AutoGatherRealVerification{Passed: true}
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	final := c.GetAutoGatherControlledState()
	if final.Phase != domain.AutoGatherControlledPhaseStopped {
		t.Fatalf("expected stopped after exec cancel, got %q", final.Phase)
	}
	if len(final.Completed) != 0 {
		t.Fatal("should not complete any show when stopped during exec")
	}
	if controlledMarkerExists(c) {
		t.Fatal("explicit Stop after command termination must remove the marker")
	}
}

func TestControlledVerificationFailureHalts(t *testing.T) {
	c := newCoreForControlledTest()
	defer installControlledHooksDefault("data/media/tv/A", "disk1", 100)()
	autoGatherControlledVerifyHook = func(c *Core, sp, td string) domain.AutoGatherRealVerification {
		return domain.AutoGatherRealVerification{Passed: false, Message: "data remains on other disks"}
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	final := c.GetAutoGatherControlledState()
	if final.Phase != domain.AutoGatherControlledPhaseFailed {
		t.Fatalf("expected failed, got %q", final.Phase)
	}
	if !strings.Contains(final.FailureReason, "data remains") {
		t.Fatalf("reason=%q", final.FailureReason)
	}
}

func TestControlledExecutionFailureHalts(t *testing.T) {
	c := newCoreForControlledTest()
	defer installControlledHooksDefault("data/media/tv/A", "disk1", 100)()
	autoGatherControlledExecuteHook = func(c *Core, plan *domain.Plan, tp string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecUncertain, reason: "rsync error"}
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	final := c.GetAutoGatherControlledState()
	if final.Phase != domain.AutoGatherControlledPhaseFailed {
		t.Fatalf("expected failed, got %q reason=%q", final.Phase, final.FailureReason)
	}
	if !strings.Contains(final.FailureReason, "rsync error") {
		t.Fatalf("reason=%q", final.FailureReason)
	}
}

func TestControlledFingerprintChangeHalts(t *testing.T) {
	c := newCoreForControlledTest()
	defer installControlledHooksDefault("data/media/tv/A", "disk1", 100)()
	callCount := int32(0)
	autoGatherControlledCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		n := atomic.AddInt32(&callCount, 1)
		move := uint64(100)
		if n > 1 {
			move = 999 // changed plan on revalidation
		}
		return domain.AutoGatherCanonicalPlanResult{
			ShowPath: show.Path, CanonicalRecommendedTarget: "disk1",
			Stage2TargetStillCanonicalEligible: true, CanonicalMoveBytes: move,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true, CanonicalBytesToMove: move, FreeBytes: gib(50), ProjectedFreeBytes: gib(40)},
			},
		}
	}
	autoGatherControlledBuildPlanHook = func(c *Core, path string) (*domain.Plan, error) {
		n := atomic.LoadInt32(&callCount)
		move := uint64(100)
		if n > 1 {
			move = 999
		}
		return stubRealCanonicalBundle(path, "disk1", move).Plan, nil
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	final := c.GetAutoGatherControlledState()
	if final.Phase != domain.AutoGatherControlledPhaseFailed {
		t.Fatalf("expected failed, got %q", final.Phase)
	}
	if !strings.Contains(final.FailureReason, "fingerprint") {
		t.Fatalf("reason=%q", final.FailureReason)
	}
}

func TestControlledStructuralPlanFailureHalts(t *testing.T) {
	c := newCoreForControlledTest()
	defer installControlledHooksDefault("data/media/tv/A", "disk1", 100)()
	autoGatherControlledBuildPlanHook = func(c *Core, path string) (*domain.Plan, error) {
		plan := stubRealCanonicalBundle(path, "disk1", 100).Plan
		plan.VDisks["/mnt/disk1"].Bin.Items = nil
		plan.VDisks["/mnt/disk1"].Bin.Size = 0
		return plan, nil
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	final := c.GetAutoGatherControlledState()
	if final.Phase != domain.AutoGatherControlledPhaseFailed {
		t.Fatalf("expected failed, got %q", final.Phase)
	}
	if !strings.Contains(final.FailureReason, "no executable items") {
		t.Fatalf("reason=%q", final.FailureReason)
	}
}

func TestControlledPermissionWarningsDoNotBlock(t *testing.T) {
	c := newCoreForControlledTest()
	defer installControlledHooksDefault("data/media/tv/A", "disk1", 100)()
	autoGatherControlledBuildPlanHook = func(c *Core, path string) (*domain.Plan, error) {
		plan := stubRealCanonicalBundle(path, "disk1", 100).Plan
		plan.FolderIssue = 26
		plan.FileIssue = 38
		return plan, nil
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	final := c.GetAutoGatherControlledState()
	if final.Phase != domain.AutoGatherControlledPhaseCompleted {
		t.Fatalf("permission warnings should not block; phase=%q reason=%q", final.Phase, final.FailureReason)
	}
	if len(final.Completed) != 1 {
		t.Fatalf("expected 1 completed, got %d", len(final.Completed))
	}
}

func TestControlledBlocksDryRun(t *testing.T) {
	c := newCoreForControlledTest()
	defer installControlledHooksDefault("data/media/tv/A", "disk1", 100)()
	// Slow the execution so we can attempt dry-run while controlled is running.
	autoGatherControlledExecuteHook = func(c *Core, plan *domain.Plan, tp string) gatherExecResult {
		time.Sleep(200 * time.Millisecond)
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 100}
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	c.ctx.DryRun = true
	_, dryErr := c.StartAutoGatherDryRun()
	c.ctx.DryRun = false
	if dryErr == nil || !strings.Contains(dryErr.Error(), "Controlled Auto Gather") {
		t.Fatalf("dry-run should be blocked during controlled session, err=%v", dryErr)
	}
	time.Sleep(500 * time.Millisecond)
}

func TestControlledBlocksStage3C(t *testing.T) {
	c := newCoreForControlledTest()
	defer installControlledHooksDefault("data/media/tv/A", "disk1", 100)()
	autoGatherControlledExecuteHook = func(c *Core, plan *domain.Plan, tp string) gatherExecResult {
		time.Sleep(200 * time.Millisecond)
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 100}
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	if prep.Error == "" || !strings.Contains(prep.Error, "Controlled Auto Gather") {
		t.Fatalf("Stage 3C prepare should be blocked, error=%q", prep.Error)
	}
	time.Sleep(500 * time.Millisecond)
}

func TestControlledBlocksManualGatherScatter(t *testing.T) {
	c := newCoreForControlledTest()
	defer installControlledHooksDefault("data/media/tv/A", "disk1", 100)()
	autoGatherControlledExecuteHook = func(c *Core, plan *domain.Plan, tp string) gatherExecResult {
		time.Sleep(200 * time.Millisecond)
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 100}
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	// Verify the state status blocks manual operations.
	if c.state.Status == common.OpNeutral {
		t.Fatal("state.Status should not be Neutral during controlled session")
	}
	time.Sleep(500 * time.Millisecond)
}

func TestControlledBrowserRecovery(t *testing.T) {
	c := newCoreForControlledTest()
	defer installControlledHooksDefault("data/media/tv/A", "disk1", 100)()
	autoGatherControlledExecuteHook = func(c *Core, plan *domain.Plan, tp string) gatherExecResult {
		time.Sleep(200 * time.Millisecond)
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 100}
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	// Simulate browser refresh: read status.
	status := c.GetAutoGatherControlledState()
	if status.Phase != domain.AutoGatherControlledPhaseRunning && status.Phase != domain.AutoGatherControlledPhaseCompleted {
		t.Fatalf("browser refresh must return running state, got %q", status.Phase)
	}
	if status.MaxShows != 5 || status.MaxBytes != gib(50) {
		t.Fatalf("bounds must be recoverable: maxShows=%d maxBytes=%d", status.MaxShows, status.MaxBytes)
	}
	time.Sleep(500 * time.Millisecond)
}

func TestControlledDaemonRestartDoesNotResume(t *testing.T) {
	c := newCoreForControlledTest()
	// Simulate a "restarted" core with no controlled state.
	state := c.GetAutoGatherControlledState()
	if state.Phase != domain.AutoGatherControlledPhaseIdle {
		t.Fatalf("fresh core must report idle, got %q", state.Phase)
	}
	if c.isAutoGatherControlledActive() {
		t.Fatal("fresh core must not be controlled-active")
	}
}

func TestControlledInterruptedMarkerOnRestart(t *testing.T) {
	c := newCoreForControlledMarkerTest(t)
	c.autoGatherControlledRun = &domain.AutoGatherControlledState{
		Phase:     domain.AutoGatherControlledPhaseRunning,
		SessionID: "sess-restart",
		MaxShows:  3,
		MaxBytes:  gib(50),
	}
	c.writeAutoGatherControlledMarker()
	c.RecoverAutoGatherControlledInterrupted()
	state := c.GetAutoGatherControlledState()
	if state.Phase != domain.AutoGatherControlledPhaseInterrupted {
		t.Fatalf("expected interrupted, got %q", state.Phase)
	}
	if c.isAutoGatherControlledActive() {
		t.Fatal("interrupted must not be active")
	}
	if !state.RequiresAcknowledgement {
		t.Fatal("interrupted state must require acknowledgement")
	}
	if !controlledMarkerExists(c) {
		t.Fatal("recovery must keep the marker until acknowledgement")
	}

	defer installControlledHooksDefault("data/media/tv/A", "disk1", 100)()
	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 1, MaxBytes: gib(50)})
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("start must be blocked while interrupted, err=%v", err)
	}
}

func TestControlledFreshScanBetweenMoves(t *testing.T) {
	c := newCoreForControlledTest()
	scanCount := int32(0)
	refreshCount := int32(0)
	defer func() {
		autoGatherControlledScanHook = nil
		autoGatherControlledRefreshHook = nil
		autoGatherControlledRescoreHook = nil
		autoGatherControlledCanonicalHook = nil
		autoGatherControlledBuildPlanHook = nil
		autoGatherControlledExecuteHook = nil
		autoGatherControlledVerifyHook = nil
	}()

	autoGatherControlledRefreshHook = func(c *Core) *domain.Unraid {
		atomic.AddInt32(&refreshCount, 1)
		return c.state.Unraid
	}
	autoGatherControlledScanHook = func(c *Core) domain.AutoGatherScanResult {
		n := atomic.AddInt32(&scanCount, 1)
		if n > 2 {
			return domain.AutoGatherScanResult{LibraryPath: "data/media/tv", Shows: []domain.AutoGatherShow{}}
		}
		return domain.AutoGatherScanResult{
			LibraryPath: "data/media/tv",
			Shows: []domain.AutoGatherShow{
				{Name: "Show", Path: "data/media/tv/S" + string(rune('0'+n)), Status: domain.AutoGatherStatusSplit, Split: true, Ready: true, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 100, TotalVideoBytes: 100, TotalBytes: 100},
			},
		}
	}
	autoGatherControlledRescoreHook = func(c *Core, base domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult {
		return base
	}
	autoGatherControlledCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		return domain.AutoGatherCanonicalPlanResult{
			ShowPath: show.Path, CanonicalRecommendedTarget: "disk1",
			Stage2TargetStillCanonicalEligible: true, CanonicalMoveBytes: 100,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true, CanonicalBytesToMove: 100, FreeBytes: gib(50), ProjectedFreeBytes: gib(40)},
			},
		}
	}
	autoGatherControlledBuildPlanHook = func(c *Core, path string) (*domain.Plan, error) {
		return stubRealCanonicalBundle(path, "disk1", 100).Plan, nil
	}
	autoGatherControlledExecuteHook = func(c *Core, plan *domain.Plan, tp string) gatherExecResult {
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 100}
	}
	autoGatherControlledVerifyHook = func(c *Core, sp, td string) domain.AutoGatherRealVerification {
		return domain.AutoGatherRealVerification{Passed: true, TargetDisk: td}
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	final := c.GetAutoGatherControlledState()
	if final.Phase != domain.AutoGatherControlledPhaseCompleted {
		t.Fatalf("phase=%q reason=%q", final.Phase, final.FailureReason)
	}
	if len(final.Completed) != 2 {
		t.Fatalf("expected 2 completed, got %d", len(final.Completed))
	}
	scans := atomic.LoadInt32(&scanCount)
	refreshes := atomic.LoadInt32(&refreshCount)
	// At least 2 full scans (one per iteration) and multiple refreshes.
	if scans < 2 {
		t.Fatalf("expected at least 2 fresh scans, got %d", scans)
	}
	if refreshes < 2 {
		t.Fatalf("expected at least 2 refreshes, got %d", refreshes)
	}
}

func TestControlledNoBatchParallelExecution(t *testing.T) {
	c := newCoreForControlledTest()
	maxConcurrent := int32(0)
	current := int32(0)
	defer func() {
		autoGatherControlledScanHook = nil
		autoGatherControlledRefreshHook = nil
		autoGatherControlledRescoreHook = nil
		autoGatherControlledCanonicalHook = nil
		autoGatherControlledBuildPlanHook = nil
		autoGatherControlledExecuteHook = nil
		autoGatherControlledVerifyHook = nil
	}()

	autoGatherControlledRefreshHook = func(c *Core) *domain.Unraid { return c.state.Unraid }
	autoGatherControlledScanHook = func(c *Core) domain.AutoGatherScanResult {
		return domain.AutoGatherScanResult{
			LibraryPath: "data/media/tv",
			Shows: []domain.AutoGatherShow{
				{Name: "A", Path: "data/media/tv/A", Status: domain.AutoGatherStatusSplit, Split: true, Ready: true, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 100, TotalVideoBytes: 100, TotalBytes: 100},
				{Name: "B", Path: "data/media/tv/B", Status: domain.AutoGatherStatusSplit, Split: true, Ready: true, RecommendedTargetDisk: "disk1", MoveRequiredBytes: 200, TotalVideoBytes: 200, TotalBytes: 200},
			},
		}
	}
	autoGatherControlledRescoreHook = func(c *Core, base domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult {
		return base
	}
	autoGatherControlledCanonicalHook = func(c *Core, show domain.AutoGatherShow) domain.AutoGatherCanonicalPlanResult {
		return domain.AutoGatherCanonicalPlanResult{
			ShowPath: show.Path, CanonicalRecommendedTarget: "disk1",
			Stage2TargetStillCanonicalEligible: true, CanonicalMoveBytes: show.MoveRequiredBytes,
			CanonicalTargets: []domain.AutoGatherCanonicalTarget{
				{DiskName: "disk1", DiskPath: "/mnt/disk1", IsPhysicalArrayDisk: true, CanonicalEligible: true, MeetsPreferredFreeFloor: true, CanonicalBytesToMove: show.MoveRequiredBytes, FreeBytes: gib(50), ProjectedFreeBytes: gib(40)},
			},
		}
	}
	autoGatherControlledBuildPlanHook = func(c *Core, path string) (*domain.Plan, error) {
		return stubRealCanonicalBundle(path, "disk1", 100).Plan, nil
	}
	autoGatherControlledExecuteHook = func(c *Core, plan *domain.Plan, tp string) gatherExecResult {
		cur := atomic.AddInt32(&current, 1)
		if cur > atomic.LoadInt32(&maxConcurrent) {
			atomic.StoreInt32(&maxConcurrent, cur)
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&current, -1)
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 100}
	}
	autoGatherControlledVerifyHook = func(c *Core, sp, td string) domain.AutoGatherRealVerification {
		return domain.AutoGatherRealVerification{Passed: true, TargetDisk: td}
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 3, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(800 * time.Millisecond)
	if atomic.LoadInt32(&maxConcurrent) > 1 {
		t.Fatalf("max concurrent executions must be 1, got %d", maxConcurrent)
	}
}

func TestControlledCannotStartTwice(t *testing.T) {
	c := newCoreForControlledTest()
	defer installControlledHooksDefault("data/media/tv/A", "disk1", 100)()
	autoGatherControlledExecuteHook = func(c *Core, plan *domain.Plan, tp string) gatherExecResult {
		time.Sleep(200 * time.Millisecond)
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 100}
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	_, err2 := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err2 == nil || !strings.Contains(err2.Error(), "already active") {
		t.Fatalf("second start should fail, err=%v", err2)
	}
	time.Sleep(500 * time.Millisecond)
}

func TestControlledShutdownLeavesInterruptedMarker(t *testing.T) {
	c := newCoreForControlledMarkerTest(t)
	defer installControlledHooksDefault("data/media/tv/A", "disk1", 100)()
	autoGatherControlledExecuteHook = func(c *Core, plan *domain.Plan, tp string) gatherExecResult {
		c.noteControlledRsyncChild(4242, "Show/Season 1")
		time.Sleep(200 * time.Millisecond)
		return gatherExecResult{outcome: gatherExecSuccess, moveBytes: 100}
	}

	_, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 5, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := c.Stop(); err != nil {
		t.Fatalf("daemon stop: %v", err)
	}
	if !controlledMarkerExists(c) {
		t.Fatal("daemon shutdown during execution must leave the marker")
	}
	time.Sleep(400 * time.Millisecond)
	if !controlledMarkerExists(c) {
		t.Fatal("loop completion after shutdown must not remove the marker")
	}

	c2 := newCoreForControlledTest()
	c2.ctx.DataDir = c.ctx.DataDir
	c2.RecoverAutoGatherControlledInterrupted()
	state := c2.GetAutoGatherControlledState()
	if state.Phase != domain.AutoGatherControlledPhaseInterrupted {
		t.Fatalf("restart must report interrupted, got %q", state.Phase)
	}
}

func TestControlledInterruptedBlocksNewOperationsUntilAck(t *testing.T) {
	c := newCoreForControlledMarkerTest(t)
	c.autoGatherControlledRun = &domain.AutoGatherControlledState{
		Phase:           domain.AutoGatherControlledPhaseRunning,
		SessionID:       "sess-1",
		CurrentShow:     "data/media/tv/A",
		CurrentTarget:   "disk1",
		LastRsyncPID:    4242,
		LastSourceEntry: "Show/file",
	}
	c.writeAutoGatherControlledMarker()
	c.autoGatherControlledRun = nil
	c.RecoverAutoGatherControlledInterrupted()

	if c.mailboxAllows(common.CommandGatherPlanStart) || c.mailboxAllows(common.CommandScatterPlanStart) {
		t.Fatal("manual Gather/Scatter must be blocked while interrupted")
	}
	if c.mailboxAllows(common.CommandGatherMove) || c.mailboxAllows(common.CommandScatterMove) {
		t.Fatal("manual move commands must be blocked while interrupted")
	}

	prep := c.PrepareAutoGatherReal(domain.AutoGatherRealPrepareRequest{ShowPath: "data/media/tv/A"})
	if prep.Error == "" || !strings.Contains(prep.Error, "interrupted") {
		t.Fatalf("Stage 3C prepare must be blocked, error=%q", prep.Error)
	}
	_, execErr := c.ExecuteAutoGatherReal(domain.AutoGatherRealExecuteRequest{PreparationID: "x", ShowPath: "data/media/tv/A", Confirm: true})
	if execErr == nil || !strings.Contains(execErr.Error(), "interrupted") {
		t.Fatalf("Stage 3C execute must be blocked, err=%v", execErr)
	}
	c.ctx.DryRun = true
	_, dryErr := c.StartAutoGatherDryRun()
	c.ctx.DryRun = false
	if dryErr == nil || !strings.Contains(dryErr.Error(), "interrupted") {
		t.Fatalf("Stage 3B must be blocked, err=%v", dryErr)
	}
	_, startErr := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 1, MaxBytes: gib(50)})
	if startErr == nil || !strings.Contains(startErr.Error(), "interrupted") {
		t.Fatalf("Stage 3D start must be blocked, err=%v", startErr)
	}

	_, ackErr := c.AcknowledgeAutoGatherControlledInterrupted(false)
	if ackErr == nil {
		t.Fatal("acknowledgement without confirm must fail")
	}
	if !controlledMarkerExists(c) {
		t.Fatal("failed acknowledgement must not clear the marker")
	}

	acked, err := c.AcknowledgeAutoGatherControlledInterrupted(true)
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	if acked.Phase != domain.AutoGatherControlledPhaseIdle {
		t.Fatalf("ack must return idle, got %q", acked.Phase)
	}
	if controlledMarkerExists(c) {
		t.Fatal("acknowledgement must remove the marker")
	}
	if c.isAutoGatherControlledInterrupted() {
		t.Fatal("acknowledged state must not remain interrupted")
	}
	if !c.mailboxAllows(common.CommandGatherPlanStart) || !c.mailboxAllows(common.CommandScatterPlanStart) {
		t.Fatal("manual Gather/Scatter must be allowed after acknowledgement")
	}

	defer installControlledHooksDefault("data/media/tv/A", "disk1", 100)()
	_, err = c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 1, MaxBytes: gib(50)})
	if err != nil {
		t.Fatalf("start after ack must succeed: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	final := c.GetAutoGatherControlledState()
	if final.Phase != domain.AutoGatherControlledPhaseCompleted {
		t.Fatalf("ack must not resume the old session; new session phase=%q", final.Phase)
	}
}

func TestControlledHardCrashMarkerRecovery(t *testing.T) {
	c := newCoreForControlledMarkerTest(t)
	c.autoGatherControlledRun = &domain.AutoGatherControlledState{
		Phase:           domain.AutoGatherControlledPhaseRunning,
		SessionID:       "crash-1",
		CurrentShow:     "data/media/tv/A",
		CurrentShowName: "A",
		CurrentTarget:   "disk1",
		LastRsyncPID:    99,
		LastSourceEntry: "A/Season 1",
		MaxShows:        3,
		MaxBytes:        gib(50),
	}
	c.writeAutoGatherControlledMarker()

	c2 := newCoreForControlledTest()
	c2.ctx.DataDir = c.ctx.DataDir
	c2.RecoverAutoGatherControlledInterrupted()
	state := c2.GetAutoGatherControlledState()
	if state.Phase != domain.AutoGatherControlledPhaseInterrupted {
		t.Fatalf("hard crash recovery must be interrupted, got %q", state.Phase)
	}
	if state.CurrentShow != "data/media/tv/A" || state.CurrentTarget != "disk1" {
		t.Fatalf("diagnostics not restored: %+v", state)
	}
	if state.LastRsyncPID != 99 || state.LastSourceEntry != "A/Season 1" {
		t.Fatalf("rsync diagnostics not restored: pid=%d entry=%q", state.LastRsyncPID, state.LastSourceEntry)
	}
	if state.RsyncProbe == nil {
		t.Fatal("expected rsync probe")
	}
	if state.RsyncProbe.Alive {
		t.Fatal("stale PID must not be treated as a live rsync on this host")
	}
	if !controlledMarkerExists(c2) {
		t.Fatal("hard crash recovery must keep the marker")
	}
}

func recoverInterruptedWithPID(t *testing.T, pid int) *Core {
	t.Helper()
	c := newCoreForControlledMarkerTest(t)
	c.autoGatherControlledRun = &domain.AutoGatherControlledState{
		Phase:           domain.AutoGatherControlledPhaseRunning,
		SessionID:       "pid-lock",
		CurrentShow:     "data/media/tv/A",
		CurrentShowName: "A",
		CurrentTarget:   "disk1",
		LastRsyncPID:    pid,
		LastSourceEntry: "A/Season 1",
	}
	c.writeAutoGatherControlledMarker()
	c.autoGatherControlledRun = nil
	c.RecoverAutoGatherControlledInterrupted()
	return c
}

func TestControlledLiveRsyncPIDBlocksAcknowledgement(t *testing.T) {
	c := recoverInterruptedWithPID(t, 777)
	t.Cleanup(func() { autoGatherControlledRsyncProbeHook = nil })
	autoGatherControlledRsyncProbeHook = func(pid int) domain.AutoGatherControlledRsyncProbe {
		return domain.AutoGatherControlledRsyncProbe{
			PID:            pid,
			Alive:          true,
			PlausibleRsync: true,
			Command:        "rsync",
			Note:           "live rsync",
		}
	}

	state := c.GetAutoGatherControlledState()
	if state.CanAcknowledge {
		t.Fatal("live rsync must block acknowledgement in status")
	}
	acked, err := c.AcknowledgeAutoGatherControlledInterrupted(true)
	if err == nil || !strings.Contains(err.Error(), "still appears active") {
		t.Fatalf("expected live-rsync refusal, err=%v", err)
	}
	if acked.Phase != domain.AutoGatherControlledPhaseInterrupted {
		t.Fatalf("phase must remain interrupted, got %q", acked.Phase)
	}
	if !acked.RequiresAcknowledgement {
		t.Fatal("requiresAcknowledgement must remain true")
	}
	if acked.CanAcknowledge {
		t.Fatal("canAcknowledge must be false while rsync is live")
	}
	if !controlledMarkerExists(c) {
		t.Fatal("refused acknowledgement must keep the marker")
	}
	_, startErr := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{Confirm: true, MaxShows: 1, MaxBytes: gib(50)})
	if startErr == nil {
		t.Fatal("refused acknowledgement must not allow a new session")
	}

	autoGatherControlledRsyncProbeHook = func(pid int) domain.AutoGatherControlledRsyncProbe {
		return domain.AutoGatherControlledRsyncProbe{PID: pid, Alive: false, Note: "pid gone"}
	}
	cleared, err := c.AcknowledgeAutoGatherControlledInterrupted(true)
	if err != nil {
		t.Fatalf("ack after pid gone: %v", err)
	}
	if cleared.Phase != domain.AutoGatherControlledPhaseIdle {
		t.Fatalf("ack after pid gone must be idle, got %q", cleared.Phase)
	}
	if controlledMarkerExists(c) {
		t.Fatal("successful ack must remove the marker")
	}
	if c.isAutoGatherControlledInterrupted() {
		t.Fatal("successful ack must not remain interrupted")
	}
}

func TestControlledNonRsyncPIDDoesNotBlockAck(t *testing.T) {
	c := recoverInterruptedWithPID(t, 888)
	t.Cleanup(func() { autoGatherControlledRsyncProbeHook = nil })
	autoGatherControlledRsyncProbeHook = func(pid int) domain.AutoGatherControlledRsyncProbe {
		return domain.AutoGatherControlledRsyncProbe{
			PID:            pid,
			Alive:          true,
			PlausibleRsync: false,
			Command:        "bash",
			Note:           "pid reuse",
		}
	}
	state := c.GetAutoGatherControlledState()
	if !state.CanAcknowledge {
		t.Fatal("non-rsync PID must not block acknowledgement forever")
	}
	acked, err := c.AcknowledgeAutoGatherControlledInterrupted(true)
	if err != nil {
		t.Fatalf("non-rsync PID should not block ack: %v", err)
	}
	if acked.Phase != domain.AutoGatherControlledPhaseIdle {
		t.Fatalf("expected idle, got %q", acked.Phase)
	}
}
