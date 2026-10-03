import type { DiscSummary, TitleSummary } from './types';

/** Pick is the user's choice for one title; unset fields mean "default". */
export interface Pick {
  ticked?: boolean;
  name?: string;
}
export type DiscPicks = Record<string, Pick>; // by title id
export type Picks = Record<string, DiscPicks>; // by disc path

export function isTicked(disc: DiscSummary, t: TitleSummary, picks: DiscPicks | undefined): boolean {
  if (!t.rippable) return false;
  const p = picks?.[t.id];
  if (p?.ticked !== undefined) return p.ticked;
  return t.main && !disc.ambiguous;
}

export function nameFor(t: TitleSummary, picks: DiscPicks | undefined): string {
  return picks?.[t.id]?.name ?? t.defaultName;
}

export function setPick(picks: Picks, path: string, id: string, change: Pick): Picks {
  const disc = picks[path] ?? {};
  return { ...picks, [path]: { ...disc, [id]: { ...disc[id], ...change } } };
}

export function tickedTitles(disc: DiscSummary, picks: DiscPicks | undefined): TitleSummary[] {
  return disc.titles.filter((t) => isTicked(disc, t, picks));
}

export function clearTicks(picks: Picks, disc: DiscSummary): Picks {
  let next = picks;
  for (const t of disc.titles) next = setPick(next, disc.path, t.id, { ticked: false });
  return next;
}
