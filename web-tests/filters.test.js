import { test } from 'node:test';
import assert from 'node:assert/strict';
import { filterServers, isFull, isEmpty } from '../internal/web/static/js/logic/filters.js';

const servers = [
  { id: 'a', t: 'o', r: 'eu', p: 5, mp: 10 },
  { id: 'b', t: 'o', r: 'na', p: 10, mp: 10 },
  { id: 'c', t: 'c', r: 'eu', p: 0, mp: 10 },
  { id: 'd', t: 'o', r: 'eu', p: 0, mp: 0 },
  { id: 'e', t: 'c', r: 'as', p: 3, mp: 8 },
];
const ids = (list) => list.map((s) => s.id);

test('isFull and isEmpty, 0/0 counts as both', () => {
  assert.equal(isFull({ p: 10, mp: 10 }), true);
  assert.equal(isFull({ p: 11, mp: 10 }), true);
  assert.equal(isFull({ p: 9, mp: 10 }), false);
  assert.equal(isFull({ p: 0, mp: 0 }), true);
  assert.equal(isEmpty({ p: 0, mp: 0 }), true);
  assert.equal(isEmpty({ p: 0, mp: 10 }), true);
  assert.equal(isEmpty({ p: 1, mp: 10 }), false);
});

test('filterServers', () => {
  const cases = [
    ['defaults show all', {}, ['a', 'b', 'c', 'd', 'e']],
    ['official type', { type: 'o' }, ['a', 'b', 'd']],
    ['community type', { type: 'c' }, ['c', 'e']],
    ['empty region list means all', { regions: [] }, ['a', 'b', 'c', 'd', 'e']],
    ['multiple regions', { regions: ['eu', 'as'] }, ['a', 'c', 'd', 'e']],
    ['hide full hides 0/0', { showFull: false }, ['a', 'c', 'e']],
    ['hide empty hides 0/0', { showEmpty: false }, ['a', 'b', 'e']],
    ['hide both', { showFull: false, showEmpty: false }, ['a', 'e']],
    ['combined', { type: 'o', regions: ['eu'], showEmpty: false }, ['a']],
  ];
  for (const [name, opts, want] of cases) assert.deepEqual(ids(filterServers(servers, opts)), want, name);
});

test('filterServers does not modify input', () => {
  const copy = structuredClone(servers);
  filterServers(servers, { type: 'c', showFull: false });
  assert.deepEqual(servers, copy);
});
