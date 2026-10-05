import { useState } from 'react';
import { api } from '../api';

const repoURL = 'https://github.com/chad3814/zenvik';
const releasesURL = 'https://github.com/chad3814/zenvik/releases/latest';

interface Info {
  version: string;
  mkvmerge: string;
  sourceURL: string;
}

type UpdateState =
  | { kind: 'idle' }
  | { kind: 'checking' }
  | { kind: 'current' }
  | { kind: 'available'; tag: string }
  | { kind: 'error'; message: string };

export function About() {
  const [info, setInfo] = useState<Info | null>(null);
  const [open, setOpen] = useState(false);
  const [update, setUpdate] = useState<UpdateState>({ kind: 'idle' });
  const show = async () => {
    setOpen(true);
    setUpdate({ kind: 'idle' });
    const [version, mkvmerge, sourceURL] = await Promise.all([
      api.version(),
      api.mkvmergeInfo(),
      api.mkvmergeSourceURL(),
    ]);
    setInfo({ version, mkvmerge, sourceURL });
  };
  const check = async () => {
    setUpdate({ kind: 'checking' });
    try {
      const tag = await api.checkForUpdate();
      setUpdate(tag ? { kind: 'available', tag } : { kind: 'current' });
    } catch (e) {
      setUpdate({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
    }
  };
  const link = (url: string, text: string) => (
    <a
      href={url}
      onClick={(e) => {
        e.preventDefault();
        api.openURL(url);
      }}
    >
      {text}
    </a>
  );
  const updateText = () => {
    switch (update.kind) {
      case 'idle':
        return null;
      case 'checking':
        return <> · Checking…</>;
      case 'current':
        return <> · <span>You have the latest version</span></>;
      case 'available':
        return <> · <span>{update.tag} is available</span> · {link(releasesURL, 'Download')}</>;
      case 'error':
        return <> · <span>{update.message}</span></>;
    }
  };
  return (
    <>
      <button onClick={() => void show()}>About</button>
      {open && (
        <div role="dialog" aria-label="About Zenvik" className="dialog">
          <p><strong>Zenvik {info?.version ?? ''}</strong></p>
          <p>Rips unencrypted Blu-ray and DVD images to MKV with mkvmerge.</p>
          {info?.mkvmerge && (
            <p>
              <span>{info.mkvmerge}</span>
              {info.sourceURL && <> · {link(info.sourceURL, 'MKVToolNix source')}</>}
            </p>
          )}
          <p>
            <a
              href="#check-for-updates"
              onClick={(e) => {
                e.preventDefault();
                void check();
              }}
            >
              Check for updates
            </a>
            {updateText()}
          </p>
          <p>{link(repoURL, 'github.com/chad3814/zenvik')}</p>
          <button onClick={() => setOpen(false)}>Close</button>
        </div>
      )}
    </>
  );
}
