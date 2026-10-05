import { api } from '../api';
import type { Banner } from '../types';

function action(b: Banner) {
  switch (b.action) {
    case 'recheck':
      return <button onClick={() => void api.recheckMkvmerge()}>Recheck</button>;
    case 'download': {
      const url = b.url;
      return url ? <button onClick={() => api.openURL(url)}>Download</button> : null;
    }
    default:
      return null;
  }
}

export function Banners({ banners }: { banners: Banner[] }) {
  if (banners.length === 0) return null;
  return (
    <div className="banners">
      {banners.map((b) => (
        <div key={b.id} role="alert" className="banner">
          <span>{b.message}</span>
          {action(b)}
        </div>
      ))}
    </div>
  );
}
