import { describe, expect, it } from 'vitest';

import { AutoGatherRealState, AutoGatherShow, Op } from '~/types';
import { getRouteFromStatus } from '~/helpers/routes';
import {
  AUTO_GATHER_REAL_GLOBAL_DRY_RUN_MESSAGE,
  autoGatherRealNonExecutableExplanation,
  autoGatherRealPermissionWarningMessage,
  headerShowsBusy,
  isAutoGatherRealActivePhase,
  isAutoGatherStopEnabled,
  isAutoGatherTerminalPhase,
  isAutoGatherControlledInterruptedPhase,
  canAcknowledgeAutoGatherControlled,
  autoGatherControlledLiveRsyncBlocksAck,
  canResetAutoGatherControlled,
  shouldShowControlledStartControls,
  shouldApplyControlledLibraryScan,
  normalizeAutoGatherShow,
  controlledLibraryRevision,
  shouldKeepAutoGatherPageForControlled,
  canCancelAutoGatherRealPrepare,
  canConfirmAutoGatherRealMove,
  routeForLoadedState,
  shouldFollowTransferEndedNavigation,
  shouldKeepAutoGatherPageVisible,
  shouldPollAutoGatherRealStatus,
  shouldReplacePageWithScatterGatherOperation,
  shouldShowAutoGatherRealConfirmPanel,
} from '~/helpers/auto-gather-ui';
import { bytesFromDecimalGB, formatByteBoundAsGB } from '~/helpers/units';

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

describe('Auto Gather Stage 3C dry-run wording', () => {
  it('does not tell the user to disable global dry-run in Settings', () => {
    expect(AUTO_GATHER_REAL_GLOBAL_DRY_RUN_MESSAGE).toBe(
      'Global dry-run is enabled. Real execution is disabled until global dry-run mode is turned off.',
    );
    expect(AUTO_GATHER_REAL_GLOBAL_DRY_RUN_MESSAGE.toLowerCase()).not.toContain(
      'settings',
    );
  });
});

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

describe('Auto Gather Stage 3C non-executable confirmation', () => {
  const structuralIssues = ['canonical Gather target has no executable items'];
  const expectedExplanation =
    'Cannot execute this move. Gather planning found: canonical Gather target has no executable items. Choose another show or resolve the Gather issues.';

  function nonExecutableStatus(): AutoGatherRealState {
    const base = preparedStatus({ globalDryRun: false });
    return {
      ...base,
      currentShow: 'data/media/tv/A Thousand Blows (2025)',
      currentShowName: 'A Thousand Blows (2025)',
      currentTarget: 'disk4',
      prepared: {
        ...base.prepared!,
        showPath: 'data/media/tv/A Thousand Blows (2025)',
        showName: 'A Thousand Blows (2025)',
        canonicalTargetDisk: 'disk4',
        stage2RecommendedTarget: 'disk4',
        estimatedMoveBytes: 2623052978,
        executable: false,
        issues: structuralIssues,
        globalDryRun: false,
      },
    };
  }

  it('keeps Confirm disabled when DRY_RUN is off but the plan is not executable', () => {
    const status = nonExecutableStatus();
    expect(shouldShowAutoGatherRealConfirmPanel(status)).toBe(true);
    expect(
      canConfirmAutoGatherRealMove({
        globalDryRun: false,
        prepared: status.prepared,
      }),
    ).toBe(false);
    expect(canCancelAutoGatherRealPrepare(status)).toBe(true);
  });

  it('may enable Confirm when DRY_RUN is off and the prepared plan is executable with no warnings', () => {
    const status = preparedStatus({ globalDryRun: false });
    expect(
      canConfirmAutoGatherRealMove({
        globalDryRun: false,
        prepared: status.prepared,
      }),
    ).toBe(true);
    expect(autoGatherRealNonExecutableExplanation(status.prepared)).toBeNull();
  });

  it('restores the non-executable explanation from /real/status after browser refresh', () => {
    const recovered = nonExecutableStatus();
    expect(shouldShowAutoGatherRealConfirmPanel(recovered)).toBe(true);
    expect(autoGatherRealNonExecutableExplanation(recovered.prepared)).toBe(
      expectedExplanation,
    );
    expect(
      canConfirmAutoGatherRealMove({
        globalDryRun: false,
        prepared: recovered.prepared,
      }),
    ).toBe(false);
    expect(canCancelAutoGatherRealPrepare(recovered)).toBe(true);
  });
});

