// Saved browser choices: type, regions, Show Full / Show Empty, sort and Find Fresh Starts
// (SPEC §7.5 steps 1 and 6, §8 Browser UI). Pure: callers pass the storage (localStorage or a stand-in).

import { sortColumns } from './sort.js';

export const PREFS_KEY = 'serverdog.prefs';

// type is null until the visitor picks one, which is when the type picker shows.
export const DEFAULT_PREFS = Object.freeze({
  type: null,
  regions: [],
  showFull: true,
  showEmpty: true,
  sort: { column: 'name', dir: 'asc' },
  freshStarts: false, // when on, the Fresh Starts order replaces the column sort
});

const defaults = () => ({ ...DEFAULT_PREFS, regions: [], sort: { ...DEFAULT_PREFS.sort } });

// loadPrefs reads saved prefs, keeping each valid field and defaulting the rest. Missing,
// corrupt or unreadable storage gives the defaults.
export function loadPrefs(storage) {
  const prefs = defaults();
  let saved;
  try {
    saved = JSON.parse(storage.getItem(PREFS_KEY));
  } catch {
    return prefs;
  }
  if (!saved || typeof saved !== 'object') return prefs;
  if (saved.type === 'o' || saved.type === 'c') prefs.type = saved.type;
  if (Array.isArray(saved.regions)) prefs.regions = [...new Set(saved.regions.filter((r) => typeof r === 'string'))];
  if (typeof saved.showFull === 'boolean') prefs.showFull = saved.showFull;
  if (typeof saved.showEmpty === 'boolean') prefs.showEmpty = saved.showEmpty;
  if (typeof saved.freshStarts === 'boolean') prefs.freshStarts = saved.freshStarts;
  const sort = saved.sort;
  if (sort && sortColumns.includes(sort.column) && (sort.dir === 'asc' || sort.dir === 'desc')) {
    prefs.sort = { column: sort.column, dir: sort.dir };
  }
  return prefs;
}

// savePrefs stores prefs. Storage errors (private mode, quota) are ignored: the choices
// still apply for this visit.
export function savePrefs(storage, prefs) {
  try {
    storage.setItem(PREFS_KEY, JSON.stringify(prefs));
  } catch {
    // Not saved; nothing else to do.
  }
}

// nextSort is the sort after a header click: the active column flips direction, a new
// column starts ascending.
export function nextSort(sort, column) {
  if (sort && sort.column === column) return { column, dir: sort.dir === 'asc' ? 'desc' : 'asc' };
  return { column, dir: 'asc' };
}
