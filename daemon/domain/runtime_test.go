package domain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRuntimePathsDefaults(t *testing.T) {
	paths, err := ResolveRuntimePaths("", "/var/log", ResolveRuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if paths.DataDir != "/boot/config/plugins/unbalanced" {
		t.Fatalf("DataDir = %q", paths.DataDir)
	}
	if paths.LogsDir != "/var/log" {
		t.Fatalf("LogsDir = %q", paths.LogsDir)
	}
	if paths.EnvFile != "/boot/config/plugins/unbalanced/unbalanced.env" {
		t.Fatalf("EnvFile = %q", paths.EnvFile)
	}
	if paths.HistoryFile != "/boot/config/plugins/unbalanced/unbalanced.hist" {
		t.Fatalf("HistoryFile = %q", paths.HistoryFile)
	}
	if paths.SessionsFile != "/boot/config/plugins/unbalanced/unbalanced.sessions" {
		t.Fatalf("SessionsFile = %q", paths.SessionsFile)
	}
	if paths.LogFile != "/var/log/unbalanced.log" {
		t.Fatalf("LogFile = %q", paths.LogFile)
	}
}

func TestResolveRuntimePathsDataDirEnvStyle(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	paths, err := ResolveRuntimePaths(data, "", ResolveRuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	wantData, err := resolvePathExisting(data)
	if err != nil {
		t.Fatal(err)
	}
	if paths.DataDir != wantData {
		t.Fatalf("DataDir = %q, want %q", paths.DataDir, wantData)
	}
	if paths.EnvFile != filepath.Join(wantData, "unbalanced.env") {
		t.Fatalf("EnvFile = %q", paths.EnvFile)
	}
	if paths.HistoryFile != filepath.Join(wantData, "unbalanced.hist") {
		t.Fatalf("HistoryFile = %q", paths.HistoryFile)
	}
	if paths.SessionsFile != filepath.Join(wantData, "unbalanced.sessions") {
		t.Fatalf("SessionsFile = %q", paths.SessionsFile)
	}
	if paths.LogFile != "/var/log/unbalanced.log" {
		t.Fatalf("LogFile = %q", paths.LogFile)
	}
}

func TestResolveRuntimePathsCLIOverridesEnvValue(t *testing.T) {
	root := t.TempDir()
	envStyle := filepath.Join(root, "from-env")
	cliStyle := filepath.Join(root, "data")
	logsStyle := filepath.Join(root, "logs")
	paths, err := ResolveRuntimePaths(cliStyle, logsStyle, ResolveRuntimeOptions{CustomLogsDir: true})
	if err != nil {
		t.Fatal(err)
	}
	wantEnv, _ := resolvePathExisting(envStyle)
	if paths.DataDir == wantEnv {
		t.Fatalf("CLI value should win, got env path")
	}
	wantData, err := resolvePathExisting(cliStyle)
	if err != nil {
		t.Fatal(err)
	}
	wantLogs, err := resolvePathExisting(logsStyle)
	if err != nil {
		t.Fatal(err)
	}
	if paths.DataDir != wantData {
		t.Fatalf("DataDir = %q", paths.DataDir)
	}
	if paths.LogsDir != wantLogs {
		t.Fatalf("LogsDir = %q", paths.LogsDir)
	}
	if paths.LogFile != filepath.Join(wantLogs, "unbalanced.log") {
		t.Fatalf("LogFile = %q", paths.LogFile)
	}
}

func TestResolveRuntimePathsRejectsExplicitEmptyData(t *testing.T) {
	if _, err := ResolveRuntimePaths("", "", ResolveRuntimeOptions{EmptyDataDirExplicit: true}); !errors.Is(err, ErrEmptyDataDir) {
		t.Fatalf("expected ErrEmptyDataDir, got %v", err)
	}
}

func TestResolveRuntimePathsRejectsExplicitEmptyLogs(t *testing.T) {
	if _, err := ResolveRuntimePaths("/tmp/data", "", ResolveRuntimeOptions{
		EmptyLogsDirExplicit: true,
		CustomLogsDir:        true,
	}); !errors.Is(err, ErrEmptyLogsDir) {
		t.Fatalf("expected ErrEmptyLogsDir, got %v", err)
	}
}

func TestSeparateRuntimeConfigurationsDoNotShareMutableFiles(t *testing.T) {
	root := t.TempDir()
	official, err := ResolveRuntimePaths("", "/var/log", ResolveRuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	isolated, err := ResolveRuntimePaths(
		filepath.Join(root, "data"),
		filepath.Join(root, "logs"),
		ResolveRuntimeOptions{CustomLogsDir: true},
	)
	if err != nil {
		t.Fatal(err)
	}

	if official.EnvFile == isolated.EnvFile {
		t.Fatalf("env files collide: %q", official.EnvFile)
	}
	if official.HistoryFile == isolated.HistoryFile {
		t.Fatalf("history files collide: %q", official.HistoryFile)
	}
	if official.SessionsFile == isolated.SessionsFile {
		t.Fatalf("sessions files collide: %q", official.SessionsFile)
	}
	if official.LogFile == isolated.LogFile {
		t.Fatalf("log files collide: %q", official.LogFile)
	}
}

func TestEmptyDataDirExplicit(t *testing.T) {
	if !EmptyDataDirExplicit([]string{"unbalanced", "--data-dir", ""}, nil) {
		t.Fatal("expected empty --data-dir to be explicit")
	}
	if !EmptyDataDirExplicit([]string{"unbalanced", "--data-dir="}, nil) {
		t.Fatal("expected --data-dir= to be explicit")
	}
	if !EmptyDataDirExplicit(nil, []string{"UNBALANCED_DATA_DIR="}) {
		t.Fatal("expected empty env to be explicit")
	}
	if !EmptyDataDirExplicit([]string{"unbalanced", "--data-dir", "   "}, nil) {
		t.Fatal("expected whitespace --data-dir to be explicit")
	}
	if EmptyDataDirExplicit([]string{"unbalanced", "--port", "7091"}, []string{"HOME=/tmp"}) {
		t.Fatal("did not expect explicit empty")
	}
}

func TestEmptyLogsDirExplicit(t *testing.T) {
	if !EmptyLogsDirExplicit([]string{"unbalanced", "--logs-dir", ""}, nil) {
		t.Fatal("expected empty --logs-dir to be explicit")
	}
	if !EmptyLogsDirExplicit([]string{"unbalanced", "--logs-dir="}, nil) {
		t.Fatal("expected --logs-dir= to be explicit")
	}
	if !EmptyLogsDirExplicit([]string{"unbalanced", "--logs-dir", "  "}, nil) {
		t.Fatal("expected whitespace --logs-dir to be explicit")
	}
	if !EmptyLogsDirExplicit(nil, []string{"UNBALANCED_LOGS_DIR="}) {
		t.Fatal("expected empty UNBALANCED_LOGS_DIR to be explicit")
	}
	if EmptyLogsDirExplicit([]string{"unbalanced", "--port", "7091"}, []string{"HOME=/tmp"}) {
		t.Fatal("omitted logs-dir must not be treated as explicit empty")
	}
	if LogsDirProvided([]string{"unbalanced", "--port", "7091"}, nil) {
		t.Fatal("omitted logs-dir must not count as provided")
	}
	if !LogsDirProvided([]string{"unbalanced", "--logs-dir", "/tmp/logs"}, nil) {
		t.Fatal("expected --logs-dir to count as provided")
	}
	if !LogsDirProvided(nil, []string{"UNBALANCED_LOGS_DIR=/tmp/logs"}) {
		t.Fatal("expected UNBALANCED_LOGS_DIR to count as provided")
	}
}

func TestEnsureRuntimeDirs(t *testing.T) {
	root := t.TempDir()
	paths, err := ResolveRuntimePaths(filepath.Join(root, "data"), filepath.Join(root, "logs"), ResolveRuntimeOptions{CustomLogsDir: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureRuntimeDirs(paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.DataDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.LogsDir); err != nil {
		t.Fatal(err)
	}
}

func TestRejectSymlinkedDataDirIntoOfficial(t *testing.T) {
	official := t.TempDir()
	prev := isolationOfficialDataDir
	isolationOfficialDataDir = official
	t.Cleanup(func() { isolationOfficialDataDir = prev })

	custom := filepath.Join(t.TempDir(), "data-link")
	if err := os.Symlink(official, custom); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveRuntimePaths(custom, filepath.Join(t.TempDir(), "logs"), ResolveRuntimeOptions{CustomLogsDir: true})
	if err == nil {
		t.Fatal("expected rejection of data-dir symlink into official plugin directory")
	}
	if !strings.Contains(err.Error(), "official plugin data directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRejectSymlinkedLogsDirIntoOfficial(t *testing.T) {
	officialLogs := t.TempDir()
	prev := isolationOfficialLogsDir
	isolationOfficialLogsDir = officialLogs
	t.Cleanup(func() { isolationOfficialLogsDir = prev })

	custom := filepath.Join(t.TempDir(), "logs-link")
	if err := os.Symlink(officialLogs, custom); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveRuntimePaths(filepath.Join(t.TempDir(), "data"), custom, ResolveRuntimeOptions{CustomLogsDir: true})
	if err == nil {
		t.Fatal("expected rejection of logs-dir symlink into official log directory")
	}
	if !strings.Contains(err.Error(), "official log file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRejectDataDirBeneathOfficial(t *testing.T) {
	official := t.TempDir()
	prev := isolationOfficialDataDir
	isolationOfficialDataDir = official
	t.Cleanup(func() { isolationOfficialDataDir = prev })

	nested := filepath.Join(official, "nested")
	_, err := ResolveRuntimePaths(nested, filepath.Join(t.TempDir(), "logs"), ResolveRuntimeOptions{CustomLogsDir: true})
	if err == nil {
		t.Fatal("expected rejection of data-dir beneath official plugin directory")
	}
}

func TestOfficialDefaultsStillWorkWhenNoCustomOptions(t *testing.T) {
	paths, err := ResolveRuntimePaths("", "", ResolveRuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if paths.DataDir != isolationOfficialDataDir {
		t.Fatalf("DataDir = %q", paths.DataDir)
	}
	if paths.LogsDir != isolationOfficialLogsDir {
		t.Fatalf("LogsDir = %q", paths.LogsDir)
	}
	if paths.LogFile != filepath.Join(isolationOfficialLogsDir, "unbalanced.log") {
		t.Fatalf("LogFile = %q", paths.LogFile)
	}
}

func TestCustomPathsDoNotSilentlyFallBack(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	logs := filepath.Join(root, "logs")
	paths, err := ResolveRuntimePaths(data, logs, ResolveRuntimeOptions{CustomLogsDir: true})
	if err != nil {
		t.Fatal(err)
	}
	if paths.DataDir == isolationOfficialDataDir || strings.HasPrefix(paths.DataDir, isolationOfficialDataDir+string(os.PathSeparator)) {
		t.Fatalf("custom data fell back to official: %s", paths.DataDir)
	}
	if paths.LogsDir == isolationOfficialLogsDir {
		t.Fatalf("custom logs fell back to official: %s", paths.LogsDir)
	}
	if paths.LogFile == filepath.Join(isolationOfficialLogsDir, "unbalanced.log") {
		t.Fatalf("custom log file fell back to official: %s", paths.LogFile)
	}
	if filepath.Dir(paths.EnvFile) != paths.DataDir {
		t.Fatalf("env not under custom data dir")
	}
	if filepath.Dir(paths.SessionsFile) != paths.DataDir {
		t.Fatalf("sessions not under custom data dir")
	}
	if filepath.Dir(paths.HistoryFile) != paths.DataDir {
		t.Fatalf("history not under custom data dir")
	}
	if filepath.Dir(paths.LogFile) != paths.LogsDir {
		t.Fatalf("log not under custom logs dir")
	}
}
