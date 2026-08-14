package core

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"unbalance/daemon/autogather"
	"unbalance/daemon/common"
	"unbalance/daemon/domain"
)

// Stage 3A — read-only canonical one-show Gather planning
//
// Proves the seam between Stage 2 advisory recommendations and the real
// Gather planner (getItems / du / Greedy) for exactly one show path.
//
// NEVER: gatherMove, createGatherOperation, runOperation, rsync, delete,
// prune, pending plan tickets, history writes, multi-show batch plans, or
// unattended execution.
//
// CanonicalEligible requires BOTH a non-nil Gather Bin AND IsArrayDiskName.
// Raw Gather may fit cache/pools; Auto Gather still rejects them.

// PlanAutoGatherCanonical verifies one Auto Gather show against a fresh
// canonical Gather plan. It is synchronous and leaves no pending Gather ticket.
func (c *Core) PlanAutoGatherCanonical(req domain.AutoGatherCanonicalPlanRequest) domain.AutoGatherCanonicalPlanResult {
	return c.planAutoGatherCanonical(req, true)
}

func (c *Core) planAutoGatherCanonical(req domain.AutoGatherCanonicalPlanRequest, refreshDisks bool) domain.AutoGatherCanonicalPlanResult {
	result := domain.AutoGatherCanonicalPlanResult{
		Stage2RecommendedTarget:  req.Stage2RecommendedTarget,
		Stage2EstimatedMoveBytes: req.Stage2EstimatedMoveBytes,
	}

	showPath, err := validateAutoGatherCanonicalSelection([]string{strings.TrimSpace(req.ShowPath)})
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.ShowPath = showPath

	if c.state == nil {
		result.Error = "internal state is not available"
		return result
	}
	if c.state.Status != common.OpNeutral {
		result.Error = fmt.Sprintf("unbalanced is busy: status %d", c.state.Status)
		return result
	}

	pendingBefore := len(c.pendingPlans)

	c.state.Status = common.OpGatherPlan
	defer func() {
		c.state.Status = common.OpNeutral
	}()

	if refreshDisks {
		c.state.Unraid = c.refreshUnraid()
	}

	result = c.buildCanonicalPlanResult(showPath, req.Stage2RecommendedTarget, req.Stage2EstimatedMoveBytes)

	if len(c.pendingPlans) != pendingBefore {
		result.Error = "internal error: canonical planning created a pending Gather ticket"
	}
	return result
}

// buildCanonicalPlanResult runs canonical Gather planning for one show without
// storing a pending ticket. Caller controls Status/Unraid refresh.
func (c *Core) buildCanonicalPlanResult(showPath, stage2Target string, stage2Move uint64) domain.AutoGatherCanonicalPlanResult {
	return c.buildCanonicalPlanResultCancellable(showPath, stage2Target, stage2Move, nil)
}

// buildCanonicalPlanResultCancellable is the Stage-3B-aware variant. shouldStop
// nil preserves Stage 3A / read-only behaviour.
func (c *Core) buildCanonicalPlanResultCancellable(showPath, stage2Target string, stage2Move uint64, shouldStop func() bool) domain.AutoGatherCanonicalPlanResult {
	result := domain.AutoGatherCanonicalPlanResult{
		ShowPath:                 showPath,
		Stage2RecommendedTarget:  stage2Target,
		Stage2EstimatedMoveBytes: stage2Move,
	}

	if shouldStop != nil && shouldStop() {
		result.Cancelled = true
		return result
	}

	if c.state.Unraid == nil || len(c.state.Unraid.Disks) == 0 {
		result.Error = "unable to read array disks"
		return result
	}

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

	items, cancelled := c.fillGatherPlanCancellable(plan, c.state.Unraid.Disks, c.state.Unraid.BlockSize, shouldStop)
	if cancelled {
		result.Cancelled = true
		return result
	}
	return assembleAutoGatherCanonicalResult(
		result,
		plan,
		c.state.Unraid.Disks,
		items,
		c.state.Unraid.BlockSize,
	)
}

func validateAutoGatherCanonicalSelection(selected []string) (string, error) {
	if len(selected) == 0 {
		return "", fmt.Errorf("exactly one show path is required")
	}
	if len(selected) > 1 {
		return "", fmt.Errorf("canonical planning accepts exactly one show path, got %d", len(selected))
	}
	raw := strings.TrimSpace(selected[0])
	if raw == "" {
		return "", fmt.Errorf("exactly one show path is required")
	}
	cleaned, err := autogather.NormalizeTvLibraryPath(raw)
	if err != nil {
		return "", fmt.Errorf("invalid show path: %w", err)
	}
	return cleaned, nil
}

