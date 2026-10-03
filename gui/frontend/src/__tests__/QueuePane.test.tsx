import { fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { QueuePane } from '../components/QueuePane';
import type { Entry, QueueSnapshot } from '../types';

vi.mock('../api', () => ({
  api: {
    setPaused: vi.fn(() => Promise.resolve()),
    clearFinished: vi.fn(() => Promise.resolve()),
    cancel: vi.fn(() => Promise.resolve()),
    retry: vi.fn(() => Promise.resolve()),
    remove: vi.fn(() => Promise.resolve()),
    move: vi.fn(() => Promise.resolve()),
    reveal: vi.fn(() => Promise.resolve('')),
    rename: vi.fn((_id: string, name: string) => Promise.resolve(name.includes('/') ? "a file name can't contain a folder" : '')),
  },
}));

const entry = (id: string, state: Entry['state'], extra: Partial<Entry> = {}): Entry => ({
  id, discPath: '/d', titleId: '01', outputPath: `/Movies/${id}.mkv`, state, addedAt: '2026-10-02T10:00:00Z', ...extra,
});
const queue = (entries: Entry[], extra: Partial<QueueSnapshot> = {}): QueueSnapshot => ({ entries, paused: false, ready: true, ...extra });

describe('QueuePane', () => {
  let api: typeof import('../api').api;
  beforeEach(async () => {
    api = (await import('../api')).api;
    vi.clearAllMocks();
  });

  it('renders every state with its actions', async () => {
    const now = Date.parse('2026-10-02T10:10:00Z');
    render(
      <QueuePane
        queue={queue([
          entry('done1', 'done', { startedAt: '2026-10-02T10:00:00Z', endedAt: '2026-10-02T10:04:12Z' }),
          entry('rip', 'ripping', { startedAt: '2026-10-02T10:05:00Z' }),
          entry('wait', 'waiting'),
          entry('bad', 'failed', { label: 'encrypted', message: 'title 01: disc is encrypted' }),
          entry('stop', 'canceled'),
        ])}
        progress={{ rip: { id: 'rip', phase: 'muxing', fraction: 0.5, bytesDone: 3e9, bytesTotal: 6e9, phaseStartedAt: now - 60_000 } }}
        now={now}
      />,
    );
    const row = (name: string) => screen.getByText(name).closest('li') as HTMLElement;
    expect(within(row('done1.mkv')).getByText('done in 4m12s')).toBeInTheDocument();
    expect(within(row('rip.mkv')).getByText(/muxing/)).toBeInTheDocument();
    expect(within(row('rip.mkv')).getByText('50%')).toBeInTheDocument();
    expect(within(row('rip.mkv')).getByText('3.0 GB / 6.0 GB')).toBeInTheDocument();
    expect(within(row('rip.mkv')).getByText('ETA 1m00s')).toBeInTheDocument();
    expect(within(row('bad.mkv')).getByText('encrypted')).toHaveAttribute('title', 'title 01: disc is encrypted');

    await userEvent.click(within(row('rip.mkv')).getByRole('button', { name: 'Cancel' }));
    expect(api.cancel).toHaveBeenCalledWith('rip');
    await userEvent.click(within(row('bad.mkv')).getByRole('button', { name: 'Retry' }));
    expect(api.retry).toHaveBeenCalledWith('bad');
    await userEvent.click(within(row('done1.mkv')).getByRole('button', { name: 'Show' }));
    expect(api.reveal).toHaveBeenCalledWith('done1');
    await userEvent.click(within(row('wait.mkv')).getByRole('button', { name: 'Move up' }));
    expect(api.move).toHaveBeenCalledWith('wait', 1);
    await userEvent.click(screen.getByRole('button', { name: 'Pause queue' }));
    expect(api.setPaused).toHaveBeenCalledWith(true);
    await userEvent.click(screen.getByRole('button', { name: 'Clear finished' }));
    expect(api.clearFinished).toHaveBeenCalled();
  });

  it("shows why Show couldn't reveal the file", async () => {
    vi.mocked(api.reveal).mockResolvedValueOnce('exec: "xdg-open": executable file not found in $PATH');
    render(<QueuePane queue={queue([entry('done1', 'done')])} progress={{}} now={0} />);
    await userEvent.click(screen.getByRole('button', { name: 'Show' }));
    expect(await screen.findByText('exec: "xdg-open": executable file not found in $PATH')).toBeInTheDocument();
  });

  it('renames a waiting entry and shows rename errors', async () => {
    render(<QueuePane queue={queue([entry('wait', 'waiting')])} progress={{}} now={0} />);
    await userEvent.click(screen.getByRole('button', { name: 'Rename' }));
    const field = screen.getByRole('textbox', { name: 'New name for wait.mkv' });
    await userEvent.clear(field);
    await userEvent.type(field, 'a/b{Enter}');
    expect(await screen.findByText("a file name can't contain a folder")).toBeInTheDocument();
    await userEvent.clear(field);
    await userEvent.type(field, 'Better{Enter}');
    expect(api.rename).toHaveBeenLastCalledWith('wait', 'Better');
    expect(screen.queryByRole('textbox', { name: 'New name for wait.mkv' })).not.toBeInTheDocument();
  });

  it('reorders by drag and drop', () => {
    render(<QueuePane queue={queue([entry('a', 'waiting'), entry('b', 'waiting')])} progress={{}} now={0} />);
    const store = new Map<string, string>();
    const dataTransfer = { setData: (k: string, v: string) => store.set(k, v), getData: (k: string) => store.get(k) ?? '', effectAllowed: '' };
    fireEvent.dragStart(screen.getByText('b.mkv').closest('li') as HTMLElement, { dataTransfer });
    fireEvent.drop(screen.getByText('a.mkv').closest('li') as HTMLElement, { dataTransfer });
    expect(api.move).toHaveBeenCalledWith('b', 0);
  });

  it('says when paused, not ready, or empty', () => {
    const { rerender } = render(<QueuePane queue={queue([], { paused: true, ready: false })} progress={{}} now={0} />);
    expect(screen.getByRole('button', { name: 'Start queue' })).toBeInTheDocument();
    expect(screen.getByText('Waiting for mkvmerge — see the message above.')).toBeInTheDocument();
    expect(screen.getByText('Nothing queued yet.')).toBeInTheDocument();
    rerender(<QueuePane queue={queue([])} progress={{}} now={0} />);
    expect(screen.queryByRole('button', { name: 'Clear finished' })).not.toBeInTheDocument();
  });
});
