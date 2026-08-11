package autogather

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"unbalance/daemon/domain"
)

// Video extensions used for Stage 1 Auto Gather classification.
var videoExts = map[string]struct{}{
	".mkv":  {},
	".mp4":  {},
	".m4v":  {},
	".avi":  {},
	".ts":   {},
	".m2ts": {},
	".wmv":  {},
	".mov":  {},
}

// DiskRef is a mount used during a read-only Auto Gather scan.
type DiskRef struct {
	Name string
	Path string
}

type showDiskStats struct {
	Exists     bool
	VideoCount uint64
	VideoBytes uint64
	TotalBytes uint64
	EmptyOnly  bool
}

func isVideo(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	_, ok := videoExts[ext]
	return ok
}

func scanShowOnDisk(entry string) (showDiskStats, error) {
	fi, err := os.Lstat(entry)
	if err != nil {
		if os.IsNotExist(err) {
			return showDiskStats{}, nil
		}
		return showDiskStats{}, err
	}

	stats := showDiskStats{Exists: true}

	if fi.Mode()&os.ModeSymlink != 0 {
		stats.EmptyOnly = true
		return stats, nil
	}

	if !fi.IsDir() {
		size := uint64(fi.Size())
		stats.TotalBytes = size
		if isVideo(fi.Name()) {
			stats.VideoCount = 1
			stats.VideoBytes = size
		}
		return stats, nil
	}

	err = filepath.WalkDir(entry, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == entry {
				return walkErr
			}
			return nil
		}

		// TotalBytes is the sum of regular-file apparent sizes (Info().Size /
		// st_size) under this show location. It includes videos, sidecars
		// (subtitles/NFO/artwork/etc.) and other regular files. It does not
		// include directory metadata sizes, symlink targets, or special files.
		// Symlink show roots are treated as EmptyOnly above and never walked.
		// Manual Gather getItems() instead uses `du -bs` on each immediate
		// child, which can include directory apparent sizes — so Stage 1
		// TotalBytes is representative but not guaranteed bit-identical.
		if !d.Type().IsRegular() {
			return nil
		}

		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}

		size := uint64(info.Size())
		stats.TotalBytes += size
		if isVideo(d.Name()) {
			stats.VideoCount++
			stats.VideoBytes += size
		}

		return nil
	})
	if err != nil {
		return showDiskStats{}, err
	}

	if stats.TotalBytes == 0 && stats.VideoCount == 0 {
		stats.EmptyOnly = true
	}

	return stats, nil
}

func classifyShow(
	name string,
	relPath string,
	arrayStats map[string]showDiskStats,
	cacheStats map[string]showDiskStats,
	arrayOrder []string,
	cacheOrder []string,
) domain.AutoGatherShow {
	show := domain.AutoGatherShow{
		Name:                  name,
		Path:                  relPath,
		VideoDisks:            make([]domain.AutoGatherDiskPresence, 0),
		SidecarOnlyDisks:      make([]domain.AutoGatherDiskPresence, 0),
		EmptyOnlyDisks:        make([]domain.AutoGatherDiskPresence, 0),
		CachePoolsWithVideo:   make([]string, 0),
		CleanupCandidateDisks: make([]string, 0),
	}

	arrayVideoDisks := 0

	for _, diskName := range arrayOrder {
		stats := arrayStats[diskName]
		if !stats.Exists {
			continue
		}

		show.TotalBytes += stats.TotalBytes
		show.TotalVideoBytes += stats.VideoBytes

		// Always retain all-file TotalBytes for every physical array presence.
		// VideoBytes remains the sole quantity used for Split / video presence.
		presence := domain.AutoGatherDiskPresence{
			DiskName:   diskName,
			VideoCount: stats.VideoCount,
			VideoBytes: stats.VideoBytes,
			TotalBytes: stats.TotalBytes,
			EmptyOnly:  stats.EmptyOnly,
		}

		switch {
		case stats.VideoBytes > 0:
			arrayVideoDisks++
			show.VideoDisks = append(show.VideoDisks, presence)
		case stats.VideoCount > 0:
			// Zero-byte video files: listed for display, ignored for split.
			show.VideoDisks = append(show.VideoDisks, presence)
		case stats.EmptyOnly:
			presence.EmptyOnly = true
			show.EmptyOnlyDisks = append(show.EmptyOnlyDisks, presence)
			// Empty-folder-only array locations are cleanup candidates (display only).
			show.CleanupCandidateDisks = append(show.CleanupCandidateDisks, diskName)
		case stats.TotalBytes > 0:
			presence.SidecarOnly = true
			show.SidecarOnlyDisks = append(show.SidecarOnlyDisks, presence)
		}
	}

	for _, poolName := range cacheOrder {
		stats := cacheStats[poolName]
		if !stats.Exists {
			continue
		}

		show.TotalBytes += stats.TotalBytes
		show.TotalVideoBytes += stats.VideoBytes
		if stats.VideoCount > 0 || stats.VideoBytes > 0 {
			show.CachePoolsWithVideo = append(show.CachePoolsWithVideo, poolName)
		}
	}

	show.CleanupCandidateCount = len(show.CleanupCandidateDisks)

	switch {
	case len(show.CachePoolsWithVideo) > 0:
		show.Status = domain.AutoGatherStatusWaitingForMover
		show.Split = false
		show.Ready = false
	case arrayVideoDisks >= 2:
		show.Status = domain.AutoGatherStatusSplit
		show.Split = true
		show.Ready = true
	case arrayVideoDisks == 1:
		show.Status = domain.AutoGatherStatusConsolidated
		show.Split = false
		show.Ready = false
	default:
		show.Status = domain.AutoGatherStatusNoVideo
		show.Split = false
		show.Ready = false
	}

	return show
}

