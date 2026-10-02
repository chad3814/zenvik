import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { mergeProgress, useAppState } from '../store';

const handlers = vi.hoisted(() => new Map<string, (v: unknown) => void>());
vi.mock('../api', () => ({
  api: { ready: vi.fn(() => Promise.resolve()) },
  on: (event: string, cb: (v: unknown) => void) => {
    handlers.set(event, cb);
    return () => handlers.delete(event);
  },
}));

describe('mergeProgress', () => {
  it('keeps the phase start within a phase and resets it on a new phase', () => {
    let p = mergeProgress({}, { id: 'a', phase: 'muxing', fraction: 0.1, bytesDone: 1, bytesTotal: 10 }, 1000);
    p = mergeProgress(p, { id: 'a', phase: 'muxing', fraction: 0.5, bytesDone: 5, bytesTotal: 10 }, 5000);
    expect(p.a.phaseStartedAt).toBe(1000);
    p = mergeProgress(p, { id: 'a', phase: 'finalizing', fraction: 0, bytesDone: 0, bytesTotal: 10 }, 9000);
    expect(p.a.phaseStartedAt).toBe(9000);
  });
});

describe('useAppState', () => {
  beforeEach(() => handlers.clear());
  it('subscribes, asks for snapshots and applies events', async () => {
    const { result } = renderHook(() => useAppState());
    const { api } = await import('../api');
    expect(api.ready).toHaveBeenCalled();
    act(() => handlers.get('discs:changed')?.([{ path: '/d', state: 'opening', ambiguous: false, outputDir: '/o', titles: [] }]));
    act(() => handlers.get('queue:changed')?.({ entries: [{ id: 'a', state: 'ripping', outputPath: '/o/a.mkv' }], paused: false, ready: true }));
    act(() => handlers.get('queue:progress')?.({ id: 'a', phase: 'muxing', fraction: 0.5, bytesDone: 1, bytesTotal: 2 }));
    act(() => handlers.get('banners:changed')?.([{ id: 'mkvmerge', message: 'missing' }]));
    expect(result.current.discs).toHaveLength(1);
    expect(result.current.queue.ready).toBe(true);
    expect(result.current.progress.a.fraction).toBe(0.5);
    expect(result.current.banners[0].id).toBe('mkvmerge');
    act(() => handlers.get('queue:changed')?.({ entries: [{ id: 'a', state: 'done', outputPath: '/o/a.mkv' }], paused: false, ready: true }));
    expect(result.current.progress.a).toBeUndefined();
  });
});
