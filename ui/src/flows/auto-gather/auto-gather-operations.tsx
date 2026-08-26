import React from 'react';
import { NavLink } from 'react-router-dom';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

import {
  AUTO_GATHER_CONTROLLED_DESCRIPTION,
  AUTO_GATHER_DRY_RUN_CARD_DESCRIPTION,
  AUTO_GATHER_DRY_RUN_OFF_HINT,
  AUTO_GATHER_ONE_SHOW_HINT,
  AUTO_GATHER_PAGE_DESCRIPTION,
  AUTO_GATHER_REAL_GLOBAL_DRY_RUN_MESSAGE,
  canAcknowledgeAutoGatherControlled,
  canResetAutoGatherControlled,
  controlledResultLabel,
  controlledTriggerLabel,
  formatControlledRunTimestamp,
  isAutoGatherControlledInterruptedPhase,
  shouldShowControlledActivePanel,
  shouldShowControlledLastRunSummary,
  shouldShowControlledStartControls,
} from '~/helpers/auto-gather-ui';
import { formatByteBoundAsGB, humanBytes } from '~/helpers/units';
import {
  AutoGatherControlledState,
  AutoGatherDryRunState,
  AutoGatherRealPrepareResult,
  AutoGatherRealState,
} from '~/types';

const cardClass =
  'rounded-lg border border-slate-200 dark:border-gray-800 bg-white dark:bg-gray-900/40 p-3';

const TriggerBadge: React.FunctionComponent<{ trigger?: string | null }> = ({
  trigger,
}) => {
  const scheduled = trigger === 'scheduled';
  return (
    <span
      className={
        scheduled
          ? 'inline-flex items-center rounded-full border border-sky-300 dark:border-sky-600 bg-sky-50/70 dark:bg-sky-950/25 px-2 py-0.5 text-xs font-medium text-sky-800 dark:text-sky-300'
          : 'inline-flex items-center rounded-full border border-slate-300 dark:border-gray-600 px-2 py-0.5 text-xs font-medium text-slate-700 dark:text-gray-300'
      }
    >
      {controlledTriggerLabel(trigger)}
    </span>
  );
};

const ResultStatusBadge: React.FunctionComponent<{ phase?: string | null }> = ({
  phase,
}) => {
  const label = controlledResultLabel(phase);
  let className =
    'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ';
  switch (phase) {
    case 'completed':
      className +=
        'border-emerald-300 dark:border-emerald-700 bg-emerald-50/70 dark:bg-emerald-950/25 text-emerald-800 dark:text-emerald-300';
      break;
    case 'failed':
      className +=
        'border-red-300 dark:border-red-700 bg-red-50/70 dark:bg-red-950/25 text-red-800 dark:text-red-300';
      break;
    case 'stopped':
      className +=
        'border-slate-300 dark:border-gray-600 text-slate-700 dark:text-gray-300';
      break;
    default:
      className +=
        'border-slate-300 dark:border-gray-600 text-slate-600 dark:text-gray-400';
      break;
  }
  return <span className={className}>{label}</span>;
};

export const GlobalDryRunBanner: React.FunctionComponent<{
  globalDryRun: boolean;
}> = ({ globalDryRun }) => (
  <div
    className={
      globalDryRun
        ? 'rounded-md border border-emerald-300/80 dark:border-emerald-700/70 bg-white dark:bg-gray-900/40 px-3 py-2 text-sm flex flex-wrap items-center justify-between gap-2'
        : 'rounded-md border border-amber-300 dark:border-amber-700 bg-amber-50 dark:bg-amber-950/30 px-3 py-2 text-sm flex flex-wrap items-center justify-between gap-2'
    }
  >
    <div className="text-slate-800 dark:text-gray-100">
      {globalDryRun ? (
        <span className="font-medium text-emerald-800 dark:text-emerald-300">
          Global Dry Run: On
        </span>
      ) : (
        <span className="font-medium text-amber-900 dark:text-amber-200">
          Global Dry Run: Off
        </span>
      )}
      <span className="text-slate-600 dark:text-gray-400">
        {' '}
        —{' '}
        {globalDryRun
          ? 'Safe mode — real moves disabled'
          : 'Real moves enabled after confirmation'}
      </span>
    </div>
    <NavLink
      to="/settings/dry-run"
      className="text-sm font-medium text-sky-700 dark:text-sky-400 hover:underline shrink-0"
    >
      Settings
    </NavLink>
  </div>
);

