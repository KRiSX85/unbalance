package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cskr/pubsub"

	"unbalance/daemon/common"
	"unbalance/daemon/domain"
)

type fixedClock struct {
	now atomic.Value // time.Time
}

func newFixedClock(t time.Time) *fixedClock {
	c := &fixedClock{}
	c.now.Store(t)
	return c
}

func (c *fixedClock) Now() time.Time {
	return c.now.Load().(time.Time)
}

func (c *fixedClock) Set(t time.Time) {
	c.now.Store(t)
}

func newCoreForScheduleTest(t *testing.T) (*Core, *fixedClock) {
	t.Helper()
	dataDir := t.TempDir()
	paths, err := domain.ResolveRuntimePaths(dataDir, "", domain.ResolveRuntimeOptions{})
	if err != nil {
		t.Fatalf("ResolveRuntimePaths: %v", err)
	}
	// Monday 2026-08-24 02:00 local (avoid depending on machine TZ by using Local with fixed wall).
	base := time.Date(2026, 8, 24, 2, 0, 0, 0, time.Local)
	clock := newFixedClock(base)
	c := &Core{
		ctx: &domain.Context{
			Config: domain.Config{
				DryRun:         false,
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
		state:       &domain.State{Status: common.OpNeutral},
		scheduleNow: clock,
	}
	c.initAutoGatherSchedule()
	return c, clock
}

func TestScheduleDisabledByDefault(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	st := c.GetAutoGatherScheduleStatus()
	if st.Enabled || st.Config.Enabled {
		t.Fatal("scheduler must be disabled by default")
	}
	if st.Config.Frequency != domain.ScheduleFrequencyWeekly {
		t.Fatalf("default frequency=%q want weekly", st.Config.Frequency)
	}
	if st.Config.MaxShows != 1 || st.Config.MaxBytes != autoGatherScheduleDefaultBytes {
		t.Fatalf("unexpected defaults: %+v", st.Config)
	}
}

func TestScheduleEnableRequiresConfirm(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	_, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled:  true,
		Hour:     3,
		Minute:   30,
		Weekdays: []int{int(time.Monday)},
		MaxShows: 1,
		MaxBytes: autoGatherScheduleDefaultBytes,
		Confirm:  false,
	})
	if err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("expected confirmation required, got %v", err)
	}
	if c.GetAutoGatherScheduleStatus().Enabled {
		t.Fatal("must remain disabled")
	}
}

func TestScheduleEnableWithConfirmPersists(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	st, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled:  true,
		Hour:     4,
		Minute:   15,
		Weekdays: []int{int(time.Tuesday), int(time.Thursday)},
		MaxShows: 2,
		MaxBytes: 5_000_000_000,
		Confirm:  true,
	})
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !st.Enabled || st.Config.Hour != 4 || st.Config.Minute != 15 {
		t.Fatalf("unexpected status: %+v", st)
	}

	raw, err := os.ReadFile(c.scheduleFilePath())
	if err != nil {
		t.Fatal(err)
	}
	var doc domain.AutoGatherScheduleFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Config.Enabled || doc.Config.MaxShows != 2 {
		t.Fatalf("persisted config: %+v", doc.Config)
	}

	// Restart load
	c2 := &Core{
		ctx:         c.ctx,
		state:       &domain.State{Status: common.OpNeutral},
		scheduleNow: c.scheduleNow,
	}
	c2.initAutoGatherSchedule()
	st2 := c2.GetAutoGatherScheduleStatus()
	if !st2.Enabled || st2.Config.MaxBytes != 5_000_000_000 {
		t.Fatalf("reload: %+v", st2.Config)
	}
}

func TestScheduleMaterialChangeRequiresConfirm(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Hour: 3, Minute: 0, Weekdays: []int{1}, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Hour: 3, Minute: 0, Weekdays: []int{1}, MaxShows: 3, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: false,
	})
	if err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("expected confirm for bound change, got %v", err)
	}
}

func TestScheduleDisableWithoutConfirm(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Hour: 3, Minute: 0, Weekdays: []int{1}, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	st, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: false, Hour: 3, Minute: 0, Weekdays: []int{1}, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled {
		t.Fatal("must be disabled")
	}
}

func TestScheduleValidation(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	_, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Hour: 3, Minute: 0, Weekdays: nil, MaxShows: 1, MaxBytes: 100, Confirm: true,
	})
	if err == nil || !strings.Contains(err.Error(), "weekday") {
		t.Fatalf("expected weekday validation, got %v", err)
	}
	_, err = c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Hour: 25, Minute: 0, Weekdays: []int{1}, MaxShows: 1, MaxBytes: 100, Confirm: true,
	})
	if err == nil {
		t.Fatal("expected hour validation")
	}
}

