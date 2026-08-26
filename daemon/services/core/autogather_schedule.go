package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"unbalance/daemon/common"
	"unbalance/daemon/domain"
	"unbalance/daemon/logger"
)

const (
	autoGatherScheduleFileVersion = 1
	autoGatherScheduleDefaultHour = 3
	autoGatherScheduleDefaultMin  = 0
	autoGatherScheduleDefaultShows = 1
	autoGatherScheduleDefaultBytes = 10 * 1000 * 1000 * 1000 // 10 decimal GB
	autoGatherScheduleMaxShows     = 50
	autoGatherScheduleTickInterval = 15 * time.Second
)

// Clock is injectable for deterministic scheduler tests.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func defaultAutoGatherScheduleConfig() domain.AutoGatherScheduleConfig {
	return domain.AutoGatherScheduleConfig{
		Enabled:  false,
		Hour:     autoGatherScheduleDefaultHour,
		Minute:   autoGatherScheduleDefaultMin,
		Weekdays: nil,
		MaxShows: autoGatherScheduleDefaultShows,
		MaxBytes: autoGatherScheduleDefaultBytes,
	}
}

func (c *Core) scheduleFilePath() string {
	dir := ""
	if c.ctx != nil {
		if c.ctx.Paths.DataDir != "" {
			dir = c.ctx.Paths.DataDir
		} else if c.ctx.DataDir != "" {
			dir = c.ctx.DataDir
		}
	}
	if dir == "" {
		dir = common.DefaultDataDir
	}
	return filepath.Join(dir, common.ScheduleFilename)
}

func (c *Core) scheduleClock() Clock {
	if c.scheduleNow != nil {
		return c.scheduleNow
	}
	return systemClock{}
}

func (c *Core) initAutoGatherSchedule() {
	c.scheduleMu.Lock()
	defer c.scheduleMu.Unlock()
	if c.scheduleNow == nil {
		c.scheduleNow = systemClock{}
	}
	doc, err := c.loadAutoGatherScheduleFile()
	if err != nil {
		logger.Yellow("autoGatherSchedule: load failed (fail-safe disabled): %s", err)
		c.scheduleConfig = defaultAutoGatherScheduleConfig()
		c.scheduleState = domain.AutoGatherScheduleState{
			ConfigError: fmt.Sprintf("malformed schedule config; scheduler disabled: %s", err),
		}
		return
	}
	c.scheduleConfig = doc.Config
	c.scheduleState = doc.State
	if verr := validateAutoGatherScheduleConfig(c.scheduleConfig, c.scheduleConfig.Enabled); verr != nil {
		logger.Yellow("autoGatherSchedule: invalid persisted config (fail-safe disabled): %s", verr)
		c.scheduleState.ConfigError = fmt.Sprintf("invalid schedule config; scheduler disabled: %s", verr)
		c.scheduleConfig.Enabled = false
	}
}

func (c *Core) startAutoGatherScheduler() {
	c.scheduleMu.Lock()
	if c.scheduleStop != nil {
		c.scheduleMu.Unlock()
		return
	}
	c.scheduleStop = make(chan struct{})
	stop := c.scheduleStop
	c.scheduleMu.Unlock()

	go c.autoGatherScheduleLoop(stop)
}

func (c *Core) stopAutoGatherScheduler() {
	c.scheduleMu.Lock()
	stop := c.scheduleStop
	c.scheduleStop = nil
	c.scheduleMu.Unlock()
	if stop != nil {
		close(stop)
	}
}

func (c *Core) autoGatherScheduleLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(autoGatherScheduleTickInterval)
	defer ticker.Stop()
	c.evaluateAutoGatherSchedule()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			c.evaluateAutoGatherSchedule()
		}
	}
}

