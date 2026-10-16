import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadPrefs, savePrefs, nextSort, DEFAULT_PREFS, PREFS_KEY } from '../internal/web/static/js/logic/prefs.js';

// memStorage is a localStorage stand-in holding one raw value.
const memStorage = (raw = null) => {
  const items = new Map(raw == null ? [] : [[PREFS_KEY, raw]]);
  return {
    getItem: (k) => (items.has(k) ? items.get(k) : null),
    setItem: (k, v) => items.set(k, String(v)),
  };
};
const throwing = {
  getItem() { throw new Error('denied'); },
  setItem() { throw new Error('denied'); },
};

test('loadPrefs', () => {
  const cases = [
    { name: 'nothing saved', raw: null, want: DEFAULT_PREFS },
    { name: 'corrupt JSON', raw: '{nope', want: DEFAULT_PREFS },
    { name: 'not an object', raw: '42', want: DEFAULT_PREFS },
    {
      name: 'all valid',
      raw: JSON.stringify({ type: 'c', regions: ['eu', 'na'], showFull: false, showEmpty: false, sort: { column: 'timer', dir: 'desc' }, freshStarts: true }),
      want: { type: 'c', regions: ['eu', 'na'], showFull: false, showEmpty: false, sort: { column: 'timer', dir: 'desc' }, freshStarts: true },
    },
    {
      name: 'invalid fields fall back one by one',
      raw: JSON.stringify({ type: 'x', regions: ['eu', 3, 'eu'], showFull: 'no', showEmpty: false, sort: { column: 'bogus', dir: 'asc' }, freshStarts: 'yes' }),
      want: { ...DEFAULT_PREFS, regions: ['eu'], showEmpty: false },
    },
    {
      name: 'bad sort direction',
      raw: JSON.stringify({ type: 'o', sort: { column: 'players', dir: 'up' } }),
      want: { ...DEFAULT_PREFS, type: 'o' },
    },
  ];
  for (const c of cases) {
    assert.deepEqual(loadPrefs(memStorage(c.raw)), c.want, c.name);
  }
  assert.deepEqual(loadPrefs(throwing), DEFAULT_PREFS, 'throwing storage');
});

test('loadPrefs returns a fresh object each time', () => {
  const a = loadPrefs(memStorage());
  a.regions.push('eu');
  a.sort.dir = 'desc';
  assert.deepEqual(loadPrefs(memStorage()), DEFAULT_PREFS);
});

test('savePrefs round-trips and ignores storage errors', () => {
  const storage = memStorage();
  const prefs = { type: 'o', regions: ['as'], showFull: false, showEmpty: true, sort: { column: 'map', dir: 'desc' }, freshStarts: true };
  savePrefs(storage, prefs);
  assert.deepEqual(loadPrefs(storage), prefs);
  assert.doesNotThrow(() => savePrefs(throwing, prefs));
});

test('nextSort', () => {
  const cases = [
    { sort: { column: 'name', dir: 'asc' }, click: 'name', want: { column: 'name', dir: 'desc' } },
    { sort: { column: 'name', dir: 'desc' }, click: 'name', want: { column: 'name', dir: 'asc' } },
    { sort: { column: 'name', dir: 'desc' }, click: 'players', want: { column: 'players', dir: 'asc' } },
    { sort: undefined, click: 'timer', want: { column: 'timer', dir: 'asc' } },
  ];
  for (const c of cases) {
    assert.deepEqual(nextSort(c.sort, c.click), c.want, `${JSON.stringify(c.sort)} + ${c.click}`);
  }
});
