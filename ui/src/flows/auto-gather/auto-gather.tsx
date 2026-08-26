import React from 'react';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useToast } from '@/components/ui/use-toast';

import {
  useConfigActions,
  useConfigDryRun,
  useConfigTvLibraryPath,
} from '~/state/config';
import {
  useAutoGatherActions,
  useAutoGatherError,
  useAutoGatherResult,
  useAutoGatherScanning,
} from '~/state/auto-gather';
import { AutoGatherCanonicalPlanResult, AutoGatherControlledState, AutoGatherDryRunState, AutoGatherRealState, AutoGatherShow } from '~/types';
import { bytesFromDecimalGB, formatByteBoundAsGB, humanBytes } from '~/helpers/units';
import {
  AUTO_GATHER_REAL_GLOBAL_DRY_RUN_MESSAGE,
  AUTO_GATHER_REAL_GLOBAL_DRY_RUN_OFF_MESSAGE,
  autoGatherRealNonExecutableExplanation,
  autoGatherRealPermissionWarningMessage,
  canCancelAutoGatherRealPrepare,
  canConfirmAutoGatherRealMove,
  isAutoGatherControlledActivePhase,
  isAutoGatherControlledInterruptedPhase,
  canAcknowledgeAutoGatherControlled,
  canResetAutoGatherControlled,
  shouldShowControlledStartControls,
  isAutoGatherRealExpiredPhase,
  isAutoGatherRealExecutionPhase,
  isAutoGatherStopEnabled,
  controlledLibraryRevision,
  shouldApplyControlledLibraryScan,
  shouldPollAutoGatherRealStatus,
  shouldShowAutoGatherRealConfirmPanel,
} from '~/helpers/auto-gather-ui';
import { Icon } from '~/shared/icons/icon';
import { Api } from '~/api';
import { useUnraidActions } from '~/state/unraid';

type ShowFilter =
  | 'attention'
  | 'split'
  | 'waiting_for_mover'
  | 'consolidated'
  | 'no_video'
  | 'all';

const statusLabel = (status: string) => {
  switch (status) {
    case 'split':
      return 'Split';
    case 'consolidated':
      return 'Consolidated';
    case 'waiting_for_mover':
      return 'Waiting for Mover';
    case 'no_video':
      return 'No video';
    default:
      return status;
  }
};

const statusClass = (status: string) => {
  switch (status) {
    case 'split':
      return 'text-amber-700 dark:text-amber-400';
    case 'waiting_for_mover':
      return 'text-sky-700 dark:text-sky-400';
    case 'consolidated':
      return 'text-green-700 dark:text-green-400';
    default:
      return 'text-slate-500 dark:text-gray-500';
  }
};

const matchesFilter = (show: AutoGatherShow, filter: ShowFilter) => {
  switch (filter) {
    case 'attention':
      return (
        show.status === 'split' || show.status === 'waiting_for_mover'
      );
    case 'split':
      return show.status === 'split';
    case 'waiting_for_mover':
      return show.status === 'waiting_for_mover';
    case 'consolidated':
      return show.status === 'consolidated';
    case 'no_video':
      return show.status === 'no_video';
    case 'all':
    default:
      return true;
  }
};

const diskNames = (
  disks: { diskName: string }[] | string[] | undefined,
): string => {
  if (!disks || disks.length === 0) {
    return 'none';
  }
  if (typeof disks[0] === 'string') {
    return (disks as string[]).join(', ');
  }
  return (disks as { diskName: string }[])
    .map((d) => d.diskName)
    .join(', ');
};