func TestScheduleMalformedFileFailsSafe(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	if err := os.WriteFile(c.scheduleFilePath(), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.initAutoGatherSchedule()
	st := c.GetAutoGatherScheduleStatus()
	if st.Enabled {
		t.Fatal("malformed config must disable scheduler")
	}
	if st.ConfigError == "" {
		t.Fatal("expected config error")
	}
}

func TestScheduleOccurrenceDueAndNext(t *testing.T) {
	cfg := domain.AutoGatherScheduleConfig{
		Enabled:  true,
		Hour:     3,
		Minute:   0,
		Weekdays: []int{int(time.Monday)},
		MaxShows: 1,
		MaxBytes: 1,
	}
	// Monday 2026-08-24 03:00 local
	now := time.Date(2026, 8, 24, 3, 0, 12, 0, time.Local)
	id, due := scheduleOccurrenceDue(cfg, now)
	if !due || id != "2026-08-24@03:00" {
		t.Fatalf("due=%v id=%q", due, id)
	}
	next, ok := nextScheduleOccurrence(cfg, now, id)
	if !ok {
		t.Fatal("expected next after attempted")
	}
	if scheduleOccurrenceID(next) == id {
		t.Fatalf("next must not be the already-attempted occurrence, got %s", scheduleOccurrenceID(next))
	}
	if next.Weekday() != time.Monday || next.Hour() != 3 {
		t.Fatalf("next=%v", next)
	}
}

func TestScheduleSkipsDryRunWithoutCallingSetDryRun(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	c.ctx.DryRun = true
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Hour: 3, Minute: 0, Weekdays: []int{int(time.Monday)}, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	clock.Set(time.Date(2026, 8, 24, 3, 0, 5, 0, time.Local))
	c.evaluateAutoGatherSchedule()
	st := c.GetAutoGatherScheduleStatus()
	if st.LastResult != domain.ScheduleResultSkippedDryRun {
		t.Fatalf("lastResult=%q detail=%q", st.LastResult, st.LastResultDetail)
	}
	if !c.ctx.DryRun {
		t.Fatal("scheduler must never clear DRY_RUN")
	}
	if c.isAutoGatherControlledActive() {
		t.Fatal("must not start Stage 3D")
	}
}

func TestScheduleSkipsBusyAndDoesNotQueue(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Hour: 3, Minute: 0, Weekdays: []int{int(time.Monday)}, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	c.state.Status = common.OpGatherMove
	clock.Set(time.Date(2026, 8, 24, 3, 0, 5, 0, time.Local))
	c.evaluateAutoGatherSchedule()
	st := c.GetAutoGatherScheduleStatus()
	if st.LastResult != domain.ScheduleResultSkippedBusy {
		t.Fatalf("lastResult=%q", st.LastResult)
	}
	// Later in same minute still skipped (already attempted).
	c.state.Status = common.OpNeutral
	c.evaluateAutoGatherSchedule()
	st2 := c.GetAutoGatherScheduleStatus()
	if st2.LastResult != domain.ScheduleResultSkippedBusy {
		t.Fatalf("must not retry same occurrence, got %q", st2.LastResult)
	}
}

func TestScheduleSkipsInterrupted(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Hour: 3, Minute: 0, Weekdays: []int{int(time.Monday)}, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	c.autoGatherControlledRun = &domain.AutoGatherControlledState{
		Phase: domain.AutoGatherControlledPhaseInterrupted,
	}
	clock.Set(time.Date(2026, 8, 24, 3, 0, 5, 0, time.Local))
	c.evaluateAutoGatherSchedule()
	st := c.GetAutoGatherScheduleStatus()
	if st.LastResult != domain.ScheduleResultSkippedInterrupted {
		t.Fatalf("lastResult=%q", st.LastResult)
	}
	if c.autoGatherControlledRun.Phase != domain.AutoGatherControlledPhaseInterrupted {
		t.Fatal("must not acknowledge/reset interrupted session")
	}
}

func TestScheduleSkipsStage3B(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Hour: 3, Minute: 0, Weekdays: []int{int(time.Monday)}, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	c.autoGatherRun = &domain.AutoGatherDryRunState{Phase: domain.AutoGatherDryRunPhaseRunning}
	clock.Set(time.Date(2026, 8, 24, 3, 0, 5, 0, time.Local))
	c.evaluateAutoGatherSchedule()
	if c.GetAutoGatherScheduleStatus().LastResult != domain.ScheduleResultSkippedBusy {
		t.Fatalf("got %q", c.GetAutoGatherScheduleStatus().LastResult)
	}
}

func TestScheduleSkipsStage3C(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Hour: 3, Minute: 0, Weekdays: []int{int(time.Monday)}, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	c.autoGatherRealState = &domain.AutoGatherRealState{Phase: domain.AutoGatherRealPhasePrepared}
	clock.Set(time.Date(2026, 8, 24, 3, 0, 5, 0, time.Local))
	c.evaluateAutoGatherSchedule()
	if c.GetAutoGatherScheduleStatus().LastResult != domain.ScheduleResultSkippedBusy {
		t.Fatalf("got %q", c.GetAutoGatherScheduleStatus().LastResult)
	}
}

func TestScheduleNoCatchUpAfterMissedOccurrence(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Hour: 3, Minute: 0, Weekdays: []int{int(time.Monday)}, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	// Restart after the window: Monday 03:30 — must not fire Monday 03:00.
	clock.Set(time.Date(2026, 8, 24, 3, 30, 0, 0, time.Local))
	c.evaluateAutoGatherSchedule()
	st := c.GetAutoGatherScheduleStatus()
	if st.LastAttemptedOccurrence == "2026-08-24@03:00" {
		t.Fatal("must not catch up missed occurrence")
	}
	if st.NextOccurrence != "2026-08-31@03:00" && !strings.HasSuffix(st.NextOccurrence, "@03:00") {
		// Next Monday
		next := time.Date(2026, 8, 31, 3, 0, 0, 0, time.Local)
		if st.NextOccurrence != scheduleOccurrenceID(next) {
			t.Fatalf("next=%q want %q", st.NextOccurrence, scheduleOccurrenceID(next))
		}
	}
}

func TestScheduleSameOccurrenceNotDoubleFired(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	c.ctx.DryRun = true // force skip path without starting real work
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Hour: 3, Minute: 0, Weekdays: []int{int(time.Monday)}, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	clock.Set(time.Date(2026, 8, 24, 3, 0, 10, 0, time.Local))
	c.evaluateAutoGatherSchedule()
	first := c.GetAutoGatherScheduleStatus().LastAttemptAt
	c.evaluateAutoGatherSchedule()
	second := c.GetAutoGatherScheduleStatus().LastAttemptAt
	if first == "" || first != second {
		t.Fatalf("second evaluate must not re-attempt; first=%q second=%q", first, second)
	}
}

func TestScheduleStartsStage3DWithConfiguredBoundsAndTrigger(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	// Prevent the real loop from doing work: hook scan to empty.
	autoGatherControlledScanHook = func(c *Core) domain.AutoGatherScanResult {
		return domain.AutoGatherScanResult{Shows: nil}
	}
	t.Cleanup(func() { autoGatherControlledScanHook = nil })

	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Hour: 3, Minute: 0, Weekdays: []int{int(time.Monday)}, MaxShows: 2, MaxBytes: 7_000_000_000, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	clock.Set(time.Date(2026, 8, 24, 3, 0, 5, 0, time.Local))
	c.evaluateAutoGatherSchedule()

	// Give the started goroutine a moment to set state.
	deadline := time.Now().Add(2 * time.Second)
	var state domain.AutoGatherControlledState
	for time.Now().Before(deadline) {
		state = c.GetAutoGatherControlledState()
		if state.SessionID != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if state.Trigger != domain.AutoGatherTriggerScheduled {
		t.Fatalf("trigger=%q", state.Trigger)
	}
	if state.MaxShows != 2 || state.MaxBytes != 7_000_000_000 {
		t.Fatalf("bounds maxShows=%d maxBytes=%d", state.MaxShows, state.MaxBytes)
	}
	st := c.GetAutoGatherScheduleStatus()
	if st.LastAttemptedOccurrence != "2026-08-24@03:00" {
		t.Fatalf("occurrence=%q", st.LastAttemptedOccurrence)
	}
	// Stop to avoid leaking goroutine work in tests.
	c.StopAutoGatherControlled()
}

func TestScheduleStatusExposesTimezone(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	st := c.GetAutoGatherScheduleStatus()
	if st.Timezone == "" || st.ServerLocalTime == "" {
		t.Fatalf("timezone info missing: %+v", st)
	}
}

func TestScheduleFilePathUsesDataDir(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	path := c.scheduleFilePath()
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		dir = filepath.Dir(path)
	}
	dataDir, err := filepath.EvalSymlinks(c.ctx.DataDir)
	if err != nil {
		dataDir = c.ctx.DataDir
	}
	if dir != dataDir {
		t.Fatalf("path=%q dataDir=%q", path, c.ctx.DataDir)
	}
	if filepath.Base(path) != common.ScheduleFilename {
		t.Fatalf("basename=%q", filepath.Base(path))
	}
}

func TestScheduleWatcherIgnoresLaterSessionTerminal(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	occ := "2026-08-24@03:00"
	watchedID := "ag3d-watched"
	laterID := "ag3d-later-manual"

	c.recordScheduleAttempt(occ, domain.ScheduleResultStarted, "scheduled Stage 3D session started", watchedID, clock.Now())

	// A later manual/reset session is now active/completed under a different ID.
	c.autoGatherControlledRun = &domain.AutoGatherControlledState{
		Phase:             domain.AutoGatherControlledPhaseCompleted,
		SessionID:         laterID,
		Trigger:           domain.AutoGatherTriggerManual,
		Completed:         []domain.AutoGatherControlledShowRecord{{ShowPath: "x"}},
		CumulativeBytes:   123,
	}

	done := make(chan struct{})
	go func() {
		c.watchScheduledControlledSession(occ, watchedID)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher did not exit after session-id mismatch")
	}

	st := c.GetAutoGatherScheduleStatus()
	if st.LastResult != domain.ScheduleResultStarted {
		t.Fatalf("watcher must not attribute later session terminal to occurrence; got %q detail=%q", st.LastResult, st.LastResultDetail)
	}
	if st.LastSessionID != watchedID {
		t.Fatalf("LastSessionID=%q want watched %q", st.LastSessionID, watchedID)
	}
}

func TestScheduleWatcherRecordsTerminalForExactSessionID(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	occ := "2026-08-24@03:00"
	sessionID := "ag3d-exact"

	c.recordScheduleAttempt(occ, domain.ScheduleResultStarted, "scheduled Stage 3D session started", sessionID, clock.Now())
	c.autoGatherControlledRun = &domain.AutoGatherControlledState{
		Phase:           domain.AutoGatherControlledPhaseCompleted,
		SessionID:       sessionID,
		Trigger:         domain.AutoGatherTriggerScheduled,
		Completed:       []domain.AutoGatherControlledShowRecord{{ShowPath: "show"}},
		CumulativeBytes: 2_000_000_000,
	}

	done := make(chan struct{})
	go func() {
		c.watchScheduledControlledSession(occ, sessionID)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher did not finish")
	}

	st := c.GetAutoGatherScheduleStatus()
	if st.LastResult != domain.ScheduleResultCompleted {
		t.Fatalf("lastResult=%q want completed", st.LastResult)
	}
	if st.LastSessionID != sessionID {
		t.Fatalf("LastSessionID=%q", st.LastSessionID)
	}
	if !strings.Contains(st.LastResultDetail, "1 shows") {
		t.Fatalf("detail=%q", st.LastResultDetail)
	}
}

func TestScheduleWatcherIgnoresResetClearedSession(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	occ := "2026-08-24@03:00"
	sessionID := "ag3d-reset"
	c.recordScheduleAttempt(occ, domain.ScheduleResultStarted, "scheduled Stage 3D session started", sessionID, clock.Now())
	c.autoGatherControlledRun = nil // reset / new-session transition

	done := make(chan struct{})
	go func() {
		c.watchScheduledControlledSession(occ, sessionID)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher did not exit after reset")
	}
	if c.GetAutoGatherScheduleStatus().LastResult != domain.ScheduleResultStarted {
		t.Fatalf("reset must not rewrite scheduled result, got %q", c.GetAutoGatherScheduleStatus().LastResult)
	}
}

func TestScheduleStartFailureAfterOccurrencePersistDoesNotRetry(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Hour: 3, Minute: 0, Weekdays: []int{int(time.Monday)}, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	occ := "2026-08-24@03:00"
	now := time.Date(2026, 8, 24, 3, 0, 5, 0, time.Local)
	clock.Set(now)

	// Fail-closed order: occurrence persisted first, then Start refuses due to a
	// race (busy) between the clear check and StartAutoGatherControlled.
	c.recordScheduleAttempt(occ, domain.ScheduleResultStarted, "evaluating scheduled occurrence", "", now)
	c.state.Status = common.OpScatterMove
	state, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{
		Confirm: true, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Trigger: domain.AutoGatherTriggerScheduled,
	})
	if err == nil {
		t.Fatal("expected Start to refuse after race")
	}
	detail := err.Error()
	if state.Error != "" {
		detail = state.Error
	}
	c.recordScheduleAttempt(occ, domain.ScheduleResultStartFailed, detail, "", now)

	st := c.GetAutoGatherScheduleStatus()
	if st.LastAttemptedOccurrence != occ {
		t.Fatalf("occurrence must remain %q, got %q", occ, st.LastAttemptedOccurrence)
	}
	if st.LastResult != domain.ScheduleResultStartFailed {
		t.Fatalf("lastResult=%q want start_failed", st.LastResult)
	}

	// System becomes free; same occurrence must not retry.
	c.state.Status = common.OpNeutral
	c.evaluateAutoGatherSchedule()
	st2 := c.GetAutoGatherScheduleStatus()
	if st2.LastResult != domain.ScheduleResultStartFailed {
		t.Fatalf("must not retry after start_failed; got %q", st2.LastResult)
	}
	if st2.LastAttemptedOccurrence != occ {
		t.Fatal("must not clear lastAttemptedOccurrence")
	}
}

