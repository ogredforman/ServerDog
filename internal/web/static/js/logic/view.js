// What the server list shows for the current prefs (SPEC §8 Browser UI, §11). Pure: callers
// pass the data and `now` in Unix milliseconds.

import { filterServers } from './filters.js';
import { sortServers } from './sort.js';
import { formatTimer } from './timer.js';

// viewServers filters then sorts servers for prefs. Saved regions that aren't in the data
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
  return sortServers(shown, prefs.sort);
}

// rowCells is the text of each cell in a server row.
export function rowCells(server, now) {
  return {
    name: server.n || server.id || '',
    players: `${server.p}/${server.mp}`,
    map: [server.map || server.base, server.exp].filter(Boolean).join(' · '),
    rulesets: (server.rs || []).join(', '),
    password: server.pw ? 'Yes' : '',
    timer: formatTimer(server, now),
  };
}

const typeNames = { o: 'Official', c: 'Community' };

// activeFilters lists the filters in effect, for the empty state.
export function activeFilters(prefs, regions = []) {
  const present = new Set(regions);
  const chosen = prefs.regions.filter((r) => present.has(r));
  const out = [];
  if (prefs.type) out.push(typeNames[prefs.type] || prefs.type);
  if (chosen.length > 0) out.push(`Regions: ${chosen.join(', ')}`);
  if (!prefs.showFull) out.push('Hiding full');
  if (!prefs.showEmpty) out.push('Hiding empty');
  return out;
}
