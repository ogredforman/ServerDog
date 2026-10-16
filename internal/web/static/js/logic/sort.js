// Column sorts and the Find Fresh Starts order (SPEC §7.5 step 6, §8 Browser UI). Pure:
// both return a new array and leave the input untouched.

// A fixed collator keeps text order the same across browsers and locales. `numeric` compares
// digit runs by value, so "Server #2" sorts before "Server #10".
const text = new Intl.Collator('en', { numeric: true }).compare;

const byName = (a, b) => text(a.n || '', b.n || '') || text(a.id || '', b.id || '');
const byPlayersDesc = (a, b) => b.p - a.p;
const byNewestStart = (a, b) => (b.ms ?? 0) - (a.ms ?? 0);

const mapName = (s) => s.map || s.base || '';

// Column keys, matching the list headers. `timer` sorts by match start: ascending shows the newest match (shortest
// elapsed) first. Servers with no start sort last in either direction.
const columns = {
  name: (a, b) => text(a.n || '', b.n || ''),
  players: (a, b) => a.p - b.p,
  mode: (a, b) => text(a.exp || '', b.exp || ''),
  map: (a, b) => text(mapName(a), mapName(b)),
  password: (a, b) => Number(Boolean(a.pw)) - Number(Boolean(b.pw)),
  timer: (a, b) => byNewestStart(a, b),
};

export const sortColumns = Object.keys(columns);

const hasStart = (s) => s.st !== 'unknown' && s.ms != null;

// sortServers orders servers by a column, `dir` 'asc' or 'desc'. Ties fall back to name, then id.
export function sortServers(servers, { column = 'name', dir = 'asc' } = {}) {
  const cmp = columns[column];
  if (!cmp) throw new Error(`unknown sort column: ${column}`);
  const sign = dir === 'desc' ? -1 : 1;
  return [...servers].sort((a, b) => {
    if (column === 'timer' && hasStart(a) !== hasStart(b)) return hasStart(a) ? -1 : 1;
    return sign * cmp(a, b) || byName(a, b);
  });
}

const stateRank = { starting: 0, exact: 1, estimate: 2, unknown: 3 };
const rank = (s) => (s.ms == null ? stateRank.unknown : stateRank[s.st] ?? stateRank.unknown);

// freshStartsOrder: starting → exact → estimate (each newest `ms` first) → unknown (players
// descending). Ties are broken by players descending, then name. It only reorders.
export function freshStartsOrder(servers) {
  return [...servers].sort((a, b) => {
    const ra = rank(a);
    const rb = rank(b);
    if (ra !== rb) return ra - rb;
    return (ra === stateRank.unknown ? 0 : byNewestStart(a, b)) || byPlayersDesc(a, b) || byName(a, b);
  });
}
