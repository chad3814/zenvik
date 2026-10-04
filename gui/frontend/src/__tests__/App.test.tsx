import { act, fireEvent, render, screen } from '@testing-library/react';
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
    platform: vi.fn(() => Promise.resolve('darwin')),
    addPaths: vi.fn(() => Promise.resolve()),
    reloadConfig: vi.fn(() => Promise.resolve()),
    enqueue: vi.fn(() => Promise.resolve(null)),
    pickISOs: vi.fn(() => Promise.resolve()),
    pickFolder: vi.fn(() => Promise.resolve()),
    removeDisc: vi.fn(() => Promise.resolve()),
    pickOutputDir: vi.fn(() => Promise.resolve()),
    setPaused: vi.fn(() => Promise.resolve()),
    openURL: vi.fn(),
    mkvmergeInfo: vi.fn(() => Promise.resolve('mkvmerge 102.0 (bundled, from MKVToolNix — GPLv2)')),
    mkvmergeSourceURL: vi.fn(() =>
      Promise.resolve('https://github.com/chad3814/zenvik/releases/download/mkvtoolnix-src-102.0/mkvtoolnix-102.0.tar.xz'),
    ),
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

  it('sends a double-clicked Add only once', async () => {
    let finish: (v: string[] | null) => void = () => {};
    vi.mocked(api.enqueue).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    render(<App />);
    act(() => handlers.get('discs:changed')?.([disc]));
    await userEvent.dblClick(screen.getByRole('button', { name: 'Add 1 title to queue' }));
    expect(api.enqueue).toHaveBeenCalledTimes(1);
    await act(async () => finish(null));
    expect(await screen.findByRole('button', { name: 'Add 0 titles to queue' })).toBeDisabled();
  });

  it("clears a title's error when its name changes or it is ticked again", async () => {
    vi.mocked(api.enqueue).mockResolvedValueOnce(['the name is empty', 'the name is empty']);
    render(<App />);
    act(() => handlers.get('discs:changed')?.([disc]));
    await userEvent.click(screen.getByRole('checkbox', { name: 'Title 02' }));
    await userEvent.click(screen.getByRole('button', { name: 'Add 2 titles to queue' }));
    expect(await screen.findAllByText('the name is empty')).toHaveLength(2);
    await userEvent.type(screen.getByRole('textbox', { name: 'Name for title 01' }), 'x');
    expect(screen.getAllByText('the name is empty')).toHaveLength(1);
    await userEvent.click(screen.getByRole('checkbox', { name: 'Title 02' }));
    await userEvent.click(screen.getByRole('checkbox', { name: 'Title 02' }));
    expect(screen.queryByText('the name is empty')).not.toBeInTheDocument();
  });

  it('adds dropped paths and reloads config on focus', () => {
    render(<App />);
    act(() => dropped.cb?.(['/a.iso', '', '/b folder']));
    expect(api.addPaths).toHaveBeenCalledWith(['/a.iso', '/b folder']);
    vi.mocked(api.addPaths).mockClear();
    act(() => dropped.cb?.(['']));
    expect(api.addPaths).not.toHaveBeenCalled();
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

  it("opens About's link in the system browser, never in the window", async () => {
    render(<App />);
    await userEvent.click(screen.getByRole('button', { name: 'About' }));
    const link = screen.getByRole('link', { name: 'github.com/chad3814/zenvik' });
    const notPrevented = fireEvent.click(link);
    expect(notPrevented).toBe(false);
    expect(api.openURL).toHaveBeenCalledWith('https://github.com/chad3814/zenvik');
  });

  it('shows the bundled mkvmerge and links its source in About', async () => {
    render(<App />);
    await userEvent.click(screen.getByRole('button', { name: 'About' }));
    expect(await screen.findByText('mkvmerge 102.0 (bundled, from MKVToolNix — GPLv2)')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('link', { name: 'MKVToolNix source' }));
    expect(api.openURL).toHaveBeenCalledWith(
      'https://github.com/chad3814/zenvik/releases/download/mkvtoolnix-src-102.0/mkvtoolnix-102.0.tar.xz',
    );
  });

  it('shows no source link when mkvmerge is not bundled', async () => {
    vi.mocked(api.mkvmergeInfo).mockResolvedValueOnce('mkvmerge 101.0.0 (from PATH)');
    vi.mocked(api.mkvmergeSourceURL).mockResolvedValueOnce('');
    render(<App />);
    await userEvent.click(screen.getByRole('button', { name: 'About' }));
    expect(await screen.findByText('mkvmerge 101.0.0 (from PATH)')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'MKVToolNix source' })).not.toBeInTheDocument();
  });
});