// ScanLibrary enumerates immediate child directories under libraryRel across the
// provided array and cache disk mounts. It is purely read-only.
//
// arrayDisks must be the PartitionDisks array collection (physical diskN only).
// cacheDisks are scanned solely for "Waiting for Mover" detection and must never
// be treated as destination-capable array disks.
func ScanLibrary(libraryRel string, arrayDisks, cacheDisks []DiskRef) domain.AutoGatherScanResult {
	libraryRel = strings.Trim(filepath.ToSlash(libraryRel), "/")

	result := domain.AutoGatherScanResult{
		LibraryPath: libraryRel,
		Shows:       make([]domain.AutoGatherShow, 0),
		Warnings:    make([]string, 0),
	}

	warningSet := map[string]struct{}{}
	addWarning := func(msg string) {
		if msg == "" {
			return
		}
		if _, ok := warningSet[msg]; ok {
			return
		}
		warningSet[msg] = struct{}{}
		result.Warnings = append(result.Warnings, msg)
	}

	names := map[string]struct{}{}
	for _, disk := range append(append([]DiskRef{}, arrayDisks...), cacheDisks...) {
		root := filepath.Join(disk.Path, filepath.FromSlash(libraryRel))
		entries, err := os.ReadDir(root)
		if err != nil {
			if !os.IsNotExist(err) {
				addWarning(scanWarning(disk.Name, libraryRel, scanWarningReason(err)))
			}
			continue
		}
		for _, entry := range entries {
			// DirEntry.IsDir follows symlinks for the type check; Lstat on the
			// show path later refuses to walk symlink roots.
			if entry.IsDir() {
				names[entry.Name()] = struct{}{}
			}
		}
	}

	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)

	arrayOrder := make([]string, 0, len(arrayDisks))
	for _, d := range arrayDisks {
		arrayOrder = append(arrayOrder, d.Name)
	}
	cacheOrder := make([]string, 0, len(cacheDisks))
	for _, d := range cacheDisks {
		cacheOrder = append(cacheOrder, d.Name)
	}

	for _, name := range ordered {
		relPath := name
		if libraryRel != "" {
			relPath = libraryRel + "/" + name
		}

		arrayStats := make(map[string]showDiskStats, len(arrayDisks))
		for _, disk := range arrayDisks {
			entry := filepath.Join(disk.Path, filepath.FromSlash(relPath))
			stats, err := scanShowOnDisk(entry)
			if err != nil {
				addWarning(scanWarning(disk.Name, relPath, scanWarningReason(err)))
				continue
			}
			arrayStats[disk.Name] = stats
		}

		cacheStats := make(map[string]showDiskStats, len(cacheDisks))
		for _, disk := range cacheDisks {
			entry := filepath.Join(disk.Path, filepath.FromSlash(relPath))
			stats, err := scanShowOnDisk(entry)
			if err != nil {
				addWarning(scanWarning(disk.Name, relPath, scanWarningReason(err)))
				continue
			}
			cacheStats[disk.Name] = stats
		}

		show := classifyShow(name, relPath, arrayStats, cacheStats, arrayOrder, cacheOrder)
		result.Shows = append(result.Shows, show)
	}

	return result
}

// PartitionDisks separates destination-capable physical array disks from cache
// pools.
//
// Invariant: the returned arrayDisks collection contains only mounts whose names
// match ^disk[1-9][0-9]*$ (via IsArrayDiskName). Named pools such as cache,
// data_cache, vm_cache, appdata, or any other pool must never enter arrayDisks.
// Cache pools are returned separately so Stage 1 can detect video waiting for
// Mover. Future Auto Gather target recommendation must consume only arrayDisks
// from this helper (or an equivalent IsArrayDiskName filter) — never cacheDisks
// or unfiltered Unraid.Disks.
func PartitionDisks(disks []*domain.Disk) (arrayDisks, cacheDisks []DiskRef) {
	for _, disk := range disks {
		if disk == nil {
			continue
		}
		ref := DiskRef{Name: disk.Name, Path: disk.Path}
		switch {
		case IsArrayDiskName(disk.Name):
			arrayDisks = append(arrayDisks, ref)
		case disk.Type == "Cache":
			cacheDisks = append(cacheDisks, ref)
		}
	}
	return arrayDisks, cacheDisks
}
