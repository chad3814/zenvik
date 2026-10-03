const two = (n: number): string => String(n).padStart(2, '0');

export function clamp01(f: number): number {
  return Number.isFinite(f) ? Math.min(1, Math.max(0, f)) : 0;
}

/** formatClock shows a title's length as h:mm:ss. */
export function formatClock(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  return `${Math.floor(s / 3600)}:${two(Math.floor((s % 3600) / 60))}:${two(s % 60)}`;
}

/** formatSpan shows an elapsed or remaining time compactly: 5s, 4m12s, 1h02m. */
export function formatSpan(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (h > 0) return `${h}h${two(m)}m`;
  if (m > 0) return `${m}m${two(s % 60)}s`;
  return `${s}s`;
}

export function formatBytes(bytes: number): string {
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(1)} GB`;
  if (bytes >= 1e6) return `${Math.round(bytes / 1e6)} MB`;
  return `${Math.round(bytes / 1e3)} kB`;
}

/** eta estimates the time left in the current phase; null until 2% is done. */
export function eta(fraction: number, phaseStartedAt: number, now: number): string | null {
  const f = clamp01(fraction);
  if (f < 0.02) return null;
  const elapsed = (now - phaseStartedAt) / 1000;
  return formatSpan((elapsed * (1 - f)) / f);
}

export function basename(p: string): string {
  return p.slice(Math.max(p.lastIndexOf('/'), p.lastIndexOf('\\')) + 1);
}