func TestScheduleStartOnceAndStop(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	c.startAutoGatherScheduler()
	c.scheduleMu.RLock()
	first := c.scheduleStop
	c.scheduleMu.RUnlock()
	if first == nil {
		t.Fatal("scheduler stop channel must be set")
	}
	c.startAutoGatherScheduler() // second start must be a no-op
	c.scheduleMu.RLock()
	second := c.scheduleStop
	c.scheduleMu.RUnlock()
	if first != second {
		t.Fatal("duplicate start must not replace the stop channel / spawn a second loop")
	}
	c.stopAutoGatherScheduler()
	c.scheduleMu.RLock()
	after := c.scheduleStop
	c.scheduleMu.RUnlock()
	if after != nil {
		t.Fatal("stop must clear scheduleStop")
	}
	// Allow a clean re-start after stop (new Core Start lifecycle).
	c.startAutoGatherScheduler()
	c.scheduleMu.RLock()
	restarted := c.scheduleStop
	c.scheduleMu.RUnlock()
	if restarted == nil || restarted == first {
		t.Fatal("after stop, start must create a new stop channel")
	}
	c.stopAutoGatherScheduler()
}

func TestSchedulePersistUsesTempRename(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	c.scheduleConfig.Enabled = false
	c.scheduleConfig.Hour = 5
	c.scheduleMu.Lock()
	if err := c.persistAutoGatherScheduleLocked(); err != nil {
		c.scheduleMu.Unlock()
		t.Fatal(err)
	}
	c.scheduleMu.Unlock()
	path := c.scheduleFilePath()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file should not remain after successful rename: %v", err)
	}
	// Corrupt final file → load fail-safe disables.
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.initAutoGatherSchedule()
	if c.GetAutoGatherScheduleStatus().Enabled {
		t.Fatal("corrupt schedule json must fail safe disabled")
	}
}

