package autogather

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"unbalance/daemon/domain"
)

func TestScanLibraryOneShowOnOneDisk(t *testing.T) {
	root := t.TempDir()
	disk1 := filepath.Join(root, "disk1")
	disk2 := filepath.Join(root, "disk2")
	mustMkdirAll(t, filepath.Join(disk1, "data/media/tv/ShowA"))
	mustWriteFile(t, filepath.Join(disk1, "data/media/tv/ShowA/ep1.mkv"), string(make([]byte, 1000)))

	result := ScanLibrary("data/media/tv", []DiskRef{
		{Name: "disk1", Path: disk1},
		{Name: "disk2", Path: disk2},
	}, nil)

	if len(result.Shows) != 1 {
		t.Fatalf("expected 1 show, got %d", len(result.Shows))
	}
	show := result.Shows[0]
	if show.Name != "ShowA" {
		t.Fatalf("name = %q", show.Name)
	}
	if show.Status != domain.AutoGatherStatusConsolidated {
		t.Fatalf("status = %q", show.Status)
	}
	if show.Split || show.Ready {
		t.Fatalf("expected consolidated not-ready, split=%v ready=%v", show.Split, show.Ready)
	}
	if show.TotalVideoBytes != 1000 {
		t.Fatalf("total video bytes = %d", show.TotalVideoBytes)
	}
	if len(show.VideoDisks) != 1 || show.VideoDisks[0].DiskName != "disk1" {
		t.Fatalf("video disks = %+v", show.VideoDisks)
	}
}

func TestScanLibrarySplitAcrossTwoDisks(t *testing.T) {
	root := t.TempDir()
	disk1 := filepath.Join(root, "disk1")
	disk2 := filepath.Join(root, "disk2")
	mustMkdirAll(t, filepath.Join(disk1, "data/media/tv/ShowB"))
	mustMkdirAll(t, filepath.Join(disk2, "data/media/tv/ShowB"))
	mustWriteFile(t, filepath.Join(disk1, "data/media/tv/ShowB/s01.mkv"), string(make([]byte, 2000)))
	mustWriteFile(t, filepath.Join(disk2, "data/media/tv/ShowB/s02.mp4"), string(make([]byte, 3000)))

	result := ScanLibrary("data/media/tv", []DiskRef{
		{Name: "disk1", Path: disk1},
		{Name: "disk2", Path: disk2},
	}, nil)

	show := mustFindShow(t, result, "ShowB")
	if show.Status != domain.AutoGatherStatusSplit || !show.Split || !show.Ready {
		t.Fatalf("expected ready split, status=%q split=%v ready=%v", show.Status, show.Split, show.Ready)
	}
	if show.TotalVideoBytes != 5000 {
		t.Fatalf("total video bytes = %d", show.TotalVideoBytes)
	}
	if len(show.VideoDisks) != 2 {
		t.Fatalf("expected 2 video disks, got %+v", show.VideoDisks)
	}
}

func TestScanLibraryEmptyDirectoryShells(t *testing.T) {
	root := t.TempDir()
	disk1 := filepath.Join(root, "disk1")
	disk2 := filepath.Join(root, "disk2")
	disk3 := filepath.Join(root, "disk3")
	mustMkdirAll(t, filepath.Join(disk1, "data/media/tv/ShowC"))
	mustMkdirAll(t, filepath.Join(disk2, "data/media/tv/ShowC"))
	mustMkdirAll(t, filepath.Join(disk3, "data/media/tv/ShowC/Season 01"))
	mustWriteFile(t, filepath.Join(disk1, "data/media/tv/ShowC/ep.mkv"), string(make([]byte, 1500)))

	result := ScanLibrary("data/media/tv", []DiskRef{
		{Name: "disk1", Path: disk1},
		{Name: "disk2", Path: disk2},
		{Name: "disk3", Path: disk3},
	}, nil)

	show := mustFindShow(t, result, "ShowC")
	if show.Status != domain.AutoGatherStatusConsolidated || show.Split {
		t.Fatalf("empty shells must not create a split: status=%q split=%v", show.Status, show.Split)
	}
	if len(show.EmptyOnlyDisks) != 2 {
		t.Fatalf("empty-only disks = %v", show.EmptyOnlyDisks)
	}
}

