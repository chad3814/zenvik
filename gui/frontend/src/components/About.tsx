import { useState } from 'react';
import { api } from '../api';

const repoURL = 'https://github.com/chad3814/zenvik';

interface Info {
  version: string;
  mkvmerge: string;
  sourceURL: string;
}

export function About() {
  const [info, setInfo] = useState<Info | null>(null);
  const [open, setOpen] = useState(false);
  const show = async () => {
    setOpen(true);
    const [version, mkvmerge, sourceURL] = await Promise.all([
      api.version(),
      api.mkvmergeInfo(),
      api.mkvmergeSourceURL(),
    ]);
    setInfo({ version, mkvmerge, sourceURL });
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
          <p>{link(repoURL, 'github.com/chad3814/zenvik')}</p>
          <button onClick={() => setOpen(false)}>Close</button>
        </div>
      )}
    </>
  );
}