// evaluateAutoGatherSchedule is the single tick entry point (also callable from tests).
func (c *Core) evaluateAutoGatherSchedule() {
	now := c.scheduleClock().Now().In(time.Local)
	c.scheduleMu.Lock()
	cfg := c.scheduleConfig
	lastAttempt := c.scheduleState.LastAttemptedOccurrence
	configErr := c.scheduleState.ConfigError
	c.scheduleMu.Unlock()

	if configErr != "" || !cfg.Enabled {
		return
	}
	id, due := scheduleOccurrenceDue(cfg, now)
	if !due || id == "" || id == lastAttempt {
		return
	}

	// Persist occurrence identity BEFORE any start attempt so restarts cannot
	// double-fire the same local occurrence.
	c.recordScheduleAttempt(id, domain.ScheduleResultStarted, "evaluating scheduled occurrence", "", now)

	if c.ctx != nil && c.ctx.DryRun {
		c.recordScheduleAttempt(id, domain.ScheduleResultSkippedDryRun, "Global Dry Run is enabled", "", now)
		logger.Blue("autoGatherSchedule: skipped %s — Global Dry Run enabled", id)
		return
	}
	if c.isAutoGatherControlledInterrupted() {
		c.recordScheduleAttempt(id, domain.ScheduleResultSkippedInterrupted, "interrupted Stage 3D session requires acknowledgement", "", now)
		logger.Blue("autoGatherSchedule: skipped %s — interrupted session", id)
		return
	}
	if reason := c.scheduleConflictReason(); reason != "" {
		c.recordScheduleAttempt(id, domain.ScheduleResultSkippedBusy, reason, "", now)
		logger.Blue("autoGatherSchedule: skipped %s — %s", id, reason)
		return
	}

	c.scheduleMu.RLock()
	maxShows := c.scheduleConfig.MaxShows
	maxBytes := c.scheduleConfig.MaxBytes
	c.scheduleMu.RUnlock()

	state, err := c.StartAutoGatherControlled(domain.AutoGatherControlledStartRequest{
		Confirm:  true,
		MaxShows: maxShows,
		MaxBytes: maxBytes,
		Trigger:  domain.AutoGatherTriggerScheduled,
	})
	if err != nil {
		detail := err.Error()
		if state.Error != "" {
			detail = state.Error
		}
		c.recordScheduleAttempt(id, domain.ScheduleResultStartFailed, detail, "", now)
		logger.Yellow("autoGatherSchedule: start failed for %s: %s", id, detail)
		return
	}

	c.recordScheduleAttempt(id, domain.ScheduleResultStarted, "scheduled Stage 3D session started", state.SessionID, now)
	logger.Blue("autoGatherSchedule: started Stage 3D for %s session=%s", id, state.SessionID)
	go c.watchScheduledControlledSession(id, state.SessionID)
}

func (c *Core) scheduleConflictReason() string {
	if c.isAutoGatherDryRunActive() {
		return "Auto Gather dry-run is active"
	}
	if c.isAutoGatherRealSessionActive() {
		return "Stage 3C one-show real move is active"
	}
	if c.isAutoGatherControlledActive() {
		return "Stage 3D controlled session is already active"
	}
	if c.state != nil && c.state.Status != common.OpNeutral {
		return fmt.Sprintf("unbalanced is busy (status %d)", c.state.Status)
	}
	return ""
}

func (c *Core) watchScheduledControlledSession(occurrenceID, sessionID string) {
	if occurrenceID == "" || sessionID == "" {
		return
	}
	for {
		time.Sleep(1 * time.Second)
		state := c.GetAutoGatherControlledState()
		// Bind strictly to the Stage 3D session created for this occurrence.
		// A later manual/scheduled Start, or a reset that clears SessionID, must
		// not attribute its terminal result to this occurrence.
		if state.SessionID != sessionID {
			return
		}
		switch state.Phase {
		case domain.AutoGatherControlledPhaseCompleted:
			detail := formatScheduleSessionDetail(state)
			c.recordScheduleTerminal(occurrenceID, domain.ScheduleResultCompleted, detail, state.SessionID)
			return
		case domain.AutoGatherControlledPhaseFailed:
			detail := state.FailureReason
			if detail == "" {
				detail = state.Error
			}
			if detail == "" {
				detail = formatScheduleSessionDetail(state)
			}
			c.recordScheduleTerminal(occurrenceID, domain.ScheduleResultFailed, detail, state.SessionID)
			return
		case domain.AutoGatherControlledPhaseStopped:
			detail := formatScheduleSessionDetail(state)
			c.recordScheduleTerminal(occurrenceID, domain.ScheduleResultStopped, detail, state.SessionID)
			return
		case domain.AutoGatherControlledPhaseInterrupted:
			c.recordScheduleTerminal(occurrenceID, domain.ScheduleResultFailed, "session interrupted; acknowledgement required", state.SessionID)
			return
		case domain.AutoGatherControlledPhaseRunning, domain.AutoGatherControlledPhaseStopping:
			continue
		default:
			return
		}
	}
}

