import { test } from 'node:test';
import assert from 'node:assert/strict';
import { viewServers, rowCells, serverName, sortIndicator, activeFilters, staleNotice } from '../internal/web/static/js/logic/view.js';
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
    { name: 'known region with no servers filters to nothing', prefs: prefs({ regions: ['south-africa'] }), want: [] },
    { name: 'known empty region kept beside a present one', prefs: prefs({ regions: ['eu-west', 'na'] }), want: ['b'] },
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
      server: { id: 'X1', n: 'Dogs', t: 'o', p: 3, mp: 16, map: 'Zestafona', mode: 'KOTH', rs: ['HC', 'NoAim'], pw: true, ms: 1_800_000_000 - 65, st: 'exact' },
      want: { full: false, empty: false, password: true, official: true, name: 'Dogs', rulesets: ['HC', 'NoAim'], mode: 'KOTH', map: 'Zestafona', players: '3/16', timer: { text: '1:05', kind: 'exact' } },
    },
    {
      name: 'sparse row falls back',
      server: { id: 'X2', t: 'c', p: 0, mp: 0, ms: null, st: 'unknown' },
      want: { full: true, empty: true, password: false, official: false, name: 'X2', rulesets: [], mode: '', map: '', players: '0/0', timer: { text: '—', kind: 'unknown' } },
    },
    {
      name: 'full but not empty, starting',
      server: { id: 'X3', n: 'Packed', t: 'c', p: 16, mp: 16, map: 'Bakurani', mode: 'KOTH', rs: ['hardcore'], ms: 1_800_000_000, st: 'starting' },
      want: { full: true, empty: false, password: false, official: false, name: 'Packed', rulesets: ['hardcore'], mode: 'KOTH', map: 'Bakurani', players: '16/16', timer: { text: 'STARTING', kind: 'starting' } },
    },
  ];
  for (const c of cases) {
    assert.deepEqual(rowCells(c.server, now), c.want, c.name);
  }
});

test('serverName adds the region as in-game', () => {
  const cases = [
    { name: 'official, in-game form', server: { n: 'Official #97', t: 'o', r: 'na-east' }, want: 'OFFICIAL WARDOGS | North America (East) | #97' },
    { name: 'community appends region', server: { n: '[ZTAX] KOTH', t: 'c', r: 'eu-central' }, want: '[ZTAX] KOTH | Europe (Central)' },
    { name: 'official with an unusual name appends', server: { n: 'Official Event', t: 'o', r: 'oceania' }, want: 'Official Event | Oceania' },
    { name: 'community named like an official server is not rewritten', server: { n: 'Official #1', t: 'c', r: 'na-west' }, want: 'Official #1 | North America (West)' },
    { name: 'unknown region uses its ID', server: { n: 'X', t: 'c', r: 'mars' }, want: 'X | mars' },
    { name: 'no region, name unchanged', server: { n: 'Dogs', t: 'o' }, want: 'Dogs' },
    { name: 'no name falls back to join code', server: { id: 'ABC', t: 'c', r: 'oceania' }, want: 'ABC | Oceania' },
    { name: 'nothing but a region', server: { t: 'c', r: 'oceania' }, want: 'Oceania' },
  ];
  for (const c of cases) {
    assert.equal(serverName(c.server), c.want, c.name);
  }
});

test('activeFilters', () => {
  const cases = [
    { prefs: prefs(), want: ['Official'] },
    { prefs: prefs({ type: 'c', regions: ['eu', 'as'], showFull: false, showEmpty: false }), want: ['Community', 'Regions: eu', 'Hiding full', 'Hiding empty'] },
    { prefs: prefs({ regions: ['na-east', 'eu-west'] }), regions: ['eu-west', 'na-east'], want: ['Official', 'Regions: North America (East), Europe (West)'] },
    { prefs: prefs({ regions: ['south-africa', 'gone'] }), regions: ['na-east'], want: ['Official', 'Regions: South Africa'] },
  ];
  for (const c of cases) {
    assert.deepEqual(activeFilters(c.prefs, c.regions ?? regions), c.want);
  }
});

test('sortIndicator', () => {
  const cases = [
    { name: 'sorted ascending', prefs: prefs({ sort: { column: 'players', dir: 'asc' } }), column: 'players', want: '▼' },
    { name: 'sorted descending', prefs: prefs({ sort: { column: 'players', dir: 'desc' } }), column: 'players', want: '▲' },
    { name: 'other column', prefs: prefs({ sort: { column: 'players', dir: 'desc' } }), column: 'map', want: '▶' },
    { name: 'fresh starts on', prefs: prefs({ freshStarts: true, sort: { column: 'players', dir: 'desc' } }), column: 'players', want: '▶' },
  ];
  for (const c of cases) {
    assert.equal(sortIndicator(c.prefs, c.column), c.want, c.name);
  }
});

test('viewServers uses the Fresh Starts order and keeps every server', () => {
  const list = [
    { id: 'u', n: 'Unknown', t: 'o', r: 'eu', p: 4, mp: 10, ms: null, st: 'unknown' },
    { id: 'e', n: 'Estimate', t: 'o', r: 'eu', p: 4, mp: 10, ms: 900, st: 'estimate' },
    { id: 'old', n: 'Old', t: 'o', r: 'eu', p: 4, mp: 10, ms: 100, st: 'exact' },
    { id: 'new', n: 'New', t: 'o', r: 'eu', p: 4, mp: 10, ms: 500, st: 'exact' },
    { id: 's', n: 'Starting', t: 'o', r: 'eu', p: 4, mp: 10, ms: 50, st: 'starting' },
  ];
  const sort = { column: 'name', dir: 'asc' };
  assert.deepEqual(ids(viewServers(list, prefs({ sort, freshStarts: true }), regions)), ['s', 'new', 'old', 'e', 'u']);
  assert.deepEqual(ids(viewServers(list, prefs({ sort, freshStarts: false }), regions)), ['e', 'new', 'old', 's', 'u']);
});

test('staleNotice', () => {
  const now = 1_800_000_000_000;
  const at = now / 1000;
  const cases = [
    { name: 'fresh', data: { generatedAt: at - 30, refreshSeconds: 60, stale: false }, want: null },
    { name: 'just under the limit', data: { generatedAt: at - 180, refreshSeconds: 60, stale: false }, want: null },
    { name: 'too old', data: { generatedAt: at - 185, refreshSeconds: 60, stale: false }, want: 'Server data may be out of date (last updated 3:05 ago).' },
    { name: 'flagged by the server', data: { generatedAt: at - 30, refreshSeconds: 60, stale: true }, want: 'Server data may be out of date (last updated 0:30 ago).' },
    { name: 'malformed times', data: { refreshSeconds: 60 }, want: 'Server data may be out of date.' },
  ];
  for (const c of cases) {
    assert.equal(staleNotice(c.data, now), c.want, c.name);
  }
});
