import { test } from 'node:test';
import assert from 'node:assert/strict';
import { REGIONS, regionName, regionList, guessRegion } from '../internal/web/static/js/logic/regions.js';

test('regionName', () => {
  const cases = [
    ['eu-central', 'Europe (Central)'],
    ['asia-se', 'Asia (South East)'],
    ['south-america', 'South America'],
    ['mars-1', 'mars-1'],
  ];
  for (const [id, want] of cases) {
    assert.equal(regionName(id), want, id);
  }
});

test('regionList orders known regions as in-game, then unknown IDs', () => {
  const got = regionList(['oceania', 'zz-new', 'na-east', 'eu-west', 'aa-new', 'eu-central', 'na-east']);
  assert.deepEqual(got.map((r) => r.id), ['eu-central', 'eu-west', 'na-east', 'oceania', 'aa-new', 'zz-new']);
  assert.equal(got[0].name, 'Europe (Central)');
  assert.equal(got[5].name, 'zz-new');
  assert.deepEqual(regionList([]), []);
});

test('REGIONS has the game order and no South Africa', () => {
  assert.equal(REGIONS.length, 13);
  assert.deepEqual(REGIONS[0], ['eu-central', 'Europe (Central)']);
  assert.deepEqual(REGIONS[12], ['oceania', 'Oceania']);
});

test('guessRegion', () => {
  const cases = [
    ['America/New_York', 'na-east'],
    ['America/Toronto', 'na-east'],
    ['America/Indiana/Knox', 'na-east'],
    ['America/Chicago', 'na-central'],
    ['America/Denver', 'na-central'],
    ['America/Los_Angeles', 'na-west'],
    ['America/Vancouver', 'na-west'],
    ['America/Sao_Paulo', 'south-america'],
    ['America/Argentina/Buenos_Aires', 'south-america'],
    ['Europe/London', 'eu-west'],
    ['Europe/Berlin', 'eu-central'],
    ['Europe/Rome', 'eu-south'],
    ['Europe/Warsaw', 'eu-east'],
    ['Europe/Kyiv', 'eu-east'],
    ['Asia/Tokyo', 'asia-east'],
    ['Asia/Singapore', 'asia-se'],
    ['Asia/Kolkata', 'asia-west'],
    ['Australia/Sydney', 'oceania'],
    ['Pacific/Auckland', 'oceania'],
    ['Africa/Lagos', null],
    ['UTC', null],
    ['', null],
    [undefined, null],
  ];
  for (const [tz, want] of cases) {
    assert.equal(guessRegion(tz), want, String(tz));
  }
});
