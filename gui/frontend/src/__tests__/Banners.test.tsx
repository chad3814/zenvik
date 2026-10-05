import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { Banners } from '../components/Banners';

vi.mock('../api', () => ({ api: { recheckMkvmerge: vi.fn(() => Promise.resolve()), openURL: vi.fn() } }));

describe('Banners', () => {
  it('shows each banner and a Recheck button for mkvmerge', async () => {
    render(<Banners banners={[{ id: 'config', message: 'bad key' }, { id: 'mkvmerge', message: 'mkvmerge missing', action: 'recheck' }]} />);
    expect(screen.getAllByRole('alert')).toHaveLength(2);
    await userEvent.click(screen.getByRole('button', { name: 'Recheck' }));
    const { api } = await import('../api');
    expect(api.recheckMkvmerge).toHaveBeenCalled();
  });

  it('offers a Download button that opens the update URL', async () => {
    const url = 'https://github.com/chad3814/zenvik/releases/latest';
    render(<Banners banners={[{ id: 'update', message: 'Zenvik v1.3.0 is available.', action: 'download', url }]} />);
    expect(screen.getByRole('alert')).toHaveTextContent('Zenvik v1.3.0 is available.');
    await userEvent.click(screen.getByRole('button', { name: 'Download' }));
    const { api } = await import('../api');
    expect(api.openURL).toHaveBeenCalledWith(url);
  });

  it('shows no button for a download banner without a URL', () => {
    render(<Banners banners={[{ id: 'update', message: 'Zenvik v1.3.0 is available.', action: 'download' }]} />);
    expect(screen.queryByRole('button')).toBeNull();
  });
});
