package domain

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"unbalance/daemon/common"
)

var (
	ErrEmptyDataDir = errors.New("data directory must not be empty")
	ErrEmptyLogsDir = errors.New("logs directory must not be empty")
)

// Overridable for symlink-isolation tests. Production keeps official defaults.
var (
	isolationOfficialDataDir = common.DefaultDataDir
	isolationOfficialLogsDir = common.DefaultLogsDir
)

// RuntimePaths holds all mutable on-disk locations for one unbalanced process.
// Read-only Unraid discovery paths (/var/local/emhttp, /mnt/*) are intentionally
// not included here.
type RuntimePaths struct {
	DataDir      string
	LogsDir      string
	EnvFile      string
	HistoryFile  string
	SessionsFile string
	LogFile      string
}

// ResolveRuntimeOptions controls empty-value rejection and custom-path isolation.
type ResolveRuntimeOptions struct {
	EmptyDataDirExplicit bool
	EmptyLogsDirExplicit bool
	// CustomLogsDir is true when --logs-dir or UNBALANCED_LOGS_DIR was supplied
	// (Kong always fills its default, so presence must be detected separately).
	CustomLogsDir bool
}

// ResolveRuntimePaths builds mutable runtime paths.
//
// dataDirValue is the Kong-resolved --data-dir / UNBALANCED_DATA_DIR value
// (CLI overrides env). An empty value means "use the official default".
//
// logsDirValue is --logs-dir / UNBALANCED_LOGS_DIR (Kong default /var/log when
// the flag and env are omitted). Custom logs directories are validated only when
// opts.CustomLogsDir is true.
func ResolveRuntimePaths(dataDirValue, logsDirValue string, opts ResolveRuntimeOptions) (RuntimePaths, error) {
	if opts.EmptyDataDirExplicit {
		return RuntimePaths{}, ErrEmptyDataDir
	}
	if opts.EmptyLogsDirExplicit {
		return RuntimePaths{}, ErrEmptyLogsDir
	}

	dataDir := strings.TrimSpace(dataDirValue)
	customData := dataDir != ""
	if !customData {
		dataDir = isolationOfficialDataDir
	} else {
		dataDir = filepath.Clean(dataDir)
		if dataDir == "." || dataDir == "" {
			return RuntimePaths{}, ErrEmptyDataDir
		}
	}

	logsDir := strings.TrimSpace(logsDirValue)
	if !opts.CustomLogsDir || logsDir == "" {
		logsDir = isolationOfficialLogsDir
	} else {
		logsDir = filepath.Clean(logsDir)
		if logsDir == "." || logsDir == "" {
			return RuntimePaths{}, ErrEmptyLogsDir
		}
	}

	if customData {
		resolvedData, err := resolvePathExisting(dataDir)
		if err != nil {
			return RuntimePaths{}, fmt.Errorf("data directory %s: %w", dataDir, err)
		}
		officialData, err := resolvePathExisting(isolationOfficialDataDir)
		if err != nil {
			officialData = filepath.Clean(isolationOfficialDataDir)
		}
		if sameOrBeneath(resolvedData, officialData) {
			return RuntimePaths{}, fmt.Errorf(
				"data directory %s resolves to %s which is the official plugin data directory (or beneath it); choose a different --data-dir",
				dataDir, resolvedData,
			)
		}
		dataDir = resolvedData
	}

	if opts.CustomLogsDir {
		resolvedLogs, err := resolvePathExisting(logsDir)
		if err != nil {
			return RuntimePaths{}, fmt.Errorf("logs directory %s: %w", logsDir, err)
		}
		officialLogs, err := resolvePathExisting(isolationOfficialLogsDir)
		if err != nil {
			officialLogs = filepath.Clean(isolationOfficialLogsDir)
		}
		officialLogFile := filepath.Join(officialLogs, common.LogFilename)
		customLogFile := filepath.Join(resolvedLogs, common.LogFilename)
		if filepath.Clean(resolvedLogs) == filepath.Clean(officialLogs) ||
			filepath.Clean(customLogFile) == filepath.Clean(officialLogFile) {
			return RuntimePaths{}, fmt.Errorf(
				"logs directory %s resolves to %s which would use the official log file %s; choose a different --logs-dir",
				logsDir, resolvedLogs, officialLogFile,
			)
		}
		logsDir = resolvedLogs
	}

	paths := RuntimePaths{
		DataDir:      dataDir,
		LogsDir:      logsDir,
		EnvFile:      filepath.Join(dataDir, common.EnvFilename),
		HistoryFile:  filepath.Join(dataDir, common.HistoryFilename),
		SessionsFile: filepath.Join(dataDir, common.SessionFilename),
		LogFile:      filepath.Join(logsDir, common.LogFilename),
	}

	if err := rejectMutablePathCollisions(paths); err != nil {
		return RuntimePaths{}, err
	}

	return paths, nil
}

