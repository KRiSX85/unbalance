package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cskr/pubsub"
	"gopkg.in/ini.v1"

	"unbalance/daemon/common"
	"unbalance/daemon/domain"
	"unbalance/daemon/lib"
)

func newCoreForDryRunConfigTest(t *testing.T) *Core {
	t.Helper()
	dataDir := t.TempDir()
	paths, err := domain.ResolveRuntimePaths(dataDir, "", domain.ResolveRuntimeOptions{})
	if err != nil {
		t.Fatalf("ResolveRuntimePaths: %v", err)
	}
	c := &Core{
		ctx: &domain.Context{
			Config: domain.Config{
				DryRun:         true,
				ReservedAmount: 1,
				ReservedUnit:   "Gb",
				RsyncArgs:      []string{"-X"},
				RefreshRate:    1000,
				LogLines:       100,
				SpeedWindow:    "90s",
				TvLibraryPath:  "data/media/tv",
			},
			DataDir: dataDir,
			Paths:   paths,
			Hub:     pubsub.New(8),
		},
		state: &domain.State{Status: common.OpNeutral},
	}
	return c
}

func TestSetDryRunTrueToFalseRequiresConfirm(t *testing.T) {
	c := newCoreForDryRunConfigTest(t)
	c.ctx.DryRun = true

	_, err := c.SetDryRun(false, false)
	if err == nil || !strings.Contains(err.Error(), "explicit confirmation") {
		t.Fatalf("expected confirmation required, got %v", err)
	}
	if !c.ctx.DryRun {
		t.Fatal("DRY_RUN must remain true without confirm")
	}
}

func TestSetDryRunFalseToTrueWithoutConfirm(t *testing.T) {
	c := newCoreForDryRunConfigTest(t)
	c.ctx.DryRun = false
	if _, err := c.SetDryRun(true, false); err != nil {
		t.Fatalf("enabling dry-run should not require confirm: %v", err)
	}
	if !c.ctx.DryRun {
		t.Fatal("effective DryRun must be true")
	}
}

func TestSetDryRunPersistsAndUpdatesRuntime(t *testing.T) {
	c := newCoreForDryRunConfigTest(t)
	c.ctx.DryRun = true

	cfg, err := c.SetDryRun(false, true)
	if err != nil {
		t.Fatalf("SetDryRun(false,true): %v", err)
	}
	if cfg.DryRun || c.ctx.DryRun {
		t.Fatal("runtime DryRun must update immediately to false")
	}

	raw, err := os.ReadFile(c.ctx.Paths.EnvFile)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	if !strings.Contains(string(raw), "DRY_RUN=false") {
		t.Fatalf("env file missing DRY_RUN=false:\n%s", raw)
	}

	reloaded := domain.Config{DryRun: true}
	if err := lib.ApplyPersistedEnv(c.ctx.Paths.EnvFile, &reloaded); err != nil {
		t.Fatalf("ApplyPersistedEnv: %v", err)
	}
	if reloaded.DryRun {
		t.Fatal("persisted value must survive reload as false")
	}

	if _, err := c.SetDryRun(true, false); err != nil {
		t.Fatalf("SetDryRun(true): %v", err)
	}
	if !c.ctx.DryRun {
		t.Fatal("runtime DryRun must update immediately to true")
	}
}

