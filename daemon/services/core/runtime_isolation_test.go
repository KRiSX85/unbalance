package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"unbalance/daemon/common"
	"unbalance/daemon/domain"
)

func TestCoreUsesIsolatedRuntimePaths(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	logsDir := filepath.Join(root, "logs")
	paths, err := domain.ResolveRuntimePaths(dataDir, logsDir, domain.ResolveRuntimeOptions{CustomLogsDir: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.EnsureRuntimeDirs(paths); err != nil {
		t.Fatal(err)
	}

	c := &Core{
		ctx: &domain.Context{
			Paths: paths,
			Config: domain.Config{
				DryRun:         true,
				ReservedAmount: 1,
				ReservedUnit:   "Gb",
				RsyncArgs:      []string{"-X"},
				SpeedWindow:    "90s",
				TvLibraryPath:  "data/media/tv",
			},
		},
	}

	if err := c.saveSettings(); err != nil {
		t.Fatalf("saveSettings: %s", err)
	}
	if _, err := os.Stat(paths.EnvFile); err != nil {
		t.Fatalf("expected env file at %s: %v", paths.EnvFile, err)
	}
	history := &domain.History{
		Version: common.HistoryVersion,
		Items:   map[string]*domain.Operation{},
		Order:   []string{},
	}
	if err := c.historyWrite(history); err != nil {
		t.Fatalf("historyWrite: %s", err)
	}
	if _, err := os.Stat(paths.HistoryFile); err != nil {
		t.Fatalf("expected history file at %s: %v", paths.HistoryFile, err)
	}

	official, _ := domain.ResolveRuntimePaths("", "", domain.ResolveRuntimeOptions{})
	if paths.EnvFile == official.EnvFile || paths.HistoryFile == official.HistoryFile || paths.SessionsFile == official.SessionsFile {
		t.Fatal("isolated paths collided with official defaults")
	}
}

func TestGetLogReadsExactConfiguredLogFile(t *testing.T) {
	root := t.TempDir()
	logsDir := filepath.Join(root, "logs")
	paths, err := domain.ResolveRuntimePaths(filepath.Join(root, "data"), logsDir, domain.ResolveRuntimeOptions{CustomLogsDir: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := domain.EnsureRuntimeDirs(paths); err != nil {
		t.Fatal(err)
	}

	marker := "runtime-isolation-getlog-marker-7091"
	if err := os.WriteFile(paths.LogFile, []byte(marker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := &Core{ctx: &domain.Context{Paths: paths, Config: domain.Config{LogLines: 10}}}
	lines := c.GetLog()
	found := false
	for _, line := range lines {
		if strings.Contains(line, marker) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("GetLog did not read configured LogFile %s; got %v", paths.LogFile, lines)
	}
	if paths.LogFile == filepath.Join(common.DefaultLogsDir, common.LogFilename) {
		t.Fatal("expected isolated log file, not official default")
	}
}

func TestGetLogMissingPathReturnsConfigError(t *testing.T) {
	c := &Core{ctx: &domain.Context{Config: domain.Config{LogLines: 10}}}
	lines := c.GetLog()
	if len(lines) != 1 || !strings.Contains(lines[0], "internal configuration error") {
		t.Fatalf("expected configuration error, got %v", lines)
	}
}