func rejectMutablePathCollisions(paths RuntimePaths) error {
	files := []string{paths.EnvFile, paths.HistoryFile, paths.SessionsFile, paths.LogFile}
	seen := make(map[string]string, len(files))
	for _, f := range files {
		key := filepath.Clean(f)
		if prev, ok := seen[key]; ok {
			return fmt.Errorf("mutable path collision: %s and %s resolve to the same file", prev, f)
		}
		seen[key] = f
	}
	return nil
}

// EnsureRuntimeDirs creates the data and log directories when missing and verifies
// they are writable. It does not copy or migrate files from another directory.
func EnsureRuntimeDirs(paths RuntimePaths) error {
	if err := ensureWritableDir(paths.DataDir); err != nil {
		return fmt.Errorf("data directory %s: %w", paths.DataDir, err)
	}
	if err := ensureWritableDir(paths.LogsDir); err != nil {
		return fmt.Errorf("logs directory %s: %w", paths.LogsDir, err)
	}
	return nil
}

func ensureWritableDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("unable to create: %w", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory")
	}

	probe := filepath.Join(dir, ".unbalanced-write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("not writable: %w", err)
	}
	_ = os.Remove(probe)
	return nil
}

// resolvePathExisting cleans path, makes it absolute, EvalSymlinks on the
// nearest existing ancestor, then re-joins any missing trailing components.
// It does not create, delete, migrate or alter the target.
func resolvePathExisting(path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return "", fmt.Errorf("path must not be empty")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	current := abs
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			// No existing ancestor (unusual); return cleaned absolute path.
			return abs, nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func sameOrBeneath(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	sep := string(os.PathSeparator)
	return strings.HasPrefix(path, root+sep)
}

// EmptyDataDirExplicit reports whether the process was started with an
// explicitly empty data-dir CLI flag or UNBALANCED_DATA_DIR value.
func EmptyDataDirExplicit(args []string, environ []string) bool {
	if value, ok := flagValue(args, "--data-dir"); ok {
		return strings.TrimSpace(value) == ""
	}
	if value, ok := envValue(environ, "UNBALANCED_DATA_DIR"); ok {
		return strings.TrimSpace(value) == ""
	}
	return false
}

// EmptyLogsDirExplicit reports whether --logs-dir or UNBALANCED_LOGS_DIR was
// supplied with an empty or whitespace-only value.
func EmptyLogsDirExplicit(args []string, environ []string) bool {
	if value, ok := flagValue(args, "--logs-dir"); ok {
		return strings.TrimSpace(value) == ""
	}
	if value, ok := envValue(environ, "UNBALANCED_LOGS_DIR"); ok {
		return strings.TrimSpace(value) == ""
	}
	return false
}

// LogsDirProvided reports whether --logs-dir or UNBALANCED_LOGS_DIR was set
// (used to distinguish Kong's default /var/log from an explicit custom value).
func LogsDirProvided(args []string, environ []string) bool {
	if _, ok := flagValue(args, "--logs-dir"); ok {
		return true
	}
	if _, ok := envValue(environ, "UNBALANCED_LOGS_DIR"); ok {
		return true
	}
	return false
}

func flagValue(args []string, name string) (string, bool) {
	prefix := name + "="
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == name:
			if i+1 >= len(args) {
				return "", true
			}
			return args[i+1], true
		case strings.HasPrefix(arg, prefix):
			return strings.TrimPrefix(arg, prefix), true
		}
	}
	return "", false
}

func envValue(environ []string, key string) (string, bool) {
	prefix := key + "="
	for _, entry := range environ {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix), true
		}
	}
	return "", false
}
