import { describe, expect, it } from 'vitest';

import {
  formatScheduleResult,
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
});
