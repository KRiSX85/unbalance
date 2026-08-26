export const WEEKDAY_OPTIONS = [
  { value: 1, label: 'Monday' },
  { value: 2, label: 'Tuesday' },
  { value: 3, label: 'Wednesday' },
  { value: 4, label: 'Thursday' },
  { value: 5, label: 'Friday' },
  { value: 6, label: 'Saturday' },
  { value: 0, label: 'Sunday' },
];

export const SCHEDULE_ENABLE_WARNING =
  'Enabling or changing Scheduled Auto Gather authorises unattended real Gather sessions. Scheduled runs may transfer files, remove successfully transferred source files, and occur without the browser being open. Global Dry Run remains an operator kill switch and is never changed by the scheduler.';

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
    if (result === 'completed' || result === 'stopped' || result === 'failed' || result === 'start_failed') {
      return `${base} — ${detail}`;
    }
    if (result === 'skipped_busy') {
      return `${base} (${detail})`;
    }
  }
  return base;
}
