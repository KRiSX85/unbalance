package core

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"

	"unbalance/daemon/algorithm"
	"unbalance/daemon/autogather"
	"unbalance/daemon/common"
	"unbalance/daemon/domain"
	"unbalance/daemon/lib"
)

// Stage 2 Auto Gather recommendations — design invariants
//
// READ-ONLY: This enrichment never creates pending Gather plans, writes history,
// changes Core operation status, runs rsync, deletes sources, or mutates media.
//
// ALL-FILE BYTES: Synthetic Greedy entries use Stage 1 per-disk TotalBytes
// (video + sidecars + other regular files), not VideoBytes. Empty-folder-only
// disks contribute zero moved bytes and are reported separately as cleanup
// candidates.
//
// AGGREGATION vs REAL GATHER:
//
// Manual Gather getItems() emits one Item per immediate child of the show
// folder, sized with GNU `du -bs` on that child (so a Season directory is one
// item whose size is the apparent size of its whole subtree). Greedy fitBlocks
// then uses sum over moved items of ceil(item.Size / blockSize).
//
// Stage 2 emits one synthetic Item per source disk, sized from Stage 1
// TotalBytes (sum of regular-file apparent sizes under the show on that disk),
// so fitBlocks uses ceil(sum(sizes) / blockSize) per source disk.
//
// For any non-negative sizes and blockSize B>0:
//   ceil(sum s_i / B)  <=  sum ceil(s_i / B)  <=  ceil(sum s_i / B) + (N-1)
// where N is the number of manual Gather items being moved from that source
// disk (immediate children). Across D source disks the total excess of the
// real Gather block count over Stage 2 is at most N_total - D blocks, which
// can exceed one block whenever N_total > D+1. The difference is usually small
// relative to multi-gigabyte TV shows, but it is not bounded by "<1 block per
// disk". Stage 2 therefore remains an ESTIMATE: UI must label moves as
// Estimated, and any future execution MUST run the canonical Gather planner
// immediately before moving. Stage 2 does not re-run getItems() solely to
// eliminate this rounding / sizing difference.
//
// Stage 1 TotalBytes vs getItems()/du -bs can also differ slightly because
// TotalBytes sums only regular-file st_size values (see scanShowOnDisk), while
// du -bs includes directory apparent sizes in each summarized subtree and may
// treat symlinks/special files differently.
//
// FUTURE EXECUTION CONTRACT: Any future Auto Gather execution MUST run the
// real canonical Gather planner immediately before moving a show:
//   - re-read current disk free space
//   - generate the real Gather plan via gatherPlan*
//   - confirm the target is still eligible
//   - confirm the target is still a physical array disk (^disk[1-9][0-9]*$)
//   - refuse execution if the live Gather plan disagrees or no longer fits
// Execution is NOT implemented in Stage 2.
//
// FUTURE EMPTY-FOLDER CLEANUP CONTRACT (display-only in Stage 2):
//   - operate only beneath the configured TV library path
//   - physical array disks only; never cache/pools/disk0
//   - re-check the target directory at execution time
//   - remove only if it is genuinely empty at that moment
//   - use empty-directory removal semantics, not recursive RemoveAll
//   - never delete sidecar-only directories
//   - prune empty parents only within the show's tree / library boundary
//   - failure to prove empty = skip cleanup
//   - cleanup-only operations should eventually be possible for Consolidated shows
// Cleanup execution is NOT implemented in Stage 2.
//
// CACHE INVARIANT: Recommendation and cleanup candidates consume only disks
// from autogather.PartitionDisks array collection / IsArrayDiskName.