const ShowRow: React.FunctionComponent<{
  show: AutoGatherShow;
  realBusy: boolean;
  dryRunActive: boolean;
  opsBlocked?: boolean;
  onPrepareReal: (show: AutoGatherShow) => void;
  preparingReal: boolean;
}> = ({ show, realBusy, dryRunActive, opsBlocked, onPrepareReal, preparingReal }) => {
  const [expanded, setExpanded] = React.useState(false);
  const [verifying, setVerifying] = React.useState(false);
  const [canonical, setCanonical] =
    React.useState<AutoGatherCanonicalPlanResult | null>(null);
  const [canonicalError, setCanonicalError] = React.useState('');

  const videoDisks = (show.videoDisks ?? [])
    .filter((disk) => disk.videoBytes > 0 || disk.videoCount > 0)
    .map(
      (disk) =>
        `${disk.diskName} (${humanBytes(disk.videoBytes)}${
          disk.videoBytes === 0 && disk.videoCount > 0 ? ', 0-byte' : ''
        })`,
    )
    .join(', ');

  const currentOnRecommended = show.gatherTargets?.find(
    (t) => t.diskName === show.recommendedTargetDisk,
  )?.currentShowBytesOnTarget;

  const cleanupDisks = show.cleanupCandidateDisks ?? [];

  const verifyWithGather = async () => {
    setVerifying(true);
    setCanonicalError('');
    try {
      const result = await Api.planAutoGatherCanonical({
        showPath: show.path,
        stage2RecommendedTarget: show.recommendedTargetDisk,
        stage2EstimatedMoveBytes: show.moveRequiredBytes,
      });
      setCanonical(result);
      if (result.error) {
        setCanonicalError(result.error);
      }
    } catch (e) {
      setCanonical(null);
      setCanonicalError(
        e instanceof Error ? e.message : 'Canonical planning failed',
      );
    } finally {
      setVerifying(false);
    }
  };

  return (
    <div className="border-b border-slate-200 dark:border-gray-800 py-3 px-2">
      <div className="flex flex-row items-start justify-between gap-4">
        <div className="min-w-0 flex-1">
          <div className="font-medium text-slate-800 dark:text-gray-100 truncate">
            {show.name}
          </div>
          <div className="text-sm text-slate-500 dark:text-gray-500 truncate">
            /mnt/user/{show.path}
          </div>
        </div>
        <div className="text-right shrink-0">
          <div className={`text-sm font-medium ${statusClass(show.status)}`}>
            {statusLabel(show.status)}
          </div>
          <div className="text-sm text-slate-600 dark:text-gray-400">
            {humanBytes(show.totalVideoBytes)} video
          </div>
        </div>
      </div>

      <div className="mt-2 grid grid-cols-1 md:grid-cols-2 gap-2 text-sm text-slate-600 dark:text-gray-400">
        <div>
          <span className="text-slate-500 dark:text-gray-500">Video disks: </span>
          {videoDisks || 'none'}
        </div>
        <div>
          <span className="text-slate-500 dark:text-gray-500">
            Sidecar-only:{' '}
          </span>
          {diskNames(show.sidecarOnlyDisks)}
        </div>
        <div>
          <span className="text-slate-500 dark:text-gray-500">
            Empty-folder-only:{' '}
          </span>
          {diskNames(show.emptyOnlyDisks)}
        </div>
        {show.cachePoolsWithVideo && show.cachePoolsWithVideo.length > 0 && (
          <div>
            <span className="text-slate-500 dark:text-gray-500">
              Cache video:{' '}
            </span>
            {show.cachePoolsWithVideo.join(', ')}
          </div>
        )}
      </div>

      {cleanupDisks.length > 0 && (
        <div className="mt-2 text-sm text-slate-600 dark:text-gray-400">
          Empty folder cleanup: {cleanupDisks.join(', ')}
        </div>
      )}

      {show.status === 'waiting_for_mover' && (
        <div className="mt-2 text-sm text-sky-700 dark:text-sky-300">
          Recommendation deferred until cache video is moved.
        </div>
      )}

      {show.status === 'split' && (
        <div className="mt-3">
          {show.recommendedTargetDisk ? (
            <>
              <div className="text-sm text-slate-700 dark:text-slate-200">
                Recommended:{' '}
                <span className="font-medium">{show.recommendedTargetDisk}</span>
              </div>
              <div className="text-sm text-slate-600 dark:text-gray-400">
                Estimated move: {humanBytes(show.moveRequiredBytes || 0)}
              </div>
              <div className="text-sm text-slate-600 dark:text-gray-400">
                Projected free:{' '}
                {humanBytes(show.projectedFreeBytes || 0)} (
                {(show.projectedFreePercent || 0).toFixed(1)}%)
              </div>
              <div className="text-sm text-slate-600 dark:text-gray-400">
                Current show data on target:{' '}
                {humanBytes(currentOnRecommended || 0)}
              </div>
              {show.belowPreferredFreeFloor && (
                <div className="text-sm text-amber-700 dark:text-amber-300">
                  Below the preferred 10% free-space floor; no eligible target
                  would remain at or above 10% free.
                </div>
              )}
              {show.minMovementAlternative && (
                <div className="text-sm text-slate-600 dark:text-gray-400">
                  Minimum-movement alternative:{' '}
                  {show.minMovementAlternative.diskName} —{' '}
                  {humanBytes(show.minMovementAlternative.moveRequiredBytes)}{' '}
                  move, {humanBytes(show.minMovementAlternative.projectedFreeBytes)}{' '}
                  projected free (
                  {show.minMovementAlternative.projectedFreePercent.toFixed(1)}%)
                </div>
              )}
              <button
                type="button"
                className="mt-2 text-xs text-slate-600 dark:text-gray-400 underline"
                onClick={() => setExpanded((v) => !v)}
                disabled={(show.gatherTargets?.length || 0) === 0}
              >
                {expanded ? 'Hide target candidates' : 'Show target candidates'}
              </button>
              {expanded && (show.gatherTargets?.length || 0) > 0 && (
                <div className="mt-2 space-y-2">
                  {show.gatherTargets!.map((t) => {
                    const isRecommended =
                      t.diskName === show.recommendedTargetDisk;
                    const isMinMovement =
                      !!show.minMovementAlternative &&
                      t.diskName === show.minMovementAlternative.diskName;
                    return (
                    <div
                      key={t.diskName}
                      className="border border-slate-200 dark:border-gray-800 rounded px-2 py-2"
                    >
                      <div className="font-medium text-slate-800 dark:text-gray-100">
                        Target: {t.diskName}{' '}
                        <span
                          className={
                            t.eligible
                              ? 'text-green-700 dark:text-green-400'
                              : 'text-amber-700 dark:text-amber-400'
                          }
                        >
                          ({t.eligible ? 'eligible' : 'not eligible'})
                        </span>
                        {isRecommended && (
                          <span className="ml-2 text-xs font-semibold text-sky-700 dark:text-sky-300">
                            Recommended
                          </span>
                        )}
                        {isMinMovement && (
                          <span className="ml-2 text-xs font-semibold text-slate-600 dark:text-gray-400">
                            Minimum movement
                          </span>
                        )}
                      </div>
                      <div className="text-sm text-slate-600 dark:text-gray-400">
                        Estimated all-file bytes to move:{' '}
                        {humanBytes(t.moveRequiredBytes)}; Current all-file on
                        target: {humanBytes(t.currentShowBytesOnTarget)}
                      </div>
                      <div className="text-sm text-slate-600 dark:text-gray-400">
                        Free: {humanBytes(t.freeBytes)}; Projected free:{' '}
                        {t.eligible
                          ? `${humanBytes(t.projectedFreeBytes)} (${t.projectedFreePercent.toFixed(1)}%)`
                          : 'n/a'}
                      </div>
                      {!t.eligible && t.ineligibleReason && (
                        <div className="text-sm text-amber-700 dark:text-amber-300">
                          {t.ineligibleReason}
                        </div>
                      )}
                    </div>
                    );
                  })}
                </div>
              )}

              <div className="mt-3 border-t border-slate-200 dark:border-gray-800 pt-3">
                <div className="text-sm font-medium text-red-800 dark:text-red-300">
                  Real Gather — ONE SHOW (Stage 3C)
                </div>
                <div className="text-xs text-red-700 dark:text-red-400 mt-1">
                  Prepare inspects the move only. Confirmation is required before
                  any files are transferred or source data removed.
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="mt-2 border-red-300 dark:border-red-700"
                  disabled={preparingReal || realBusy || dryRunActive || !!opsBlocked}
                  onClick={() => onPrepareReal(show)}
                >
                  {preparingReal ? 'Preparing…' : 'Prepare real Gather'}
                </Button>
              </div>

              <div className="mt-3 border-t border-slate-200 dark:border-gray-800 pt-3">
                <div className="text-sm font-medium text-slate-700 dark:text-slate-200">
                  Canonical Gather verification (read-only)
                </div>
                <div className="text-xs text-slate-500 dark:text-gray-500 mt-1">
                  Uses the real Gather planner for this show only. Estimated
                  figures above remain Stage 2 advisory values. No move is
                  started.
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="mt-2"
                  disabled={verifying}
                  onClick={() => void verifyWithGather()}
                >
                  {verifying ? 'Planning…' : 'Verify with Gather'}
                </Button>
                {canonicalError && (
                  <div className="mt-2 text-sm text-amber-700 dark:text-amber-300">
                    {canonicalError}
                  </div>
                )}
                {canonical && !canonical.error && (
                  <div className="mt-2 space-y-1 text-sm text-slate-600 dark:text-gray-400">
                    <div>
                      Stage 2 target still valid:{' '}
                      <span className="font-medium text-slate-800 dark:text-gray-100">
                        {canonical.stage2TargetStillCanonicalEligible
                          ? 'Yes'
                          : 'No'}
                      </span>
                    </div>
                    <div>
                      Canonical target:{' '}
                      <span className="font-medium text-slate-800 dark:text-gray-100">
                        {canonical.canonicalRecommendedTarget || 'none'}
                      </span>
                      {canonical.belowPreferredFreeFloor && (
                        <span className="ml-2 text-amber-700 dark:text-amber-300">
                          (below 10% preferred floor)
                        </span>
                      )}
                    </div>
                    <div>
                      Canonical move:{' '}
                      {humanBytes(canonical.canonicalMoveBytes || 0)}
                      {canonical.canonicalProjectedFreePercent != null && (
                        <>
                          {'; projected free '}
                          {(canonical.canonicalProjectedFreePercent || 0).toFixed(
                            1,
                          )}
                          %
                        </>
                      )}
                    </div>
                    <div>
                      Stage 2 estimated move:{' '}
                      {humanBytes(canonical.stage2EstimatedMoveBytes || 0)}
                    </div>
                    {canonical.noEligibleReason && (
                      <div className="text-amber-700 dark:text-amber-300">
                        {canonical.noEligibleReason}
                      </div>
                    )}
                    <div className="text-xs text-slate-500 dark:text-gray-500">
                      Eligible physical targets:{' '}
                      {(canonical.canonicalTargets || [])
                        .filter((t) => t.canonicalEligible)
                        .map(
                          (t) =>
                            `${t.diskName} (${humanBytes(t.canonicalBytesToMove)})`,
                        )
                        .join(', ') || 'none'}
                    </div>
                  </div>
                )}
              </div>
            </>
          ) : (
            <div className="mt-2 space-y-2">
              <div className="text-sm text-amber-700 dark:text-amber-300">
                No eligible physical array target found.
                {show.noEligibleReason ? ` ${show.noEligibleReason}` : ''}
              </div>
              <div className="text-sm font-medium text-slate-700 dark:text-slate-200">
                Canonical Gather verification (read-only)
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={verifying}
                onClick={() => void verifyWithGather()}
              >
                {verifying ? 'Planning…' : 'Verify with Gather'}
              </Button>
              {canonicalError && (
                <div className="text-sm text-amber-700 dark:text-amber-300">
                  {canonicalError}
                </div>
              )}
              {canonical && !canonical.error && (
                <div className="text-sm text-slate-600 dark:text-gray-400">
                  Canonical target:{' '}
                  {canonical.canonicalRecommendedTarget || 'none'}; move{' '}
                  {humanBytes(canonical.canonicalMoveBytes || 0)}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export const AutoGather: React.FunctionComponent = () => {
  const tvLibraryPath = useConfigTvLibraryPath();
  const globalDryRun = useConfigDryRun();
  const { setTvLibraryPath } = useConfigActions();
  const { scan, applyResult } = useAutoGatherActions();
  const scanning = useAutoGatherScanning();
  const result = useAutoGatherResult();
  const error = useAutoGatherError();
  const { toast } = useToast();
  const { syncAutoGatherDryRun, syncAutoGatherReal } = useUnraidActions();

  const [pathValue, setPathValue] = React.useState(tvLibraryPath);
  const [filter, setFilter] = React.useState<ShowFilter>('attention');
  const [saving, setSaving] = React.useState(false);
  const [dryRunState, setDryRunState] = React.useState<AutoGatherDryRunState | null>(null);
  const [dryRunBusy, setDryRunBusy] = React.useState(false);
  const [realState, setRealState] = React.useState<AutoGatherRealState | null>(null);
  const [realBusy, setRealBusy] = React.useState(false);
  const [preparingShowPath, setPreparingShowPath] = React.useState('');
  const [controlledState, setControlledState] = React.useState<AutoGatherControlledState | null>(null);
  const [controlledBusy, setControlledBusy] = React.useState(false);
  const [controlledMaxShows, setControlledMaxShows] = React.useState(3);
  const [controlledMaxGB, setControlledMaxGB] = React.useState(50);

  const refreshRealStatus = React.useCallback(async () => {
    try {
      const status = await Api.getAutoGatherRealStatus();
      setRealState(status);
    } catch {
      // ignore transient poll errors
    }
  }, []);

  const refreshDryRunStatus = React.useCallback(async () => {
    try {
      const status = await Api.getAutoGatherDryRunStatus();
      setDryRunState(status);
    } catch {
      // ignore transient poll errors
    }
  }, []);

  const appliedLibraryRevision = React.useRef(0);

  const applyControlledStatus = React.useCallback(
    (status: AutoGatherControlledState) => {
      setControlledState(status);
      const revision = controlledLibraryRevision(status);
      if (revision === 0 || revision === appliedLibraryRevision.current) {
        return;
      }
      appliedLibraryRevision.current = revision;
      void (async () => {
        try {
          const snapshot = await Api.getAutoGatherLibrary();
          if (!shouldApplyControlledLibraryScan(snapshot)) {
            return;
          }
          applyResult(snapshot);
        } catch {
          // presentation-only; keep the existing scan store
        }
      })();
    },
    [applyResult],
  );

  const refreshControlledStatus = React.useCallback(async () => {
    try {
      const status = await Api.getAutoGatherControlledStatus();
      applyControlledStatus(status);
    } catch {
      // ignore transient poll errors
    }
  }, [applyControlledStatus]);

  React.useEffect(() => {
    void refreshDryRunStatus();
    void refreshRealStatus();
    void refreshControlledStatus();
  }, [refreshDryRunStatus, refreshRealStatus]);

  React.useEffect(() => {
    const phase = dryRunState?.phase;
    if (phase !== 'running' && phase !== 'stopping') {
      return;
    }
    const id = window.setInterval(() => {
      void refreshDryRunStatus();
    }, 2000);
    return () => window.clearInterval(id);
  }, [dryRunState?.phase, refreshDryRunStatus]);

  React.useEffect(() => {
    const phase = realState?.phase;
    if (!shouldPollAutoGatherRealStatus(phase)) {
      return;
    }
    const id = window.setInterval(() => {
      void refreshRealStatus();
    }, 2000);
    return () => window.clearInterval(id);
  }, [realState?.phase, refreshRealStatus]);

  React.useEffect(() => {
    const phase = controlledState?.phase;
    if (!isAutoGatherControlledActivePhase(phase) && !isAutoGatherControlledInterruptedPhase(phase)) {
      return;
    }
    const id = window.setInterval(() => {
      void refreshControlledStatus();
    }, 2000);
    return () => window.clearInterval(id);
  }, [controlledState?.phase, refreshControlledStatus]);

  const onStartControlled = async () => {
    setControlledBusy(true);
    try {
      const state = await Api.startAutoGatherControlled({
        confirm: true,
        maxShows: controlledMaxShows,
        maxBytes: bytesFromDecimalGB(controlledMaxGB),
        trigger: 'manual',
      });
      applyControlledStatus(state);
      toast({
        title: 'Controlled Real Auto Gather started',
        description: `Up to ${controlledMaxShows} shows / ${controlledMaxGB} GB. This is NOT a dry run.`,
      });
    } catch (e) {
      toast({
        title: e instanceof Error ? e.message : 'Unable to start controlled Auto Gather',
        variant: 'destructive',
      });
      await refreshControlledStatus();
    } finally {
      setControlledBusy(false);
    }
  };

  const onStopControlled = async () => {
    setControlledBusy(true);
    try {
      const state = await Api.stopAutoGatherControlled();
      applyControlledStatus(state);
      toast({ title: 'Controlled Auto Gather stop requested' });
    } catch (e) {
      toast({
        title: e instanceof Error ? e.message : 'Unable to stop',
        variant: 'destructive',
      });
    } finally {
      setControlledBusy(false);
    }
  };

  const onAcknowledgeControlled = async () => {
    setControlledBusy(true);
    try {
      const state = await Api.acknowledgeAutoGatherControlled({ confirm: true });
      applyControlledStatus(state);
      toast({
        title: 'Interrupted session acknowledged',
        description: 'The previous session was not resumed. You may start a new operation after verifying the array.',
      });
    } catch (e) {
      toast({
        title: e instanceof Error ? e.message : 'Unable to acknowledge interrupted session',
        variant: 'destructive',
      });
    } finally {
      setControlledBusy(false);
    }
  };

  const onResetControlled = async () => {
    setControlledBusy(true);
    try {
      const state = await Api.resetAutoGatherControlled({ confirm: true });
      applyControlledStatus(state);
      toast({
        title: 'Ready for a new controlled session',
        description: 'Set Max shows / Max GB, then Start with a fresh confirmation. Nothing has been started.',
      });
    } catch (e) {
      toast({
        title: e instanceof Error ? e.message : 'Unable to clear finished session',
        variant: 'destructive',
      });
      await refreshControlledStatus();
    } finally {
      setControlledBusy(false);
    }
  };

  const onStartDryRun = async () => {
    setDryRunBusy(true);
    try {
      const state = await Api.startAutoGatherDryRun();
      setDryRunState(state);
      if (state.error) {
        toast({ title: state.error, variant: 'destructive' });
      } else {
        syncAutoGatherDryRun(true);
        toast({
          title: 'Dry-run Auto Gather started',
          description: 'No files will be transferred or deleted.',
        });
      }
    } catch (e) {
      toast({
        title: e instanceof Error ? e.message : 'Unable to start dry-run',
        variant: 'destructive',
      });
    } finally {
      setDryRunBusy(false);
    }
  };

  const onStopDryRun = async () => {
    setDryRunBusy(true);
    try {
      const state = await Api.stopAutoGatherDryRun();
      setDryRunState(state);
      toast({ title: 'Dry-run Auto Gather stop requested' });
    } catch (e) {
      toast({
        title: e instanceof Error ? e.message : 'Unable to stop dry-run',
        variant: 'destructive',
      });
    } finally {
      setDryRunBusy(false);
    }
  };

  const onPrepareReal = async (show: AutoGatherShow) => {
    setPreparingShowPath(show.path);
    setRealBusy(true);
    try {
      const prep = await Api.prepareAutoGatherReal({
        showPath: show.path,
        stage2RecommendedTarget: show.recommendedTargetDisk,
        stage2EstimatedMoveBytes: show.moveRequiredBytes,
      });
      if (prep.error) {
        toast({ title: prep.error, variant: 'destructive' });
        return;
      }
      setRealState({
        phase: 'prepared',
        globalDryRun: prep.globalDryRun,
        currentShow: prep.showPath,
        currentShowName: prep.showName,
        currentTarget: prep.canonicalTargetDisk,
        preparationId: prep.preparationId,
        prepared: prep,
      });
      await refreshRealStatus();
    } catch (e) {
      toast({
        title: e instanceof Error ? e.message : 'Unable to prepare real move',
        variant: 'destructive',
      });
    } finally {
      setPreparingShowPath('');
      setRealBusy(false);
    }
  };

  const onConfirmRealExecute = async () => {
    if (!realState?.prepared?.preparationId) {
      return;
    }
    setRealBusy(true);
    try {
      const state = await Api.executeAutoGatherReal({
        preparationId: realState.prepared.preparationId,
        showPath: realState.prepared.showPath,
        confirm: true,
      });
      setRealState(state);
      syncAutoGatherReal(true);
      toast({
        title: 'Real Gather started',
        description: 'This is NOT a dry run. Only the selected show will be moved.',
      });
    } catch (e) {
      toast({
        title: e instanceof Error ? e.message : 'Unable to execute real move',
        variant: 'destructive',
      });
      await refreshRealStatus();
    } finally {
      setRealBusy(false);
    }
  };

  const onCancelPrepared = async () => {
    setRealBusy(true);
    try {
      const state = await Api.cancelAutoGatherRealPrepare();
      setRealState(state);
      toast({ title: 'Preparation cancelled' });
    } catch (e) {
      toast({
        title: e instanceof Error ? e.message : 'Unable to cancel preparation',
        variant: 'destructive',
      });
      await refreshRealStatus();
    } finally {
      setRealBusy(false);
    }
  };

  const onStopReal = async () => {
    setRealBusy(true);
    try {
      const state = await Api.stopAutoGatherReal();
      setRealState(state);
      toast({ title: 'Real Gather stop requested' });
    } catch (e) {
      toast({
        title: e instanceof Error ? e.message : 'Unable to stop real move',
        variant: 'destructive',
      });
    } finally {
      setRealBusy(false);
    }
  };

  React.useEffect(() => {
    setPathValue(tvLibraryPath);
  }, [tvLibraryPath]);

  const onSavePath = async () => {
    setSaving(true);
    try {
      const normalized = await setTvLibraryPath(pathValue.trim());
      setPathValue(normalized);
      toast({ title: 'TV library path saved', description: normalized });
    } catch (e) {
      toast({
        title: e instanceof Error ? e.message : 'Unable to save path',
        variant: 'destructive',
      });
    } finally {
      setSaving(false);
    }
  };

  const onScan = async () => {
    await scan();
  };

  const shows = result?.shows ?? [];
  const filtered = shows.filter((show) => matchesFilter(show, filter));
  const splitCount = shows.filter((show) => show.status === 'split').length;
  const waitingCount = shows.filter(
    (show) => show.status === 'waiting_for_mover',
  ).length;
  const recommendationsAvailable = shows.filter(
    (show) =>
      show.status === 'split' && Boolean(show.recommendedTargetDisk),
  ).length;
  const noEligibleCount = splitCount - recommendationsAvailable;
  const sumIndependentMoves = shows.reduce((acc, show) => {
    if (show.status !== 'split' || !show.recommendedTargetDisk) {
      return acc;
    }
    return acc + (show.moveRequiredBytes || 0);
  }, 0);
  const showsWithCleanup = shows.filter(
    (show) => (show.cleanupCandidateCount || 0) > 0,
  ).length;
  const emptyFolderLocations = shows.reduce(
    (acc, show) => acc + (show.cleanupCandidateCount || 0),
    0,
  );
  const warnings = result?.warnings ?? [];
  const dryRunPhase = dryRunState?.phase ?? 'idle';
  const dryRunActive = dryRunPhase === 'running' || dryRunPhase === 'stopping';
  const realPhase = realState?.phase ?? 'idle';
  const realExecutionActive = isAutoGatherRealExecutionPhase(realPhase);
  const controlledInterrupted = isAutoGatherControlledInterruptedPhase(controlledState?.phase);
  const stopEnabled = isAutoGatherStopEnabled(dryRunPhase, realPhase);
  const showConfirmPanel = shouldShowAutoGatherRealConfirmPanel(realState);
  const preparedMove = showConfirmPanel ? realState?.prepared ?? null : null;
  const confirmEnabled = canConfirmAutoGatherRealMove({
    globalDryRun: globalDryRun || !!realState?.globalDryRun,
    busy: realBusy || controlledInterrupted,
    executionActive: realExecutionActive,
    prepared: preparedMove,
  });
  const cancelEnabled = canCancelAutoGatherRealPrepare(realState);
  const nonExecutableExplanation =
    autoGatherRealNonExecutableExplanation(preparedMove);
  const permissionWarningMessage =
    autoGatherRealPermissionWarningMessage(preparedMove?.permissionWarnings);

  React.useEffect(() => {
    syncAutoGatherDryRun(dryRunActive);
  }, [dryRunActive, syncAutoGatherDryRun]);

  React.useEffect(() => {
    syncAutoGatherReal(realExecutionActive);
  }, [realExecutionActive, syncAutoGatherReal]);

  return (
    <div className="flex flex-col h-full bg-neutral-100 dark:bg-gray-950">
      <div className="p-4 border-b border-slate-200 dark:border-gray-800">
        <h1 className="text-lg text-slate-800 dark:text-gray-100">
          Auto Gather
        </h1>
        <p className="text-sm text-slate-500 dark:text-gray-500 mt-1">
          Scan your TV library, review destination recommendations, and run
          dry-run or confirmed real Gather moves. Global Dry Run is controlled
          in Settings.
        </p>

        <div
          className={
            globalDryRun
              ? 'mt-3 rounded border border-lime-400 bg-lime-50 dark:bg-lime-950/30 dark:border-lime-700 px-3 py-2 text-sm text-lime-900 dark:text-lime-100'
              : 'mt-3 rounded border border-orange-400 bg-orange-50 dark:bg-orange-950/30 dark:border-orange-700 px-3 py-2 text-sm text-orange-900 dark:text-orange-100'
          }
        >
          <span className="font-semibold">
            Global Dry Run: {globalDryRun ? 'On' : 'Off'}
          </span>
          <span className="mx-2">—</span>
          {globalDryRun
            ? AUTO_GATHER_REAL_GLOBAL_DRY_RUN_MESSAGE
            : AUTO_GATHER_REAL_GLOBAL_DRY_RUN_OFF_MESSAGE}
        </div>

        <div className="mt-4 rounded border border-amber-300 dark:border-amber-700 bg-amber-50 dark:bg-amber-950/40 p-3">
          <div className="text-sm font-semibold text-amber-900 dark:text-amber-200">
            Dry-run Auto Gather (Stage 3B — not a real move)
          </div>
          <p className="text-xs text-amber-800 dark:text-amber-300 mt-1">
            One Stage 1 library scan at start, then each iteration refreshes
            Unraid disk state, re-scores Stage 2 from the retained inventory,
            canonical-plans only the selected show, and invokes Gather with{' '}
            <code>--dry-run</code>. Dry-run does not change disk free space or
            show placement, so free-space adaptation cannot be proven until
            real moves.
          </p>
          <div className="flex flex-wrap gap-2 mt-3">
            <Button
              variant="secondary"
              disabled={!globalDryRun || dryRunBusy || dryRunActive || controlledInterrupted}
              onClick={() => void onStartDryRun()}
            >
              Start dry run
            </Button>
            <Button
              variant="outline"
              disabled={!stopEnabled}
              onClick={() => void onStopDryRun()}
            >
              Stop
            </Button>
          </div>
          {!globalDryRun && (
            <p className="text-xs text-amber-800 dark:text-amber-300 mt-2">
              Enable global dry-run mode before starting Stage 3B.
            </p>
          )}
          <div className="mt-3 text-sm text-amber-900 dark:text-amber-100 space-y-1">
            <div>
              Status: <span className="font-medium">{dryRunPhase}</span>
            </div>
            {(dryRunState?.currentShowName || dryRunState?.currentShow) && (
              <div>
                Current show:{' '}
                {dryRunState?.currentShowName || dryRunState?.currentShow}
                {dryRunState?.currentTarget
                  ? ` → ${dryRunState.currentTarget}`
                  : ''}
              </div>
            )}
            <div>
              Completed: {dryRunState?.completed?.length ?? 0}; Skipped:{' '}
              {dryRunState?.skipped?.length ?? 0}
              {typeof dryRunState?.splitRemaining === 'number' &&
                `; Split remaining: ${dryRunState.splitRemaining}`}
            </div>
            {dryRunState?.failureReason && (
              <div className="text-amber-800 dark:text-amber-300">
                Failure
                {dryRunState.failedShowName
                  ? ` (${dryRunState.failedShowName})`
                  : ''}
                : {dryRunState.failureReason}
              </div>
            )}
            {dryRunState?.message && (
              <div className="text-xs text-amber-800 dark:text-amber-300">
                {dryRunState.message}
              </div>
            )}
          </div>
        </div>

        <div className="mt-4 rounded border border-red-400 dark:border-red-800 bg-red-50 dark:bg-red-950/40 p-3">
          <div className="text-sm font-semibold text-red-900 dark:text-red-200">
            Real Gather — ONE SHOW (Stage 3C)
          </div>
          <p className="text-xs text-red-800 dark:text-red-300 mt-1">
            This will move files and may remove successfully transferred source
            files. Only the selected show will be processed. This is NOT a dry
            run. Turn Global Dry Run Off in Settings before confirming a real
            move.
          </p>
          {globalDryRun && (
            <p className="text-xs text-red-800 dark:text-red-300 mt-2 font-medium">
              {AUTO_GATHER_REAL_GLOBAL_DRY_RUN_MESSAGE}
            </p>
          )}
          {preparedMove && showConfirmPanel && (
            <div className="mt-3 rounded border border-red-300 dark:border-red-700 bg-white/70 dark:bg-gray-950/50 p-3 space-y-2 text-sm">
              <div className="font-semibold text-red-900 dark:text-red-100">
                Confirm real move (not a dry run)
              </div>
              <div>Show: {preparedMove.showName || preparedMove.showPath}</div>
              <div>
                Source disks: {(preparedMove.sourceDisks || []).join(', ') || 'none'}
              </div>
              <div>Destination: {preparedMove.canonicalTargetDisk}</div>
              <div>
                Bytes to move: {humanBytes(preparedMove.estimatedMoveBytes || 0)}
              </div>
              <div>
                Projected target free:{' '}
                {humanBytes(preparedMove.projectedTargetFreeBytes || 0)}
              </div>
              {permissionWarningMessage && (
                <div className="font-medium text-amber-800 dark:text-amber-200">
                  {permissionWarningMessage}
                </div>
              )}
              {nonExecutableExplanation && (
                <div className="font-medium text-red-800 dark:text-red-200">
                  {nonExecutableExplanation}
                </div>
              )}
              {(preparedMove.emptyFolderOnlyDisks?.length || 0) > 0 && (
                <div className="text-xs">
                  Empty-folder-only disks:{' '}
                  {preparedMove.emptyFolderOnlyDisks!.join(', ')}
                </div>
              )}
              <div className="flex flex-wrap gap-2 pt-2">
                <Button
                  variant="destructive"
                  disabled={!confirmEnabled}
                  onClick={() => void onConfirmRealExecute()}
                >
                  Confirm real Gather
                </Button>
                <Button
                  variant="outline"
                  disabled={realExecutionActive}
                  onClick={() => void onCancelPrepared()}
                >
                  Cancel
                </Button>
              </div>
            </div>
          )}
          {isAutoGatherRealExpiredPhase(realPhase) && (
            <div className="mt-3 rounded border border-red-300 dark:border-red-700 bg-white/70 dark:bg-gray-950/50 p-3 space-y-2 text-sm">
              <div className="font-semibold text-red-900 dark:text-red-100">
                Preparation expired
              </div>
              <div>
                {realState?.error || 'preparation expired; prepare again'}
              </div>
              <Button
                variant="outline"
                disabled={!cancelEnabled || realExecutionActive}
                onClick={() => void onCancelPrepared()}
              >
                Cancel
              </Button>
            </div>
          )}
          <div className="flex flex-wrap gap-2 mt-3">
            <Button
              variant="outline"
              className="border-red-300 dark:border-red-700"
              disabled={!stopEnabled}
              onClick={() => void onStopReal()}
            >
              Stop real move
            </Button>
          </div>
          <div className="mt-3 text-sm text-red-900 dark:text-red-100 space-y-1">
            <div>
              Status: <span className="font-medium">{realPhase}</span>
              {realState?.operationPhase
                ? ` (${realState.operationPhase})`
                : ''}
            </div>
            {(realState?.currentShowName || realState?.currentShow) && (
              <div>
                Current show:{' '}
                {realState?.currentShowName || realState?.currentShow}
                {realState?.currentTarget
                  ? ` → ${realState.currentTarget}`
                  : ''}
              </div>
            )}
            {realState?.stoppedMessage && (
              <div>{realState.stoppedMessage}</div>
            )}
            {realState?.error && <div>{realState.error}</div>}
            {realState?.verification && realPhase !== 'prepared' && (
              <div>
                Verification: {realState.verification.message}
                {(realState.verification.substantiveDisks?.length || 0) > 0 && (
                  <> Remaining disks: {realState.verification.substantiveDisks!.join(', ')}</>
                )}
                {(realState.verification.emptyFolderRemnants?.length || 0) > 0 && (
                  <> Empty-folder remnants: {realState.verification.emptyFolderRemnants!.join(', ')}</>
                )}
              </div>
            )}
          </div>
        </div>

        {/* Stage 3D: Controlled Real Auto Gather */}
        {isAutoGatherControlledInterruptedPhase(controlledState?.phase) && (
          <div className="mt-4 rounded border border-red-400 dark:border-red-700 bg-red-50 dark:bg-red-950/30 p-3 space-y-2">
            <div className="font-semibold text-red-800 dark:text-red-200">
              Previous real Auto Gather session was interrupted
            </div>
            <div className="text-sm text-red-800 dark:text-red-200">
              {controlledState?.message}
            </div>
            {controlledState?.currentShowName && (
              <div className="text-sm">
                Last show: {controlledState.currentShowName}
                {controlledState.currentTarget ? ` → ${controlledState.currentTarget}` : ''}
              </div>
            )}
            {controlledState?.lastSourceEntry && (
              <div className="text-sm">Last source entry: {controlledState.lastSourceEntry}</div>
            )}
            {controlledState?.rsyncProbe && (
              <div className="text-sm">
                Recorded rsync PID: {controlledState.rsyncProbe.pid || 'none'}.{' '}
                {controlledState.rsyncProbe.note}
              </div>
            )}
            {canAcknowledgeAutoGatherControlled(controlledState) ? (
              <>
                <div className="text-xs text-red-700 dark:text-red-300">
                  Acknowledgement clears this warning only. It does not resume, retry, or kill any process.
                </div>
                <Button
                  variant="destructive"
                  size="sm"
                  disabled={controlledBusy}
                  onClick={() => void onAcknowledgeControlled()}
                >
                  I have verified the array — acknowledge interruption
                </Button>
              </>
            ) : (
              <div className="text-sm font-medium text-red-800 dark:text-red-200">
                Acknowledgement is unavailable while the recorded rsync still appears to be running.
                Wait for that process to finish or stop it yourself. Unbalanced will not kill it.
              </div>
            )}
          </div>
        )}
        {!globalDryRun && (
          <div className="mt-4 rounded border border-orange-300 dark:border-orange-700 bg-orange-50/50 dark:bg-orange-950/20 p-3 space-y-2">
            <div className="font-semibold text-orange-900 dark:text-orange-100">
              Controlled Real Auto Gather (Stage 3D)
              {controlledState?.trigger === 'scheduled' ? ' — Scheduled' : ''}
            </div>
            <div className="text-xs text-orange-800 dark:text-orange-200">
              Orchestrates multiple one-show real Gather moves sequentially.
              This is NOT a dry run — files will be moved and successfully transferred sources removed.
              {controlledState?.trigger === 'scheduled'
                ? ' This session was started by the scheduler.'
                : ''}
            </div>
            {shouldShowControlledStartControls(controlledState?.phase) && (
              <div className="flex flex-wrap items-center gap-2">
                <label className="text-sm">Max shows:</label>
                <Input
                  className="w-20"
                  type="number"
                  min={1}
                  max={50}
                  value={controlledMaxShows}
                  onChange={(e) => setControlledMaxShows(Math.max(1, parseInt(e.target.value) || 1))}
                />
                <label className="text-sm">Max GB:</label>
                <Input
                  className="w-24"
                  type="number"
                  min={1}
                  value={controlledMaxGB}
                  onChange={(e) => setControlledMaxGB(Math.max(1, parseInt(e.target.value) || 1))}
                />
                <Button
                  variant="destructive"
                  disabled={controlledBusy || realExecutionActive || dryRunActive}
                  onClick={() => void onStartControlled()}
                >
                  Start controlled real Auto Gather
                </Button>
              </div>
            )}
            {controlledState && controlledState.phase !== 'idle' && controlledState.phase !== 'interrupted' && (
              <div className="text-sm space-y-1">
                <div>
                  Phase: <span className="font-medium">{controlledState.phase}</span>
                  {controlledState.operationPhase ? ` (${controlledState.operationPhase})` : ''}
                </div>
                <div>
                  Bounds: {controlledState.maxShows} shows / {formatByteBoundAsGB(controlledState.maxBytes)}
                </div>
                <div>
                  Completed: {(controlledState.completed?.length || 0)} shows;{' '}
                  {humanBytes(controlledState.cumulativeBytes || 0)} moved
                </div>
                {(controlledState.completed?.length || 0) > 0 && (
                  <ul className="list-disc pl-5 text-xs">
                    {controlledState.completed!.map((item) => (
                      <li key={item.showPath}>
                        {item.showName || item.showPath}
                        {item.targetDisk ? ` → ${item.targetDisk}` : ''}
                        {item.moveBytes ? ` (${humanBytes(item.moveBytes)})` : ''}
                      </li>
                    ))}
                  </ul>
                )}
                {(controlledState.skipped?.length || 0) > 0 && (
                  <div>Skipped: {controlledState.skipped!.length}</div>
                )}
                {controlledState.currentShowName && (
                  <div>
                    Current: {controlledState.currentShowName}
                    {controlledState.currentTarget ? ` → ${controlledState.currentTarget}` : ''}
                  </div>
                )}
                {controlledState.failureReason && (
                  <div className="text-red-700 dark:text-red-300">
                    Failed: {controlledState.failedShowName || controlledState.failedShow} — {controlledState.failureReason}
                  </div>
                )}
                {controlledState.error && (
                  <div className="text-red-700 dark:text-red-300">{controlledState.error}</div>
                )}
                {isAutoGatherControlledActivePhase(controlledState.phase) && (
                  <Button
                    variant="destructive"
                    size="sm"
                    disabled={controlledBusy}
                    onClick={() => void onStopControlled()}
                  >
                    Stop controlled Auto Gather
                  </Button>
                )}
                {canResetAutoGatherControlled(controlledState) && (
                  <div className="pt-2 space-y-1">
                    <div className="text-xs text-orange-800 dark:text-orange-200">
                      Session finished. Start another requires a fresh confirmation; nothing starts automatically.
                    </div>
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={controlledBusy || realExecutionActive || dryRunActive}
                      onClick={() => void onResetControlled()}
                    >
                      New controlled session
                    </Button>
                  </div>
                )}
              </div>
            )}
          </div>
        )}

        <div className="flex flex-row flex-wrap items-center gap-2 mt-4">
          <span className="text-sm text-slate-500 dark:text-gray-500">
            /mnt/user/
          </span>
          <Input
            className="w-80"
            value={pathValue}
            onChange={(e) => setPathValue(e.target.value)}
            placeholder="data/media/tv"
            disabled={dryRunActive}
          />
          <Button
            variant="secondary"
            onClick={() => void onSavePath()}
            disabled={saving || dryRunActive}
          >
            {saving ? 'Saving…' : 'Save path'}
          </Button>
          <Button onClick={() => void onScan()} disabled={scanning || dryRunActive || realExecutionActive}>
            {scanning ? 'Scanning…' : 'Scan library'}
          </Button>
          {scanning && (
            <Icon
              name="loading"
              size={18}
              style="animate-spin fill-slate-500 dark:fill-gray-500"
            />
          )}
        </div>
        <p className="text-xs text-slate-500 dark:text-gray-500 mt-2">
          Scan uses the last saved path ({tvLibraryPath || 'data/media/tv'}).
          Save path before scanning if you change it.
        </p>
      </div>

      {(error || result?.error) && (
        <div className="px-4 py-2 text-sm text-red-600 dark:text-red-400">
          {error || result?.error}
        </div>
      )}

      {warnings.length > 0 && (
        <div className="px-4 py-2 text-sm text-amber-700 dark:text-amber-400">
          <div className="font-medium mb-1">
            {warnings.length} warning{warnings.length === 1 ? '' : 's'} during
            scan
          </div>
          <ul className="list-disc pl-5 space-y-0.5">
            {warnings.slice(0, 20).map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
            {warnings.length > 20 && (
              <li>…and {warnings.length - 20} more</li>
            )}
          </ul>
        </div>
      )}

      {result && (
        <div className="px-4 py-2 text-sm text-slate-500 dark:text-gray-500 flex flex-col gap-2">
          <div className="flex flex-row flex-wrap items-center gap-4">
            <span>Library: /mnt/user/{result.libraryPath}</span>
            <span>{shows.length} shows</span>
            <span>{splitCount} split</span>
            <span>{waitingCount} waiting for mover</span>
            <span>{recommendationsAvailable} recommendations</span>
            <span>{noEligibleCount} no-eligible</span>
            <span>{showsWithCleanup} shows with cleanup candidates</span>
            <span>{emptyFolderLocations} empty folder locations</span>
            <label className="flex flex-row items-center gap-2">
              <span>Filter</span>
              <select
                className="bg-white dark:bg-gray-900 border border-slate-300 dark:border-gray-700 rounded px-2 py-1"
                value={filter}
                onChange={(e) => setFilter(e.target.value as ShowFilter)}
              >
                <option value="attention">Split + Waiting for Mover</option>
                <option value="split">Split</option>
                <option value="waiting_for_mover">Waiting for Mover</option>
                <option value="consolidated">Consolidated</option>
                <option value="no_video">No video</option>
                <option value="all">All</option>
              </select>
            </label>
          </div>
          <div className="text-xs text-slate-500 dark:text-gray-500">
            Sum of independent recommended moves: {humanBytes(sumIndependentMoves)}.
            Recommendations do not reserve capacity against one another; each
            is calculated against current free space only. Move sizes are
            estimates and can differ slightly from a live Gather plan near a
            free-space boundary.
          </div>
        </div>
      )}

      <div className="flex-1 overflow-auto px-2">
        {!result && !scanning && (
          <div className="p-4 text-slate-500 dark:text-gray-500">
            Save a library path if needed, then scan to list shows that need
            attention.
          </div>
        )}
        {result && shows.length === 0 && !result.error && (
          <div className="p-4 text-slate-500 dark:text-gray-500">
            No show folders found under /mnt/user/{result.libraryPath}. Check
            that the saved path is correct and that shows are immediate child
            directories.
          </div>
        )}
        {result && shows.length > 0 && filtered.length === 0 && (
          <div className="p-4 text-slate-500 dark:text-gray-500">
            No shows match the current filter. Switch the filter to All to see
            every scanned show.
          </div>
        )}
        {filtered.map((show) => (
          <ShowRow
            key={show.path}
            show={show}
            realBusy={realBusy || realExecutionActive}
            dryRunActive={dryRunActive}
            opsBlocked={controlledInterrupted}
            onPrepareReal={(s) => void onPrepareReal(s)}
            preparingReal={preparingShowPath === show.path}
          />
        ))}
      </div>
    </div>
  );
};
