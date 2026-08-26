import { Op, AutoGatherRealPermissionWarnings, AutoGatherRealPrepareResult, AutoGatherRealState, AutoGatherScanResult, AutoGatherShow } from '~/types';
import { canonicalAppPath, getRouteFromStatus } from '~/helpers/routes';

export function isAutoGatherDryRunActivePhase(phase?: string | null): boolean {
  return phase === 'running' || phase === 'stopping';
}

export function isAutoGatherRealActivePhase(phase?: string | null): boolean {
  return (
    phase === 'preparing' ||
    phase === 'prepared' ||
    phase === 'executing' ||
    phase === 'stopping'
  );
}

export function isAutoGatherRealExecutionPhase(phase?: string | null): boolean {
  return phase === 'executing' || phase === 'stopping';
}

export function isAutoGatherRealExpiredPhase(phase?: string | null): boolean {
  return phase === 'expired';
}

export function isAutoGatherRealTerminalPhase(phase?: string | null): boolean {
  return (
    phase === 'stopped' ||
    phase === 'completed' ||
    phase === 'failed' ||
    phase === 'verification_warning' ||
    phase === 'expired' ||
    phase === 'idle' ||
    !phase
  );
}

export function isAutoGatherStopEnabled(
  dryRunPhase?: string | null,
  realPhase?: string | null,
): boolean {
  return (
    isAutoGatherDryRunActivePhase(dryRunPhase) ||
    isAutoGatherRealExecutionPhase(realPhase)
  );
}

export function isAutoGatherTerminalPhase(phase?: string | null): boolean {
  return (
    phase === 'stopped' ||
    phase === 'completed' ||
    phase === 'failed' ||
    phase === 'idle' ||
    !phase
  );
}

export const AUTO_GATHER_REAL_GLOBAL_DRY_RUN_MESSAGE =
  'Global Dry Run is On — real Auto Gather moves are disabled. Turn it Off in Settings → Global Dry Run to enable real transfers (with confirmation).';

export const AUTO_GATHER_REAL_GLOBAL_DRY_RUN_OFF_MESSAGE =
  'Global Dry Run is Off — real transfers are enabled after the usual confirmations. Successfully transferred sources may be removed.';

export function isAutoGatherRealPrepareExpired(
  expiresAt?: string | null,
  nowMs: number = Date.now(),
): boolean {
  if (!expiresAt) {
    return false;
  }
  const parsed = Date.parse(expiresAt);
  if (Number.isNaN(parsed)) {
    return true;
  }
  return nowMs > parsed;
}

export function shouldShowAutoGatherRealConfirmPanel(
  state?: AutoGatherRealState | null,
  nowMs: number = Date.now(),
): boolean {
  if (!state || state.phase !== 'prepared') {
    return false;
  }
  if (!state.prepared?.preparationId) {
    return false;
  }
  if (isAutoGatherRealPrepareExpired(state.prepared.expiresAt, nowMs)) {
    return false;
  }
  return true;
}

export function autoGatherRealPermissionWarningMessage(
  warnings?: AutoGatherRealPermissionWarnings | null,
): string | null {
  if (!warnings) {
    return null;
  }
  const parts: string[] = [];
  if ((warnings.ownerIssues || 0) > 0) {
    parts.push(`${warnings.ownerIssues} owner(s)`);
  }
  if ((warnings.groupIssues || 0) > 0) {
    parts.push(`${warnings.groupIssues} group(s)`);
  }
  if ((warnings.folderIssues || 0) > 0) {
    parts.push(`${warnings.folderIssues} folder(s)`);
  }
  if ((warnings.fileIssues || 0) > 0) {
    parts.push(`${warnings.fileIssues} file(s)`);
  }
  if (parts.length === 0) {
    return null;
  }
  return (
    `Gather permission warnings: ${parts.join('; ')}. ` +
    'These are legacy unbalanced permission checks and do not prevent Gather execution.'
  );
}

export function autoGatherRealNonExecutableExplanation(
  prepared?: AutoGatherRealPrepareResult | null,
): string | null {
  if (!prepared || prepared.executable) {
    return null;
  }
  const issues = (prepared.issues || []).filter((issue) => issue.trim() !== '');
  if (issues.length > 0) {
    return (
      `Cannot execute this move. Gather planning found: ${issues.join('; ')}. ` +
      'Choose another show or resolve the Gather issues.'
    );
  }
  return (
    'Cannot execute this move. The canonical Gather plan has no executable items.'
  );
}