func TestScheduleLegacyConfigWithoutFrequencyLoadsAsWeekly(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	// Pre-frequency document shape (no frequency / monthlyDay fields).
	legacy := `{
  "version": 1,
  "config": {
    "enabled": true,
    "hour": 3,
    "minute": 0,
    "weekdays": [1, 3],
    "maxShows": 2,
    "maxBytes": 5000000000
  },
  "state": {}
}`
	if err := os.WriteFile(c.scheduleFilePath(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	c.initAutoGatherSchedule()
	st := c.GetAutoGatherScheduleStatus()
	if st.ConfigError != "" {
		t.Fatalf("legacy weekly config must load cleanly, got configError=%q", st.ConfigError)
	}
	if !st.Enabled {
		t.Fatal("legacy enabled schedule must remain enabled")
	}
	if st.Config.Frequency != domain.ScheduleFrequencyWeekly {
		t.Fatalf("frequency=%q want weekly", st.Config.Frequency)
	}
	if len(st.Config.Weekdays) != 2 || st.Config.Weekdays[0] != 1 || st.Config.Weekdays[1] != 3 {
		t.Fatalf("weekdays must be preserved: %v", st.Config.Weekdays)
	}
	if st.Config.MaxShows != 2 || st.Config.MaxBytes != 5_000_000_000 {
		t.Fatalf("bounds altered: %+v", st.Config)
	}
}

func TestScheduleMonthlyValidation(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	for _, day := range []int{1, 28} {
		if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
			Enabled: true, Frequency: domain.ScheduleFrequencyMonthly, Hour: 3, Minute: 0,
			MonthlyDay: day, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
		}); err != nil {
			t.Fatalf("day %d should be valid: %v", day, err)
		}
	}
	for _, day := range []int{0, 29, 31} {
		_, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
			Enabled: true, Frequency: domain.ScheduleFrequencyMonthly, Hour: 3, Minute: 0,
			MonthlyDay: day, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
		})
		if err == nil || !strings.Contains(err.Error(), "monthlyDay") {
			t.Fatalf("day %d should be invalid, got %v", day, err)
		}
	}
}

