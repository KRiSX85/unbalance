package lib

import (
	"os"
	"path/filepath"
	"testing"

	"unbalance/daemon/domain"
)

func TestSaveEnvWritesRestrictivePermissions(t *testing.T) {
	location := filepath.Join(t.TempDir(), "unbalanced.env")
	if err := os.WriteFile(location, []byte("DRY_RUN=false\n"), 0o644); err != nil {
		t.Fatalf("seed env: %s", err)
	}

	err := SaveEnv(location, domain.Config{
		DryRun:         true,
		NotifyPlan:     1,
		NotifyTransfer: 2,
		ReservedAmount: 42,
		ReservedUnit:   "GB",
		RsyncArgs:      []string{"-X"},
		Verbosity:      1,
		RefreshRate:    1000,
		AuthPassword:   "$argon2id$v=19$m=1,t=1,p=1$c2FsdA$hash",
	})
	if err != nil {
		t.Fatalf("SaveEnv returned error: %s", err)
	}

	info, err := os.Stat(location)
	if err != nil {
		t.Fatalf("stat env: %s", err)
	}

	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("env permissions = %o, want 600", got)
	}
}

func TestLoadEnvDryRunFalseOverridesKongDefaultTrue(t *testing.T) {
	location := filepath.Join(t.TempDir(), "unbalanced.env")
	if err := os.WriteFile(location, []byte("DRY_RUN=false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := domain.Config{DryRun: true, TvLibraryPath: "data/media/tv"}
	if err := LoadEnv(location, &config); err != nil {
		t.Fatalf("LoadEnv: %s", err)
	}
	if config.DryRun {
		t.Fatal("DRY_RUN=false in unbalanced.env must set DryRun=false")
	}
	if config.TvLibraryPath != "data/media/tv" {
		t.Fatalf("missing file keys must not clobber Kong defaults, got %q", config.TvLibraryPath)
	}
}

func TestLoadEnvDryRunTrue(t *testing.T) {
	location := filepath.Join(t.TempDir(), "unbalanced.env")
	if err := os.WriteFile(location, []byte("DRY_RUN=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := domain.Config{DryRun: false}
	if err := LoadEnv(location, &config); err != nil {
		t.Fatalf("LoadEnv: %s", err)
	}
	if !config.DryRun {
		t.Fatal("DRY_RUN=true in unbalanced.env must set DryRun=true")
	}
}

func TestLoadEnvIgnoresProcessEnvironmentOverride(t *testing.T) {
	t.Setenv("DRY_RUN", "true")
	location := filepath.Join(t.TempDir(), "unbalanced.env")
	if err := os.WriteFile(location, []byte("DRY_RUN=false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := domain.Config{DryRun: true}
	if err := LoadEnv(location, &config); err != nil {
		t.Fatalf("LoadEnv: %s", err)
	}
	if config.DryRun {
		t.Fatal("process DRY_RUN must not override a present unbalanced.env key")
	}
}

func TestApplyPersistedEnvMissingFileLeavesKongValue(t *testing.T) {
	config := domain.Config{DryRun: true}
	missing := filepath.Join(t.TempDir(), "unbalanced.env")
	if err := ApplyPersistedEnv(missing, &config); err != nil {
		t.Fatalf("missing file must be a no-op, err=%v", err)
	}
	if !config.DryRun {
		t.Fatal("Kong default true must remain when env file is absent")
	}
}

func TestLoadEnvMalformedDryRunDoesNotEnableRealExecution(t *testing.T) {
	for _, raw := range []string{"maybe", "yesn't", "", "2", "TRUEISH"} {
		t.Run("value="+raw, func(t *testing.T) {
			location := filepath.Join(t.TempDir(), "unbalanced.env")
			content := "DRY_RUN=" + raw + "\n"
			if err := os.WriteFile(location, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			config := domain.Config{DryRun: true}
			if err := LoadEnv(location, &config); err != nil {
				t.Fatalf("LoadEnv: %s", err)
			}
			if !config.DryRun {
				t.Fatalf("unparseable DRY_RUN=%q must keep Kong default true, not enable real execution", raw)
			}
		})
	}
}
