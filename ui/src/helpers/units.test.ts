import { describe, expect, it } from 'vitest';

import {
  bytesFromDecimalGB,
  formatByteBoundAsGB,
  humanBytes,
} from '~/helpers/units';

describe('decimal GB bounds', () => {
  it('converts Max GB input with decimal gigabytes, not GiB', () => {
    expect(bytesFromDecimalGB(10)).toBe(10_000_000_000);
    expect(bytesFromDecimalGB(10)).not.toBe(10 * 1024 * 1024 * 1024);
  });

  it('displays an exact decimal-GB bound as the same number entered', () => {
    expect(formatByteBoundAsGB(bytesFromDecimalGB(10))).toBe('10 GB');
    expect(formatByteBoundAsGB(bytesFromDecimalGB(50))).toBe('50 GB');
  });

  it('does not display a 10 GiB byte count as 10 GB', () => {
    const tenGib = 10 * 1024 * 1024 * 1024;
    expect(humanBytes(tenGib)).toBe('10.7 GB');
    expect(formatByteBoundAsGB(tenGib)).toBe('10.7 GB');
  });
});