func TestScheduleMalformedMonthlyFailsSafe(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	bad := `{
  "version": 1,
  "config": {
    "enabled": true,
    "frequency": "monthly",
    "hour": 3,
    "minute": 0,
    "monthlyDay": 29,
    "maxShows": 1,
    "maxBytes": 1000000000
  },
  "state": {}
}`
	if err := os.WriteFile(c.scheduleFilePath(), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	c.initAutoGatherSchedule()
	st := c.GetAutoGatherScheduleStatus()
	if st.Enabled {
		t.Fatal("invalid monthly day must fail safe disabled")
	}
	if st.ConfigError == "" {
		t.Fatal("expected config error")
	}
}

func TestScheduleFrequencyChangeRequiresConfirm(t *testing.T) {
	c, _ := newCoreForScheduleTest(t)
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Frequency: domain.ScheduleFrequencyWeekly, Hour: 3, Minute: 0,
		Weekdays: []int{1}, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Frequency: domain.ScheduleFrequencyMonthly, Hour: 3, Minute: 0,
		MonthlyDay: 15, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: false,
	})
	if err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("frequency change on armed schedule requires confirm, got %v", err)
	}
}

func TestScheduleMonthlyNextOccurrence(t *testing.T) {
	cfg := domain.AutoGatherScheduleConfig{
		Enabled: true, Frequency: domain.ScheduleFrequencyMonthly,
		Hour: 3, Minute: 0, MonthlyDay: 15, MaxShows: 1, MaxBytes: 1,
	}
	// Before this month's day → same month.
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.Local)
	next, ok := nextScheduleOccurrence(cfg, now, "")
	if !ok || scheduleOccurrenceID(next) != "2026-08-15@03:00" {
		t.Fatalf("same month next=%v ok=%v", next, ok)
	}
	// After this month's slot → next month.
	now = time.Date(2026, 8, 15, 3, 1, 0, 0, time.Local)
	next, ok = nextScheduleOccurrence(cfg, now, "")
	if !ok || scheduleOccurrenceID(next) != "2026-09-15@03:00" {
		t.Fatalf("roll month next=%v ok=%v", next, ok)
	}
	// December → January year rollover.
	now = time.Date(2026, 12, 20, 0, 0, 0, 0, time.Local)
	cfg.MonthlyDay = 15
	next, ok = nextScheduleOccurrence(cfg, now, "")
	if !ok || scheduleOccurrenceID(next) != "2027-01-15@03:00" {
		t.Fatalf("year roll next=%v ok=%v", next, ok)
	}
	// February day 28.
	cfg.MonthlyDay = 28
	now = time.Date(2026, 2, 10, 0, 0, 0, 0, time.Local)
	next, ok = nextScheduleOccurrence(cfg, now, "")
	if !ok || scheduleOccurrenceID(next) != "2026-02-28@03:00" {
		t.Fatalf("feb next=%v ok=%v", next, ok)
	}
	// Exact scheduled minute due.
	cfg.MonthlyDay = 15
	now = time.Date(2026, 8, 15, 3, 0, 20, 0, time.Local)
	id, due := scheduleOccurrenceDue(cfg, now)
	if !due || id != "2026-08-15@03:00" {
		t.Fatalf("due=%v id=%q", due, id)
	}
	next, ok = nextScheduleOccurrence(cfg, now, "")
	if !ok || scheduleOccurrenceID(next) != id {
		t.Fatalf("exact minute next=%v", next)
	}
	// Already attempted → advance one month.
	next, ok = nextScheduleOccurrence(cfg, now, id)
	if !ok || scheduleOccurrenceID(next) != "2026-09-15@03:00" {
		t.Fatalf("after attempt next=%v", next)
	}
}