func formatScheduleSessionDetail(state domain.AutoGatherControlledState) string {
	completed := len(state.Completed)
	return fmt.Sprintf("%d shows, %s moved", completed, humanBytesApprox(state.CumulativeBytes))
}

func humanBytesApprox(bytes uint64) string {
	const gb = 1000 * 1000 * 1000
	if bytes >= gb {
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	}
	const mb = 1000 * 1000
	if bytes >= mb {
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	}
	return fmt.Sprintf("%d bytes", bytes)
}

func (c *Core) recordScheduleAttempt(occurrenceID, result, detail, sessionID string, now time.Time) {
	c.scheduleMu.Lock()
	defer c.scheduleMu.Unlock()
	c.scheduleState.LastAttemptedOccurrence = occurrenceID
	c.scheduleState.LastResult = result
	c.scheduleState.LastResultDetail = detail
	c.scheduleState.LastAttemptAt = now.Format(time.RFC3339)
	if sessionID != "" {
		c.scheduleState.LastSessionID = sessionID
	}
	if result != domain.ScheduleResultStarted {
		c.scheduleState.LastEndedAt = now.Format(time.RFC3339)
	}
	_ = c.persistAutoGatherScheduleLocked()
}

func (c *Core) recordScheduleTerminal(occurrenceID, result, detail, sessionID string) {
	now := c.scheduleClock().Now().In(time.Local)
	c.scheduleMu.Lock()
	defer c.scheduleMu.Unlock()
	if c.scheduleState.LastAttemptedOccurrence != occurrenceID {
		return
	}
	c.scheduleState.LastResult = result
	c.scheduleState.LastResultDetail = detail
	c.scheduleState.LastEndedAt = now.Format(time.RFC3339)
	if sessionID != "" {
		c.scheduleState.LastSessionID = sessionID
	}
	_ = c.persistAutoGatherScheduleLocked()
}

// GetAutoGatherScheduleStatus returns the server-authoritative schedule view.
func (c *Core) GetAutoGatherScheduleStatus() domain.AutoGatherScheduleStatus {
	now := c.scheduleClock().Now().In(time.Local)
	c.scheduleMu.RLock()
	cfg := c.scheduleConfig
	st := c.scheduleState
	c.scheduleMu.RUnlock()

	name, offset := now.Zone()
	status := domain.AutoGatherScheduleStatus{
		Config:                  cfg,
		Enabled:                 cfg.Enabled && st.ConfigError == "",
		Timezone:                name,
		TimezoneOffsetMinutes:   offset / 60,
		ServerLocalTime:         now.Format(time.RFC3339),
		LastAttemptedOccurrence: st.LastAttemptedOccurrence,
		LastResult:              st.LastResult,
		LastResultDetail:        st.LastResultDetail,
		LastAttemptAt:           st.LastAttemptAt,
		LastEndedAt:             st.LastEndedAt,
		LastSessionID:           st.LastSessionID,
		ConfigError:             st.ConfigError,
		GlobalDryRun:            c.ctx != nil && c.ctx.DryRun,
	}
	if cfg.Enabled && st.ConfigError == "" {
		if next, ok := nextScheduleOccurrence(cfg, now, st.LastAttemptedOccurrence); ok {
			status.NextOccurrence = scheduleOccurrenceID(next)
			status.NextOccurrenceAt = next.Format(time.RFC3339)
		}
	}
	return status
}

