package core

import (
	"fmt"

	"unbalance/daemon/autogather"
	"unbalance/daemon/domain"
	"unbalance/daemon/logger"
)

// ScanAutoGather performs a read-only collection-wide TV library scan using the
// saved TvLibraryPath configuration. It does not take the Gather/Scatter
// operation busy lock, never mutates media, and never persists config.
func (c *Core) ScanAutoGather() domain.AutoGatherScanResult {
	result := c.scanAutoGatherWithCancel(nil)
	if !result.Cancelled && result.Error == "" {
		c.storePresentedAutoGatherLibrary(result, false)
	}
	return result
}

// GetAutoGatherLibrary returns the last already-computed Auto Gather scan
// without walking the filesystem. ok is false when no snapshot has been stored.
func (c *Core) GetAutoGatherLibrary() (domain.AutoGatherLibraryView, bool) {
	c.autoGatherMu.RLock()
	defer c.autoGatherMu.RUnlock()
	if c.autoGatherLibraryScan == nil {
		return domain.AutoGatherLibraryView{
			AutoGatherScanResult: domain.AutoGatherScanResult{Shows: []domain.AutoGatherShow{}},
		}, false
	}
	return domain.AutoGatherLibraryView{
		Revision:             c.autoGatherLibraryRevision,
		AutoGatherScanResult: cloneAutoGatherScanResult(*c.autoGatherLibraryScan),
	}, true
}

// scanAutoGatherWithCancel is the Stage-3B-aware scan path. shouldStop nil
// preserves ordinary user-triggered "Scan library" behaviour.
func (c *Core) scanAutoGatherWithCancel(shouldStop func() bool) domain.AutoGatherScanResult {
	scan, unraid := c.scanAutoGatherStage1WithCancel(shouldStop)
	if scan.Cancelled || scan.Error != "" {
		return scan
	}
	if shouldStop != nil && shouldStop() {
		scan.Cancelled = true
		return scan
	}
	// Stage 2 read-only enrichment: compute eligible gather destination
	// recommendations for split shows using the array disk free space.
	return c.addAutoGatherRecommendationsCancellable(scan, unraid, shouldStop)
}

// scanAutoGatherStage1WithCancel runs only the Stage 1 filesystem library scan.
// Stage 3B uses this once per run, then re-scores Stage 2 from retained
// discovery data and refreshed disk state without scanning the library again.
func (c *Core) scanAutoGatherStage1WithCancel(shouldStop func() bool) (domain.AutoGatherScanResult, *domain.Unraid) {
	path := c.ctx.Config.TvLibraryPath
	if path == "" {
		path = autogather.DefaultTvLibraryPath
	}

	cleaned, err := autogather.ValidateTvLibraryPath(path, autogather.UserShareRoot)
	if err != nil {
		return domain.AutoGatherScanResult{
			LibraryPath: path,
			Shows:       []domain.AutoGatherShow{},
			Warnings:    []string{},
			Error:       autogather.FormatLibraryValidationError(err),
		}, nil
	}

	if shouldStop != nil && shouldStop() {
		return domain.AutoGatherScanResult{
			LibraryPath: cleaned,
			Shows:       []domain.AutoGatherShow{},
			Warnings:    []string{},
			Cancelled:   true,
		}, nil
	}

	unraid, err := getArrayData()
	if err != nil {
		logger.Yellow("auto-gather scan: unable to refresh disks: %s", err)
		if c.state != nil && c.state.Unraid != nil {
			unraid = c.state.Unraid
		} else {
			return domain.AutoGatherScanResult{
				LibraryPath: cleaned,
				Shows:       []domain.AutoGatherShow{},
				Warnings:    []string{},
				Error:       "unable to read array disks",
			}, nil
		}
	}

	arrayDisks, cacheDisks := autogather.PartitionDisks(unraid.Disks)
	return autogather.ScanLibraryWithCancel(cleaned, arrayDisks, cacheDisks, shouldStop), unraid
}

// SetTvLibraryPath validates, persists, and returns the normalized library path
// relative to /mnt/user.
func (c *Core) SetTvLibraryPath(path string) (*domain.Config, error) {
	if err := c.configMutationBlocked(); err != nil {
		return &c.ctx.Config, err
	}
	cleaned, err := autogather.ValidateTvLibraryPath(path, autogather.UserShareRoot)
	if err != nil {
		return &c.ctx.Config, fmt.Errorf("%s", autogather.FormatLibraryValidationError(err))
	}

	c.ctx.Config.TvLibraryPath = cleaned
	if err := c.saveSettings(); err != nil {
		logger.Yellow("setTvLibraryPath: unable to save settings: %s", err)
		return &c.ctx.Config, err
	}
	return &c.ctx.Config, nil
}