// addAutoGatherRecommendations enriches scan results with informational
// Gather-target recommendations (split shows) and empty-folder cleanup
// reporting (split and consolidated). It is purely read-only.
func (c *Core) addAutoGatherRecommendations(scan domain.AutoGatherScanResult, unraid *domain.Unraid) domain.AutoGatherScanResult {
	if unraid == nil || len(unraid.Disks) == 0 {
		return scan
	}

	arrayRefs, _ := autogather.PartitionDisks(unraid.Disks)
	if len(arrayRefs) == 0 {
		return scan
	}

	diskByName := make(map[string]*domain.Disk, len(unraid.Disks))
	for _, d := range unraid.Disks {
		if d == nil {
			continue
		}
		diskByName[d.Name] = d
	}

	blockSize := unraid.BlockSize

	for i := range scan.Shows {
		show := &scan.Shows[i]

		// Cleanup candidates are already populated from Stage 1 empty-folder
		// array presences. Re-assert the array-disk invariant defensively.
		show.CleanupCandidateDisks = filterArrayCleanupCandidates(show.CleanupCandidateDisks)
		show.CleanupCandidateCount = len(show.CleanupCandidateDisks)

		if show.Status != domain.AutoGatherStatusSplit {
			continue
		}

		items, totalAllFile := buildAllFileGatherItems(show, blockSize)
		if len(items) == 0 || totalAllFile == 0 {
			show.NoEligibleReason = "no eligible all-file bytes available for recommendation"
			continue
		}

		candidates := make([]domain.AutoGatherTargetCandidate, 0, len(arrayRefs))
		eligible := make([]domain.AutoGatherTargetCandidate, 0)

		for _, ref := range arrayRefs {
			d := diskByName[ref.Name]
			if d == nil || !autogather.IsArrayDiskName(d.Name) {
				continue
			}

			// Same reserved-space ceil as gatherPlanStart.
			reserved := c.getReservedAmount(d.Size)
			ceil := lib.Max(common.ReservedSpace, reserved)

			packer := algorithm.NewGreedy(d, items, ceil, blockSize)
			bin := packer.FitAll()

			currentOnTarget := uint64(0)
			for _, it := range items {
				if it.Location == d.Path {
					currentOnTarget += it.Size
				}
			}
			moveRequired := totalAllFile - currentOnTarget
			if bin != nil {
				moveRequired = bin.Size
			}

			cand := domain.AutoGatherTargetCandidate{
				DiskName:                 d.Name,
				MoveRequiredBytes:        moveRequired,
				CurrentShowBytesOnTarget: currentOnTarget,
				FreeBytes:                d.Free,
			}

			if bin == nil {
				cand.Eligible = false
				cand.IneligibleReason = fmt.Sprintf(
					"insufficient free space after reserved-space requirement (%s reserved)",
					lib.ByteSize(ceil),
				)
				cand.ProjectedFreeBytes = 0
			} else {
				cand.Eligible = true
				if d.Free >= moveRequired {
					cand.ProjectedFreeBytes = d.Free - moveRequired
				}
				eligible = append(eligible, cand)
			}
			candidates = append(candidates, cand)
		}

		show.GatherTargets = candidates

		if len(eligible) == 0 {
			show.NoEligibleReason = "insufficient free space on all eligible physical array disks"
			continue
		}

		sort.SliceStable(eligible, func(i, j int) bool {
			if eligible[i].MoveRequiredBytes != eligible[j].MoveRequiredBytes {
				return eligible[i].MoveRequiredBytes < eligible[j].MoveRequiredBytes
			}
			return eligible[i].DiskName < eligible[j].DiskName
		})

		// Also present candidates with eligible first (least movement), then
		// ineligible, for stable UI expansion.
		sort.SliceStable(show.GatherTargets, func(i, j int) bool {
			a, b := show.GatherTargets[i], show.GatherTargets[j]
			if a.Eligible != b.Eligible {
				return a.Eligible
			}
			if a.Eligible && b.Eligible {
				if a.MoveRequiredBytes != b.MoveRequiredBytes {
					return a.MoveRequiredBytes < b.MoveRequiredBytes
				}
			}
			return a.DiskName < b.DiskName
		})

		best := eligible[0]
		show.RecommendedTargetDisk = best.DiskName
		show.MoveRequiredBytes = best.MoveRequiredBytes
	}

	return scan
}

// buildAllFileGatherItems creates one synthetic Item per physical array disk
// that holds any all-file bytes (video disks + sidecar-only disks). Empty-
// folder-only disks are omitted (zero contribution to moved bytes).
func buildAllFileGatherItems(show *domain.AutoGatherShow, blockSize uint64) ([]*domain.Item, uint64) {
	items := make([]*domain.Item, 0, len(show.VideoDisks)+len(show.SidecarOnlyDisks))
	var total uint64

	add := func(presence domain.AutoGatherDiskPresence) {
		if !autogather.IsArrayDiskName(presence.DiskName) {
			return
		}
		if presence.EmptyOnly || presence.TotalBytes == 0 {
			return
		}
		total += presence.TotalBytes
		item := &domain.Item{
			Name:     show.Name,
			Size:     presence.TotalBytes,
			Path:     show.Path,
			Location: filepath.Join("/", "mnt", presence.DiskName),
		}
		if blockSize > 0 {
			item.BlocksUsed = uint64(math.Ceil(float64(presence.TotalBytes) / float64(blockSize)))
		}
		items = append(items, item)
	}

	for _, vd := range show.VideoDisks {
		add(vd)
	}
	for _, sd := range show.SidecarOnlyDisks {
		add(sd)
	}
	return items, total
}

func filterArrayCleanupCandidates(names []string) []string {
	out := make([]string, 0, len(names))
	seen := map[string]struct{}{}
	for _, name := range names {
		if !autogather.IsArrayDiskName(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