export const DryRunAutoGatherCard: React.FunctionComponent<{
  globalDryRun: boolean;
  dryRunState: AutoGatherDryRunState | null;
  dryRunPhase: string;
  dryRunBusy: boolean;
  dryRunActive: boolean;
  controlledInterrupted: boolean;
  stopEnabled: boolean;
  onStart: () => void;
  onStop: () => void;
}> = ({
  globalDryRun,
  dryRunState,
  dryRunPhase,
  dryRunBusy,
  dryRunActive,
  controlledInterrupted,
  stopEnabled,
  onStart,
  onStop,
}) => (
  <div className={cardClass}>
    <div className="font-medium text-slate-800 dark:text-gray-100">Dry-run Auto Gather</div>
    <p className="text-xs text-slate-600 dark:text-gray-400 mt-1">
      {AUTO_GATHER_DRY_RUN_CARD_DESCRIPTION}
    </p>
    <div className="flex flex-wrap gap-2 mt-2">
      <Button
        variant="secondary"
        size="sm"
        disabled={!globalDryRun || dryRunBusy || dryRunActive || controlledInterrupted}
        onClick={onStart}
      >
        Start dry run
      </Button>
      <Button variant="outline" size="sm" disabled={!stopEnabled} onClick={onStop}>
        Stop
      </Button>
    </div>
    {!globalDryRun && (
      <p className="text-xs text-amber-800 dark:text-amber-300 mt-2">
        {AUTO_GATHER_DRY_RUN_OFF_HINT}
      </p>
    )}
    {(dryRunActive || (dryRunPhase && dryRunPhase !== 'idle')) && (
      <div className="mt-2 text-xs text-slate-600 dark:text-gray-400 space-y-0.5">
        <div>
          Status: <span className="font-medium">{dryRunPhase}</span>
        </div>
        {(dryRunState?.currentShowName || dryRunState?.currentShow) && (
          <div>
            Current: {dryRunState?.currentShowName || dryRunState?.currentShow}
            {dryRunState?.currentTarget ? ` → ${dryRunState.currentTarget}` : ''}
          </div>
        )}
        <div>
          Completed: {dryRunState?.completed?.length ?? 0}; Skipped:{' '}
          {dryRunState?.skipped?.length ?? 0}
        </div>
        {dryRunState?.failureReason && (
          <div className="text-red-700 dark:text-red-300">
            {dryRunState.failureReason}
          </div>
        )}
      </div>
    )}
  </div>
);

export const OneShowGatherCard: React.FunctionComponent<{
  globalDryRun: boolean;
  showConfirmPanel: boolean;
  preparedMove: AutoGatherRealPrepareResult | null;
  realState: AutoGatherRealState | null;
  realPhase: string;
  realExecutionActive: boolean;
  isExpired: boolean;
  confirmEnabled: boolean;
  cancelEnabled: boolean;
  stopEnabled: boolean;
  permissionWarningMessage: string | null;
  nonExecutableExplanation: string | null;
  onConfirm: () => void;
  onCancel: () => void;
  onStop: () => void;
}> = ({
  globalDryRun,
  showConfirmPanel,
  preparedMove,
  realState,
  realPhase,
  realExecutionActive,
  isExpired,
  confirmEnabled,
  cancelEnabled,
  stopEnabled,
  permissionWarningMessage,
  nonExecutableExplanation,
  onConfirm,
  onCancel,
  onStop,
}) => {
  const showActive =
    showConfirmPanel || realExecutionActive || isExpired || (realPhase && realPhase !== 'idle');

  return (
    <div className={cardClass}>
      <div className="font-medium text-slate-800 dark:text-gray-100">One-show Gather</div>
      {!showActive && (
        <p className="text-xs text-slate-600 dark:text-gray-400 mt-1">
          {AUTO_GATHER_ONE_SHOW_HINT}
        </p>
      )}
      {globalDryRun && showConfirmPanel && (
        <p className="text-xs text-amber-800 dark:text-amber-300 mt-2">
          {AUTO_GATHER_REAL_GLOBAL_DRY_RUN_MESSAGE}
        </p>
      )}
      {showConfirmPanel && preparedMove && (
        <div className="mt-2 rounded border border-red-300 dark:border-red-800 bg-white/90 dark:bg-gray-950/50 p-2 text-xs space-y-1">
          <div className="font-semibold text-red-900 dark:text-red-100">
            Confirm real move
          </div>
          <div>{preparedMove.showName || preparedMove.showPath}</div>
          <div>Destination: {preparedMove.canonicalTargetDisk}</div>
          <div>Bytes to move: {humanBytes(preparedMove.estimatedMoveBytes || 0)}</div>
          {permissionWarningMessage && (
            <div className="text-amber-800 dark:text-amber-200">{permissionWarningMessage}</div>
          )}
          {nonExecutableExplanation && (
            <div className="text-red-800 dark:text-red-200">{nonExecutableExplanation}</div>
          )}
          <div className="flex flex-wrap gap-2 pt-1">
            <Button variant="destructive" size="sm" disabled={!confirmEnabled} onClick={onConfirm}>
              Confirm Gather
            </Button>
            <Button variant="outline" size="sm" disabled={realExecutionActive} onClick={onCancel}>
              Cancel
            </Button>
          </div>
        </div>
      )}
      {isExpired && (
        <div className="mt-2 rounded border border-amber-300 dark:border-amber-700 bg-amber-50/80 dark:bg-amber-950/20 p-2 text-xs">
          <div className="font-medium">Preparation expired</div>
          <div>{realState?.error || 'Prepare again from a show below.'}</div>
          <Button
            variant="outline"
            size="sm"
            className="mt-2"
            disabled={!cancelEnabled || realExecutionActive}
            onClick={onCancel}
          >
            Dismiss
          </Button>
        </div>
      )}
      {realExecutionActive && (
        <div className="mt-2 text-xs text-slate-600 dark:text-gray-400 space-y-0.5">
          <div>
            Status: <span className="font-medium">{realPhase}</span>
          </div>
          {(realState?.currentShowName || realState?.currentShow) && (
            <div>
              Current: {realState?.currentShowName || realState?.currentShow}
              {realState?.currentTarget ? ` → ${realState.currentTarget}` : ''}
            </div>
          )}
          <Button variant="outline" size="sm" className="mt-1" disabled={!stopEnabled} onClick={onStop}>
            Stop move
          </Button>
        </div>
      )}
    </div>
  );
};