export function canConfirmAutoGatherRealMove(opts: {
  globalDryRun: boolean;
  busy?: boolean;
  executionActive?: boolean;
  prepared?: AutoGatherRealPrepareResult | null;
  nowMs?: number;
}): boolean {
  if (opts.globalDryRun || opts.busy || opts.executionActive) {
    return false;
  }
  if (!opts.prepared?.preparationId || !opts.prepared.executable) {
    return false;
  }
  if (isAutoGatherRealPrepareExpired(opts.prepared.expiresAt, opts.nowMs)) {
    return false;
  }
  return true;
}

export function canCancelAutoGatherRealPrepare(
  state?: AutoGatherRealState | null,
  nowMs: number = Date.now(),
): boolean {
  return (
    shouldShowAutoGatherRealConfirmPanel(state, nowMs) ||
    isAutoGatherRealExpiredPhase(state?.phase)
  );
}

export function shouldPollAutoGatherRealStatus(phase?: string | null): boolean {
  return isAutoGatherRealExecutionPhase(phase) || phase === 'prepared';
}

export function shouldKeepAutoGatherPageVisible(
  status: Op,
  dryRunPhase?: string | null,
  realPhase?: string | null,
  controlledPhase?: string | null,
): boolean {
  return (
    status === Op.AutoGatherDryRun ||
    status === Op.AutoGatherReal ||
    isAutoGatherDryRunActivePhase(dryRunPhase) ||
    isAutoGatherRealActivePhase(realPhase) ||
    isAutoGatherRealExpiredPhase(realPhase) ||
    isAutoGatherControlledActivePhase(controlledPhase) ||
    isAutoGatherControlledInterruptedPhase(controlledPhase)
  );
}

export function routeForLoadedState(
  status: Op,
  dryRunPhase?: string | null,
  realPhase?: string | null,
  options?: { pathname?: string | null; controlledPhase?: string | null },
): string {
  if (
    shouldKeepAutoGatherPageVisible(
      status,
      dryRunPhase,
      realPhase,
      options?.controlledPhase,
    )
  ) {
    return '/auto-gather';
  }
  if (status !== Op.Neutral) {
    return getRouteFromStatus(status);
  }
  return canonicalAppPath(options?.pathname) ?? '/scatter/select';
}

export function shouldFollowTransferEndedNavigation(
  status: Op,
  dryRunPhase?: string | null,
  realPhase?: string | null,
): boolean {
  return !shouldKeepAutoGatherPageVisible(status, dryRunPhase, realPhase);
}

export function isAutoGatherControlledActivePhase(phase?: string | null): boolean {
  return phase === 'running' || phase === 'stopping';
}

export function isAutoGatherControlledInterruptedPhase(phase?: string | null): boolean {
  return phase === 'interrupted';
}

export function autoGatherControlledLiveRsyncBlocksAck(
  probe?: { alive?: boolean; plausibleRsync?: boolean } | null,
): boolean {
  return Boolean(probe?.alive && probe?.plausibleRsync);
}

export function canAcknowledgeAutoGatherControlled(state?: {
  phase?: string;
  canAcknowledge?: boolean;
  rsyncProbe?: { alive?: boolean; plausibleRsync?: boolean } | null;
} | null): boolean {
  if (!isAutoGatherControlledInterruptedPhase(state?.phase)) {
    return false;
  }
  if (state?.canAcknowledge === false) {
    return false;
  }
  return !autoGatherControlledLiveRsyncBlocksAck(state?.rsyncProbe);
}

export function isAutoGatherControlledCleanTerminalPhase(phase?: string | null): boolean {
  return phase === 'completed' || phase === 'stopped' || phase === 'failed';
}

export function canResetAutoGatherControlled(state?: { phase?: string } | null): boolean {
  return isAutoGatherControlledCleanTerminalPhase(state?.phase);
}

export function shouldShowControlledStartControls(phase?: string | null): boolean {
  return !phase || phase === 'idle';
}

export function shouldKeepAutoGatherPageForControlled(
  controlledPhase?: string | null,
): boolean {
  return (
    isAutoGatherControlledActivePhase(controlledPhase) ||
    controlledPhase === 'stopped' ||
    controlledPhase === 'failed' ||
    controlledPhase === 'completed' ||
    controlledPhase === 'interrupted'
  );
}

export function shouldApplyControlledLibraryScan(
  scan?: AutoGatherScanResult | null,
): boolean {
  if (!scan || scan.cancelled || scan.error) {
    return false;
  }
  return Array.isArray(scan.shows);
}