func TestScanLibrarySidecarOnlyOnAdditionalDisk(t *testing.T) {
	root := t.TempDir()
	disk1 := filepath.Join(root, "disk1")
	disk2 := filepath.Join(root, "disk2")
	mustMkdirAll(t, filepath.Join(disk1, "data/media/tv/ShowD"))
	mustMkdirAll(t, filepath.Join(disk2, "data/media/tv/ShowD"))
	mustWriteFile(t, filepath.Join(disk1, "data/media/tv/ShowD/ep.mkv"), string(make([]byte, 4000)))
	mustWriteFile(t, filepath.Join(disk2, "data/media/tv/ShowD/poster.jpg"), string(make([]byte, 200)))
	mustWriteFile(t, filepath.Join(disk2, "data/media/tv/ShowD/info.nfo"), "meta")

	result := ScanLibrary("data/media/tv", []DiskRef{
		{Name: "disk1", Path: disk1},
		{Name: "disk2", Path: disk2},
	}, nil)

	show := mustFindShow(t, result, "ShowD")
	if show.Split {
		t.Fatalf("sidecar-only disk must not create a split")
	}
	if len(show.SidecarOnlyDisks) != 1 || show.SidecarOnlyDisks[0] != "disk2" {
		t.Fatalf("sidecar-only disks = %v", show.SidecarOnlyDisks)
	}
}

func TestScanLibraryCachePoolVideoWaitingForMover(t *testing.T) {
	root := t.TempDir()
	disk1 := filepath.Join(root, "disk1")
	disk2 := filepath.Join(root, "disk2")
	cache := filepath.Join(root, "cache")
	mustMkdirAll(t, filepath.Join(disk1, "data/media/tv/ShowE"))
	mustMkdirAll(t, filepath.Join(disk2, "data/media/tv/ShowE"))
	mustMkdirAll(t, filepath.Join(cache, "data/media/tv/ShowE"))
	mustWriteFile(t, filepath.Join(disk1, "data/media/tv/ShowE/s01.mkv"), string(make([]byte, 1000)))
	mustWriteFile(t, filepath.Join(disk2, "data/media/tv/ShowE/s02.mkv"), string(make([]byte, 2000)))
	mustWriteFile(t, filepath.Join(cache, "data/media/tv/ShowE/s03.mkv"), string(make([]byte, 500)))

	result := ScanLibrary("data/media/tv", []DiskRef{
		{Name: "disk1", Path: disk1},
		{Name: "disk2", Path: disk2},
	}, []DiskRef{
		{Name: "cache", Path: cache},
	})

	show := mustFindShow(t, result, "ShowE")
	if show.Status != domain.AutoGatherStatusWaitingForMover {
		t.Fatalf("status = %q", show.Status)
	}
	if show.Split || show.Ready {
		t.Fatalf("cache video must block readiness: split=%v ready=%v", show.Split, show.Ready)
	}
	if len(show.CachePoolsWithVideo) != 1 || show.CachePoolsWithVideo[0] != "cache" {
		t.Fatalf("cache pools = %v", show.CachePoolsWithVideo)
	}
}

func TestScanLibraryUnsupportedFilesDoNotCountAsVideo(t *testing.T) {
	root := t.TempDir()
	disk1 := filepath.Join(root, "disk1")
	disk2 := filepath.Join(root, "disk2")
	mustMkdirAll(t, filepath.Join(disk1, "data/media/tv/ShowF"))
	mustMkdirAll(t, filepath.Join(disk2, "data/media/tv/ShowF"))
	mustWriteFile(t, filepath.Join(disk1, "data/media/tv/ShowF/ep.mkv"), string(make([]byte, 800)))
	mustWriteFile(t, filepath.Join(disk2, "data/media/tv/ShowF/notes.txt"), "hello")
	mustWriteFile(t, filepath.Join(disk2, "data/media/tv/ShowF/sample.iso"), string(make([]byte, 900)))

	result := ScanLibrary("data/media/tv", []DiskRef{
		{Name: "disk1", Path: disk1},
		{Name: "disk2", Path: disk2},
	}, nil)

	show := mustFindShow(t, result, "ShowF")
	if show.Split {
		t.Fatalf("unsupported files must not create a split")
	}
	if len(show.SidecarOnlyDisks) != 1 || show.SidecarOnlyDisks[0] != "disk2" {
		t.Fatalf("sidecar-only disks = %v", show.SidecarOnlyDisks)
	}
}

