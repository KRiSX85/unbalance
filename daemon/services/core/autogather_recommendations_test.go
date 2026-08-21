package core

import (
	"math"
	"testing"

	"github.com/cskr/pubsub"

	"unbalance/daemon/autogather"
	"unbalance/daemon/common"
	"unbalance/daemon/domain"
)

func TestBlockRoundingAggregateVsPerItemBound(t *testing.T) {
	// Documents the corrected bound used by Stage 2 comments:
	// for sizes s_i and blockSize B,
	//   ceil(sum/B) <= sum(ceil(s_i/B)) <= ceil(sum/B)+(N-1)
	const B = 4096
	sizes := []uint64{1, 1, 1, 1, 1} // N=5, each needs 1 block; sum=5 needs 1 block
	var sum uint64
	var sumCeil uint64
	for _, s := range sizes {
		sum += s
		sumCeil += uint64(math.Ceil(float64(s) / float64(B)))
	}
	aggCeil := uint64(math.Ceil(float64(sum) / float64(B)))
	if aggCeil > sumCeil {
		t.Fatalf("aggregate ceil %d > per-item sum ceil %d", aggCeil, sumCeil)
	}
	diff := sumCeil - aggCeil
	if diff > uint64(len(sizes)-1) {
		t.Fatalf("diff %d exceeds N-1=%d", diff, len(sizes)-1)
	}
	// This fixture differs by more than one block (4), proving the old
	// "<1 block per disk" claim is false.
	if diff <= 1 {
		t.Fatalf("fixture should demonstrate multi-block gap; diff=%d", diff)
	}
}

func gib(n uint64) uint64 { return n * 1024 * 1024 * 1024 }

func newCoreForReserved(reservedAmount uint64, reservedUnit string) *Core {
	return &Core{
		ctx: &domain.Context{
			Config: domain.Config{
				ReservedAmount: reservedAmount,
				ReservedUnit:   reservedUnit,
			},
			Hub: pubsub.New(64),
		},
		state:        &domain.State{Status: common.OpNeutral},
		pendingPlans: make(map[string]*planTicket),
	}
}

func presence(name string, video, total uint64) domain.AutoGatherDiskPresence {
	return domain.AutoGatherDiskPresence{
		DiskName:   name,
		VideoBytes: video,
		VideoCount: 1,
		TotalBytes: total,
	}
}

func sidecar(name string, total uint64) domain.AutoGatherDiskPresence {
	return domain.AutoGatherDiskPresence{
		DiskName:    name,
		TotalBytes:  total,
		SidecarOnly: true,
	}
}

func emptyOnly(name string) domain.AutoGatherDiskPresence {
	return domain.AutoGatherDiskPresence{
		DiskName:  name,
		EmptyOnly: true,
	}
}

func TestAutoGatherRecommendationsLeastDataMovement(t *testing.T) {
	show := domain.AutoGatherShow{
		Name:            "Britannia",
		Status:          domain.AutoGatherStatusSplit,
		Split:           true,
		TotalVideoBytes: gib(74),
		TotalBytes:      gib(74),
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(55), gib(55)),
			presence("disk8", gib(19), gib(19)),
		},
	}

	unraid := &domain.Unraid{
		Disks: []*domain.Disk{
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(30)},
			{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(60)},
			{Name: "disk9", Path: "/mnt/disk9", Type: "Data", Size: gib(100), Free: gib(100)},
		},
	}

	c := newCoreForReserved(1, "Gb")
	out := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid)
	got := out.Shows[0]

	if got.RecommendedTargetDisk != "disk1" {
		t.Fatalf("recommended = %q, want disk1", got.RecommendedTargetDisk)
	}
	if got.MoveRequiredBytes != gib(19) {
		t.Fatalf("moveRequiredBytes = %d, want %d", got.MoveRequiredBytes, gib(19))
	}
	if got.BelowPreferredFreeFloor {
		t.Fatalf("disk1 projected free is 11%%, should meet 10%% floor")
	}
	if got.MinMovementAlternative != nil {
		t.Fatalf("min-movement alternative should be omitted when it matches Balanced")
	}
}

