import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { Banners } from '../components/Banners';

vi.mock('../api', () => ({ api: { recheckMkvmerge: vi.fn(() => Promise.resolve()) } }));

describe('Banners', () => {
  it('shows each banner and a Recheck button for mkvmerge', async () => {
    render(<Banners banners={[{ id: 'config', message: 'bad key' }, { id: 'mkvmerge', message: 'mkvmerge missing', action: 'recheck' }]} />);
    expect(screen.getAllByRole('alert')).toHaveLength(2);
    await userEvent.click(screen.getByRole('button', { name: 'Recheck' }));
    const { api } = await import('../api');
    expect(api.recheckMkvmerge).toHaveBeenCalled();
  });
});
