import { describe, expect, it } from 'vitest';

import { canonicalAppPath } from '~/helpers/routes';
import { Op } from '~/types';
import { routeForLoadedState } from '~/helpers/auto-gather-ui';

describe('SPA path preservation on reload', () => {
  it('keeps the requested app path when the daemon is idle', () => {
    expect(
      routeForLoadedState(Op.Neutral, undefined, undefined, {
        pathname: '/auto-gather',
      }),
    ).toBe('/auto-gather');
    expect(
      routeForLoadedState(Op.Neutral, undefined, undefined, {
        pathname: '/history',
      }),
    ).toBe('/history');
    expect(
      routeForLoadedState(Op.Neutral, undefined, undefined, {
        pathname: '/settings',
      }),
    ).toBe('/settings');
    expect(
      routeForLoadedState(Op.Neutral, undefined, undefined, {
        pathname: '/logs',
      }),
    ).toBe('/logs');
    expect(
      routeForLoadedState(Op.Neutral, undefined, undefined, {
        pathname: '/log',
      }),
    ).toBe('/logs');
    expect(
      routeForLoadedState(Op.Neutral, undefined, undefined, {
        pathname: '/scatter',
      }),
    ).toBe('/scatter/select');
    expect(
      routeForLoadedState(Op.Neutral, undefined, undefined, {
        pathname: '/gather',
      }),
    ).toBe('/gather/select');
  });

  it('still recovers an in-flight Scatter/Gather operation over the URL', () => {
    expect(
      routeForLoadedState(Op.ScatterMove, undefined, undefined, {
        pathname: '/auto-gather',
      }),
    ).toBe('/scatter/transfer/operation');
    expect(
      routeForLoadedState(Op.GatherMove, undefined, undefined, {
        pathname: '/history',
      }),
    ).toBe('/gather/transfer/operation');
  });

  it('recovers an interrupted controlled Auto Gather session to /auto-gather', () => {
    expect(
      routeForLoadedState(Op.Neutral, undefined, undefined, {
        pathname: '/history',
        controlledPhase: 'interrupted',
      }),
    ).toBe('/auto-gather');
  });
});

describe('canonicalAppPath', () => {
  it('maps /log to /logs and leaves unknown paths null', () => {
    expect(canonicalAppPath('/log')).toBe('/logs');
    expect(canonicalAppPath('/not-a-page')).toBeNull();
  });
});