export const ControlledLastRunSummary: React.FunctionComponent<{
  state: AutoGatherControlledState;
  controlledBusy: boolean;
  realExecutionActive: boolean;
  dryRunActive: boolean;
  onReset: () => void;
}> = ({ state, controlledBusy, realExecutionActive, dryRunActive, onReset }) => {
  const [expanded, setExpanded] = React.useState(false);
  const completed = state.completed || [];

  return (
    <div className={`${cardClass} border-slate-200 dark:border-gray-700`}>
      <div className="flex flex-wrap items-center gap-2">
        <div className="font-medium text-slate-800 dark:text-gray-100">Last Auto Gather Run</div>
        <TriggerBadge trigger={state.trigger} />
        <ResultStatusBadge phase={state.phase} />
      </div>
      {state.startedAt && (
        <div className="text-xs text-slate-500 dark:text-gray-500 mt-1">
          {formatControlledRunTimestamp(state.startedAt)}
        </div>
      )}
      <div className="text-sm text-slate-700 dark:text-gray-300 mt-1">
        {completed.length} show{completed.length === 1 ? '' : 's'} ·{' '}
        {humanBytes(state.cumulativeBytes || 0)} moved
      </div>
      {state.failureReason && (
        <div className="text-sm text-red-700 dark:text-red-300 mt-1">
          {state.failedShowName || state.failedShow}: {state.failureReason}
        </div>
      )}
      {completed.length > 0 && (
        <>
          <button
            type="button"
            className="text-xs text-sky-700 dark:text-sky-400 underline mt-2"
            onClick={() => setExpanded((v) => !v)}
          >
            {expanded ? 'Hide show list' : 'Show list'}
          </button>
          {expanded && (
            <ul className="mt-1 text-xs text-slate-600 dark:text-gray-400 space-y-0.5 list-none pl-0">
              {completed.map((item) => (
                <li key={item.showPath}>
                  {item.showName || item.showPath}
                  {item.targetDisk ? ` → ${item.targetDisk}` : ''}
                  {item.moveBytes ? ` · ${humanBytes(item.moveBytes)}` : ''}
                </li>
              ))}
            </ul>
          )}
        </>
      )}
      {canResetAutoGatherControlled(state) && (
        <Button
          variant="secondary"
          size="sm"
          className="mt-3"
          disabled={controlledBusy || realExecutionActive || dryRunActive}
          onClick={onReset}
        >
          Start another controlled run
        </Button>
      )}
    </div>
  );
};

