import { useEffect, useState } from 'react';
import { api, offFileDrop, on, onFileDrop } from './api';
import { About } from './components/About';
import { Banners } from './components/Banners';
import { DiscList } from './components/DiscList';
import { QueuePane } from './components/QueuePane';
import { TitlesPanel } from './components/TitlesPanel';
import { clearTicks, nameFor, setPick, tickedTitles, type Picks } from './picks';
import { useAppState } from './store';
import type { DiscSummary } from './types';

export default function App() {
  const state = useAppState();
  const [selected, setSelected] = useState<string | null>(null);
  const [picks, setPicks] = useState<Picks>({});
  const [errors, setErrors] = useState<Record<string, Record<string, string>>>({});
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => on<string>('discs:select', setSelected), []);
  useEffect(() => {
    onFileDrop((paths) => void api.addPaths(paths));
    const focus = () => void api.reloadConfig();
    window.addEventListener('focus', focus);
    return () => {
      offFileDrop();
      window.removeEventListener('focus', focus);
    };
  }, []);
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  const disc = state.discs.find((d) => d.path === selected) ?? state.discs[0];
  const queued = state.queue.entries.filter((e) => e.state === 'waiting' || e.state === 'ripping').length;

  const enqueue = async (d: DiscSummary) => {
    const titles = tickedTitles(d, picks[d.path]);
    const msgs = await api.enqueue(d.path, titles.map((t) => t.id), titles.map((t) => nameFor(t, picks[d.path])));
    if (!msgs || msgs.every((m) => m === '')) {
      setPicks((p) => clearTicks(p, d));
      setErrors((e) => ({ ...e, [d.path]: {} }));
      return;
    }
    const byTitle: Record<string, string> = {};
    titles.forEach((t, i) => {
      if (msgs[i]) byTitle[t.id] = msgs[i];
    });
    setErrors((e) => ({ ...e, [d.path]: byTitle }));
  };

  return (
    <div className="app">
      <header className="top">
        <h1>Zenvik</h1>
        <span className="muted">
          {state.discs.length} {state.discs.length === 1 ? 'disc' : 'discs'} · {queued} queued
        </span>
        <About />
      </header>
      <Banners banners={state.banners} />
      <main className="panes">
        <div className="left">
          <DiscList
            discs={state.discs}
            selected={disc?.path ?? null}
            onSelect={setSelected}
            onRemove={(path) => void api.removeDisc(path)}
            onPickISOs={() => void api.pickISOs()}
            onPickFolder={() => void api.pickFolder()}
          />
          {disc && (
            <TitlesPanel
              disc={disc}
              picks={picks[disc.path]}
              errors={errors[disc.path] ?? {}}
              onTick={(id, ticked) => setPicks((p) => setPick(p, disc.path, id, { ticked }))}
              onName={(id, name) => setPicks((p) => setPick(p, disc.path, id, { name }))}
              onPickOutputDir={() => void api.pickOutputDir(disc.path)}
              onEnqueue={() => void enqueue(disc)}
            />
          )}
        </div>
        <QueuePane queue={state.queue} progress={state.progress} now={now} />
      </main>
    </div>
  );
}
