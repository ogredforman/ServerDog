// What the server list shows for the current prefs (SPEC §8 Browser UI, §11). Pure: callers
// pass the data and `now` in Unix milliseconds.

import { filterServers, isFull, isEmpty } from './filters.js';
import { sortServers, freshStartsOrder } from './sort.js';
import { regionName } from './regions.js';
import { isStale } from './stale.js';
import { formatDuration, formatTimer } from './timer.js';

// viewServers filters then sorts servers for prefs: the Fresh Starts order when
// prefs.freshStarts is on, the column sort otherwise. Saved regions that aren't in the data
// are ignored, so a region that has gone away doesn't hide everything.
export function viewServers(servers, prefs, regions = []) {
  const present = new Set(regions);
  const chosen = prefs.regions.filter((r) => present.has(r));
  const shown = filterServers(servers, {
    type: prefs.type,
    regions: chosen,
    showFull: prefs.showFull,
    showEmpty: prefs.showEmpty,
  });
  return prefs.freshStarts ? freshStartsOrder(shown) : sortServers(shown, prefs.sort);
}

// rowCells is what each cell in a server row shows, in header order: password and official
// flags, the name with its ruleset chips, mode, map, players and the timer, plus the full /
// empty flags that style the row.
export function rowCells(server, now) {
  return {
    full: isFull(server),
    empty: isEmpty(server),
    password: Boolean(server.pw),
    official: server.t === 'o',
    name: server.n || server.id || '',
    rulesets: [...(server.rs || [])],
    mode: server.exp || '',
    map: server.map || server.base || '',
    players: `${server.p}/${server.mp}`,
    timer: formatTimer(server, now),
  };
}

// sortIndicator is a header's arrow, as in-game: ▶ unsorted, ▲ descending, ▼ ascending.
// Every header shows ▶ while the Fresh Starts order is on.
export function sortIndicator(prefs, column) {
  if (prefs.freshStarts || prefs.sort.column !== column) return '▶';
  return prefs.sort.dir === 'desc' ? '▲' : '▼';
}

const typeNames = { o: 'Official', c: 'Community' };

// activeFilters lists the filters in effect, for the empty state.
export function activeFilters(prefs, regions = []) {
  const present = new Set(regions);
  const chosen = prefs.regions.filter((r) => present.has(r));
  const out = [];
  if (prefs.type) out.push(typeNames[prefs.type] || prefs.type);
  if (chosen.length > 0) out.push(`Regions: ${chosen.map(regionName).join(', ')}`);
  if (!prefs.showFull) out.push('Hiding full');
  if (!prefs.showEmpty) out.push('Hiding empty');
  return out;
}

// staleNotice is the navbar notice text when the data is stale (SPEC §7.5 step 5), or null.
export function staleNotice(data, now) {
  if (!isStale(data, now)) return null;
  if (!Number.isFinite(data.generatedAt)) return 'Server data may be out of date.';
  return `Server data may be out of date (last updated ${formatDuration(now / 1000 - data.generatedAt)} ago).`;
}
