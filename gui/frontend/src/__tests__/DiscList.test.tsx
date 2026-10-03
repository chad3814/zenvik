import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { DiscList } from '../components/DiscList';
import type { DiscSummary } from '../types';

const d = (path: string, extra: Partial<DiscSummary>): DiscSummary => ({
  path, state: 'ready', ambiguous: false, outputDir: '/o', titles: [], name: path.slice(1), ...extra,
});

describe('DiscList', () => {
  it('shows each disc state and wires the buttons', async () => {
    const h = { onSelect: vi.fn(), onRemove: vi.fn(), onPickISOs: vi.fn(), onPickFolder: vi.fn() };
    render(
      <DiscList
        discs={[
          d('/Movie', { format: 'Blu-ray' }),
          d('/Opening', { state: 'opening' }),
          d('/Locked', { state: 'error', errorLabel: 'encrypted', error: 'disc is encrypted; zenvik only reads unencrypted discs' }),
        ]}
        selected="/Movie"
        {...h}
      />,
    );
    expect(screen.getByRole('button', { name: /^Movie/ })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByText('Blu-ray')).toBeInTheDocument();
    expect(screen.getByText('opening…')).toBeInTheDocument();
    expect(screen.getByText('encrypted')).toHaveAttribute('title', 'disc is encrypted; zenvik only reads unencrypted discs');
    await userEvent.click(screen.getByRole('button', { name: /^Opening/ }));
    expect(h.onSelect).toHaveBeenCalledWith('/Opening');
    await userEvent.click(screen.getByRole('button', { name: 'Remove Locked' }));
    expect(h.onRemove).toHaveBeenCalledWith('/Locked');
    await userEvent.click(screen.getByRole('button', { name: 'Add ISO…' }));
    await userEvent.click(screen.getByRole('button', { name: 'Add folder…' }));
    expect(h.onPickISOs).toHaveBeenCalled();
    expect(h.onPickFolder).toHaveBeenCalled();
  });

  it('falls back to the error message when a failed disc has no label', () => {
    render(<DiscList discs={[d('/Bad', { state: 'error', error: 'cannot read' })]} selected={null} onSelect={vi.fn()} onRemove={vi.fn()} onPickISOs={vi.fn()} onPickFolder={vi.fn()} />);
    expect(screen.getByText('cannot read')).toBeInTheDocument();
  });
  it('shows a generic label when a failed disc has neither label nor message', () => {
    render(<DiscList discs={[d('/Bad', { state: 'error' })]} selected={null} onSelect={vi.fn()} onRemove={vi.fn()} onPickISOs={vi.fn()} onPickFolder={vi.fn()} />);
    expect(screen.getByText('error')).toBeInTheDocument();
  });
});