// SetAutoGatherSchedule updates and persists the schedule. It never starts a run.
// Confirm is required when enabling or changing an already-enabled schedule.
func (c *Core) SetAutoGatherSchedule(req domain.AutoGatherScheduleSetRequest) (domain.AutoGatherScheduleStatus, error) {
	cfg := domain.AutoGatherScheduleConfig{
		Enabled:  req.Enabled,
		Hour:     req.Hour,
		Minute:   req.Minute,
		Weekdays: append([]int(nil), req.Weekdays...),
		MaxShows: req.MaxShows,
		MaxBytes: req.MaxBytes,
	}
	normalizeScheduleWeekdays(&cfg)

	c.scheduleMu.Lock()
	defer c.scheduleMu.Unlock()

	prev := c.scheduleConfig
	needsConfirm := false
	if cfg.Enabled && !prev.Enabled {
		needsConfirm = true
	}
	if prev.Enabled && cfg.Enabled && scheduleConfigMateriallyChanged(prev, cfg) {
		needsConfirm = true
	}
	if needsConfirm && !req.Confirm {
		return c.scheduleStatusLocked(c.scheduleClock().Now().In(time.Local)), fmt.Errorf("explicit confirmation is required to enable or change scheduled real Auto Gather")
	}

	if err := validateAutoGatherScheduleConfig(cfg, cfg.Enabled); err != nil {
		return c.scheduleStatusLocked(c.scheduleClock().Now().In(time.Local)), err
	}

	c.scheduleConfig = cfg
	c.scheduleState.ConfigError = ""
	if err := c.persistAutoGatherScheduleLocked(); err != nil {
		c.scheduleConfig = prev
		return c.scheduleStatusLocked(c.scheduleClock().Now().In(time.Local)), fmt.Errorf("unable to persist schedule: %w", err)
	}
	return c.scheduleStatusLocked(c.scheduleClock().Now().In(time.Local)), nil
}

func (c *Core) scheduleStatusLocked(now time.Time) domain.AutoGatherScheduleStatus {
	cfg := c.scheduleConfig
	st := c.scheduleState
	name, offset := now.Zone()
	status := domain.AutoGatherScheduleStatus{
		Config:                  cfg,
		Enabled:                 cfg.Enabled && st.ConfigError == "",
		Timezone:                name,
		TimezoneOffsetMinutes:   offset / 60,
		ServerLocalTime:         now.Format(time.RFC3339),
		LastAttemptedOccurrence: st.LastAttemptedOccurrence,
		LastResult:              st.LastResult,
		LastResultDetail:        st.LastResultDetail,
		LastAttemptAt:           st.LastAttemptAt,
		LastEndedAt:             st.LastEndedAt,
		LastSessionID:           st.LastSessionID,
		ConfigError:             st.ConfigError,
		GlobalDryRun:            c.ctx != nil && c.ctx.DryRun,
	}
	if cfg.Enabled && st.ConfigError == "" {
		if next, ok := nextScheduleOccurrence(cfg, now, st.LastAttemptedOccurrence); ok {
			status.NextOccurrence = scheduleOccurrenceID(next)
			status.NextOccurrenceAt = next.Format(time.RFC3339)
		}
	}
	return status
}

func scheduleConfigMateriallyChanged(a, b domain.AutoGatherScheduleConfig) bool {
	if a.Hour != b.Hour || a.Minute != b.Minute || a.MaxShows != b.MaxShows || a.MaxBytes != b.MaxBytes {
		return true
	}
	if len(a.Weekdays) != len(b.Weekdays) {
		return true
	}
	aa := append([]int(nil), a.Weekdays...)
	bb := append([]int(nil), b.Weekdays...)
	sort.Ints(aa)
	sort.Ints(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return true
		}
	}
	return false
}

