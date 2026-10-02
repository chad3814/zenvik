import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { TitlesPanel } from '../components/TitlesPanel';
import { setPick, type Picks } from '../picks';
import type { DiscSummary, TitleSummary } from '../types';

const title = (id: string, extra: Partial<TitleSummary> = {}): TitleSummary => ({
  id, durationSeconds: 2520, chapters: 7, sizeBytes: 1.2e9, main: false, rippable: true,
  skippedCells: [], defaultName: `Firefly (${id}).mkv`, ...extra,
});
const disc: DiscSummary = {
  path: '/Firefly_D1', state: 'ready', name: 'Firefly', format: 'DVD', ambiguous: false, outputDir: '/Movies',
  titles: [
    title('01', { main: true, defaultName: 'Firefly.mkv', durationSeconds: 7581, chapters: 18 }),
    title('02', { skippedCells: ['skipped cell 22 (1.0 s at sectors 0–438)'] }),
    title('05', { rippable: false, reason: 'interleaved angle block (not supported yet)' }),
  ],
};

function Harness({ d = disc, errors = {}, onEnqueue = vi.fn() }: { d?: DiscSummary; errors?: Record<string, string>; onEnqueue?: () => void }) {
  const [picks, setPicks] = useState<Picks>({});
  return (
    <TitlesPanel
      disc={d}
      picks={picks[d.path]}
      errors={errors}
      onTick={(id, ticked) => setPicks((p) => setPick(p, d.path, id, { ticked }))}
      onName={(id, name) => setPicks((p) => setPick(p, d.path, id, { name }))}
      onPickOutputDir={vi.fn()}
      onEnqueue={onEnqueue}
    />
  );
}

describe('TitlesPanel', () => {
  it('ticks the main title with its default name and greys unsupported titles', () => {
    render(<Harness />);
    expect(screen.getByRole('checkbox', { name: 'Title 01' })).toBeChecked();
    expect(screen.getByRole('textbox', { name: 'Name for title 01' })).toHaveValue('Firefly.mkv');
    expect(screen.getByText('2:06:21')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Title 05' })).toBeDisabled();
    expect(screen.getByText('interleaved angle block (not supported yet)')).toBeInTheDocument();
    expect(screen.getByTitle('skipped cell 22 (1.0 s at sectors 0–438)')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add 1 title to queue' })).toBeEnabled();
    expect(screen.getByText('/Movies')).toBeInTheDocument();
  });

  it('keeps an edited name when unticked and re-ticked', async () => {
    render(<Harness />);
    await userEvent.click(screen.getByRole('checkbox', { name: 'Title 02' }));
    const field = screen.getByRole('textbox', { name: 'Name for title 02' });
    await userEvent.clear(field);
    await userEvent.type(field, 'The Train Job.mkv');
    await userEvent.click(screen.getByRole('checkbox', { name: 'Title 02' }));
    expect(screen.queryByRole('textbox', { name: 'Name for title 02' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('checkbox', { name: 'Title 02' }));
    expect(screen.getByRole('textbox', { name: 'Name for title 02' })).toHaveValue('The Train Job.mkv');
    expect(screen.getByRole('button', { name: 'Add 2 titles to queue' })).toBeEnabled();
  });

  it('pre-ticks nothing on an ambiguous disc and says why', () => {
    render(<Harness d={{ ...disc, ambiguous: true }} />);
    expect(screen.getByText('No clear main title — tick the titles you want.')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Title 01' })).not.toBeChecked();
    expect(screen.getByRole('button', { name: 'Add 0 titles to queue' })).toBeDisabled();
  });

  it('shows field errors and calls onEnqueue', async () => {
    const onEnqueue = vi.fn();
    render(<Harness errors={{ '01': 'another queue entry already writes this file' }} onEnqueue={onEnqueue} />);
    expect(screen.getByText('another queue entry already writes this file')).toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: 'Name for title 01' })).toHaveAttribute('aria-invalid', 'true');
    await userEvent.click(screen.getByRole('button', { name: 'Add 1 title to queue' }));
    expect(onEnqueue).toHaveBeenCalled();
  });

  it('shows opening and error states instead of titles', () => {
    const { rerender } = render(<Harness d={{ ...disc, state: 'opening', titles: [] }} />);
    expect(screen.getByText('Opening the disc…')).toBeInTheDocument();
    rerender(<Harness d={{ ...disc, state: 'error', error: 'no playlists found', titles: [] }} />);
    expect(screen.getByText('no playlists found')).toBeInTheDocument();
  });
});