func TestAutoGatherRecommendationsSidecarBytesContributeToMove(t *testing.T) {
	// Video split 10+10, but disk8 also has 5 GiB of sidecars on a sidecar-only disk3.
	// Targeting disk1 must move disk8 video (10) + disk3 sidecars (5) = 15.
	show := domain.AutoGatherShow{
		Name:   "WithSidecars",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(10), gib(10)),
			presence("disk8", gib(10), gib(10)),
		},
		SidecarOnlyDisks: []domain.AutoGatherDiskPresence{
			sidecar("disk3", gib(5)),
		},
	}

	unraid := &domain.Unraid{
		Disks: []*domain.Disk{
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(20)},
			{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(20)},
			{Name: "disk3", Path: "/mnt/disk3", Type: "Data", Size: gib(100), Free: gib(20)},
		},
	}

	c := newCoreForReserved(1, "Gb")
	out := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid)
	got := out.Shows[0]

	if got.RecommendedTargetDisk != "disk1" && got.RecommendedTargetDisk != "disk8" {
		t.Fatalf("recommended = %q", got.RecommendedTargetDisk)
	}
	// Least movement among disk1/disk8 is 15 GiB (other video+sidecars).
	if got.MoveRequiredBytes != gib(15) {
		t.Fatalf("moveRequiredBytes = %d, want %d (sidecar bytes must count)", got.MoveRequiredBytes, gib(15))
	}
}

func TestAutoGatherRecommendationsSidecarOnlyAffectsDestination(t *testing.T) {
	// Without sidecars, disk1 (55) wins over disk8 (19) => move 19.
	// With a large sidecar-only presence on disk9 that tips disk1 over the free-space edge,
	// recommendation can change.
	show := domain.AutoGatherShow{
		Name:   "SidecarTipsFit",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(55), gib(55)),
			presence("disk8", gib(19), gib(19)),
		},
		SidecarOnlyDisks: []domain.AutoGatherDiskPresence{
			sidecar("disk9", gib(20)),
		},
	}

	unraid := &domain.Unraid{
		Disks: []*domain.Disk{
			// disk1 free 30: needs to move 19+20=39 > 30-1 reserved => ineligible
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(30)},
			// disk8 free 80: needs to move 55+20=75 <= 80-1 => eligible, move 75
			{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(80)},
			{Name: "disk9", Path: "/mnt/disk9", Type: "Data", Size: gib(100), Free: gib(5)},
		},
	}

	c := newCoreForReserved(1, "Gb")
	out := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid)
	got := out.Shows[0]

	if got.RecommendedTargetDisk != "disk8" {
		t.Fatalf("recommended = %q, want disk8 (sidecar pushed disk1 over capacity)", got.RecommendedTargetDisk)
	}
	if got.MoveRequiredBytes != gib(75) {
		t.Fatalf("moveRequiredBytes = %d, want %d", got.MoveRequiredBytes, gib(75))
	}
}

func TestAutoGatherRecommendationsVideoOnlyTotalsNotUsedForFit(t *testing.T) {
	// VideoBytes look tiny on disk8, but TotalBytes (with co-located sidecars) is large.
	show := domain.AutoGatherShow{
		Name:   "VideoVsTotal",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(10), gib(10)),
			// VideoBytes=1 but TotalBytes=40 (sidecars co-located with video)
			presence("disk8", gib(1), gib(40)),
		},
	}

	unraid := &domain.Unraid{
		Disks: []*domain.Disk{
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(50)},
			{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(50)},
		},
	}

	c := newCoreForReserved(1, "Gb")
	out := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid)
	got := out.Shows[0]

	var disk1Move, disk8Move uint64
	for _, tcand := range got.GatherTargets {
		switch tcand.DiskName {
		case "disk1":
			disk1Move = tcand.MoveRequiredBytes
		case "disk8":
			disk8Move = tcand.MoveRequiredBytes
		}
	}
	// Targeting disk1 must move disk8's all-file 40 GiB, not video-only 1 GiB.
	if disk1Move != gib(40) {
		t.Fatalf("disk1 moveRequired = %d, want %d (must use TotalBytes not VideoBytes)", disk1Move, gib(40))
	}
	// Targeting disk8 moves only disk1's 10 GiB of all-file data.
	if disk8Move != gib(10) {
		t.Fatalf("disk8 moveRequired = %d, want %d", disk8Move, gib(10))
	}
	// Least movement therefore recommends disk8.
	if got.RecommendedTargetDisk != "disk8" || got.MoveRequiredBytes != gib(10) {
		t.Fatalf("recommended = %q move=%d, want disk8/%d", got.RecommendedTargetDisk, got.MoveRequiredBytes, gib(10))
	}
}

