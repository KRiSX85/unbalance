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
import { AutoGatherShow } from '~/types';
import { humanBytes } from '~/helpers/units';
import { Icon } from '~/shared/icons/icon';

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

const ShowRow: React.FunctionComponent<{ show: AutoGatherShow }> = ({
  show,
}) => {
  const videoDisks = show.videoDisks
    .filter((disk) => disk.videoBytes > 0 || disk.videoCount > 0)
    .map(
      (disk) =>
        `${disk.diskName} (${humanBytes(disk.videoBytes)}${
          disk.videoBytes === 0 && disk.videoCount > 0 ? ', 0-byte' : ''
        })`,
    )
    .join(', ');

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
          {show.sidecarOnlyDisks.length > 0
            ? show.sidecarOnlyDisks.join(', ')
            : 'none'}
        </div>
        <div>
          <span className="text-slate-500 dark:text-gray-500">
            Empty-folder-only:{' '}
          </span>
          {show.emptyOnlyDisks.length > 0
            ? show.emptyOnlyDisks.join(', ')
            : 'none'}
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
  const warnings = result?.warnings ?? [];

  return (
    <div className="flex flex-col h-full bg-neutral-100 dark:bg-gray-950">
      <div className="p-4 border-b border-slate-200 dark:border-gray-800">
        <h1 className="text-lg text-slate-800 dark:text-gray-100">
          Auto Gather
        </h1>
        <p className="text-sm text-slate-500 dark:text-gray-500 mt-1">
          Read-only scan of immediate show folders under your saved TV library
          path. No planning, transfers, or deletions in this stage.
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
        <div className="px-4 py-2 text-sm text-slate-500 dark:text-gray-500 flex flex-row flex-wrap items-center gap-4">
          <span>Library: /mnt/user/{result.libraryPath}</span>
          <span>{shows.length} shows</span>
          <span>{splitCount} split</span>
          <span>{waitingCount} waiting for mover</span>
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