export const ControlledAutoGatherCard: React.FunctionComponent<{
  controlledState: AutoGatherControlledState | null;
  controlledBusy: boolean;
  controlledMaxShows: number;
  controlledMaxGB: number;
  realExecutionActive: boolean;
  dryRunActive: boolean;
  onMaxShowsChange: (n: number) => void;
  onMaxGBChange: (n: number) => void;
  onStart: () => void;
  onStop: () => void;
  onReset: () => void;
}> = ({
  controlledState,
  controlledBusy,
  controlledMaxShows,
  controlledMaxGB,
  realExecutionActive,
  dryRunActive,
  onMaxShowsChange,
  onMaxGBChange,
  onStart,
  onStop,
  onReset,
}) => {
  if (
    controlledState &&
    shouldShowControlledLastRunSummary(controlledState) &&
    !shouldShowControlledActivePanel(controlledState.phase)
  ) {
    return (
      <ControlledLastRunSummary
        state={controlledState}
        controlledBusy={controlledBusy}
        realExecutionActive={realExecutionActive}
        dryRunActive={dryRunActive}
        onReset={onReset}
      />
    );
  }

  return (
    <div className={cardClass}>
      <div className="flex flex-wrap items-center gap-2">
        <div className="font-medium text-slate-800 dark:text-gray-100">
          Controlled Auto Gather
        </div>
        {controlledState?.trigger && (
          <TriggerBadge trigger={controlledState.trigger} />
        )}
      </div>
      <p className="text-xs text-slate-600 dark:text-gray-400 mt-1">
        {AUTO_GATHER_CONTROLLED_DESCRIPTION}
      </p>
      {shouldShowControlledStartControls(controlledState?.phase) && (
        <div className="flex flex-wrap items-center gap-2 mt-2">
          <label className="text-xs">Max shows</label>
          <Input
            className="w-16 h-8 text-sm"
            type="number"
            min={1}
            max={50}
            value={controlledMaxShows}
            onChange={(e) => onMaxShowsChange(Math.max(1, parseInt(e.target.value) || 1))}
          />
          <label className="text-xs">Max GB</label>
          <Input
            className="w-16 h-8 text-sm"
            type="number"
            min={1}
            value={controlledMaxGB}
            onChange={(e) => onMaxGBChange(Math.max(1, parseInt(e.target.value) || 1))}
          />
          <Button
            variant="default"
            size="sm"
            disabled={controlledBusy || realExecutionActive || dryRunActive}
            onClick={onStart}
          >
            Start Controlled Auto Gather
          </Button>
        </div>
      )}
      {controlledState &&
        shouldShowControlledActivePanel(controlledState.phase) && (
          <div className="mt-2 text-xs text-slate-600 dark:text-gray-400 space-y-1">
            <div>
              Status:{' '}
              <ResultStatusBadge phase={controlledState.phase} />
            </div>
            <div>
              Bounds: {controlledState.maxShows} shows /{' '}
              {formatByteBoundAsGB(controlledState.maxBytes)}
            </div>
            <div>
              Completed: {controlledState.completed?.length ?? 0} ·{' '}
              {humanBytes(controlledState.cumulativeBytes || 0)} moved
            </div>
            {controlledState.currentShowName && (
              <div>
                Current: {controlledState.currentShowName}
                {controlledState.currentTarget
                  ? ` → ${controlledState.currentTarget}`
                  : ''}
              </div>
            )}
            <Button
              variant="outline"
              size="sm"
              disabled={controlledBusy}
              onClick={onStop}
            >
              Stop
            </Button>
          </div>
        )}
    </div>
  );
};

export const ControlledInterruptedBanner: React.FunctionComponent<{
  controlledState: AutoGatherControlledState;
  controlledBusy: boolean;
  onAcknowledge: () => void;
}> = ({ controlledState, controlledBusy, onAcknowledge }) => (
  <div className="rounded-lg border border-red-400 dark:border-red-700 bg-red-50 dark:bg-red-950/30 p-3 space-y-2">
    <div className="font-semibold text-red-800 dark:text-red-200">
      Previous Auto Gather session was interrupted
    </div>
    <div className="text-sm text-red-800 dark:text-red-200">
      {controlledState.message}
    </div>
    {controlledState.currentShowName && (
      <div className="text-sm">
        Last show: {controlledState.currentShowName}
        {controlledState.currentTarget ? ` → ${controlledState.currentTarget}` : ''}
      </div>
    )}
    {canAcknowledgeAutoGatherControlled(controlledState) ? (
      <>
        <div className="text-xs text-red-700 dark:text-red-300">
          Acknowledgement clears this warning only. It does not resume or retry the session.
        </div>
        <Button
          variant="destructive"
          size="sm"
          disabled={controlledBusy}
          onClick={onAcknowledge}
        >
          Acknowledge interruption
        </Button>
      </>
    ) : (
      <div className="text-sm font-medium text-red-800 dark:text-red-200">
        Wait for the recorded rsync process to finish before acknowledging.
      </div>
    )}
  </div>
);

export const AutoGatherPageHeader: React.FunctionComponent<
  React.PropsWithChildren<{ globalDryRun: boolean }>
> = ({ globalDryRun, children }) => (
  <div className="p-4 border-b border-slate-200 dark:border-gray-800 space-y-3">
    <div>
      <h1 className="text-lg font-semibold text-slate-800 dark:text-gray-100">Auto Gather</h1>
      <p className="text-sm text-slate-500 dark:text-gray-500 mt-0.5">
        {AUTO_GATHER_PAGE_DESCRIPTION}
      </p>
    </div>
    <GlobalDryRunBanner globalDryRun={globalDryRun} />
    {children}
  </div>
);

export { isAutoGatherControlledInterruptedPhase };
