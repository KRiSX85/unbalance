package lib

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/ini.v1"

	"unbalance/daemon/domain"
)

// Exists - Check if File / Directory Exists
func Exists(path string) (bool, error) {
	_, err := os.Stat(path)

	if err == nil {
		return true, nil
	}

	if os.IsNotExist(err) {
		return false, nil
	}

	return false, err
}

// IsEmpty - checks if a folder is empty
func IsEmpty(folder string) (bool, error) {
	f, err := os.Open(folder)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	_, err = f.Readdirnames(1) // Or f.Readdir(1)
	if err == io.EOF {
		return true, nil
	}

	return false, err // Either not empty or error, suits both cases
}

// SearchFile -
func SearchFile(name string, locations []string) string {
	for _, location := range locations {
		if b, _ := Exists(filepath.Join(location, name)); b {
			return location
		}
	}

	return ""
}

var sizes = []string{"B", "KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"}

// ByteSize -
func ByteSize(bytes uint64) string {
	if bytes == 0 {
		return "0B"
	}

	k := float64(1000)
	i := math.Floor(math.Log(float64(bytes)) / math.Log(k))

	return fmt.Sprintf("%.2f %s", float64(bytes)/math.Pow(k, i), sizes[int64(i)])
}

// WriteLine -
func WriteLine(fullpath, line string) error {
	f, err := os.OpenFile(fullpath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(line + "\n")

	return err
}

// WriteLines -
func WriteLines(fullpath string, lines []string) error {
	f, err := os.OpenFile(fullpath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, line := range lines {
		_, err = f.WriteString(line + "\n")
		if err != nil {
			return err
		}
	}

	return nil
}

// Round -
func Round(d, r time.Duration) time.Duration {
	if r <= 0 {
		return d
	}
	neg := d < 0
	if neg {
		d = -d
	}
	if m := d % r; m+m < r {
		d -= d
	} else {
		d = d + r - m
	}
	if neg {
		return -d
	}
	return d
}

// Max -
func Max(x, y uint64) uint64 {
	if x > y {
		return x
	}
	return y
}

// Min -
func Min(x, y uint64) uint64 {
	if x < y {
		return x
	}
	return y
}

func Bind(content any, data any) error {
	m := content.(map[string]interface{})
	s, err := json.Marshal(m)
	if err != nil {
		return err
	}

	err = json.Unmarshal(s, &data)
	if err != nil {
		return err
	}

	return nil
}

// LoadAuthHash reads AUTH_PASSWORD_HASH directly from the env file via the ini
// library. We bypass the start script's bash sourcing because password hashes
// contain $-prefixed segments (for example bcrypt and Argon2id PHC strings)
// that bash treats as parameter expansion, mangling the value before the daemon
// ever sees it.
func LoadAuthHash(location string) (string, error) {
	file, err := ini.Load(location)
	if err != nil {
		return "", err
	}
	return file.Section("").Key("AUTH_PASSWORD_HASH").String(), nil
}

// ApplyPersistedEnv overlays data-dir unbalanced.env onto config when the file
// exists. Missing files leave config unchanged (Kong CLI/env/defaults remain).
//
// Precedence for DRY_RUN and other persisted keys:
//  1. unbalanced.env in the resolved data directory, when the key is present
//     and parseable. This is the runtime source of truth (ToggleDryRun writes it).
//  2. Kong process environment (env:"DRY_RUN") and CLI, used only as the
//     initial value before overlay / when the file or key is absent.
//  3. Kong default (DRY_RUN defaults to true).
//
// A process environment DRY_RUN value does not override a present file key.
func ApplyPersistedEnv(location string, config *domain.Config) error {
	if config == nil {
		return fmt.Errorf("config is nil")
	}
	_, err := os.Stat(location)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return LoadEnv(location, config)
}

func LoadEnv(location string, config *domain.Config) error {
	// load file
	file, err := ini.Load(location)
	if err != nil {
		return err
	}
	_ = os.Chmod(location, 0o600)

	sec := file.Section("")
	if key, err := sec.GetKey("DRY_RUN"); err == nil {
		if v, parseErr := key.Bool(); parseErr == nil {
			config.DryRun = v
		}
	}
	if key, err := sec.GetKey("NOTIFY_PLAN"); err == nil {
		if v, parseErr := key.Int(); parseErr == nil {
			config.NotifyPlan = v
		}
	}
	if key, err := sec.GetKey("NOTIFY_TRANSFER"); err == nil {
		if v, parseErr := key.Int(); parseErr == nil {
			config.NotifyTransfer = v
		}
	}
	if key, err := sec.GetKey("RESERVED_AMOUNT"); err == nil {
		if v, parseErr := key.Uint64(); parseErr == nil {
			config.ReservedAmount = v
		}
	}
	if key, err := sec.GetKey("RESERVED_UNIT"); err == nil {
		config.ReservedUnit = key.String()
	}
	if key, err := sec.GetKey("RSYNC_ARGS"); err == nil {
		config.RsyncArgs = key.Strings(",")
	}
	if key, err := sec.GetKey("VERBOSITY"); err == nil {
		if v, parseErr := key.Int(); parseErr == nil {
			config.Verbosity = v
		}
	}
	if key, err := sec.GetKey("REFRESH_RATE"); err == nil {
		if v, parseErr := key.Int(); parseErr == nil {
			config.RefreshRate = v
		}
	}
	if key, err := sec.GetKey("LOG_LINES"); err == nil {
		if v, parseErr := key.Int(); parseErr == nil {
			config.LogLines = v
		}
	}
	if key, err := sec.GetKey("SPEED_WINDOW"); err == nil {
		if v := strings.TrimSpace(key.String()); v != "" {
			config.SpeedWindow = v
		}
	}
	if key, err := sec.GetKey("TV_LIBRARY_PATH"); err == nil {
		if v := strings.TrimSpace(key.String()); v != "" {
			config.TvLibraryPath = v
		}
	}
	if key, err := sec.GetKey("AUTH_PASSWORD_HASH"); err == nil {
		config.AuthPassword = key.String()
	}

	return nil
}

func SaveEnv(location string, config domain.Config) error {
	file, err := ini.Load(location)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		file = ini.Empty()
	}

	ini.PrettyFormat = false

	// fill data
	file.Section("").Key("DRY_RUN").SetValue(strconv.FormatBool(config.DryRun))
	file.Section("").Key("NOTIFY_PLAN").SetValue(strconv.Itoa(config.NotifyPlan))
	file.Section("").Key("NOTIFY_TRANSFER").SetValue(strconv.Itoa(config.NotifyTransfer))
	file.Section("").Key("RESERVED_AMOUNT").SetValue(strconv.FormatUint(config.ReservedAmount, 10))
	file.Section("").Key("RESERVED_UNIT").SetValue(config.ReservedUnit)
	file.Section("").Key("RSYNC_ARGS").SetValue(strings.Join(config.RsyncArgs, ","))
	file.Section("").Key("VERBOSITY").SetValue(strconv.Itoa(config.Verbosity))
	file.Section("").Key("REFRESH_RATE").SetValue(strconv.Itoa(config.RefreshRate))
	file.Section("").Key("LOG_LINES").SetValue(strconv.Itoa(config.LogLines))
	if config.SpeedWindow == "" {
		config.SpeedWindow = "90s"
	}
	file.Section("").Key("SPEED_WINDOW").SetValue(config.SpeedWindow)
	if config.TvLibraryPath == "" {
		config.TvLibraryPath = "data/media/tv"
	}
	file.Section("").Key("TV_LIBRARY_PATH").SetValue(config.TvLibraryPath)
	file.Section("").Key("AUTH_PASSWORD_HASH").SetValue(config.AuthPassword)

	tmpName := location + ".tmp"
	if err := file.SaveTo(tmpName); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, location); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	return os.Chmod(location, 0o600)
}
