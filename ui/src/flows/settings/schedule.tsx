import React from 'react';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';

import { Api } from '~/api';
import { AutoGatherScheduleStatus } from '~/types';
import { bytesFromDecimalGB, formatByteBoundAsGB } from '~/helpers/units';
import {
  formatScheduleResult,
  SCHEDULE_ENABLE_WARNING,
  WEEKDAY_OPTIONS,
} from '~/helpers/auto-gather-schedule-ui';

const pad2 = (n: number) => String(n).padStart(2, '0');

export const Schedule: React.FunctionComponent = () => {
  const [status, setStatus] = React.useState<AutoGatherScheduleStatus | null>(null);
  const [enabled, setEnabled] = React.useState(false);
  const [hour, setHour] = React.useState(3);
  const [minute, setMinute] = React.useState(0);
  const [weekdays, setWeekdays] = React.useState<number[]>([]);
  const [maxShows, setMaxShows] = React.useState(1);
  const [maxGB, setMaxGB] = React.useState(10);
  const [pendingEnable, setPendingEnable] = React.useState(false);
  const [pendingSave, setPendingSave] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState('');
  const [loaded, setLoaded] = React.useState(false);

  const refresh = React.useCallback(async () => {
    const next = await Api.getAutoGatherSchedule();
    setStatus(next);
    setEnabled(!!next.config?.enabled);
    setHour(next.config?.hour ?? 3);
    setMinute(next.config?.minute ?? 0);
    setWeekdays([...(next.config?.weekdays || [])]);
    setMaxShows(next.config?.maxShows || 1);
    const bytes = next.config?.maxBytes || bytesFromDecimalGB(10);
    setMaxGB(Math.max(1, Math.round(bytes / 1_000_000_000)));
    setLoaded(true);
  }, []);

  React.useEffect(() => {
    void refresh().catch((e) =>
      setError(e instanceof Error ? e.message : 'Unable to load schedule'),
    );
  }, [refresh]);

  const dirty =
    loaded &&
    status != null &&
    (enabled !== !!status.config.enabled ||
      hour !== status.config.hour ||
      minute !== status.config.minute ||
      maxShows !== status.config.maxShows ||
      bytesFromDecimalGB(maxGB) !== status.config.maxBytes ||
      JSON.stringify([...weekdays].sort()) !==
        JSON.stringify([...(status.config.weekdays || [])].sort()));

  const apply = async (confirm: boolean) => {
    setBusy(true);
    setError('');
    try {
      const next = await Api.setAutoGatherSchedule({
        enabled,
        hour,
        minute,
        weekdays,
        maxShows,
        maxBytes: bytesFromDecimalGB(maxGB),
        confirm,
      });
      setStatus(next);
      setPendingEnable(false);
      setPendingSave(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Unable to save schedule');
    } finally {
      setBusy(false);
    }
  };

  const onEnabledChange = (value: string) => {
    const next = value === 'on';
    setEnabled(next);
    setError('');
    if (next && !status?.config.enabled) {
      setPendingEnable(true);
      setPendingSave(false);
      return;
    }
    setPendingEnable(false);
    if (!next && status?.config.enabled) {
      // Disable immediately without destructive confirm.
      setBusy(true);
      void Api.setAutoGatherSchedule({
        enabled: false,
        hour,
        minute,
        weekdays,
        maxShows,
        maxBytes: bytesFromDecimalGB(maxGB),
        confirm: false,
      })
        .then((s) => {
          setStatus(s);
          setEnabled(false);
        })
        .catch((e) => setError(e instanceof Error ? e.message : 'Unable to disable'))
        .finally(() => setBusy(false));
    }
  };

  const toggleDay = (day: number) => {
    setWeekdays((prev) =>
      prev.includes(day) ? prev.filter((d) => d !== day) : [...prev, day].sort(),
    );
  };

  const onSaveClick = () => {
    if (enabled && status?.config.enabled && dirty) {
      setPendingSave(true);
      return;
    }
    if (enabled && !status?.config.enabled) {
      setPendingEnable(true);
      return;
    }
    void apply(false);
  };

  if (!loaded && !error) {
    return <div className="p-4 text-sm">Loading schedule…</div>;
  }

  return (
    <div className="p-4 max-w-3xl space-y-4">
      <h1 className="text-xl font-bold">Scheduled Auto Gather</h1>
      <p className="text-sm text-gray-700 dark:text-gray-300">
        Automatically starts the existing controlled real Auto Gather (Stage 3D)
        at configured server-local times. Times use the Unraid server clock — not
        your browser timezone.
      </p>

      {status && (
        <div className="rounded border border-slate-300 dark:border-slate-700 bg-slate-50 dark:bg-slate-900/40 p-3 text-sm space-y-1">
          <div>
            <span className="font-semibold">Server timezone:</span>{' '}
            {status.timezone || 'Local'} (offset {status.timezoneOffsetMinutes} min)
          </div>
          <div>
            <span className="font-semibold">Server local time:</span>{' '}
            {status.serverLocalTime}
          </div>
          <div>
            <span className="font-semibold">Schedule:</span>{' '}
            {status.enabled ? 'Enabled' : 'Disabled'}
            {status.enabled
              ? ` — ${pad2(status.config.hour)}:${pad2(status.config.minute)} server local`
              : ''}
          </div>
          <div>
            <span className="font-semibold">Bounds:</span>{' '}
            {status.config.maxShows} shows / {formatByteBoundAsGB(status.config.maxBytes)}
          </div>
          <div>
            <span className="font-semibold">Next run:</span>{' '}
            {status.nextOccurrence || '—'}
          </div>
          <div>
            <span className="font-semibold">Last attempt:</span>{' '}
            {status.lastAttemptedOccurrence || '—'}
          </div>
          <div>
            <span className="font-semibold">Last result:</span>{' '}
            {formatScheduleResult(status.lastResult, status.lastResultDetail)}
          </div>
          {status.configError && (
            <div className="text-red-700 dark:text-red-300">{status.configError}</div>
          )}
          {status.globalDryRun && (
            <div className="text-amber-800 dark:text-amber-200">
              Global Dry Run is On — scheduled real runs will be skipped until it is turned Off.
            </div>
          )}
        </div>
      )}

      <div>
        <h2 className="font-semibold mb-2">Enable Scheduled Auto Gather</h2>
        <RadioGroup
          value={enabled ? 'on' : 'off'}
          onValueChange={onEnabledChange}
          disabled={busy}
        >
          <div className="flex items-center space-x-2">
            <RadioGroupItem value="on" id="sched-on" />
            <Label htmlFor="sched-on">On</Label>
          </div>
          <div className="flex items-center space-x-2">
            <RadioGroupItem value="off" id="sched-off" />
            <Label htmlFor="sched-off">Off</Label>
          </div>
        </RadioGroup>
      </div>

      <div className="flex flex-wrap gap-4 items-end">
        <div>
          <Label htmlFor="sched-hour">Hour (0–23, server local)</Label>
          <Input
            id="sched-hour"
            className="w-24 mt-1"
            type="number"
            min={0}
            max={23}
            value={hour}
            disabled={busy}
            onChange={(e) => setHour(Math.min(23, Math.max(0, parseInt(e.target.value) || 0)))}
          />
        </div>
        <div>
          <Label htmlFor="sched-minute">Minute (0–59)</Label>
          <Input
            id="sched-minute"
            className="w-24 mt-1"
            type="number"
            min={0}
            max={59}
            value={minute}
            disabled={busy}
            onChange={(e) => setMinute(Math.min(59, Math.max(0, parseInt(e.target.value) || 0)))}
          />
        </div>
        <div className="text-sm text-slate-600 dark:text-slate-300 pb-2">
          Configured time: {pad2(hour)}:{pad2(minute)} <strong>server local</strong>
        </div>
      </div>

      <div>
        <h2 className="font-semibold mb-2">Days of week</h2>
        <div className="flex flex-wrap gap-3">
          {WEEKDAY_OPTIONS.map((day) => (
            <label key={day.value} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={weekdays.includes(day.value)}
                disabled={busy}
                onChange={() => toggleDay(day.value)}
              />
              {day.label}
            </label>
          ))}
        </div>
      </div>

      <div className="flex flex-wrap gap-4">
        <div>
          <Label htmlFor="sched-shows">Max shows</Label>
          <Input
            id="sched-shows"
            className="w-24 mt-1"
            type="number"
            min={1}
            max={50}
            value={maxShows}
            disabled={busy}
            onChange={(e) => setMaxShows(Math.max(1, parseInt(e.target.value) || 1))}
          />
        </div>
        <div>
          <Label htmlFor="sched-gb">Max GB</Label>
          <Input
            id="sched-gb"
            className="w-24 mt-1"
            type="number"
            min={1}
            value={maxGB}
            disabled={busy}
            onChange={(e) => setMaxGB(Math.max(1, parseInt(e.target.value) || 1))}
          />
        </div>
      </div>

      {(pendingEnable || pendingSave) && (
        <div className="rounded border border-red-400 bg-red-50 dark:bg-red-950/40 dark:border-red-700 p-3 space-y-3">
          <div className="font-semibold text-red-900 dark:text-red-100">
            {pendingEnable
              ? 'Confirm enabling Scheduled Auto Gather'
              : 'Confirm changing armed schedule bounds/time'}
          </div>
          <p className="text-sm text-red-800 dark:text-red-200">{SCHEDULE_ENABLE_WARNING}</p>
          <div className="flex gap-2">
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => void apply(true)}
            >
              I understand — save schedule
            </Button>
            <Button
              variant="secondary"
              disabled={busy}
              onClick={() => {
                setPendingEnable(false);
                setPendingSave(false);
                void refresh();
              }}
            >
              Cancel
            </Button>
          </div>
        </div>
      )}

      {!pendingEnable && !pendingSave && (
        <Button disabled={busy || (!dirty && enabled === !!status?.config.enabled)} onClick={onSaveClick}>
          Save schedule
        </Button>
      )}

      {error !== '' && (
        <p className="text-sm text-red-900 dark:text-red-700">{error}</p>
      )}
    </div>
  );
};