func TestScanLibraryZeroByteVideoFiles(t *testing.T) {
	root := t.TempDir()
	disk1 := filepath.Join(root, "disk1")
	disk2 := filepath.Join(root, "disk2")
	mustMkdirAll(t, filepath.Join(disk1, "data/media/tv/ShowG"))
	mustMkdirAll(t, filepath.Join(disk2, "data/media/tv/ShowG"))
	mustWriteFile(t, filepath.Join(disk1, "data/media/tv/ShowG/real.mkv"), string(make([]byte, 1200)))
	mustWriteFile(t, filepath.Join(disk2, "data/media/tv/ShowG/empty.mkv"), "")

	result := ScanLibrary("data/media/tv", []DiskRef{
		{Name: "disk1", Path: disk1},
		{Name: "disk2", Path: disk2},
	}, nil)

	show := mustFindShow(t, result, "ShowG")
	if show.Split {
		t.Fatalf("zero-byte video must not create a split")
	}
	if show.Status != domain.AutoGatherStatusConsolidated {
		t.Fatalf("status = %q", show.Status)
	}
}

func TestScanLibraryContinuesWithWarnings(t *testing.T) {
	root := t.TempDir()
	disk1 := filepath.Join(root, "disk1")
	disk2 := filepath.Join(root, "disk2")
	mustMkdirAll(t, filepath.Join(disk1, "data/media/tv/ShowH"))
	mustWriteFile(t, filepath.Join(disk1, "data/media/tv/ShowH/ep.mkv"), string(make([]byte, 500)))
	mustMkdirAll(t, filepath.Join(disk2, "data/media/tv/ShowH"))
	// Make ShowH on disk2 unreadable.
	if err := os.Chmod(filepath.Join(disk2, "data/media/tv/ShowH"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(filepath.Join(disk2, "data/media/tv/ShowH"), 0o755)
	})

	result := ScanLibrary("data/media/tv", []DiskRef{
		{Name: "disk1", Path: disk1},
		{Name: "disk2", Path: disk2},
	}, nil)

	if len(result.Shows) != 1 {
		t.Fatalf("expected scan to continue with 1 show, got %d", len(result.Shows))
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected warnings for inaccessible path")
	}
	for _, warning := range result.Warnings {
		if filepath.IsAbs(warning) || containsAbsLeak(warning, root) {
			t.Fatalf("warning leaked absolute path: %q", warning)
		}
	}
}

func TestPartitionDisksRejectsPoolsAndDisk0(t *testing.T) {
	array, cache := PartitionDisks([]*domain.Disk{
		{Name: "disk1", Path: "/mnt/disk1", Type: "Data"},
		{Name: "disk18", Path: "/mnt/disk18", Type: "Data"},
		{Name: "disk0", Path: "/mnt/disk0", Type: "Data"},
		{Name: "cache", Path: "/mnt/cache", Type: "Cache"},
		{Name: "data_cache", Path: "/mnt/data_cache", Type: "Cache"},
		{Name: "vm_cache", Path: "/mnt/vm_cache", Type: "Cache"},
		{Name: "appdata", Path: "/mnt/appdata", Type: "Cache"},
		{Name: "downloads", Path: "/mnt/downloads", Type: "Cache"},
		{Name: "parity", Path: "/mnt/parity", Type: "Parity"},
	})

	if len(array) != 2 || array[0].Name != "disk1" || array[1].Name != "disk18" {
		t.Fatalf("array disks = %+v", array)
	}
	for _, disk := range array {
		if !IsArrayDiskName(disk.Name) {
			t.Fatalf("non-array name in array collection: %s", disk.Name)
		}
	}
	if len(cache) != 5 {
		t.Fatalf("cache disks = %+v", cache)
	}
	for _, disk := range cache {
		if IsArrayDiskName(disk.Name) {
			t.Fatalf("array name in cache collection: %s", disk.Name)
		}
	}
}

