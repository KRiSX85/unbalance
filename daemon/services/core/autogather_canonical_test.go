package core

import (
	"path/filepath"
	"testing"

	"unbalance/daemon/common"
	"unbalance/daemon/domain"
)

func TestValidateAutoGatherCanonicalSelection(t *testing.T) {
	if _, err := validateAutoGatherCanonicalSelection(nil); err == nil {
		t.Fatal("expected reject empty selection")
	}
	if _, err := validateAutoGatherCanonicalSelection([]string{}); err == nil {
		t.Fatal("expected reject zero selections")
	}
	if _, err := validateAutoGatherCanonicalSelection([]string{"a", "b"}); err == nil {
		t.Fatal("expected reject multiple selections")
	}
	if _, err := validateAutoGatherCanonicalSelection([]string{""}); err == nil {
		t.Fatal("expected reject blank path")
	}
	if _, err := validateAutoGatherCanonicalSelection([]string{"/mnt/user/data/media/tv/Show"}); err == nil {
		t.Fatal("expected reject absolute path")
	}
	got, err := validateAutoGatherCanonicalSelection([]string{"data/media/tv/Britannia (2018)"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "data/media/tv/Britannia (2018)" {
		t.Fatalf("got %q", got)
	}
}

func TestCanonicalEligibleRequiresBinAndArrayDisk(t *testing.T) {
	disks := []*domain.Disk{
		{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(50)},
		{Name: "cache", Path: "/mnt/cache", Type: "Cache", Size: gib(100), Free: gib(90)},
		{Name: "disk0", Path: "/mnt/disk0", Type: "Data", Size: gib(100), Free: gib(90)},
		{Name: "data_cache", Path: "/mnt/data_cache", Type: "Cache", Size: gib(100), Free: gib(90)},
		{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(5)},
	}
	plan := &domain.Plan{VDisks: map[string]*domain.VDisk{
		"/mnt/disk1":      {Path: "/mnt/disk1", Bin: &domain.Bin{Size: gib(10), Items: []*domain.Item{{Size: gib(10)}}}},
		"/mnt/cache":      {Path: "/mnt/cache", Bin: &domain.Bin{Size: gib(10), Items: []*domain.Item{{Size: gib(10)}}}},
		"/mnt/disk0":      {Path: "/mnt/disk0", Bin: &domain.Bin{Size: gib(10), Items: []*domain.Item{{Size: gib(10)}}}},
		"/mnt/data_cache": {Path: "/mnt/data_cache", Bin: &domain.Bin{Size: gib(10), Items: []*domain.Item{{Size: gib(10)}}}},
		"/mnt/disk8":      {Path: "/mnt/disk8", Bin: nil}, // physical but no Bin
	}}

	got := assembleAutoGatherCanonicalResult(domain.AutoGatherCanonicalPlanResult{
		Stage2RecommendedTarget: "disk1",
	}, plan, disks, nil, 0)

	byName := map[string]domain.AutoGatherCanonicalTarget{}
	for _, tgt := range got.CanonicalTargets {
		byName[tgt.DiskName] = tgt
	}

	if !byName["disk1"].CanonicalEligible || !byName["disk1"].RawGatherBinPresent {
		t.Fatalf("disk1 should be eligible: %+v", byName["disk1"])
	}
	for _, name := range []string{"cache", "disk0", "data_cache"} {
		if byName[name].CanonicalEligible {
			t.Fatalf("%s must not be Auto Gather eligible despite raw Bin", name)
		}
		if !byName[name].RawGatherBinPresent {
			t.Fatalf("%s should retain RawGatherBinPresent for debug", name)
		}
	}
	if byName["disk8"].CanonicalEligible || byName["disk8"].RawGatherBinPresent {
		t.Fatalf("disk8 without Bin must be ineligible: %+v", byName["disk8"])
	}
	if !got.Stage2TargetStillCanonicalEligible {
		t.Fatal("stage2 disk1 should still be canonical eligible")
	}
	if got.CanonicalRecommendedTarget != "disk1" {
		t.Fatalf("canonical recommended = %q", got.CanonicalRecommendedTarget)
	}
}

func TestCanonicalStage2StaleSelectsBalancedAlternative(t *testing.T) {
	// Stage 2 wanted disk1 (least move) but disk1 no longer has a Bin.
	// disk8 meets >=10% floor; disk9 is below floor with same move as disk8 would lose.
	disks := []*domain.Disk{
		{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(2)},
		{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(40)},
		{Name: "disk9", Path: "/mnt/disk9", Type: "Data", Size: gib(100), Free: gib(12)},
	}
	plan := &domain.Plan{VDisks: map[string]*domain.VDisk{
		"/mnt/disk1": {Path: "/mnt/disk1", Bin: nil},
		"/mnt/disk8": {Path: "/mnt/disk8", Bin: &domain.Bin{Size: gib(20), Items: []*domain.Item{{Size: gib(20)}}}},
		// disk9: move 20, projected 12-20 underflow → 0% with byte mode via projectedFreeAfterMove
		"/mnt/disk9": {Path: "/mnt/disk9", Bin: &domain.Bin{Size: gib(20), Items: []*domain.Item{{Size: gib(20)}}}},
	}}
	// Fix disk9 to be below floor but eligible: Free 25, move 20 → projected 5% of 100
	disks[2].Free = gib(25)

	got := assembleAutoGatherCanonicalResult(domain.AutoGatherCanonicalPlanResult{
		Stage2RecommendedTarget:  "disk1",
		Stage2EstimatedMoveBytes: gib(10),
	}, plan, disks, nil, 0)

	if got.Stage2TargetStillCanonicalEligible {
		t.Fatal("stale stage2 target must not remain eligible")
	}
	if got.CanonicalRecommendedTarget != "disk8" {
		t.Fatalf("want disk8 (only >=10%%), got %q", got.CanonicalRecommendedTarget)
	}
	if got.BelowPreferredFreeFloor {
		t.Fatal("disk8 at 20% should meet floor")
	}
}

func TestCanonicalBalancedFloorAndFallback(t *testing.T) {
	disks := []*domain.Disk{
		{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(200), Free: gib(12)},
		{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(200), Free: gib(25)},
	}
	// disk1 move 5 → 7/200 = 3.5%; disk8 move 20 → 5/200 = 2.5%
	plan := &domain.Plan{VDisks: map[string]*domain.VDisk{
		"/mnt/disk1": {Path: "/mnt/disk1", Bin: &domain.Bin{Size: gib(5), Items: []*domain.Item{{Size: gib(5)}}}},
		"/mnt/disk8": {Path: "/mnt/disk8", Bin: &domain.Bin{Size: gib(20), Items: []*domain.Item{{Size: gib(20)}}}},
	}}

	got := assembleAutoGatherCanonicalResult(domain.AutoGatherCanonicalPlanResult{}, plan, disks, nil, 0)
	if got.CanonicalRecommendedTarget != "disk1" {
		t.Fatalf("fallback least-move want disk1, got %q", got.CanonicalRecommendedTarget)
	}
	if !got.BelowPreferredFreeFloor {
		t.Fatal("expected belowPreferredFreeFloor")
	}

	// Exact 10% floor meets preference over below-floor peer.
	disks2 := []*domain.Disk{
		{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(20)},
		{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(15)},
	}
	plan2 := &domain.Plan{VDisks: map[string]*domain.VDisk{
		"/mnt/disk1": {Path: "/mnt/disk1", Bin: &domain.Bin{Size: gib(10)}},
		"/mnt/disk8": {Path: "/mnt/disk8", Bin: &domain.Bin{Size: gib(10)}},
	}}
	got2 := assembleAutoGatherCanonicalResult(domain.AutoGatherCanonicalPlanResult{}, plan2, disks2, nil, 0)
	if got2.CanonicalRecommendedTarget != "disk1" {
		t.Fatalf("exact 10%% should win, got %q", got2.CanonicalRecommendedTarget)
	}
	if got2.BelowPreferredFreeFloor {
		t.Fatal("exact 10% must not flag below floor")
	}
}

func TestPlanAutoGatherCanonicalUsesGetItemsNotStage2Totals(t *testing.T) {
	root := t.TempDir()
	disk1 := filepath.Join(root, "disk1")
	disk8 := filepath.Join(root, "disk8")
	show := "data/media/tv/ShowX"
	mustMkdirAll(t, filepath.Join(disk1, show))
	mustMkdirAll(t, filepath.Join(disk8, show))
	mustWriteFile(t, filepath.Join(disk1, show, "ep1.mkv"), string(make([]byte, 8000)))
	mustWriteFile(t, filepath.Join(disk8, show, "ep2.mkv"), string(make([]byte, 2000)))

	// Probe whether this host supports the canonical getItems/du path.
	if _, _, err := getItems(0, reItems, disk1, show); err != nil {
		t.Skipf("canonical getItems/du unavailable on this host: %v", err)
	}

	c := newCoreForReserved(1, "Gb")
	c.state = &domain.State{
		Status: common.OpNeutral,
		Unraid: &domain.Unraid{
			Disks: []*domain.Disk{
				{Name: "disk1", Path: disk1, Type: "Data", Size: gib(100), Free: gib(50)},
				{Name: "disk8", Path: disk8, Type: "Data", Size: gib(100), Free: gib(50)},
			},
		},
		History: &domain.History{Items: map[string]*domain.Operation{}, Order: []string{}},
	}
	c.pendingPlans = map[string]*planTicket{}

	histBefore := len(c.state.History.Order)
	pendingBefore := len(c.pendingPlans)

	got := c.planAutoGatherCanonical(domain.AutoGatherCanonicalPlanRequest{
		ShowPath:                 show,
		Stage2RecommendedTarget:  "disk1",
		Stage2EstimatedMoveBytes: gib(99), // deliberately wrong vs real du sizes
	}, false)

	if got.Error != "" {
		t.Fatalf("unexpected error: %s", got.Error)
	}
	if c.state.Status != common.OpNeutral {
		t.Fatalf("status = %d, want Neutral", c.state.Status)
	}
	if len(c.pendingPlans) != pendingBefore {
		t.Fatalf("pending plans changed: %d → %d", pendingBefore, len(c.pendingPlans))
	}
	if len(c.state.History.Order) != histBefore {
		t.Fatal("history must not change")
	}
	if c.state.Operation != nil {
		t.Fatal("operation must remain nil")
	}

	var disk1Tgt *domain.AutoGatherCanonicalTarget
	for i := range got.CanonicalTargets {
		if got.CanonicalTargets[i].DiskName == "disk1" {
			disk1Tgt = &got.CanonicalTargets[i]
		}
		if !got.CanonicalTargets[i].IsPhysicalArrayDisk && got.CanonicalTargets[i].CanonicalEligible {
			t.Fatalf("non-array eligible: %+v", got.CanonicalTargets[i])
		}
	}
	if disk1Tgt == nil || !disk1Tgt.CanonicalEligible {
		t.Fatalf("disk1 should be eligible: %+v", disk1Tgt)
	}
	if disk1Tgt.CanonicalBytesToMove == gib(99) {
		t.Fatal("canonical bytes must not use Stage 2 synthetic TotalBytes")
	}
	if disk1Tgt.CanonicalBytesToMove < 1500 || disk1Tgt.CanonicalBytesToMove > 4000 {
		t.Fatalf("canonical move for disk1 = %d, want ~2000 from getItems", disk1Tgt.CanonicalBytesToMove)
	}
	if got.Stage2EstimatedMoveBytes != gib(99) {
		t.Fatal("stage2 estimate should be echoed unchanged")
	}
}

func TestPlanAutoGatherCanonicalRejectsBusyAndMultiPath(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	c.state = &domain.State{Status: common.OpGatherMove, Unraid: &domain.Unraid{Disks: []*domain.Disk{
		{Name: "disk1", Path: "/mnt/disk1", Free: gib(10), Size: gib(100)},
	}}}
	c.pendingPlans = map[string]*planTicket{}

	got := c.planAutoGatherCanonical(domain.AutoGatherCanonicalPlanRequest{ShowPath: "data/media/tv/A"}, false)
	if got.Error == "" {
		t.Fatal("expected busy error")
	}

	c.state.Status = common.OpNeutral
	got = c.PlanAutoGatherCanonical(domain.AutoGatherCanonicalPlanRequest{ShowPath: ""})
	if got.Error == "" {
		t.Fatal("expected empty path error")
	}
}
