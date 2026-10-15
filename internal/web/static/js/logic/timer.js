// Match timer formatting (SPEC §8 Browser UI). Pure: callers pass `now` in Unix milliseconds.

const pad2 = (n) => String(n).padStart(2, '0');

// formatDuration renders whole seconds as m:ss below one hour and h:mm:ss from one hour up.
// Negative input (clock skew) renders as 0:00.
export function formatDuration(sec) {
  const total = Math.max(0, Math.floor(sec));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  return h > 0 ? `${h}:${pad2(m)}:${pad2(s)}` : `${m}:${pad2(s)}`;
}

// formatTimer returns the timer cell for a published server: `text` to show and `kind`
// (exact / estimate / starting / unknown) for styling. `ms` is Unix seconds or null.
export function formatTimer(server, now) {
  switch (server.st) {
    case 'starting':
      return { text: 'STARTING', kind: 'starting' };
    case 'exact':
    case 'estimate': {
      if (server.ms == null) break;
      const text = formatDuration(now / 1000 - server.ms);
      return server.st === 'estimate' ? { text: `~${text}`, kind: 'estimate' } : { text, kind: 'exact' };
    }
  }
  return { text: '—', kind: 'unknown' };
}
