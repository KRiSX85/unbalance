import { describe, expect, it } from 'vitest';

import {
  formatScheduleDescription,
  formatScheduleResult,
  normalizeScheduleFrequency,
  SCHEDULE_ENABLE_WARNING,
} from '~/helpers/auto-gather-schedule-ui';

describe('schedule UI helpers', () => {
  it('formats skipped dry-run clearly', () => {
    expect(formatScheduleResult('skipped_dry_run')).toContain('Global Dry Run');
  });

  it('formats completed with detail', () => {
    expect(formatScheduleResult('completed', '2 shows, 1.5 GB moved')).toBe(
      'Completed — 2 shows, 1.5 GB moved',
    );
  });

  it('warns that scheduling is unattended and does not mutate DRY_RUN', () => {
    expect(SCHEDULE_ENABLE_WARNING.toLowerCase()).toContain('unattended');
    expect(SCHEDULE_ENABLE_WARNING.toLowerCase()).toContain('never changed');
  });

  it('defaults missing frequency to weekly', () => {
    expect(normalizeScheduleFrequency(undefined)).toBe('weekly');
    expect(normalizeScheduleFrequency('')).toBe('weekly');
    expect(normalizeScheduleFrequency('monthly')).toBe('monthly');
  });

  it('formats weekly schedule description with weekday controls implied', () => {
    expect(
      formatScheduleDescription({
        frequency: 'weekly',
        weekdays: [3],
        hour: 3,
        minute: 0,
      }),
    ).toBe('Weekly — Wednesday at 03:00 server local');
  });

  it('formats monthly schedule description with day-of-month', () => {
    expect(
      formatScheduleDescription({
        frequency: 'monthly',
        monthlyDay: 15,
        hour: 3,
        minute: 0,
      }),
    ).toBe('Monthly — day 15 at 03:00 server local');
  });

  it('treats frequency change on armed schedule as a material UI dirty field', () => {
    // Helper contract used by Settings: normalize before comparing dirty state.
    expect(normalizeScheduleFrequency('weekly')).not.toBe(
      normalizeScheduleFrequency('monthly'),
    );
  });
});
