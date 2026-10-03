import { api } from '../api';
import type { Banner } from '../types';

export function Banners({ banners }: { banners: Banner[] }) {
  if (banners.length === 0) return null;
  return (
    <div className="banners">
      {banners.map((b) => (
        <div key={b.id} role="alert" className="banner">
          <span>{b.message}</span>
          {b.action === 'recheck' && <button onClick={() => void api.recheckMkvmerge()}>Recheck</button>}
        </div>
      ))}
    </div>
  );
}
