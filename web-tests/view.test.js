import { test } from 'node:test';
import assert from 'node:assert/strict';
import { viewServers, rowCells, activeFilters } from '../internal/web/static/js/logic/view.js';
import { DEFAULT_PREFS } from '../internal/web/static/js/logic/prefs.js';

const servers = [
  { id: 'a', n: 'Bravo', t: 'o', r: 'eu', p: 5, mp: 10 },
  { id: 'b', n: 'Alpha', t: 'o', r: 'na', p: 10, mp: 10 },
  { id: 'c', n: 'Charlie', t: 'c', r: 'eu', p: 0, mp: 10 },
  { id: 'd', n: 'Delta', t: 'o', r: 'eu', p: 0, mp: 10 },
];
const regions = ['eu', 'na'];
const ids = (list) => list.map((s) => s.id);
const prefs = (change) => ({ ...DEFAULT_PREFS, type: 'o', ...change });

test('viewServers filters then sorts', () => {
  const cases = [
    { name: 'type only, by name', prefs: prefs(), want: ['b', 'a', 'd'] },
    { name: 'players desc', prefs: prefs({ sort: { column: 'players', dir: 'desc' } }), want: ['b', 'a', 'd'] },
    { name: 'region', prefs: prefs({ regions: ['eu'] }), want: ['a', 'd'] },
    { name: 'hide full and empty', prefs: prefs({ showFull: false, showEmpty: false }), want: ['a'] },
    { name: 'region gone from data is ignored', prefs: prefs({ regions: ['as'] }), want: ['b', 'a', 'd'] },
    { name: 'gone region dropped, present kept', prefs: prefs({ regions: ['as', 'na'] }), want: ['b'] },
    { name: 'community', prefs: prefs({ type: 'c' }), want: ['c'] },
  ];
  for (const c of cases) {
    assert.deepEqual(ids(viewServers(servers, c.prefs, regions)), c.want, c.name);
  }
});

test('viewServers leaves the input untouched', () => {
  const copy = structuredClone(servers);
  viewServers(servers, prefs({ sort: { column: 'players', dir: 'asc' } }), regions);
  assert.deepEqual(servers, copy);
});

test('rowCells', () => {
  const now = 1_800_000_000_000;
  const cases = [
    {
      name: 'full row',
      server: { id: 'X1', n: 'Dogs', p: 3, mp: 16, map: 'Harbor Night', base: 'Harbor', exp: 'Conquest', rs: ['HC', 'NoAim'], pw: true, ms: 1_800_000_000 - 65, st: 'exact' },
      want: { name: 'Dogs', players: '3/16', map: 'Harbor Night · Conquest', rulesets: 'HC, NoAim', password: 'Yes', timer: { text: '1:05', kind: 'exact' } },
    },
    {
      name: 'sparse row falls back',
      server: { id: 'X2', p: 0, mp: 0, base: 'Harbor', ms: null, st: 'unknown' },
      want: { name: 'X2', players: '0/0', map: 'Harbor', rulesets: '', password: '', timer: { text: '—', kind: 'unknown' } },
    },
  ];
  for (const c of cases) {
    assert.deepEqual(rowCells(c.server, now), c.want, c.name);
  }
});

test('activeFilters', () => {
  const cases = [
    { prefs: prefs(), want: ['Official'] },
    { prefs: prefs({ type: 'c', regions: ['eu', 'as'], showFull: false, showEmpty: false }), want: ['Community', 'Regions: eu', 'Hiding full', 'Hiding empty'] },
  ];
  for (const c of cases) {
    assert.deepEqual(activeFilters(c.prefs, regions), c.want);
  }
});