func TestAutoGatherRecommendationsEmptyFoldersContributeZeroMove(t *testing.T) {
	show := domain.AutoGatherShow{
		Name:   "EmptyShells",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(10), gib(10)),
			presence("disk8", gib(5), gib(5)),
		},
		EmptyOnlyDisks: []domain.AutoGatherDiskPresence{
			emptyOnly("disk2"),
			emptyOnly("disk11"),
		},
		CleanupCandidateDisks: []string{"disk2", "disk11"},
		CleanupCandidateCount: 2,
	}

	unraid := &domain.Unraid{
		Disks: []*domain.Disk{
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(20)},
			{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(20)},
			{Name: "disk2", Path: "/mnt/disk2", Type: "Data", Size: gib(100), Free: gib(20)},
			{Name: "disk11", Path: "/mnt/disk11", Type: "Data", Size: gib(100), Free: gib(20)},
		},
	}

	c := newCoreForReserved(1, "Gb")
	out := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid)
	got := out.Shows[0]

	if got.MoveRequiredBytes != gib(5) {
		t.Fatalf("moveRequiredBytes = %d, want %d (empty folders must not add bytes)", got.MoveRequiredBytes, gib(5))
	}
	if got.CleanupCandidateCount != 2 {
		t.Fatalf("cleanup count = %d", got.CleanupCandidateCount)
	}
}

func TestAutoGatherRecommendationsCleanupOnConsolidated(t *testing.T) {
	show := domain.AutoGatherShow{
		Name:   "ConsolidatedCleanup",
		Status: domain.AutoGatherStatusConsolidated,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk7", gib(12), gib(12)),
		},
		EmptyOnlyDisks: []domain.AutoGatherDiskPresence{
			emptyOnly("disk2"),
			emptyOnly("disk11"),
		},
		CleanupCandidateDisks: []string{"disk2", "disk11"},
		CleanupCandidateCount: 2,
	}

	unraid := &domain.Unraid{
		Disks: []*domain.Disk{
			{Name: "disk7", Path: "/mnt/disk7", Type: "Data", Size: gib(100), Free: gib(20)},
			{Name: "disk2", Path: "/mnt/disk2", Type: "Data", Size: gib(100), Free: gib(20)},
			{Name: "disk11", Path: "/mnt/disk11", Type: "Data", Size: gib(100), Free: gib(20)},
		},
	}

	c := newCoreForReserved(1, "Gb")
	out := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid)
	got := out.Shows[0]

	if got.RecommendedTargetDisk != "" {
		t.Fatalf("consolidated must not get gather recommendation")
	}
	if got.CleanupCandidateCount != 2 || len(got.CleanupCandidateDisks) != 2 {
		t.Fatalf("expected cleanup candidates on consolidated show: %v", got.CleanupCandidateDisks)
	}
}

func TestAutoGatherRecommendationsSidecarNeverCleanup(t *testing.T) {
	show := domain.AutoGatherShow{
		Name:   "SidecarNotCleanup",
		Status: domain.AutoGatherStatusConsolidated,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(8), gib(8)),
		},
		SidecarOnlyDisks: []domain.AutoGatherDiskPresence{
			sidecar("disk4", gib(1)),
		},
		CleanupCandidateDisks: nil,
		CleanupCandidateCount: 0,
	}

	c := newCoreForReserved(1, "Gb")
	out := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, &domain.Unraid{
		Disks: []*domain.Disk{
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(20)},
			{Name: "disk4", Path: "/mnt/disk4", Type: "Data", Size: gib(100), Free: gib(20)},
		},
	})
	got := out.Shows[0]
	if got.CleanupCandidateCount != 0 {
		t.Fatalf("sidecar-only must never be cleanup candidates: %v", got.CleanupCandidateDisks)
	}
}