func TestScheduleMonthlyIgnoresWeekdays(t *testing.T) {
	cfg := domain.AutoGatherScheduleConfig{
		Enabled: true, Frequency: domain.ScheduleFrequencyMonthly,
		Hour: 3, Minute: 0, MonthlyDay: 15,
		Weekdays: []int{int(time.Monday)}, // must not affect monthly
		MaxShows: 1, MaxBytes: 1,
	}
	// 2026-08-15 is a Saturday; weekly Monday would not match.
	now := time.Date(2026, 8, 15, 3, 0, 5, 0, time.Local)
	id, due := scheduleOccurrenceDue(cfg, now)
	if !due || id != "2026-08-15@03:00" {
		t.Fatalf("monthly must ignore weekdays: due=%v id=%q", due, id)
	}
}

func TestScheduleWeeklyIgnoresMonthlyDay(t *testing.T) {
	cfg := domain.AutoGatherScheduleConfig{
		Enabled: true, Frequency: domain.ScheduleFrequencyWeekly,
		Hour: 3, Minute: 0, Weekdays: []int{int(time.Monday)},
		MonthlyDay: 15, // must not affect weekly
		MaxShows:   1, MaxBytes: 1,
	}
	// Monday 2026-08-24 is day 24, not 15.
	now := time.Date(2026, 8, 24, 3, 0, 5, 0, time.Local)
	id, due := scheduleOccurrenceDue(cfg, now)
	if !due || id != "2026-08-24@03:00" {
		t.Fatalf("weekly must ignore monthlyDay: due=%v id=%q", due, id)
	}
}

