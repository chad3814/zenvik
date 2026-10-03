import { formatBytes, formatClock } from '../format';
import { isTicked, nameFor, type DiscPicks } from '../picks';
import type { DiscSummary } from '../types';

interface Props {
  disc: DiscSummary;
  picks: DiscPicks | undefined;
  errors: Record<string, string>;
  onTick: (id: string, ticked: boolean) => void;
  onName: (id: string, name: string) => void;
  onPickOutputDir: () => void;
  onEnqueue: () => void;
}

export function TitlesPanel({ disc, picks, errors, onTick, onName, onPickOutputDir, onEnqueue }: Props) {
  const n = disc.titles.filter((t) => isTicked(disc, t, picks)).length;
  return (
    <section className="titles" aria-label="Titles">
      <header>
        <h2>Titles — {disc.name ?? disc.path}</h2>
        <span className="folder">
          Folder <code>{disc.outputDir}</code> <button onClick={onPickOutputDir}>Change…</button>
        </span>
      </header>
      {disc.state === 'opening' && <p className="muted">Opening the disc…</p>}
      {disc.state === 'error' && <p className="error">{disc.error}</p>}
      {disc.state === 'ready' && (
        <>
          {disc.ambiguous && <p className="hint">No clear main title — tick the titles you want.</p>}
          <ul className="title-rows">
            {disc.titles.map((t) => {
              const ticked = isTicked(disc, t, picks);
              const err = errors[t.id];
              return (
                <li key={t.id} className={t.rippable ? '' : 'disabled'}>
                  <label className="title-row">
                    <input
                      type="checkbox"
                      aria-label={`Title ${t.id}`}
                      checked={ticked}
                      disabled={!t.rippable}
                      onChange={(e) => onTick(t.id, e.target.checked)}
                    />
                    <span className="id">{t.id}</span>
                    <span>{formatClock(t.durationSeconds)}</span>
                    <span className="muted">{t.chapters} ch</span>
                    <span className="muted">{formatBytes(t.sizeBytes)}</span>
                    {t.main && <span className="badge">main</span>}
                    {t.skippedCells.length > 0 && (
                      <span className="warn" title={t.skippedCells.join('\n')}>⚠</span>
                    )}
                    {t.reason && <span className="muted">{t.reason}</span>}
                  </label>
                  {ticked && (
                    <div className="name-field">
                      <input
                        type="text"
                        aria-label={`Name for title ${t.id}`}
                        aria-invalid={err ? 'true' : 'false'}
                        value={nameFor(t, picks)}
                        onChange={(e) => onName(t.id, e.target.value)}
                      />
                      {err && <span className="error">{err}</span>}
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
          <button className="primary" disabled={n === 0} onClick={onEnqueue}>
            Add {n} {n === 1 ? 'title' : 'titles'} to queue
          </button>
        </>
      )}
    </section>
  );
}
