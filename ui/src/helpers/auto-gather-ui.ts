import { Op } from '~/types';
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

export function isAutoGatherRealTerminalPhase(phase?: string | null): boolean {
  return (
    phase === 'stopped' ||
    phase === 'completed' ||
    phase === 'failed' ||
    phase === 'verification_warning' ||
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

export function shouldKeepAutoGatherPageVisible(
  status: Op,
  dryRunPhase?: string | null,
  realPhase?: string | null,
): boolean {
  return (
    status === Op.AutoGatherDryRun ||
    status === Op.AutoGatherReal ||
    isAutoGatherDryRunActivePhase(dryRunPhase) ||
    isAutoGatherRealActivePhase(realPhase)
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
