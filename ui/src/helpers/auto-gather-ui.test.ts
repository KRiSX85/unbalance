import { describe, expect, it } from 'vitest';

import { Op } from '~/types';
import { getRouteFromStatus } from '~/helpers/routes';
import {
  headerShowsBusy,
  isAutoGatherRealActivePhase,
  isAutoGatherStopEnabled,
  isAutoGatherTerminalPhase,
  canCancelAutoGatherRealPrepare,
  canConfirmAutoGatherRealMove,
  routeForLoadedState,
  shouldFollowTransferEndedNavigation,
  shouldKeepAutoGatherPageVisible,
  shouldPollAutoGatherRealStatus,
  shouldReplacePageWithScatterGatherOperation,
  shouldShowAutoGatherRealConfirmPanel,
} from '~/helpers/auto-gather-ui';
import { AutoGatherRealState } from '~/types';

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

describe('Auto Gather Stage 3C UI routing', () => {
  it('keeps Auto Gather visible during real execution phases', () => {
    expect(getRouteFromStatus(Op.AutoGatherReal)).toBe('/auto-gather');
    expect(isAutoGatherRealActivePhase('executing')).toBe(true);
    expect(isAutoGatherRealActivePhase('prepared')).toBe(true);
    expect(routeForLoadedState(Op.AutoGatherReal, undefined, 'executing')).toBe(
      '/auto-gather',
    );
    expect(
      shouldKeepAutoGatherPageVisible(Op.Neutral, undefined, 'executing'),
    ).toBe(true);
    expect(isAutoGatherStopEnabled(undefined, 'executing')).toBe(true);
    expect(
      shouldFollowTransferEndedNavigation(Op.AutoGatherReal, undefined, 'executing'),
    ).toBe(false);
  });
});

function preparedStatus(
  overrides: Partial<AutoGatherRealState> = {},
): AutoGatherRealState {
  return {
    phase: 'prepared',
    globalDryRun: true,
    currentShow: 'data/media/tv/A Series of Unfortunate Events (2017)',
    currentShowName: 'A Series of Unfortunate Events (2017)',
    currentTarget: 'disk5',
    preparationId: 'lKW2oEInM',
    prepared: {
      preparationId: 'lKW2oEInM',
      showPath: 'data/media/tv/A Series of Unfortunate Events (2017)',
      showName: 'A Series of Unfortunate Events (2017)',
      sourceDisks: ['disk1', 'disk2'],
      canonicalTargetDisk: 'disk5',
      stage2RecommendedTarget: 'disk5',
      stage2AgreesWithCanonical: true,
      currentBytesOnTarget: 0,
      estimatedMoveBytes: 13000566787,
      targetFreeBytes: 20000000000,
      projectedTargetFreeBytes: 6999433213,
      executable: true,
      emptyFolderOnlyDisks: ['disk8'],
      expiresAt: '2099-01-01T00:00:00Z',
      globalDryRun: true,
    },
    ...overrides,
  };
}

describe('Auto Gather Stage 3C confirmation recovery', () => {
  it('restores the full confirmation panel from server prepared status after reload', () => {
    const status = preparedStatus();
    expect(shouldShowAutoGatherRealConfirmPanel(status)).toBe(true);
    expect(status.prepared?.showName).toBe(
      'A Series of Unfortunate Events (2017)',
    );
    expect(status.prepared?.sourceDisks).toEqual(['disk1', 'disk2']);
    expect(status.prepared?.canonicalTargetDisk).toBe('disk5');
    expect(status.prepared?.estimatedMoveBytes).toBe(13000566787);
    expect(status.prepared?.projectedTargetFreeBytes).toBe(6999433213);
    expect(status.prepared?.emptyFolderOnlyDisks).toEqual(['disk8']);
    expect(shouldPollAutoGatherRealStatus(status.phase)).toBe(true);
  });

  it('keeps Confirm disabled after reload when global DRY_RUN is true', () => {
    const status = preparedStatus();
    expect(
      canConfirmAutoGatherRealMove({
        globalDryRun: true,
        prepared: status.prepared,
      }),
    ).toBe(false);
    expect(canCancelAutoGatherRealPrepare(status)).toBe(true);
  });

  it('keeps Cancel available after reload of a valid prepared state', () => {
    const status = preparedStatus({ globalDryRun: false });
    expect(canCancelAutoGatherRealPrepare(status)).toBe(true);
    expect(
      canConfirmAutoGatherRealMove({
        globalDryRun: false,
        prepared: status.prepared,
      }),
    ).toBe(true);
  });

  it('does not restore a usable confirmation panel after expiry', () => {
    const status = preparedStatus({
      phase: 'expired',
      preparationId: undefined,
      prepared: undefined,
      error: 'preparation expired; prepare again',
    });
    expect(shouldShowAutoGatherRealConfirmPanel(status)).toBe(false);
    expect(
      canConfirmAutoGatherRealMove({
        globalDryRun: false,
        prepared: preparedStatus().prepared,
        nowMs: Date.parse('2099-01-02T00:00:00Z'),
      }),
    ).toBe(false);
    expect(canCancelAutoGatherRealPrepare(status)).toBe(true);
    expect(shouldKeepAutoGatherPageVisible(Op.Neutral, undefined, 'expired')).toBe(
      true,
    );
    expect(isAutoGatherRealActivePhase('expired')).toBe(false);
    expect(isAutoGatherStopEnabled(undefined, 'expired')).toBe(false);
  });

  it('hides the confirmation panel if a stale prepared payload is already past expiresAt', () => {
    const status = preparedStatus({
      prepared: {
        ...preparedStatus().prepared!,
        expiresAt: '2026-08-18T01:42:53Z',
      },
    });
    expect(
      shouldShowAutoGatherRealConfirmPanel(
        status,
        Date.parse('2026-08-18T01:50:00Z'),
      ),
    ).toBe(false);
    expect(
      canConfirmAutoGatherRealMove({
        globalDryRun: false,
        prepared: status.prepared,
        nowMs: Date.parse('2026-08-18T01:50:00Z'),
      }),
    ).toBe(false);
  });

  it('treats daemon restart with no prepared object as a safe refusal, not a confirmable panel', () => {
    const status: AutoGatherRealState = {
      phase: 'idle',
      globalDryRun: true,
    };
    expect(shouldShowAutoGatherRealConfirmPanel(status)).toBe(false);
    expect(
      canConfirmAutoGatherRealMove({
        globalDryRun: false,
        prepared: null,
      }),
    ).toBe(false);
    expect(canCancelAutoGatherRealPrepare(status)).toBe(false);
  });
});
