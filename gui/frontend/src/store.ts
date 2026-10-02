import { useEffect, useState } from 'react';
import { api, on } from './api';
import type { Banner, DiscSummary, Progress, QueueSnapshot } from './types';

export interface LiveProgress extends Progress {
  phaseStartedAt: number; // ms, when this phase's first event arrived
}

export interface AppState {
  discs: DiscSummary[];
  queue: QueueSnapshot;
  progress: Record<string, LiveProgress>;
  banners: Banner[];
}

const initial: AppState = {
  discs: [],
  queue: { entries: [], paused: false, ready: false },
  progress: {},
  banners: [],
};

export function mergeProgress(prev: Record<string, LiveProgress>, p: Progress, now: number): Record<string, LiveProgress> {
  const old = prev[p.id];
  const phaseStartedAt = old && old.phase === p.phase ? old.phaseStartedAt : now;
  return { ...prev, [p.id]: { ...p, phaseStartedAt } };
}

function pruneProgress(progress: Record<string, LiveProgress>, queue: QueueSnapshot): Record<string, LiveProgress> {
  const running = new Set(queue.entries.filter((e) => e.state === 'ripping').map((e) => e.id));
  return Object.fromEntries(Object.entries(progress).filter(([id]) => running.has(id)));
}

/** useAppState mirrors the backend's snapshots. */
export function useAppState(): AppState {
  const [state, setState] = useState<AppState>(initial);
  useEffect(() => {
    const offs = [
      on<DiscSummary[]>('discs:changed', (discs) => setState((s) => ({ ...s, discs }))),
      on<QueueSnapshot>('queue:changed', (queue) =>
        setState((s) => ({ ...s, queue, progress: pruneProgress(s.progress, queue) })),
      ),
      on<Progress>('queue:progress', (p) =>
        setState((s) => ({ ...s, progress: mergeProgress(s.progress, p, Date.now()) })),
      ),
      on<Banner[]>('banners:changed', (banners) => setState((s) => ({ ...s, banners }))),
    ];
    void api.ready();
    return () => offs.forEach((off) => off());
  }, []);
  return state;
}