describe('Auto Gather Stage 3C permission warnings', () => {
  it('formats permission warning counts for the confirmation panel', () => {
    expect(
      autoGatherRealPermissionWarningMessage({
        folderIssues: 26,
        fileIssues: 38,
      }),
    ).toBe(
      'Gather permission warnings: 26 folder(s); 38 file(s). These are legacy unbalanced permission checks and do not prevent Gather execution.',
    );
    expect(
      autoGatherRealPermissionWarningMessage({
        ownerIssues: 2,
        groupIssues: 1,
      }),
    ).toBe(
      'Gather permission warnings: 2 owner(s); 1 group(s). These are legacy unbalanced permission checks and do not prevent Gather execution.',
    );
  });

  it('allows Confirm when only permission warnings are present', () => {
    const status = preparedStatus({
      globalDryRun: false,
      prepared: {
        ...preparedStatus().prepared!,
        executable: true,
        permissionWarnings: {
          folderIssues: 26,
          fileIssues: 38,
        },
        globalDryRun: false,
      },
    });
    expect(
      canConfirmAutoGatherRealMove({
        globalDryRun: false,
        prepared: status.prepared,
      }),
    ).toBe(true);
    expect(autoGatherRealNonExecutableExplanation(status.prepared)).toBeNull();
    expect(
      autoGatherRealPermissionWarningMessage(status.prepared?.permissionWarnings),
    ).toContain('26 folder(s); 38 file(s)');
  });

  it('keeps Confirm disabled when DRY_RUN is true even with permission warnings only', () => {
    const status = preparedStatus({
      prepared: {
        ...preparedStatus().prepared!,
        executable: true,
        permissionWarnings: { folderIssues: 5 },
      },
    });
    expect(
      canConfirmAutoGatherRealMove({
        globalDryRun: true,
        prepared: status.prepared,
      }),
    ).toBe(false);
  });

  it('retains permission warnings and correct Confirm gating after recovered prepared state', () => {
    const status = preparedStatus({
      globalDryRun: false,
      prepared: {
        ...preparedStatus().prepared!,
        executable: true,
        permissionWarnings: {
          ownerIssues: 1,
          folderIssues: 3,
          fileIssues: 4,
        },
        globalDryRun: false,
      },
    });
    expect(shouldShowAutoGatherRealConfirmPanel(status)).toBe(true);
    expect(
      canConfirmAutoGatherRealMove({
        globalDryRun: false,
        prepared: status.prepared,
      }),
    ).toBe(true);
    expect(
      autoGatherRealPermissionWarningMessage(status.prepared?.permissionWarnings),
    ).toContain('1 owner(s); 3 folder(s); 4 file(s)');
  });
});