func TestAutoGatherRecommendationsPassesRealBlockSize(t *testing.T) {
	const blockSize = 4096
	show := domain.AutoGatherShow{
		Name:   "BlockSize",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", 10000, 10000),
			presence("disk8", 10000, 10000),
		},
	}

	// Free in blocks just enough / not enough to observe block mode.
	// moveRequired to disk1 = 10000 bytes => ceil(10000/4096)=3 blocks
	// reserved ceil = 1GiB => bufferBlocks = 1GiB/4096
	// Make free tiny so only byte-mode would incorrectly pass if blockSize ignored...
	// Simpler: set BlocksFree very low so block-mode rejects, while Free bytes would pass.
	unraid := &domain.Unraid{
		BlockSize: blockSize,
		Disks: []*domain.Disk{
			{
				Name: "disk1", Path: "/mnt/disk1", Type: "Data",
				Size: gib(100), Free: gib(50),
				BlocksFree: 100, // far too small once reserved buffer is applied
			},
			{
				Name: "disk8", Path: "/mnt/disk8", Type: "Data",
				Size: gib(100), Free: gib(50),
				BlocksFree: 100,
			},
		},
	}

	c := newCoreForReserved(1, "Gb")
	out := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid)
	got := out.Shows[0]

	if got.RecommendedTargetDisk != "" {
		t.Fatalf("expected no recommendation when BlocksFree is exhausted by reserved buffer; got %q", got.RecommendedTargetDisk)
	}
	if got.NoEligibleReason == "" {
		t.Fatalf("expected noEligibleReason when block-mode rejects all targets")
	}
}

func TestAutoGatherRecommendationsReservedSpaceHandling(t *testing.T) {
	show := domain.AutoGatherShow{
		Name:   "ShowX",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(5), gib(5)),
			presence("disk8", gib(1), gib(1)),
		},
	}

	unraid := &domain.Unraid{
		Disks: []*domain.Disk{
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(5)},
			{Name: "disk2", Path: "/mnt/disk2", Type: "Data", Size: gib(100), Free: gib(6)},
			{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(5)},
		},
	}

	c := newCoreForReserved(2, "Gb") // ceil=2GiB
	out := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid)
	got := out.Shows[0]

	for _, et := range got.GatherTargets {
		if et.DiskName == "disk2" && et.Eligible {
			t.Fatalf("disk2 should be ineligible with reserved-space handling")
		}
	}
	if got.RecommendedTargetDisk != "disk1" {
		t.Fatalf("recommended = %q, want disk1", got.RecommendedTargetDisk)
	}
}

func TestAutoGatherRecommendationsCachePoolsNeverEligibleAndDisk0NeverEligible(t *testing.T) {
	show := domain.AutoGatherShow{
		Name:   "CacheCheck",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(4), gib(4)),
			presence("disk8", gib(6), gib(6)),
		},
		CleanupCandidateDisks: []string{"disk1", "cache", "disk0", "data_cache"},
		CleanupCandidateCount: 4,
	}

	unraid := &domain.Unraid{
		Disks: []*domain.Disk{
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(20)},
			{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(20)},
			{Name: "cache", Path: "/mnt/cache", Type: "Cache", Size: gib(100), Free: gib(100)},
			{Name: "data_cache", Path: "/mnt/data_cache", Type: "Cache", Size: gib(100), Free: gib(100)},
			{Name: "disk0", Path: "/mnt/disk0", Type: "Data", Size: gib(100), Free: gib(100)},
		},
	}

	array, _ := autogather.PartitionDisks(unraid.Disks)
	if len(array) != 2 {
		t.Fatalf("array disks = %d, want 2", len(array))
	}

	c := newCoreForReserved(1, "Gb")
	out := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid)
	got := out.Shows[0]

	for _, et := range got.GatherTargets {
		if et.DiskName == "cache" || et.DiskName == "data_cache" || et.DiskName == "disk0" {
			t.Fatalf("unexpected gather target %q", et.DiskName)
		}
	}
	for _, name := range got.CleanupCandidateDisks {
		if name == "cache" || name == "data_cache" || name == "disk0" {
			t.Fatalf("unexpected cleanup candidate %q", name)
		}
	}
	if got.CleanupCandidateCount != 1 || got.CleanupCandidateDisks[0] != "disk1" {
		t.Fatalf("cleanup filtered incorrectly: %v", got.CleanupCandidateDisks)
	}
}