func TestScheduleMonthlyNoDoubleFireOrCatchUp(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	c.ctx.DryRun = true // force skip path without Stage 3D
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Frequency: domain.ScheduleFrequencyMonthly, Hour: 3, Minute: 0,
		MonthlyDay: 15, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	clock.Set(time.Date(2026, 8, 15, 3, 0, 5, 0, time.Local))
	c.evaluateAutoGatherSchedule()
	st := c.GetAutoGatherScheduleStatus()
	if st.LastAttemptedOccurrence != "2026-08-15@03:00" || st.LastResult != domain.ScheduleResultSkippedDryRun {
		t.Fatalf("unexpected first attempt: %+v", st)
	}
	c.evaluateAutoGatherSchedule()
	if c.GetAutoGatherScheduleStatus().LastResult != domain.ScheduleResultSkippedDryRun {
		t.Fatal("same monthly occurrence must not re-fire")
	}
	// Restart must not re-fire.
	c2 := &Core{ctx: c.ctx, state: &domain.State{Status: common.OpNeutral}, scheduleNow: clock}
	c2.initAutoGatherSchedule()
	c2.evaluateAutoGatherSchedule()
	if c2.GetAutoGatherScheduleStatus().LastAttemptedOccurrence != "2026-08-15@03:00" {
		t.Fatal("restart must retain lastAttemptedOccurrence")
	}
	if c2.isAutoGatherControlledActive() {
		t.Fatal("restart must not start Stage 3D for same occurrence")
	}
	// Missed: jump past the minute without a prior attempt on a fresh core — no catch-up.
	c3, clock3 := newCoreForScheduleTest(t)
	if _, err := c3.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Frequency: domain.ScheduleFrequencyMonthly, Hour: 3, Minute: 0,
		MonthlyDay: 15, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	clock3.Set(time.Date(2026, 8, 15, 4, 0, 0, 0, time.Local))
	c3.evaluateAutoGatherSchedule()
	if c3.GetAutoGatherScheduleStatus().LastAttemptedOccurrence != "" {
		t.Fatal("missed monthly occurrence must not be caught up")
	}
	next := c3.GetAutoGatherScheduleStatus().NextOccurrence
	if next != "2026-09-15@03:00" {
		t.Fatalf("next after miss=%q", next)
	}
}

func TestScheduleMonthlyBusyAndInterruptedSkip(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Frequency: domain.ScheduleFrequencyMonthly, Hour: 3, Minute: 0,
		MonthlyDay: 15, MaxShows: 2, MaxBytes: 7_000_000_000, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	clock.Set(time.Date(2026, 8, 15, 3, 0, 5, 0, time.Local))
	c.state.Status = common.OpScatterMove
	c.evaluateAutoGatherSchedule()
	if c.GetAutoGatherScheduleStatus().LastResult != domain.ScheduleResultSkippedBusy {
		t.Fatalf("busy: %q", c.GetAutoGatherScheduleStatus().LastResult)
	}
	// New core/month day for interrupted path.
	c2, clock2 := newCoreForScheduleTest(t)
	if _, err := c2.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Frequency: domain.ScheduleFrequencyMonthly, Hour: 3, Minute: 0,
		MonthlyDay: 15, MaxShows: 2, MaxBytes: 7_000_000_000, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	c2.autoGatherControlledRun = &domain.AutoGatherControlledState{Phase: domain.AutoGatherControlledPhaseInterrupted}
	clock2.Set(time.Date(2026, 8, 15, 3, 0, 5, 0, time.Local))
	c2.evaluateAutoGatherSchedule()
	if c2.GetAutoGatherScheduleStatus().LastResult != domain.ScheduleResultSkippedInterrupted {
		t.Fatalf("interrupted: %q", c2.GetAutoGatherScheduleStatus().LastResult)
	}
}

func TestScheduleMonthlyPassesBoundsUnchanged(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	const shows = 3
	const bytes = uint64(4_000_000_000)
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Frequency: domain.ScheduleFrequencyMonthly, Hour: 3, Minute: 0,
		MonthlyDay: 15, MaxShows: shows, MaxBytes: bytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	st := c.GetAutoGatherScheduleStatus()
	if st.Config.MaxShows != shows || st.Config.MaxBytes != bytes {
		t.Fatalf("bounds not preserved: %+v", st.Config)
	}
	// Dry-run skip still records attempt; bounds used only on Start path — verify config read at evaluate.
	c.ctx.DryRun = true
	clock.Set(time.Date(2026, 8, 15, 3, 0, 5, 0, time.Local))
	c.evaluateAutoGatherSchedule()
	if c.scheduleConfig.MaxShows != shows || c.scheduleConfig.MaxBytes != bytes {
		t.Fatal("evaluate must not mutate stored bounds")
	}
}

