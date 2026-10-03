import { describe, expect, it } from 'vitest';
import { clearTicks, isTicked, nameFor, setPick, tickedTitles } from '../picks';
import type { DiscSummary, TitleSummary } from '../types';

const title = (id: string, extra: Partial<TitleSummary> = {}): TitleSummary => ({
  id, durationSeconds: 60, chapters: 1, sizeBytes: 1, main: false, rippable: true,
  skippedCells: [], defaultName: `Disc (${id}).mkv`, ...extra,
});
const disc = (extra: Partial<DiscSummary> = {}): DiscSummary => ({
  path: '/d', state: 'ready', ambiguous: false, outputDir: '/out',
  titles: [title('01', { main: true, defaultName: 'Disc.mkv' }), title('02'), title('03', { rippable: false, reason: 'encrypted' })],
  ...extra,
});

describe('picks', () => {
  it('ticks the clear main title by default', () => {
    const d = disc();
    expect(tickedTitles(d, undefined).map((t) => t.id)).toEqual(['01']);
  });
  it('ticks nothing on an ambiguous disc', () => {
    expect(tickedTitles(disc({ ambiguous: true }), undefined)).toEqual([]);
  });
  it('never ticks unrippable titles', () => {
    const d = disc();
    expect(isTicked(d, d.titles[2], { '03': { ticked: true } })).toBe(false);
  });
  it('keeps edited names through unticking', () => {
    const d = disc();
    let p = setPick({}, '/d', '02', { ticked: true, name: 'Extra.mkv' });
    p = setPick(p, '/d', '02', { ticked: false });
    expect(nameFor(d.titles[1], p['/d'])).toBe('Extra.mkv');
    expect(nameFor(d.titles[0], p['/d'])).toBe('Disc.mkv');
  });
  it('clears every tick but keeps names', () => {
    const d = disc();
    let p = setPick({}, '/d', '02', { ticked: true, name: 'Extra.mkv' });
    p = clearTicks(p, d);
    expect(tickedTitles(d, p['/d'])).toEqual([]);
    expect(p['/d']['02'].name).toBe('Extra.mkv');
  });
});