func TestAutoGatherRecommendationsWaitingForMoverNoRecommendation(t *testing.T) {
	show := domain.AutoGatherShow{
		Name:                "WaitingShow",
		Status:              domain.AutoGatherStatusWaitingForMover,
		CachePoolsWithVideo: []string{"cache"},
		CleanupCandidateDisks: []string{"disk2"},
		CleanupCandidateCount: 1,
	}

	c := newCoreForReserved(1, "Gb")
	out := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, &domain.Unraid{
		Disks: []*domain.Disk{
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(20)},
			{Name: "disk2", Path: "/mnt/disk2", Type: "Data", Size: gib(100), Free: gib(20)},
		},
	})
	got := out.Shows[0]
	if got.RecommendedTargetDisk != "" || len(got.GatherTargets) != 0 {
		t.Fatalf("expected no recommendation for waiting_for_mover; got %+v", got)
	}
}

func TestAutoGatherRecommendationsNoEligiblePhysicalDisk(t *testing.T) {
	show := domain.AutoGatherShow{
		Name:   "NoFit",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(50), gib(50)),
			presence("disk8", gib(1), gib(1)),
		},
	}

	unraid := &domain.Unraid{
		Disks: []*domain.Disk{
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(1)},
			{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(1)},
		},
	}

	c := newCoreForReserved(1, "Gb")
	if common.ReservedSpace != gib(1) {
		t.Skip("common.ReservedSpace does not equal 1GiB in this build")
	}

	out := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid)
	got := out.Shows[0]
	if got.RecommendedTargetDisk != "" {
		t.Fatalf("expected no recommendation, got %q", got.RecommendedTargetDisk)
	}
	if got.NoEligibleReason == "" {
		t.Fatalf("expected noEligibleReason")
	}
}

func TestAutoGatherRecommendationsIndependentDoNotMutateFreeSpace(t *testing.T) {
	showA := domain.AutoGatherShow{
		Name: "A", Status: domain.AutoGatherStatusSplit, Split: true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(10), gib(10)),
			presence("disk8", gib(10), gib(10)),
		},
	}
	showB := domain.AutoGatherShow{
		Name: "B", Status: domain.AutoGatherStatusSplit, Split: true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(10), gib(10)),
			presence("disk8", gib(10), gib(10)),
		},
	}

	disk1 := &domain.Disk{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(15)}
	disk8 := &domain.Disk{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(15)}
	unraid := &domain.Unraid{Disks: []*domain.Disk{disk1, disk8}}

	c := newCoreForReserved(1, "Gb")
	beforeFree := disk1.Free
	out := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{showA, showB}}, unraid)

	if disk1.Free != beforeFree {
		t.Fatalf("disk free space mutated: before=%d after=%d", beforeFree, disk1.Free)
	}
	// Both shows independently see the same free space (both can recommend disk1).
	if out.Shows[0].RecommendedTargetDisk != out.Shows[1].RecommendedTargetDisk {
		t.Fatalf("independent recommendations should see identical free space; got %q vs %q",
			out.Shows[0].RecommendedTargetDisk, out.Shows[1].RecommendedTargetDisk)
	}
	if c.state.Status != common.OpNeutral {
		t.Fatalf("Core status mutated: %d", c.state.Status)
	}
	if len(c.pendingPlans) != 0 {
		t.Fatalf("pending plans created: %d", len(c.pendingPlans))
	}
}