export function controlledLibraryRevision(
  status?: { libraryRevision?: number; librarySummary?: { revision?: number } } | null,
): number {
  return status?.libraryRevision || status?.librarySummary?.revision || 0;
}

export function normalizeAutoGatherShow(show: AutoGatherShow): AutoGatherShow {
  return {
    ...show,
    videoDisks: show.videoDisks ?? [],
    sidecarOnlyDisks: show.sidecarOnlyDisks ?? [],
    emptyOnlyDisks: show.emptyOnlyDisks ?? [],
    cachePoolsWithVideo: show.cachePoolsWithVideo ?? [],
    cleanupCandidateDisks: show.cleanupCandidateDisks ?? [],
    gatherTargets: show.gatherTargets ?? [],
  };
}

export function normalizeAutoGatherScanResult(
  scan: AutoGatherScanResult,
): AutoGatherScanResult {
  return {
    ...scan,
    shows: (scan.shows ?? []).map(normalizeAutoGatherShow),
    warnings: scan.warnings ?? [],
  };
}

export function headerShowsBusy(status: Op): boolean {
  return status !== Op.Neutral;
}

export function shouldReplacePageWithScatterGatherOperation(
  status: Op,
  dryRunPhase?: string | null,
  realPhase?: string | null,
): boolean {
  if (shouldKeepAutoGatherPageVisible(status, dryRunPhase, realPhase)) {
    return false;
  }
  return status !== Op.Neutral;
}

/** User-facing strings must not expose internal stage labels (3B/3C/3D). */
export const AUTO_GATHER_FORBIDDEN_USER_TERMS = [
  'Stage 3B',
  'Stage 3C',
  'Stage 3D',
  'Stage 3b',
  'Stage 3c',
  'Stage 3d',
  'Stage 1',
  'Stage 2',
] as const;

export function autoGatherUserFacingTextAllowed(text: string): boolean {
  const lower = text.toLowerCase();
  return !AUTO_GATHER_FORBIDDEN_USER_TERMS.some((term) =>
    lower.includes(term.toLowerCase()),
  );
}

export function controlledTriggerLabel(trigger?: string | null): 'Scheduled' | 'Manual' {
  return trigger === 'scheduled' ? 'Scheduled' : 'Manual';
}

export function controlledResultLabel(phase?: string | null): string {
  switch (phase) {
    case 'completed':
      return 'Completed';
    case 'stopped':
      return 'Stopped';
    case 'failed':
      return 'Failed';
    case 'running':
      return 'Running';
    case 'stopping':
      return 'Stopping';
    default:
      return phase ? phase.charAt(0).toUpperCase() + phase.slice(1) : 'Unknown';
  }
}

export function formatControlledRunTimestamp(startedAt?: string | null): string {
  if (!startedAt) {
    return '';
  }
  const parsed = Date.parse(startedAt);
  if (Number.isNaN(parsed)) {
    return startedAt;
  }
  return new Date(parsed).toLocaleString(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  });
}

export function shouldShowControlledLastRunSummary(
  state?: { phase?: string } | null,
): boolean {
  return canResetAutoGatherControlled(state);
}

export function shouldShowControlledActivePanel(phase?: string | null): boolean {
  return isAutoGatherControlledActivePhase(phase);
}

export function shouldShowOneShowConfirmOrStatus(opts: {
  showConfirmPanel: boolean;
  realPhase?: string | null;
  realExecutionActive: boolean;
  isExpired: boolean;
}): boolean {
  return (
    opts.showConfirmPanel ||
    opts.realExecutionActive ||
    opts.isExpired ||
    (opts.realPhase != null &&
      opts.realPhase !== 'idle' &&
      !opts.isExpired &&
      !opts.showConfirmPanel)
  );
}

export const AUTO_GATHER_PAGE_DESCRIPTION =
  'Scan your TV library, review consolidation recommendations, and run manual or controlled Auto Gather operations.';

export const AUTO_GATHER_DRY_RUN_CARD_DESCRIPTION =
  'Preview consolidation plans without moving or deleting files.';

export const AUTO_GATHER_DRY_RUN_OFF_HINT =
  'Enable Global Dry Run in Settings to run a dry-run preview.';

export const AUTO_GATHER_ONE_SHOW_HINT =
  'Individual shows can be prepared and gathered from the library results below.';

export const AUTO_GATHER_CONTROLLED_DESCRIPTION =
  'Consolidate multiple shows sequentially using the limits below.';