describe('Auto Gather Stage 3D interrupted recovery', () => {
  it('treats interrupted as a recovered Auto Gather page state that is not idle', () => {
    expect(isAutoGatherControlledInterruptedPhase('interrupted')).toBe(true);
    expect(isAutoGatherControlledInterruptedPhase('idle')).toBe(false);
    expect(shouldKeepAutoGatherPageForControlled('interrupted')).toBe(true);
  });

  it('blocks acknowledgement while a recorded rsync PID is still live', () => {
    expect(
      autoGatherControlledLiveRsyncBlocksAck({ alive: true, plausibleRsync: true }),
    ).toBe(true);
    expect(
      canAcknowledgeAutoGatherControlled({
        phase: 'interrupted',
        canAcknowledge: false,
        rsyncProbe: { alive: true, plausibleRsync: true },
      }),
    ).toBe(false);
  });

  it('allows acknowledgement when the PID is gone or is not rsync', () => {
    expect(
      canAcknowledgeAutoGatherControlled({
        phase: 'interrupted',
        canAcknowledge: true,
        rsyncProbe: { alive: false, plausibleRsync: false },
      }),
    ).toBe(true);
    expect(
      canAcknowledgeAutoGatherControlled({
        phase: 'interrupted',
        canAcknowledge: true,
        rsyncProbe: { alive: true, plausibleRsync: false },
      }),
    ).toBe(true);
  });

  it('offers New controlled session only for clean terminal phases', () => {
    expect(canResetAutoGatherControlled({ phase: 'completed' })).toBe(true);
    expect(canResetAutoGatherControlled({ phase: 'stopped' })).toBe(true);
    expect(canResetAutoGatherControlled({ phase: 'failed' })).toBe(true);
    expect(canResetAutoGatherControlled({ phase: 'interrupted' })).toBe(false);
    expect(canResetAutoGatherControlled({ phase: 'running' })).toBe(false);
    expect(canResetAutoGatherControlled({ phase: 'idle' })).toBe(false);
    expect(shouldShowControlledStartControls('idle')).toBe(true);
    expect(shouldShowControlledStartControls(undefined)).toBe(true);
    expect(shouldShowControlledStartControls('completed')).toBe(false);
    expect(shouldShowControlledStartControls('interrupted')).toBe(false);
  });

  it('keeps completed summary visible on refresh until an explicit new-session reset', () => {
    expect(shouldKeepAutoGatherPageForControlled('completed')).toBe(true);
    expect(shouldShowControlledStartControls('completed')).toBe(false);
    expect(canResetAutoGatherControlled({ phase: 'completed' })).toBe(true);
  });
});

describe('Auto Gather Stage 3D Max GB display', () => {
  it('keeps the entered Max GB value after Start', () => {
    const maxBytes = bytesFromDecimalGB(10);
    expect(maxBytes).toBe(10_000_000_000);
    expect(formatByteBoundAsGB(maxBytes)).toBe('10 GB');
  });
});

describe('Auto Gather Stage 3D library snapshot apply', () => {
  it('applies a successful published library scan and ignores cancelled or error scans', () => {
    expect(
      shouldApplyControlledLibraryScan({
        libraryPath: 'data/media/tv',
        shows: [],
      }),
    ).toBe(true);
    expect(
      shouldApplyControlledLibraryScan({
        libraryPath: 'data/media/tv',
        shows: [],
        cancelled: true,
      }),
    ).toBe(false);
    expect(
      shouldApplyControlledLibraryScan({
        libraryPath: 'data/media/tv',
        shows: [],
        error: 'scan failed',
      }),
    ).toBe(false);
    expect(shouldApplyControlledLibraryScan(undefined)).toBe(false);
  });

  it('reads the library revision from compact controlled/status fields', () => {
    expect(controlledLibraryRevision({ libraryRevision: 4 })).toBe(4);
    expect(
      controlledLibraryRevision({ librarySummary: { revision: 7 } }),
    ).toBe(7);
    expect(controlledLibraryRevision({})).toBe(0);
  });

  it('reproduces the ShowRow crash on null disk arrays and normalizes it', () => {
    const raw = {
      name: 'The Fresh Prince of Bel-Air (1990)',
      path: 'data/media/tv/The Fresh Prince of Bel-Air (1990)',
      status: 'split',
      split: true,
      ready: true,
      totalVideoBytes: 1,
      totalBytes: 1,
    } as AutoGatherShow;
    expect(() => raw.videoDisks.filter(() => true)).toThrow();
    expect(() => raw.cachePoolsWithVideo.length).toThrow();
    const normalized = normalizeAutoGatherShow(raw);
    expect(normalized.videoDisks.filter(() => true)).toEqual([]);
    expect(normalized.cachePoolsWithVideo.length).toBe(0);
  });
});