func TestBalancedPrefersFloorOverLowestMovement(t *testing.T) {
	// Britannia-style: disk1 least movement but ~6% projected free; disk8 more
	// movement but ~20% projected free.
	show := domain.AutoGatherShow{
		Name:   "Britannia",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(56), gib(56)),
			presence("disk8", gib(19), gib(19)),
		},
	}
	unraid := &domain.Unraid{
		Disks: []*domain.Disk{
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(20000), Free: gib(1250)},
			{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(10000), Free: gib(2040)},
		},
	}

	c := newCoreForReserved(1, "Gb")
	got := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid).Shows[0]

	if got.RecommendedTargetDisk != "disk8" {
		t.Fatalf("recommended = %q, want disk8 (Balanced 10%% floor)", got.RecommendedTargetDisk)
	}
	if got.MoveRequiredBytes != gib(56) {
		t.Fatalf("moveRequiredBytes = %d, want %d", got.MoveRequiredBytes, gib(56))
	}
	if got.BelowPreferredFreeFloor {
		t.Fatalf("disk8 should meet the 10%% floor")
	}
	if got.MinMovementAlternative == nil || got.MinMovementAlternative.DiskName != "disk1" {
		t.Fatalf("expected min-movement alternative disk1, got %+v", got.MinMovementAlternative)
	}
	if got.MinMovementAlternative.MoveRequiredBytes != gib(19) {
		t.Fatalf("min-movement move = %d, want %d", got.MinMovementAlternative.MoveRequiredBytes, gib(19))
	}
}

func TestBalancedLeastMovementAmongAboveFloor(t *testing.T) {
	show := domain.AutoGatherShow{
		Name:   "AboveFloor",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(20), gib(20)),
			presence("disk8", gib(10), gib(10)),
			presence("disk9", gib(5), gib(5)),
		},
	}
	unraid := &domain.Unraid{
		Disks: []*domain.Disk{
			// Targeting disk1 moves 15, projected free 85/100 = 85%.
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(100)},
			// Targeting disk8 moves 25, projected free 75/100 = 75%.
			{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(100)},
			// Targeting disk9 moves 30, projected free 70/100 = 70%.
			{Name: "disk9", Path: "/mnt/disk9", Type: "Data", Size: gib(100), Free: gib(100)},
		},
	}

	c := newCoreForReserved(1, "Gb")
	got := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid).Shows[0]
	if got.RecommendedTargetDisk != "disk1" {
		t.Fatalf("recommended = %q, want disk1 (least movement among >=10%%)", got.RecommendedTargetDisk)
	}
	if got.MoveRequiredBytes != gib(15) {
		t.Fatalf("moveRequiredBytes = %d, want %d", got.MoveRequiredBytes, gib(15))
	}
	if got.MinMovementAlternative != nil {
		t.Fatalf("min-movement should match Balanced; got %+v", got.MinMovementAlternative)
	}
}

func TestBalancedEqualMovementHigherProjectedFreeWins(t *testing.T) {
	show := domain.AutoGatherShow{
		Name:   "EqualMove",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(10), gib(10)),
			presence("disk8", gib(10), gib(10)),
		},
	}
	unraid := &domain.Unraid{
		Disks: []*domain.Disk{
			// Both require moving 10 GiB. disk8 has more remaining percent.
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(20)}, // projected 10%
			{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(50), Free: gib(20)},  // projected 20%
		},
	}

	c := newCoreForReserved(1, "Gb")
	got := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid).Shows[0]
	if got.RecommendedTargetDisk != "disk8" {
		t.Fatalf("recommended = %q, want disk8 (higher projected free %%)", got.RecommendedTargetDisk)
	}
	if got.MinMovementAlternative != nil {
		t.Fatalf("equal movement: min-movement is the same candidate")
	}
}

