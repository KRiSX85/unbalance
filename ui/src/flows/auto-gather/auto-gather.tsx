import React from 'react';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useToast } from '@/components/ui/use-toast';

import {
  useConfigActions,
  useConfigTvLibraryPath,
} from '~/state/config';
import {
  useAutoGatherActions,
  useAutoGatherError,
  useAutoGatherResult,
  useAutoGatherScanning,
} from '~/state/auto-gather';
import { AutoGatherCanonicalPlanResult, AutoGatherShow } from '~/types';
import { humanBytes } from '~/helpers/units';
import { Icon } from '~/shared/icons/icon';
import { Api } from '~/api';

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

const ShowRow: React.FunctionComponent<{ show: AutoGatherShow }> = ({
  show,
}) => {
  const [expanded, setExpanded] = React.useState(false);
  const [verifying, setVerifying] = React.useState(false);
  const [canonical, setCanonical] =
    React.useState<AutoGatherCanonicalPlanResult | null>(null);
  const [canonicalError, setCanonicalError] = React.useState('');

  const videoDisks = show.videoDisks
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
        {show.cachePoolsWithVideo.length > 0 && (
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
  const { setTvLibraryPath } = useConfigActions();
  const { scan } = useAutoGatherActions();
  const scanning = useAutoGatherScanning();
  const result = useAutoGatherResult();
  const error = useAutoGatherError();
  const { toast } = useToast();

  const [pathValue, setPathValue] = React.useState(tvLibraryPath);
  const [filter, setFilter] = React.useState<ShowFilter>('attention');
  const [saving, setSaving] = React.useState(false);

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

  return (
    <div className="flex flex-col h-full bg-neutral-100 dark:bg-gray-950">
      <div className="p-4 border-b border-slate-200 dark:border-gray-800">
        <h1 className="text-lg text-slate-800 dark:text-gray-100">
          Auto Gather
        </h1>
        <p className="text-sm text-slate-500 dark:text-gray-500 mt-1">
          Read-only scan and destination recommendations. No planning tickets,
          transfers, or deletions in this stage.
        </p>

        <div className="flex flex-row flex-wrap items-center gap-2 mt-4">
          <span className="text-sm text-slate-500 dark:text-gray-500">
            /mnt/user/
          </span>
          <Input
            className="w-80"
            value={pathValue}
            onChange={(e) => setPathValue(e.target.value)}
            placeholder="data/media/tv"
          />
          <Button
            variant="secondary"
            onClick={() => void onSavePath()}
            disabled={saving}
          >
            {saving ? 'Saving…' : 'Save path'}
          </Button>
          <Button onClick={() => void onScan()} disabled={scanning}>
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
          <ShowRow key={show.path} show={show} />
        ))}
      </div>
    </div>
  );
};
