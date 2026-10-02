import type { DiscSummary } from '../types';

interface Props {
  discs: DiscSummary[];
  selected: string | null;
  onSelect: (path: string) => void;
  onRemove: (path: string) => void;
  onPickISOs: () => void;
  onPickFolder: () => void;
}

function status(d: DiscSummary) {
  switch (d.state) {
    case 'opening':
      return <span className="muted">opening…</span>;
    case 'error':
      return <span className="error" title={d.error}>{d.errorLabel}</span>;
    default:
      return <span className="muted">{d.format}</span>;
  }
}

export function DiscList({ discs, selected, onSelect, onRemove, onPickISOs, onPickFolder }: Props) {
  return (
    <section className="discs" aria-label="Discs">
      <h2>Discs</h2>
      <ul>
        {discs.map((d) => {
          const name = d.name ?? d.path;
          return (
            <li key={d.path}>
              <button className="disc" aria-pressed={d.path === selected} onClick={() => onSelect(d.path)} title={d.path}>
                <span className="disc-name">{name}</span> {status(d)}
              </button>
              <button className="icon" aria-label={`Remove ${name}`} onClick={() => onRemove(d.path)}>
                ✕
              </button>
            </li>
          );
        })}
      </ul>
      <div className="add">
        <button onClick={onPickISOs}>Add ISO…</button>
        <button onClick={onPickFolder}>Add folder…</button>
        <span className="muted">or drop ISOs and disc folders anywhere in the window</span>
      </div>
    </section>
  );
}