func normalizeScheduleWeekdays(cfg *domain.AutoGatherScheduleConfig) {
	if cfg == nil {
		return
	}
	seen := map[int]struct{}{}
	out := make([]int, 0, len(cfg.Weekdays))
	for _, d := range cfg.Weekdays {
		if d < 0 || d > 6 {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	sort.Ints(out)
	cfg.Weekdays = out
}

func validateAutoGatherScheduleConfig(cfg domain.AutoGatherScheduleConfig, requireDays bool) error {
	if cfg.Hour < 0 || cfg.Hour > 23 {
		return fmt.Errorf("hour must be 0-23")
	}
	if cfg.Minute < 0 || cfg.Minute > 59 {
		return fmt.Errorf("minute must be 0-59")
	}
	if cfg.MaxShows <= 0 {
		return fmt.Errorf("maxShows must be greater than 0")
	}
	if cfg.MaxShows > autoGatherScheduleMaxShows {
		return fmt.Errorf("maxShows must be <= %d", autoGatherScheduleMaxShows)
	}
	if cfg.MaxBytes == 0 {
		return fmt.Errorf("maxBytes must be greater than 0")
	}
	for _, d := range cfg.Weekdays {
		if d < 0 || d > 6 {
			return fmt.Errorf("invalid weekday %d", d)
		}
	}
	if requireDays && len(cfg.Weekdays) == 0 {
		return fmt.Errorf("at least one weekday is required when the schedule is enabled")
	}
	return nil
}

func scheduleOccurrenceID(t time.Time) string {
	return t.Format("2006-01-02@15:04")
}

func scheduleOccurrenceDue(cfg domain.AutoGatherScheduleConfig, now time.Time) (string, bool) {
	if !cfg.Enabled {
		return "", false
	}
	if !weekdaySelected(cfg.Weekdays, now.Weekday()) {
		return "", false
	}
	if now.Hour() != cfg.Hour || now.Minute() != cfg.Minute {
		return "", false
	}
	return scheduleOccurrenceID(now), true
}

func weekdaySelected(days []int, day time.Weekday) bool {
	d := int(day)
	for _, x := range days {
		if x == d {
			return true
		}
	}
	return false
}

// nextScheduleOccurrence returns the next future (or still-due unattempted) occurrence.
// It never returns a past occurrence, so restarts do not catch up missed runs.
func nextScheduleOccurrence(cfg domain.AutoGatherScheduleConfig, from time.Time, lastAttempted string) (time.Time, bool) {
	if !cfg.Enabled || len(cfg.Weekdays) == 0 {
		return time.Time{}, false
	}
	local := from.In(time.Local)
	// Candidate for "today" at configured time.
	today := time.Date(local.Year(), local.Month(), local.Day(), cfg.Hour, cfg.Minute, 0, 0, local.Location())
	for dayOffset := 0; dayOffset <= 8; dayOffset++ {
		candidate := today.AddDate(0, 0, dayOffset)
		if !weekdaySelected(cfg.Weekdays, candidate.Weekday()) {
			continue
		}
		if candidate.After(local) {
			return candidate, true
		}
		// Still inside the scheduled minute and not yet attempted → due now.
		if dayOffset == 0 {
			if oid, due := scheduleOccurrenceDue(cfg, local); due && oid != lastAttempted {
				return candidate, true
			}
		}
	}
	return time.Time{}, false
}

func (c *Core) loadAutoGatherScheduleFile() (domain.AutoGatherScheduleFile, error) {
	path := c.scheduleFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return domain.AutoGatherScheduleFile{
				Version: autoGatherScheduleFileVersion,
				Config:  defaultAutoGatherScheduleConfig(),
			}, nil
		}
		return domain.AutoGatherScheduleFile{}, err
	}
	var doc domain.AutoGatherScheduleFile
	if err := json.Unmarshal(data, &doc); err != nil {
		return domain.AutoGatherScheduleFile{}, err
	}
	if doc.Version == 0 {
		doc.Version = autoGatherScheduleFileVersion
	}
	normalizeScheduleWeekdays(&doc.Config)
	if doc.Config.MaxShows == 0 && doc.Config.MaxBytes == 0 && !doc.Config.Enabled {
		// Empty/zero document → defaults (disabled).
		doc.Config = defaultAutoGatherScheduleConfig()
	}
	return doc, nil
}

func (c *Core) persistAutoGatherScheduleLocked() error {
	doc := domain.AutoGatherScheduleFile{
		Version: autoGatherScheduleFileVersion,
		Config:  c.scheduleConfig,
		State:   c.scheduleState,
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	path := c.scheduleFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