func TestBalancedFallbackWhenAllBelowFloor(t *testing.T) {
	show := domain.AutoGatherShow{
		Name:   "AllTight",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(20), gib(20)),
			presence("disk8", gib(5), gib(5)),
		},
	}
	unraid := &domain.Unraid{
		Disks: []*domain.Disk{
			// disk1: move 5, projected free 7/200 = 3.5%
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(200), Free: gib(12)},
			// disk8: move 20, projected free 5/200 = 2.5%
			{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(200), Free: gib(25)},
		},
	}

	c := newCoreForReserved(1, "Gb")
	got := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid).Shows[0]
	if got.RecommendedTargetDisk != "disk1" {
		t.Fatalf("recommended = %q, want disk1 least-movement fallback", got.RecommendedTargetDisk)
	}
	if !got.BelowPreferredFreeFloor {
		t.Fatalf("expected belowPreferredFreeFloor")
	}
	if got.MinMovementAlternative != nil {
		t.Fatalf("fallback already is min-movement; alternative should be omitted")
	}
}

func TestProjectedFreeAfterMoveByteAndBlockModes(t *testing.T) {
	const blockSize = uint64(4096)

	byteDisk := &domain.Disk{Free: 1000, BlocksFree: 99999}
	byteBin := &domain.Bin{Size: 400, BlocksUsed: 1}
	if got := projectedFreeAfterMove(byteDisk, byteBin, 0); got != 600 {
		t.Fatalf("byte mode projected = %d, want 600", got)
	}
	if got := projectedFreeAfterMove(byteDisk, &domain.Bin{Size: 2000}, 0); got != 0 {
		t.Fatalf("byte underflow clamp = %d, want 0", got)
	}

	blockDisk := &domain.Disk{Free: 10, BlocksFree: 50} // Free << blocks*size
	blockBin := &domain.Bin{Size: 10000, BlocksUsed: 10}
	want := uint64(40) * blockSize
	if got := projectedFreeAfterMove(blockDisk, blockBin, blockSize); got != want {
		t.Fatalf("block mode projected = %d, want %d", got, want)
	}
	if got := projectedFreeAfterMove(blockDisk, &domain.Bin{BlocksUsed: 60}, blockSize); got != 0 {
		t.Fatalf("block underflow clamp = %d, want 0", got)
	}
}

func TestProjectedFreeBlockModeEligibleNotZeroFromRawBytes(t *testing.T) {
	// Block eligibility succeeds while Free bytes are far below bin.Size.
	// Old Free−bin.Size accounting would incorrectly show 0% projected free.
	// Projected free keeps reserved blocks in the free pool (eligibility-only).
	const blockSize = uint64(4096)
	reservedBlocks := common.ReservedSpace / blockSize // 1GiB buffer in blocks
	moveBlocks := uint64(1)                           // disk8 → disk1
	extraBlocks := uint64(2500)

	show := domain.AutoGatherShow{
		Name:   "BlockProjected",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", 8192, 8192), // 2 blocks
			presence("disk8", 4096, 4096), // 1 block
		},
	}

	unraid := &domain.Unraid{
		BlockSize: blockSize,
		Disks: []*domain.Disk{
			{
				Name: "disk1", Path: "/mnt/disk1", Type: "Data",
				Size:       gib(100),
				Free:       100, // << moveRequired bytes; must not force 0% projected
				BlocksFree: reservedBlocks + moveBlocks + extraBlocks,
			},
			{
				Name: "disk8", Path: "/mnt/disk8", Type: "Data",
				Size:       gib(100),
				Free:       gib(50),
				BlocksFree: reservedBlocks + 100000,
			},
		},
	}

	c := newCoreForReserved(1, "Gb")
	got := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid).Shows[0]

	var disk1 *domain.AutoGatherTargetCandidate
	for i := range got.GatherTargets {
		if got.GatherTargets[i].DiskName == "disk1" {
			disk1 = &got.GatherTargets[i]
			break
		}
	}
	if disk1 == nil || !disk1.Eligible {
		t.Fatalf("disk1 should be eligible in block mode: %+v", disk1)
	}
	wantBytes := (reservedBlocks + extraBlocks) * blockSize
	if disk1.ProjectedFreeBytes != wantBytes {
		t.Fatalf("disk1 projectedFreeBytes = %d, want %d (from remaining blocks)", disk1.ProjectedFreeBytes, wantBytes)
	}
	if disk1.ProjectedFreeBytes == 0 {
		t.Fatalf("eligible block-mode candidate must not show 0 projected free")
	}
	wantPct := float64(wantBytes) * 100 / float64(gib(100))
	if disk1.ProjectedFreePercent != wantPct {
		t.Fatalf("disk1 projectedFreePercent = %v, want %v", disk1.ProjectedFreePercent, wantPct)
	}
}

