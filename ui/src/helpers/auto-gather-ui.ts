import { Op, AutoGatherRealPrepareResult, AutoGatherRealState } from '~/types';
import { getRouteFromStatus } from '~/helpers/routes';

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
  'Global dry-run is enabled. Real execution is disabled until global dry-run mode is turned off.';

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

export function autoGatherRealPlanHasIssues(
  prepared?: AutoGatherRealPrepareResult | null,
): boolean {
  return (prepared?.issues?.length || 0) > 0;
}

export function autoGatherRealNonExecutableExplanation(
  prepared?: AutoGatherRealPrepareResult | null,
): string | null {
  if (!prepared) {
    return null;
  }
  if (prepared.executable && !autoGatherRealPlanHasIssues(prepared)) {
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
    'Cannot execute this move. Gather planning found issues that prevent ' +
    'execution. Choose another show or resolve the Gather issues.'
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
  if (autoGatherRealPlanHasIssues(opts.prepared)) {
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
): boolean {
  return (
    status === Op.AutoGatherDryRun ||
    status === Op.AutoGatherReal ||
    isAutoGatherDryRunActivePhase(dryRunPhase) ||
    isAutoGatherRealActivePhase(realPhase) ||
    isAutoGatherRealExpiredPhase(realPhase)
  );
}

export function routeForLoadedState(
  status: Op,
  dryRunPhase?: string | null,
  realPhase?: string | null,
): string {
  if (shouldKeepAutoGatherPageVisible(status, dryRunPhase, realPhase)) {
    return '/auto-gather';
  }
  return getRouteFromStatus(status);
}

export function shouldFollowTransferEndedNavigation(
  status: Op,
  dryRunPhase?: string | null,
  realPhase?: string | null,
): boolean {
  return !shouldKeepAutoGatherPageVisible(status, dryRunPhase, realPhase);
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