func assembleAutoGatherCanonicalResult(
	base domain.AutoGatherCanonicalPlanResult,
	plan *domain.Plan,
	disks []*domain.Disk,
	items []*domain.Item,
	blockSize uint64,
) domain.AutoGatherCanonicalPlanResult {
	base.CanonicalItemCountTotal = len(items)
	base.CanonicalTargets = make([]domain.AutoGatherCanonicalTarget, 0, len(disks))

	currentByLocation := map[string]uint64{}
	for _, it := range items {
		if it == nil {
			continue
		}
		currentByLocation[it.Location] += it.Size
	}

	eligible := make([]domain.AutoGatherTargetCandidate, 0)

	for _, d := range disks {
		if d == nil {
			continue
		}
		vdisk := plan.VDisks[d.Path]
		rawBin := vdisk != nil && vdisk.Bin != nil
		isArray := autogather.IsArrayDiskName(d.Name)

		t := domain.AutoGatherCanonicalTarget{
			DiskName:                      d.Name,
			DiskPath:                      d.Path,
			IsPhysicalArrayDisk:           isArray,
			RawGatherBinPresent:           rawBin,
			FreeBytes:                     d.Free,
			DiskSizeBytes:                 d.Size,
			CanonicalCurrentBytesOnTarget: currentByLocation[d.Path],
		}

		switch {
		case !isArray:
			t.CanonicalEligible = false
			t.IneligibleReason = "not a physical array disk (Auto Gather forbids cache/pools/disk0)"
			if rawBin {
				// Preserve raw Gather bytes for comparison/debug only.
				t.CanonicalBytesToMove = vdisk.Bin.Size
				t.CanonicalItemCount = len(vdisk.Bin.Items)
			}
		case !rawBin:
			t.CanonicalEligible = false
			t.IneligibleReason = "canonical Gather produced no Bin (destination does not fit)"
		default:
			t.CanonicalEligible = true
			t.CanonicalBytesToMove = vdisk.Bin.Size
			t.CanonicalItemCount = len(vdisk.Bin.Items)
			t.ProjectedFreeBytes = projectedFreeAfterMove(d, vdisk.Bin, blockSize)
			if d.Size > 0 {
				t.ProjectedFreePercent = float64(t.ProjectedFreeBytes) * 100 / float64(d.Size)
			}
			t.MeetsPreferredFreeFloor = t.ProjectedFreePercent >= PreferredProjectedFreePercent

			eligible = append(eligible, domain.AutoGatherTargetCandidate{
				DiskName:                 t.DiskName,
				Eligible:                 true,
				MoveRequiredBytes:        t.CanonicalBytesToMove,
				CurrentShowBytesOnTarget: t.CanonicalCurrentBytesOnTarget,
				FreeBytes:                t.FreeBytes,
				DiskSizeBytes:            t.DiskSizeBytes,
				ProjectedFreeBytes:       t.ProjectedFreeBytes,
				ProjectedFreePercent:     t.ProjectedFreePercent,
				MeetsPreferredFreeFloor:  t.MeetsPreferredFreeFloor,
			})
		}

		base.CanonicalTargets = append(base.CanonicalTargets, t)
	}

	sort.SliceStable(base.CanonicalTargets, func(i, j int) bool {
		a, b := base.CanonicalTargets[i], base.CanonicalTargets[j]
		if a.CanonicalEligible != b.CanonicalEligible {
			return a.CanonicalEligible
		}
		if a.CanonicalEligible && b.CanonicalEligible {
			if a.CanonicalBytesToMove != b.CanonicalBytesToMove {
				return a.CanonicalBytesToMove < b.CanonicalBytesToMove
			}
			if a.ProjectedFreePercent != b.ProjectedFreePercent {
				return a.ProjectedFreePercent > b.ProjectedFreePercent
			}
		}
		return a.DiskName < b.DiskName
	})

	if base.Stage2RecommendedTarget != "" {
		for _, t := range base.CanonicalTargets {
			if t.DiskName == base.Stage2RecommendedTarget && t.CanonicalEligible {
				base.Stage2TargetStillCanonicalEligible = true
				break
			}
		}
	}

	if len(eligible) == 0 {
		base.NoEligibleReason = "insufficient free space on all eligible physical array disks"
		return base
	}

	balanced, belowFloor := selectBalanced(eligible)
	base.CanonicalRecommendedTarget = balanced.DiskName
	base.CanonicalMoveBytes = balanced.MoveRequiredBytes
	base.CanonicalProjectedFreeBytes = balanced.ProjectedFreeBytes
	base.CanonicalProjectedFreePercent = balanced.ProjectedFreePercent
	base.BelowPreferredFreeFloor = belowFloor
	return base
}
