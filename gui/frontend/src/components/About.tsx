import { useState } from 'react';
import { api } from '../api';

const repoURL = 'https://github.com/chad3814/zenvik';

export function About() {
  const [version, setVersion] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const show = async () => {
    setOpen(true);
    setVersion(await api.version());
  };
  return (
    <>
      <button onClick={() => void show()}>About</button>
      {open && (
        <div role="dialog" aria-label="About Zenvik" className="dialog">
          <p><strong>Zenvik {version ?? ''}</strong></p>
          <p>Rips unencrypted Blu-ray and DVD images to MKV with mkvmerge.</p>
          <p>
            <a
              href={repoURL}
              onClick={(e) => {
                e.preventDefault();
                api.openURL(repoURL);
              }}
            >
              github.com/chad3814/zenvik
            </a>
          </p>
          <button onClick={() => setOpen(false)}>Close</button>
        </div>
      )}
    </>
  );
}
