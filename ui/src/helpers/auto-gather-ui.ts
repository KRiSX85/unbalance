import { Op } from '~/types';
import { getRouteFromStatus } from '~/helpers/routes';

export function isAutoGatherDryRunActivePhase(phase?: string | null): boolean {
  return phase === 'running' || phase === 'stopping';
}

export function isAutoGatherStopEnabled(phase?: string | null): boolean {
  return isAutoGatherDryRunActivePhase(phase);
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
  phase?: string | null,
): boolean {
  return status === Op.AutoGatherDryRun || isAutoGatherDryRunActivePhase(phase);
}

export function routeForLoadedState(status: Op, phase?: string | null): string {
  if (shouldKeepAutoGatherPageVisible(status, phase)) {
    return '/auto-gather';
  }
  return getRouteFromStatus(status);
}

export function shouldFollowTransferEndedNavigation(
  status: Op,
  phase?: string | null,
): boolean {
  return !shouldKeepAutoGatherPageVisible(status, phase);
}

export function headerShowsBusy(status: Op): boolean {
  return status !== Op.Neutral;
}

export function shouldReplacePageWithScatterGatherOperation(
  status: Op,
  phase?: string | null,
): boolean {
  if (shouldKeepAutoGatherPageVisible(status, phase)) {
    return false;
  }
  return status !== Op.Neutral;
}