func TestScheduleMonthlyStartsStage3DOnceWithBounds(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	autoGatherControlledScanHook = func(c *Core) domain.AutoGatherScanResult {
		return domain.AutoGatherScanResult{Shows: nil}
	}
	t.Cleanup(func() { autoGatherControlledScanHook = nil })

	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Frequency: domain.ScheduleFrequencyMonthly, Hour: 3, Minute: 0,
		MonthlyDay: 15, MaxShows: 2, MaxBytes: 7_000_000_000, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	clock.Set(time.Date(2026, 8, 15, 3, 0, 5, 0, time.Local))
	c.evaluateAutoGatherSchedule()

	deadline := time.Now().Add(2 * time.Second)
	var state domain.AutoGatherControlledState
	for time.Now().Before(deadline) {
		state = c.GetAutoGatherControlledState()
		if state.SessionID != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if state.Trigger != domain.AutoGatherTriggerScheduled {
		t.Fatalf("trigger=%q", state.Trigger)
	}
	if state.MaxShows != 2 || state.MaxBytes != 7_000_000_000 {
		t.Fatalf("bounds maxShows=%d maxBytes=%d", state.MaxShows, state.MaxBytes)
	}
	st := c.GetAutoGatherScheduleStatus()
	if st.LastAttemptedOccurrence != "2026-08-15@03:00" {
		t.Fatalf("occurrence=%q", st.LastAttemptedOccurrence)
	}
	session := state.SessionID
	c.evaluateAutoGatherSchedule()
	if c.GetAutoGatherControlledState().SessionID != session {
		t.Fatal("second evaluate must not start a second Stage 3D session")
	}
	c.StopAutoGatherControlled()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ph := c.GetAutoGatherControlledState().Phase
		if ph != domain.AutoGatherControlledPhaseRunning && ph != domain.AutoGatherControlledPhaseStopping {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestScheduleClearsStaleSessionIDOnSkipAndStartFailed(t *testing.T) {
	c, clock := newCoreForScheduleTest(t)
	autoGatherControlledScanHook = func(c *Core) domain.AutoGatherScanResult {
		return domain.AutoGatherScanResult{Shows: nil}
	}
	t.Cleanup(func() { autoGatherControlledScanHook = nil })

	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Frequency: domain.ScheduleFrequencyWeekly, Hour: 3, Minute: 0,
		Weekdays: []int{int(time.Monday)}, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}

	// Successful scheduled start records a session ID.
	clock.Set(time.Date(2026, 8, 24, 3, 0, 5, 0, time.Local))
	c.evaluateAutoGatherSchedule()
	deadline := time.Now().Add(2 * time.Second)
	var started domain.AutoGatherControlledState
	for time.Now().Before(deadline) {
		started = c.GetAutoGatherControlledState()
		if started.SessionID != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if started.SessionID == "" {
		t.Fatal("expected scheduled Stage 3D session")
	}
	st := c.GetAutoGatherScheduleStatus()
	if st.LastSessionID != started.SessionID {
		t.Fatalf("completed/started attempt must record session ID; got %q want %q", st.LastSessionID, started.SessionID)
	}
	priorSession := st.LastSessionID
	c.StopAutoGatherControlled()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ph := c.GetAutoGatherControlledState().Phase
		if ph != domain.AutoGatherControlledPhaseRunning && ph != domain.AutoGatherControlledPhaseStopping {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Next weekly Monday: skipped_dry_run must clear the prior session ID.
	if _, err := c.SetAutoGatherSchedule(domain.AutoGatherScheduleSetRequest{
		Enabled: true, Frequency: domain.ScheduleFrequencyWeekly, Hour: 3, Minute: 0,
		Weekdays: []int{int(time.Monday)}, MaxShows: 1, MaxBytes: autoGatherScheduleDefaultBytes, Confirm: true,
	}); err != nil {
		t.Fatal(err)
	}
	c.ctx.DryRun = true
	clock.Set(time.Date(2026, 8, 31, 3, 0, 5, 0, time.Local))
	c.evaluateAutoGatherSchedule()
	st = c.GetAutoGatherScheduleStatus()
	if st.LastResult != domain.ScheduleResultSkippedDryRun {
		t.Fatalf("lastResult=%q", st.LastResult)
	}
	if st.LastSessionID != "" {
		t.Fatalf("skipped_dry_run must clear stale LastSessionID; still %q (was %q)", st.LastSessionID, priorSession)
	}

	// start_failed after claim must also leave LastSessionID empty.
	c.ctx.DryRun = false
	c.recordScheduleAttempt("2026-09-01@03:00", domain.ScheduleResultStarted, "evaluating scheduled occurrence", "stale-should-clear", clock.Now())
	if c.GetAutoGatherScheduleStatus().LastSessionID != "stale-should-clear" {
		t.Fatal("precondition: planted stale session id")
	}
	c.recordScheduleAttempt("2026-09-01@03:00", domain.ScheduleResultStartFailed, "refused", "", clock.Now())
	if c.GetAutoGatherScheduleStatus().LastSessionID != "" {
		t.Fatalf("start_failed must leave LastSessionID empty, got %q", c.GetAutoGatherScheduleStatus().LastSessionID)
	}

	// A later successful scheduled start records the new session ID only.
	clock.Set(time.Date(2026, 9, 7, 3, 0, 5, 0, time.Local))
	c.evaluateAutoGatherSchedule()
	deadline = time.Now().Add(2 * time.Second)
	var later domain.AutoGatherControlledState
	for time.Now().Before(deadline) {
		later = c.GetAutoGatherControlledState()
		if later.SessionID != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if later.SessionID == "" {
		t.Fatal("expected later scheduled Stage 3D session")
	}
	st = c.GetAutoGatherScheduleStatus()
	if st.LastSessionID != later.SessionID {
		t.Fatalf("later success LastSessionID=%q want %q", st.LastSessionID, later.SessionID)
	}
	if st.LastSessionID == priorSession {
		t.Fatal("later success must not reuse prior session id")
	}
	c.StopAutoGatherControlled()
}

