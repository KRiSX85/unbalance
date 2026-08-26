export const WEEKDAY_OPTIONS = [
  { value: 1, label: 'Monday' },
  { value: 2, label: 'Tuesday' },
  { value: 3, label: 'Wednesday' },
  { value: 4, label: 'Thursday' },
  { value: 5, label: 'Friday' },
  { value: 6, label: 'Saturday' },
  { value: 0, label: 'Sunday' },
];

export type ScheduleFrequency = 'weekly' | 'monthly';

export const SCHEDULE_ENABLE_WARNING =
  'Enabling or changing Scheduled Auto Gather authorises unattended real Gather sessions. Scheduled runs may transfer files, remove successfully transferred source files, and occur without the browser being open. Global Dry Run remains an operator kill switch and is never changed by the scheduler.';

const pad2 = (n: number) => String(n).padStart(2, '0');

export function normalizeScheduleFrequency(
  frequency?: string | null,
): ScheduleFrequency {
  return frequency === 'monthly' ? 'monthly' : 'weekly';
}

export function formatScheduleDescription(opts: {
  frequency?: string | null;
  weekdays?: number[] | null;
  monthlyDay?: number | null;
  hour: number;
  minute: number;
}): string {
  const time = `${pad2(opts.hour)}:${pad2(opts.minute)} server local`;
  if (normalizeScheduleFrequency(opts.frequency) === 'monthly') {
    const day = opts.monthlyDay && opts.monthlyDay >= 1 ? opts.monthlyDay : '?';
    return `Monthly — day ${day} at ${time}`;
  }
  const days = [...(opts.weekdays || [])]
    .map((v) => WEEKDAY_OPTIONS.find((d) => d.value === v)?.label)
    .filter(Boolean);
  const dayLabel = days.length > 0 ? days.join(', ') : 'no days selected';
  return `Weekly — ${dayLabel} at ${time}`;
}

export function formatScheduleResult(
  result?: string | null,
  detail?: string | null,
): string {
  if (!result) {
    return '—';
  }
  const labels: Record<string, string> = {
    skipped_dry_run: 'Skipped — Global Dry Run enabled',
    skipped_busy: 'Skipped — another operation was active',
    skipped_interrupted: 'Skipped — interrupted session requires acknowledgement',
    skipped_disabled: 'Skipped — scheduler disabled',
    started: 'Started',
    completed: 'Completed',
    failed: 'Failed',
    stopped: 'Stopped — operator requested Stop',
    start_failed: 'Failed to start',
  };
  const base = labels[result] || result;
  if (detail && detail.trim() !== '') {
    if (
      result === 'completed' ||
      result === 'stopped' ||
      result === 'failed' ||
      result === 'start_failed'
    ) {
      return `${base} — ${detail}`;
    }
    if (result === 'skipped_busy') {
      return `${base} (${detail})`;
    }
  }
  return base;
}
