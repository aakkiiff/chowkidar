import { describe, it, expect } from 'vitest';

// Inline helpers extracted from AgentsList.tsx for testing
function pct(used: number | null, total: number | null): number {
  if (!used || !total || total === 0) return 0;
  return Math.round((used / total) * 100);
}

function fmtGB(gb: number | null): string {
  if (gb == null) return '—';
  return gb >= 1 ? `${gb.toFixed(1)} GB` : `${(gb * 1024).toFixed(0)} MB`;
}

function barColor(p: number): string {
  if (p < 60) return '#22c55e';
  if (p < 85) return '#f59e0b';
  return '#ef4444';
}

describe('pct (CPU/mem/disk percentage)', () => {
  it('returns 0 for null used', () => expect(pct(null, 16)).toBe(0));
  it('returns 0 for null total', () => expect(pct(4, null)).toBe(0));
  it('returns 0 for zero total', () => expect(pct(4, 0)).toBe(0));
  it('computes percentage', () => expect(pct(4, 16)).toBe(25));
  it('rounds correctly', () => expect(pct(1, 3)).toBe(33));
});

describe('fmtGB', () => {
  it('returns em dash for null', () => expect(fmtGB(null)).toBe('—'));
  it('shows GB for values >= 1', () => expect(fmtGB(8)).toBe('8.0 GB'));
  it('shows MB for values < 1', () => expect(fmtGB(0.5)).toBe('512 MB'));
  it('formats decimal GB', () => expect(fmtGB(1.5)).toBe('1.5 GB'));
});

describe('barColor', () => {
  it('green below 60', () => expect(barColor(50)).toBe('#22c55e'));
  it('amber 60-84', () => expect(barColor(75)).toBe('#f59e0b'));
  it('red at 85+', () => expect(barColor(90)).toBe('#ef4444'));
  it('green at exactly 0', () => expect(barColor(0)).toBe('#22c55e'));
});
