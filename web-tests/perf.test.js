// Client performance budget (SPEC §9): a full re-sort of 4,000 rows and the 1s timer update
// each finish in under 16 ms. Best of several runs, so a single GC pause doesn't fail it.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { viewServers } from '../internal/web/static/js/logic/view.js';
import { formatTimer } from '../internal/web/static/js/logic/timer.js';
import { sortColumns } from '../internal/web/static/js/logic/sort.js';
import { DEFAULT_PREFS } from '../internal/web/static/js/logic/prefs.js';

const budgetMs = 16;
const now = 1_800_000_000_000;
const regions = ['eu-central', 'eu-west', 'na-east', 'na-west', 'asia-east', 'oceania'];
const states = ['exact', 'estimate', 'starting', 'unknown'];
const maps = ['Bakurani', 'Ozeti', 'Zestafona'];

// servers is a deterministic 4,000-server list with every published state.
function servers(n) {
  let seed = 7;
  const rand = (m) => {
    seed = (seed * 1103515245 + 12345) % 2147483648;
    return seed % m;
  };
  return Array.from({ length: n }, (_, i) => {
    const mp = [16, 32, 64][rand(3)];
    return {
      id: `s${i}`,
      n: `Server #${rand(5000)} ${maps[rand(maps.length)]}`,
      t: rand(3) === 0 ? 'o' : 'c',
      r: regions[rand(regions.length)],
      p: rand(mp + 1),
      mp,
      map: maps[rand(maps.length)],
      mode: 'KOTH',
      rs: rand(4) === 0 ? ['hardcore'] : [],
      pw: rand(10) === 0,
      ms: 1_800_000_000 - rand(7200),
      st: states[rand(states.length)],
    };
  });
}

function best(fn, runs = 5) {
  let min = Infinity;
  for (let i = 0; i < runs; i++) {
    const start = performance.now();
    fn();
    min = Math.min(min, performance.now() - start);
  }
  return min;
}

const list = servers(4000);

test('re-sorting 4,000 rows fits the frame budget', () => {
  const cases = [
    ...sortColumns.flatMap((column) => ['asc', 'desc'].map((dir) => ({ name: `${column} ${dir}`, prefs: { sort: { column, dir } } }))),
    { name: 'fresh starts', prefs: { freshStarts: true } },
  ];
  for (const c of cases) {
    const prefs = { ...DEFAULT_PREFS, type: null, ...c.prefs };
    const ms = best(() => viewServers(list, prefs, regions));
    assert.ok(ms < budgetMs, `${c.name}: ${ms.toFixed(2)} ms`);
  }
});

test('the 1s tick formats a screen of timers well within budget', () => {
  const screen = list.slice(0, 100);
  const ms = best(() => {
    for (const s of screen) formatTimer(s, now);
  });
  assert.ok(ms < budgetMs / 4, `${ms.toFixed(2)} ms`);
});
