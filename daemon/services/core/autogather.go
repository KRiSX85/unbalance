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
		}
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
			}
		}
	}

	arrayDisks, cacheDisks := autogather.PartitionDisks(unraid.Disks)
	return autogather.ScanLibrary(cleaned, arrayDisks, cacheDisks)
}

// SetTvLibraryPath validates, persists, and returns the normalized library path
// relative to /mnt/user.
func (c *Core) SetTvLibraryPath(path string) (*domain.Config, error) {
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