func TestIsArrayDiskName(t *testing.T) {
	accepted := []string{"disk1", "disk18", "disk2", "disk99"}
	rejected := []string{
		"disk0", "disk", "disk01", "cache", "data_cache", "vm_cache",
		"appdata", "downloads", "Disk1", "disk1a", "parity",
	}
	for _, name := range accepted {
		if !IsArrayDiskName(name) {
			t.Fatalf("expected %q to be accepted", name)
		}
	}
	for _, name := range rejected {
		if IsArrayDiskName(name) {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
}

func TestNormalizeTvLibraryPath(t *testing.T) {
	got, err := NormalizeTvLibraryPath(" data/media/tv/ ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "data/media/tv" {
		t.Fatalf("got %q", got)
	}

	got, err = NormalizeTvLibraryPath("data/./media//tv")
	if err != nil {
		t.Fatal(err)
	}
	if got != "data/media/tv" {
		t.Fatalf("cleaned = %q", got)
	}

	got, err = NormalizeTvLibraryPath("library/Show..Name")
	if err != nil {
		t.Fatal(err)
	}
	if got != "library/Show..Name" {
		t.Fatalf("dots in name rejected: %q", got)
	}

	cases := []struct {
		in   string
		want error
	}{
		{"", ErrEmptyTvLibraryPath},
		{"/data/media/tv", ErrAbsoluteTvLibraryPath},
		{"mnt/user/data/media/tv", ErrMisleadingTvLibraryPath},
		{"mnt/user", ErrMisleadingTvLibraryPath},
		{"data/../tv", ErrUnsafeTvLibraryPath},
		{"..", ErrUnsafeTvLibraryPath},
		{".", ErrEmptyTvLibraryPath},
		{"//", ErrAbsoluteTvLibraryPath},
	}
	for _, tc := range cases {
		_, err := NormalizeTvLibraryPath(tc.in)
		if !errors.Is(err, tc.want) {
			t.Fatalf("NormalizeTvLibraryPath(%q) err=%v want %v", tc.in, err, tc.want)
		}
	}
}

func TestEnsureLibraryPathUnderRootSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "user")
	outside := filepath.Join(root, "outside")
	mustMkdirAll(t, user)
	mustMkdirAll(t, outside)
	mustWriteFile(t, filepath.Join(outside, "secret.txt"), "x")

	// library -> outside
	link := filepath.Join(user, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported: %s", err)
	}

	err := EnsureLibraryPathUnderRoot(user, "escape")
	if !errors.Is(err, ErrOutsideUserShare) {
		t.Fatalf("err=%v want %v", err, ErrOutsideUserShare)
	}

	mustMkdirAll(t, filepath.Join(user, "data/media/tv"))
	if err := EnsureLibraryPathUnderRoot(user, "data/media/tv"); err != nil {
		t.Fatalf("valid path rejected: %s", err)
	}
}

func TestFormatLibraryValidationErrorSafe(t *testing.T) {
	msg := FormatLibraryValidationError(ErrAbsoluteTvLibraryPath)
	if msg == "" || filepath.IsAbs(msg) {
		t.Fatalf("unsafe or empty message: %q", msg)
	}
}

func mustFindShow(t *testing.T, result domain.AutoGatherScanResult, name string) domain.AutoGatherShow {
	t.Helper()
	for _, show := range result.Shows {
		if show.Name == name {
			return show
		}
	}
	t.Fatalf("show %q not found in %+v", name, result.Shows)
	return domain.AutoGatherShow{}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %s", path, err)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent %s: %s", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %s", path, err)
	}
}

func containsAbsLeak(warning, root string) bool {
	return strings.Contains(warning, root) || strings.Contains(warning, "/mnt/")
}
