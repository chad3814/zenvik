import { describe, expect, it } from 'vitest';
import { basename, clamp01, eta, formatBytes, formatClock, formatSpan } from '../format';

describe('format', () => {
  it('formats clock durations', () => {
    expect(formatClock(7581)).toBe('2:06:21');
    expect(formatClock(59)).toBe('0:00:59');
    expect(formatClock(-4)).toBe('0:00:00');
  });
  it('formats spans', () => {
    expect(formatSpan(5)).toBe('5s');
    expect(formatSpan(252)).toBe('4m12s');
    expect(formatSpan(3725)).toBe('1h02m');
  });
  it('formats sizes', () => {
    expect(formatBytes(6.2e9)).toBe('6.2 GB');
    expect(formatBytes(700e6)).toBe('700 MB');
    expect(formatBytes(12_300)).toBe('12 kB');
  });
  it('clamps fractions', () => {
    expect(clamp01(1.5)).toBe(1);
    expect(clamp01(-1)).toBe(0);
    expect(clamp01(Number.NaN)).toBe(0);
  });
  it('estimates time left from the phase start', () => {
    expect(eta(0.01, 0, 10_000)).toBeNull();
    expect(eta(0.5, 0, 60_000)).toBe('1m00s');
  });
  it('takes base names of both path styles', () => {
    expect(basename('/a/b/Movie.mkv')).toBe('Movie.mkv');
    expect(basename('C:\\a\\Movie.mkv')).toBe('Movie.mkv');
  });
});