func TestProjectedFreeByteModeUnchanged(t *testing.T) {
	show := domain.AutoGatherShow{
		Name:   "ByteProjected",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(55), gib(55)),
			presence("disk8", gib(19), gib(19)),
		},
	}
	unraid := &domain.Unraid{
		// BlockSize == 0 → byte-mode Greedy / projected free
		Disks: []*domain.Disk{
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(30)},
			{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(60)},
		},
	}

	c := newCoreForReserved(1, "Gb")
	got := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid).Shows[0]

	var disk1 *domain.AutoGatherTargetCandidate
	for i := range got.GatherTargets {
		if got.GatherTargets[i].DiskName == "disk1" {
			disk1 = &got.GatherTargets[i]
			break
		}
	}
	if disk1 == nil || !disk1.Eligible {
		t.Fatalf("disk1 should be eligible: %+v", disk1)
	}
	// move 19 GiB onto Free 30 → projected 11 GiB = 11%
	if disk1.ProjectedFreeBytes != gib(11) {
		t.Fatalf("byte-mode projectedFreeBytes = %d, want %d", disk1.ProjectedFreeBytes, gib(11))
	}
	if disk1.ProjectedFreePercent != 11.0 {
		t.Fatalf("byte-mode projectedFreePercent = %v, want 11", disk1.ProjectedFreePercent)
	}
	if !disk1.MeetsPreferredFreeFloor {
		t.Fatalf("11%% should meet the 10%% floor")
	}
}

func TestProjectedFreeExactTenPercentFloor(t *testing.T) {
	show := domain.AutoGatherShow{
		Name:   "ExactTen",
		Status: domain.AutoGatherStatusSplit,
		Split:  true,
		VideoDisks: []domain.AutoGatherDiskPresence{
			presence("disk1", gib(10), gib(10)),
			presence("disk8", gib(10), gib(10)),
		},
	}
	unraid := &domain.Unraid{
		Disks: []*domain.Disk{
			// Targeting disk1: move 10, Free 20, Size 100 → exactly 10%
			{Name: "disk1", Path: "/mnt/disk1", Type: "Data", Size: gib(100), Free: gib(20)},
			// Targeting disk8: move 10, Free 15, Size 100 → 5% (below floor)
			{Name: "disk8", Path: "/mnt/disk8", Type: "Data", Size: gib(100), Free: gib(15)},
		},
	}

	c := newCoreForReserved(1, "Gb")
	got := c.addAutoGatherRecommendations(domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{show}}, unraid).Shows[0]

	var disk1, disk8 *domain.AutoGatherTargetCandidate
	for i := range got.GatherTargets {
		switch got.GatherTargets[i].DiskName {
		case "disk1":
			disk1 = &got.GatherTargets[i]
		case "disk8":
			disk8 = &got.GatherTargets[i]
		}
	}
	if disk1 == nil || disk8 == nil {
		t.Fatalf("missing candidates: disk1=%v disk8=%v", disk1, disk8)
	}
	if disk1.ProjectedFreePercent != 10.0 {
		t.Fatalf("disk1 percent = %v, want exactly 10", disk1.ProjectedFreePercent)
	}
	if !disk1.MeetsPreferredFreeFloor {
		t.Fatalf("exact 10%% must meet PreferredProjectedFreePercent")
	}
	if disk8.MeetsPreferredFreeFloor {
		t.Fatalf("disk8 at 5%% must be below floor")
	}
	if got.RecommendedTargetDisk != "disk1" {
		t.Fatalf("recommended = %q, want disk1 (only target at exact 10%% floor)", got.RecommendedTargetDisk)
	}
	if got.BelowPreferredFreeFloor {
		t.Fatalf("should not flag below-floor when exact 10%% exists")
	}
}
