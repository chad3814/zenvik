import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import App from '../App';
import type { DiscSummary } from '../types';

const handlers = vi.hoisted(() => new Map<string, (v: unknown) => void>());
const dropped = vi.hoisted(() => ({ cb: null as ((paths: string[]) => void) | null }));
vi.mock('../api', () => ({
  api: {
    ready: vi.fn(() => Promise.resolve()),
    version: vi.fn(() => Promise.resolve('v1.1.0')),
    addPaths: vi.fn(() => Promise.resolve()),
    reloadConfig: vi.fn(() => Promise.resolve()),
    enqueue: vi.fn(() => Promise.resolve(null)),
    pickISOs: vi.fn(() => Promise.resolve()),
    pickFolder: vi.fn(() => Promise.resolve()),
    removeDisc: vi.fn(() => Promise.resolve()),
    pickOutputDir: vi.fn(() => Promise.resolve()),
    setPaused: vi.fn(() => Promise.resolve()),
  },
  on: (event: string, cb: (v: unknown) => void) => {
    handlers.set(event, cb);
    return () => handlers.delete(event);
  },
  onFileDrop: (cb: (paths: string[]) => void) => {
    dropped.cb = cb;
  },
  offFileDrop: () => {
    dropped.cb = null;
  },
}));

const disc: DiscSummary = {
  path: '/Swiss.iso', state: 'ready', name: 'Swiss Family Robinson', format: 'DVD', ambiguous: false, outputDir: '/Movies',
  titles: [
    { id: '01', durationSeconds: 7581, chapters: 18, sizeBytes: 6.2e9, main: true, rippable: true, skippedCells: [], defaultName: 'Swiss Family Robinson.mkv' },
    { id: '02', durationSeconds: 151, chapters: 1, sizeBytes: 1e8, main: false, rippable: true, skippedCells: [], defaultName: 'Swiss Family Robinson (2).mkv' },
  ],
};

describe('App', () => {
  let api: typeof import('../api').api;
  beforeEach(async () => {
    handlers.clear();
    api = (await import('../api')).api;
    vi.clearAllMocks();
  });

  it('shows discs from events, enqueues ticked titles and clears ticks', async () => {
    render(<App />);
    act(() => handlers.get('discs:changed')?.([disc]));
    act(() => handlers.get('discs:select')?.('/Swiss.iso'));
    await userEvent.click(screen.getByRole('button', { name: 'Add 1 title to queue' }));
    expect(api.enqueue).toHaveBeenCalledWith('/Swiss.iso', ['01'], ['Swiss Family Robinson.mkv']);
    expect(await screen.findByRole('button', { name: 'Add 0 titles to queue' })).toBeDisabled();
  });

  it('shows per-title errors from enqueue', async () => {
    vi.mocked(api.enqueue).mockResolvedValueOnce(['another queue entry already writes this file']);
    render(<App />);
    act(() => handlers.get('discs:changed')?.([disc]));
    await userEvent.click(screen.getByRole('button', { name: 'Add 1 title to queue' }));
    expect(await screen.findByText('another queue entry already writes this file')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Title 01' })).toBeChecked();
  });

  it('adds dropped paths and reloads config on focus', () => {
    render(<App />);
    act(() => dropped.cb?.(['/a.iso', '/b folder']));
    expect(api.addPaths).toHaveBeenCalledWith(['/a.iso', '/b folder']);
    act(() => {
      window.dispatchEvent(new Event('focus'));
    });
    expect(api.reloadConfig).toHaveBeenCalled();
  });

  it('reloads config when the window becomes visible, once per activation', () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    try {
      render(<App />);
      const visible = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
      act(() => {
        document.dispatchEvent(new Event('visibilitychange'));
        window.dispatchEvent(new Event('focus'));
      });
      expect(api.reloadConfig).toHaveBeenCalledTimes(1);
      vi.advanceTimersByTime(1000);
      visible.mockReturnValue('hidden');
      act(() => {
        document.dispatchEvent(new Event('visibilitychange'));
      });
      expect(api.reloadConfig).toHaveBeenCalledTimes(1);
      visible.mockReturnValue('visible');
      act(() => {
        document.dispatchEvent(new Event('visibilitychange'));
      });
      expect(api.reloadConfig).toHaveBeenCalledTimes(2);
      visible.mockRestore();
    } finally {
      vi.useRealTimers();
    }
  });

  it('opens About with the version', async () => {
    render(<App />);
    await userEvent.click(screen.getByRole('button', { name: 'About' }));
    expect(await screen.findByText('Zenvik v1.1.0')).toBeInTheDocument();
  });
});
