import { useState, type KeyboardEvent } from 'react';
import { api } from '../api';
import { basename, clamp01, eta, formatBytes, formatSpan } from '../format';
import type { LiveProgress } from '../store';
import type { Entry, QueueSnapshot } from '../types';

interface Props {
  queue: QueueSnapshot;
  progress: Record<string, LiveProgress>;
  now: number;
}

interface Renaming {
  id: string;
  value: string;
  error: string;
}

const finishedStates: Entry['state'][] = ['done', 'failed', 'canceled'];

export function QueuePane({ queue, progress, now }: Props) {
  const [renaming, setRenaming] = useState<Renaming | null>(null);
  const [revealError, setRevealError] = useState<{ id: string; message: string } | null>(null);
  const finished = queue.entries.some((e) => finishedStates.includes(e.state));

  const submitRename = async (r: Renaming) => {
    const msg = await api.rename(r.id, r.value);
    setRenaming(msg ? { ...r, error: msg } : null);
  };

  const reveal = async (id: string) => {
    const msg = await api.reveal(id);
    setRevealError(msg ? { id, message: msg } : null);
  };

  return (
    <section className="queue" aria-label="Queue">
      <header>
        <h2>Queue</h2>
        <button onClick={() => void api.setPaused(!queue.paused)}>{queue.paused ? 'Start queue' : 'Pause queue'}</button>
        {finished && <button onClick={() => void api.clearFinished()}>Clear finished</button>}
      </header>
      {!queue.ready && <p className="hint">Waiting for mkvmerge — see the message above.</p>}
      {queue.entries.length === 0 ? (
        <p className="muted">Nothing queued yet.</p>
      ) : (
        <ol className="entries">
          {queue.entries.map((e, i) => (
            <li
              key={e.id}
              data-state={e.state}
              draggable={e.state === 'waiting'}
              onDragStart={(ev) => {
                ev.dataTransfer.setData('text/plain', e.id);
                ev.dataTransfer.effectAllowed = 'move';
              }}
              onDragOver={(ev) => ev.preventDefault()}
              onDrop={(ev) => {
                ev.preventDefault();
                const id = ev.dataTransfer.getData('text/plain');
                if (id && id !== e.id) void api.move(id, i);
              }}
            >
              {renaming?.id === e.id ? (
                <span className="rename">
                  <input
                    type="text"
                    aria-label={`New name for ${basename(e.outputPath)}`}
                    autoFocus
                    value={renaming.value}
                    onChange={(ev) => setRenaming({ ...renaming, value: ev.target.value })}
                    onKeyDown={(ev: KeyboardEvent<HTMLInputElement>) => {
                      if (ev.key === 'Enter') void submitRename(renaming);
                      if (ev.key === 'Escape') setRenaming(null);
                    }}
                  />
                  {renaming.error && <span className="error">{renaming.error}</span>}
                </span>
              ) : (
                <span className="entry-name" title={e.outputPath}>{basename(e.outputPath)}</span>
              )}
              <EntryStatus entry={e} progress={progress[e.id]} now={now} />
              <span className="actions">
                {e.state === 'waiting' && (
                  <>
                    <button onClick={() => setRenaming({ id: e.id, value: basename(e.outputPath), error: '' })}>Rename</button>
                    <button aria-label="Move up" disabled={i === 0} onClick={() => void api.move(e.id, i - 1)}>↑</button>
                    <button aria-label="Move down" disabled={i === queue.entries.length - 1} onClick={() => void api.move(e.id, i + 1)}>↓</button>
                  </>
                )}
                {e.state === 'ripping' && <button onClick={() => void api.cancel(e.id)}>Cancel</button>}
                {e.state === 'done' && <button onClick={() => void reveal(e.id)}>Show</button>}
                {(e.state === 'failed' || e.state === 'canceled') && <button onClick={() => void api.retry(e.id)}>Retry</button>}
                {e.state !== 'ripping' && (
                  <button aria-label={`Remove ${basename(e.outputPath)}`} onClick={() => void api.remove(e.id)}>✕</button>
                )}
              </span>
              {revealError?.id === e.id && <span className="error">{revealError.message}</span>}
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function EntryStatus({ entry: e, progress: p, now }: { entry: Entry; progress: LiveProgress | undefined; now: number }) {
  switch (e.state) {
    case 'waiting':
      return <span className="muted">waiting</span>;
    case 'ripping': {
      if (!p) return <span className="muted">starting…</span>;
      const f = clamp01(p.fraction);
      const left = eta(f, p.phaseStartedAt, now);
      return (
        <span className="progress">
          <span>{p.phase}</span>
          <progress max={1} value={f} aria-label={`${p.phase} progress`} />
          <span>{Math.floor(f * 100)}%</span>
          {p.bytesTotal > 0 && <span className="muted">{formatBytes(p.bytesDone)} / {formatBytes(p.bytesTotal)}</span>}
          {left && <span className="muted">ETA {left}</span>}
        </span>
      );
    }
    case 'done': {
      const took = e.startedAt && e.endedAt ? (Date.parse(e.endedAt) - Date.parse(e.startedAt)) / 1000 : 0;
      return <span className="ok">done in {formatSpan(took)}</span>;
    }
    case 'failed':
      return (
        <span className="error" title={e.message}>
          {e.label ?? 'failed'}
        </span>
      );
    case 'canceled':
      return <span className="muted">canceled</span>;
  }
}
