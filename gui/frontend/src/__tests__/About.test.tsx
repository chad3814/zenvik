import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { About } from '../components/About';

vi.mock('../api', () => ({
  api: {
    version: vi.fn(() => Promise.resolve('v1.2.0')),
    mkvmergeInfo: vi.fn(() => Promise.resolve('')),
    mkvmergeSourceURL: vi.fn(() => Promise.resolve('')),
    checkForUpdate: vi.fn(() => Promise.resolve('')),
    openURL: vi.fn(),
  },
}));

describe('About', () => {
  let api: typeof import('../api').api;
  beforeEach(async () => {
    api = (await import('../api')).api;
    vi.clearAllMocks();
  });

  const open = async () => {
    render(<About />);
    await userEvent.click(screen.getByRole('button', { name: 'About' }));
    await screen.findByText('Zenvik v1.2.0');
  };

  it('says when this build is the latest', async () => {
    await open();
    await userEvent.click(screen.getByRole('link', { name: 'Check for updates' }));
    expect(await screen.findByText('You have the latest version')).toBeInTheDocument();
    expect(api.checkForUpdate).toHaveBeenCalledTimes(1);
  });

  it('names the newer release and links to the releases page', async () => {
    vi.mocked(api.checkForUpdate).mockResolvedValueOnce('v1.3.0');
    await open();
    await userEvent.click(screen.getByRole('link', { name: 'Check for updates' }));
    expect(await screen.findByText('v1.3.0 is available')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('link', { name: 'Download' }));
    expect(api.openURL).toHaveBeenCalledWith('https://github.com/chad3814/zenvik/releases/latest');
  });

  it('shows the error text when the check fails', async () => {
    vi.mocked(api.checkForUpdate).mockRejectedValueOnce('update check failed: HTTP 500');
    await open();
    await userEvent.click(screen.getByRole('link', { name: 'Check for updates' }));
    expect(await screen.findByText('update check failed: HTTP 500')).toBeInTheDocument();
  });
});
