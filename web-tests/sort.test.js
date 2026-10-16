import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sortServers, freshStartsOrder, sortColumns } from '../internal/web/static/js/logic/sort.js';

const ids = (list) => list.map((s) => s.id);
const srv = (id, fields) => ({ id, n: id, p: 0, mp: 10, map: '', rs: [], pw: false, ms: null, st: 'unknown', ...fields });

test('sortServers by column and direction', () => {
  const servers = [
    srv('b', { p: 5, map: 'Zestafona', mode: 'KOTH', rs: ['hc'], pw: true, st: 'exact', ms: 100 }),
    srv('a', { p: 9, map: 'Bakurani', mode: 'Assault', rs: ['vanilla'], st: 'estimate', ms: 300 }),
    srv('d', { p: 0 }),
    srv('c', { p: 5, map: 'Ozeti', mode: 'KOTH', rs: ['ffa', 'hc'], st: 'starting', ms: 200 }),
  ];
  const cases = [
    ['name', 'asc', ['a', 'b', 'c', 'd']],
    ['name', 'desc', ['d', 'c', 'b', 'a']],
    ['players', 'asc', ['d', 'b', 'c', 'a']],
    ['players', 'desc', ['a', 'b', 'c', 'd']],
    ['map', 'asc', ['d', 'a', 'c', 'b']],
    ['mode', 'asc', ['d', 'a', 'b', 'c']],
    ['mode', 'desc', ['b', 'c', 'a', 'd']],
    ['password', 'asc', ['a', 'c', 'd', 'b']],
    ['password', 'desc', ['b', 'a', 'c', 'd']],
    ['timer', 'asc', ['a', 'c', 'b', 'd']],
    ['timer', 'desc', ['b', 'c', 'a', 'd']],
  ];
  for (const [column, dir, want] of cases) {
    assert.deepEqual(ids(sortServers(servers, { column, dir })), want, `${column} ${dir}`);
  }
});

test('sortServers ties break by name then id, input untouched', () => {
  const servers = [srv('2', { n: 'Same', p: 1 }), srv('1', { n: 'Same', p: 1 }), srv('0', { n: 'Alpha', p: 1 })];
  const copy = structuredClone(servers);
  assert.deepEqual(ids(sortServers(servers, { column: 'players', dir: 'desc' })), ['0', '1', '2']);
  assert.deepEqual(servers, copy);
});

test('sortServers compares numbers in names by value', () => {
  const servers = [srv('10', { n: 'EU Server #10' }), srv('2', { n: 'EU Server #2' }), srv('1', { n: 'EU Server #1' })];
  assert.deepEqual(ids(sortServers(servers, { column: 'name', dir: 'asc' })), ['1', '2', '10']);
  assert.deepEqual(ids(sortServers(servers, { column: 'name', dir: 'desc' })), ['10', '2', '1']);
});

test('sortServers rejects unknown column', () => {
  assert.throws(() => sortServers([], { column: 'nope' }));
  assert.ok(sortColumns.includes('timer'));
});

test('freshStartsOrder: states, newest first, tie-breaks', () => {
  const servers = [
    srv('unk-few', { p: 2 }),
    srv('est-old', { st: 'estimate', ms: 100, p: 8 }),
    srv('exact-old', { st: 'exact', ms: 100, p: 8 }),
    srv('unk-many', { p: 9 }),
    srv('start-old', { st: 'starting', ms: 500, p: 8 }),
    srv('exact-new', { st: 'exact', ms: 400, p: 1 }),
    srv('est-new', { st: 'estimate', ms: 400, p: 1 }),
    srv('start-new', { st: 'starting', ms: 600, p: 1 }),
    srv('exact-tie-b', { n: 'Bravo', st: 'exact', ms: 100, p: 3 }),
    srv('exact-tie-a', { n: 'Alpha', st: 'exact', ms: 100, p: 3 }),
    srv('unk-tie-b', { n: 'Bravo', p: 2 }),
    srv('unk-empty', { p: 0 }),
  ];
  assert.deepEqual(ids(freshStartsOrder(servers)), [
    'start-new', 'start-old',
    'exact-new', 'exact-old', 'exact-tie-a', 'exact-tie-b',
    'est-new', 'est-old',
    'unk-many', 'unk-tie-b', 'unk-few', 'unk-empty',
  ]);
});

test('freshStartsOrder only reorders', () => {
  const servers = [srv('a', { p: 0 }), srv('b', { st: 'exact', ms: 1 }), srv('c', { p: 0, mp: 0 })];
  const out = freshStartsOrder(servers);
  assert.equal(out.length, servers.length);
  assert.deepEqual(new Set(ids(out)), new Set(['a', 'b', 'c']));
});