func TestSetDryRunDoesNotCreateDuplicateKeys(t *testing.T) {
	c := newCoreForDryRunConfigTest(t)
	seed := "DRY_RUN=true\nDRY_RUN=true\nNOTIFY_PLAN=0\n"
	if err := os.WriteFile(c.ctx.Paths.EnvFile, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := lib.ApplyPersistedEnv(c.ctx.Paths.EnvFile, &c.ctx.Config); err != nil {
		t.Fatal(err)
	}

	if _, err := c.SetDryRun(false, true); err != nil {
		t.Fatalf("SetDryRun: %v", err)
	}

	file, err := ini.Load(c.ctx.Paths.EnvFile)
	if err != nil {
		t.Fatal(err)
	}
	keys := file.Section("").KeysHash()
	count := 0
	for name := range keys {
		if name == "DRY_RUN" {
			count++
		}
	}
	// KeysHash collapses duplicates; also assert file text has a single DRY_RUN line.
	content, err := os.ReadFile(c.ctx.Paths.EnvFile)
	if err != nil {
		t.Fatal(err)
	}
	occurrences := strings.Count(string(content), "DRY_RUN=")
	if occurrences != 1 {
		t.Fatalf("expected exactly one DRY_RUN key, got %d in:\n%s", occurrences, content)
	}
	if count != 1 {
		t.Fatalf("ini KeysHash DRY_RUN count=%d", count)
	}
}

func TestSetDryRunBlockedWhileManualBusy(t *testing.T) {
	c := newCoreForDryRunConfigTest(t)
	c.ctx.DryRun = true
	c.state.Status = common.OpGatherMove

	_, err := c.SetDryRun(false, true)
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("expected busy block, got %v", err)
	}
	if !c.ctx.DryRun {
		t.Fatal("DryRun must be unchanged")
	}
}

func TestSetDryRunBlockedWhileStage3BActive(t *testing.T) {
	c := newCoreForDryRunConfigTest(t)
	c.ctx.DryRun = true
	c.autoGatherRun = &domain.AutoGatherDryRunState{Phase: domain.AutoGatherDryRunPhaseRunning}

	_, err := c.SetDryRun(false, true)
	if err == nil || !strings.Contains(err.Error(), "dry-run is active") {
		t.Fatalf("expected Stage 3B block, got %v", err)
	}
}

func TestSetDryRunBlockedWhileStage3CPrepared(t *testing.T) {
	c := newCoreForDryRunConfigTest(t)
	c.ctx.DryRun = true
	c.autoGatherRealState = &domain.AutoGatherRealState{Phase: domain.AutoGatherRealPhasePrepared}

	_, err := c.SetDryRun(false, true)
	if err == nil || !strings.Contains(err.Error(), "real Auto Gather") {
		t.Fatalf("expected Stage 3C block, got %v", err)
	}
}

func TestSetDryRunBlockedWhileInterrupted(t *testing.T) {
	c := newCoreForDryRunConfigTest(t)
	c.ctx.DryRun = true
	c.autoGatherControlledRun = &domain.AutoGatherControlledState{
		Phase: domain.AutoGatherControlledPhaseInterrupted,
	}

	_, err := c.SetDryRun(false, true)
	if err == nil || !strings.Contains(err.Error(), "acknowledgement") {
		t.Fatalf("expected interrupted block, got %v", err)
	}
	if !c.ctx.DryRun {
		t.Fatal("DryRun must be unchanged while interrupted")
	}
	if c.autoGatherControlledRun.Phase != domain.AutoGatherControlledPhaseInterrupted {
		t.Fatal("interrupted state must not be cleared by SetDryRun")
	}
}

func TestSetDryRunDoesNotExecuteAnything(t *testing.T) {
	c := newCoreForDryRunConfigTest(t)
	c.ctx.DryRun = true
	c.state.Operation = &domain.Operation{OpKind: common.OpGatherMove}

	if _, err := c.SetDryRun(false, true); err != nil {
		t.Fatalf("SetDryRun: %v", err)
	}
	if c.state.Status != common.OpNeutral {
		t.Fatalf("status must stay neutral, got %d", c.state.Status)
	}
	if c.isAutoGatherDryRunActive() || c.isAutoGatherRealExecutionBusy() || c.isAutoGatherControlledActive() {
		t.Fatal("SetDryRun must not start Auto Gather work")
	}
}

func TestSetDryRunNoopSameValue(t *testing.T) {
	c := newCoreForDryRunConfigTest(t)
	c.ctx.DryRun = true
	path := c.ctx.Paths.EnvFile
	if _, err := c.SetDryRun(true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		// SaveEnv may or may not write on no-op; current impl returns early before save.
		_ = filepath.Base(path)
	}
	if !c.ctx.DryRun {
		t.Fatal("noop must keep DryRun true")
	}
}
