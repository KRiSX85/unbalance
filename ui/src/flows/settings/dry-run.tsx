import React from 'react';

import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';

import { useConfigActions, useConfigDryRun } from '~/state/config';

const DISABLE_DRY_RUN_WARNING =
  'Turning Global Dry Run Off enables real Gather and Scatter transfers. Successfully transferred source files can be removed. Existing confirmation prompts for real Auto Gather moves still apply.';

export const DryRun: React.FunctionComponent = () => {
  const dryRun = useConfigDryRun();
  const { setDryRun } = useConfigActions();
  const [pendingOff, setPendingOff] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState('');

  const apply = async (next: boolean, confirm: boolean) => {
    setBusy(true);
    setError('');
    try {
      await setDryRun(next, confirm);
      setPendingOff(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Unable to update Global Dry Run');
    } finally {
      setBusy(false);
    }
  };

  const onModeChange = (value: string) => {
    const next = value === 'on';
    if (next === dryRun) {
      setPendingOff(false);
      setError('');
      return;
    }
    if (!next) {
      // Enabling real transfers requires an explicit second step.
      setPendingOff(true);
      setError('');
      return;
    }
    setPendingOff(false);
    void apply(true, false);
  };

  return (
    <div className="p-4 max-w-3xl space-y-4">
      <h1 className="text-xl font-bold">Global Dry Run</h1>
      <p className="text-sm text-gray-700 dark:text-gray-300">
        This setting is stored in the plugin configuration and takes effect
        immediately (no service restart). When On, unbalanced plans and previews
        transfers without moving or deleting files. When Off, real transfers are
        allowed subject to the normal safety confirmations.
      </p>

      <div
        className={
          dryRun
            ? 'rounded border border-lime-400 bg-lime-50 dark:bg-lime-950/30 dark:border-lime-700 p-3 text-sm'
            : 'rounded border border-orange-400 bg-orange-50 dark:bg-orange-950/30 dark:border-orange-700 p-3 text-sm'
        }
      >
        <div className="font-semibold">
          Current mode:{' '}
          {dryRun ? 'On — dry-run only (safe)' : 'Off — real transfers enabled'}
        </div>
        <div className="mt-1 text-xs opacity-90">
          {dryRun
            ? 'Auto Gather dry-run is available. Real one-show and controlled Auto Gather moves stay disabled.'
            : 'Real Gather/Scatter and Auto Gather moves can transfer files and remove successfully transferred sources after confirmation.'}
        </div>
      </div>

      <RadioGroup
        value={dryRun ? 'on' : 'off'}
        onValueChange={onModeChange}
        disabled={busy}
        className="space-y-2"
      >
        <div className="flex items-center space-x-2">
          <RadioGroupItem value="on" id="dry-run-on" />
          <Label htmlFor="dry-run-on">On</Label>
        </div>
        <div className="flex items-center space-x-2">
          <RadioGroupItem value="off" id="dry-run-off" />
          <Label htmlFor="dry-run-off">Off</Label>
        </div>
      </RadioGroup>

      {pendingOff && (
        <div className="rounded border border-red-400 bg-red-50 dark:bg-red-950/40 dark:border-red-700 p-3 space-y-3">
          <div className="font-semibold text-red-900 dark:text-red-100">
            Confirm disabling Global Dry Run
          </div>
          <p className="text-sm text-red-800 dark:text-red-200">
            {DISABLE_DRY_RUN_WARNING}
          </p>
          <div className="flex gap-2">
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => void apply(false, true)}
            >
              I understand — turn Global Dry Run Off
            </Button>
            <Button
              variant="secondary"
              disabled={busy}
              onClick={() => setPendingOff(false)}
            >
              Cancel
            </Button>
          </div>
        </div>
      )}

      {error !== '' && (
        <p className="text-sm text-red-900 dark:text-red-700">{error}</p>
      )}
    </div>
  );
};
