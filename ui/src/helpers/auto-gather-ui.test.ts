import { describe, expect, it } from 'vitest';

import { Op } from '~/types';
import { getRouteFromStatus } from '~/helpers/routes';
import {
  headerShowsBusy,
  isAutoGatherStopEnabled,
  isAutoGatherTerminalPhase,
  routeForLoadedState,
  shouldFollowTransferEndedNavigation,
  shouldKeepAutoGatherPageVisible,
  shouldReplacePageWithScatterGatherOperation,
} from '~/helpers/auto-gather-ui';

describe('Auto Gather Stage 3B UI routing', () => {
  it('does not map an active Auto Gather run to the generic Scatter/Gather busy pages', () => {
    expect(getRouteFromStatus(Op.AutoGatherDryRun)).toBe('/auto-gather');
    expect(routeForLoadedState(Op.AutoGatherDryRun, 'running')).toBe(
      '/auto-gather',
    );
    expect(routeForLoadedState(Op.GatherMove, 'running')).toBe('/auto-gather');
    expect(routeForLoadedState(Op.GatherPlan, 'stopping')).toBe('/auto-gather');
    expect(shouldReplacePageWithScatterGatherOperation(Op.AutoGatherDryRun, 'running')).toBe(
      false,
    );
    expect(shouldKeepAutoGatherPageVisible(Op.AutoGatherDryRun, 'running')).toBe(
      true,
    );
  });

  it('keeps the Auto Gather page as the recovered route after a reload while running', () => {
    expect(routeForLoadedState(Op.AutoGatherDryRun, 'running')).toBe(
      '/auto-gather',
    );
    expect(routeForLoadedState(Op.Neutral, 'running')).toBe('/auto-gather');
    expect(isAutoGatherStopEnabled('running')).toBe(true);
    expect(isAutoGatherStopEnabled('stopping')).toBe(true);
  });

  it('keeps Stop available for running and stopping phases', () => {
    expect(isAutoGatherStopEnabled('running')).toBe(true);
    expect(isAutoGatherStopEnabled('stopping')).toBe(true);
    expect(isAutoGatherStopEnabled('completed')).toBe(false);
    expect(isAutoGatherStopEnabled('failed')).toBe(false);
    expect(isAutoGatherStopEnabled('stopped')).toBe(false);
    expect(isAutoGatherStopEnabled('idle')).toBe(false);
  });

  it('does not send transfer-ended navigation to History during Auto Gather', () => {
    expect(
      shouldFollowTransferEndedNavigation(Op.AutoGatherDryRun, 'running'),
    ).toBe(false);
    expect(shouldFollowTransferEndedNavigation(Op.GatherMove, 'running')).toBe(
      false,
    );
    expect(shouldFollowTransferEndedNavigation(Op.GatherMove, 'idle')).toBe(
      true,
    );
  });

  it('restores normal Scatter/Gather routing once Auto Gather is terminal', () => {
    expect(isAutoGatherTerminalPhase('completed')).toBe(true);
    expect(isAutoGatherTerminalPhase('stopped')).toBe(true);
    expect(isAutoGatherTerminalPhase('failed')).toBe(true);
    expect(routeForLoadedState(Op.Neutral, 'completed')).toBe('/scatter/select');
    expect(shouldKeepAutoGatherPageVisible(Op.Neutral, 'completed')).toBe(false);
    expect(shouldReplacePageWithScatterGatherOperation(Op.ScatterMove, 'idle')).toBe(
      true,
    );
  });

  it('may still show the header spinner while Auto Gather is the active op', () => {
    expect(headerShowsBusy(Op.AutoGatherDryRun)).toBe(true);
    expect(headerShowsBusy(Op.Neutral)).toBe(false);
    expect(headerShowsBusy(Op.GatherMove)).toBe(true);
  });
});
