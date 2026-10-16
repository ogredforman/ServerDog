// Page wiring: reads prefs, keeps servers.json fresh and renders the list (SPEC §7.5, §11).
// Logic lives in ./logic/; this file only touches the DOM.

import { clockOffset } from './logic/clock.js';
import { loadPrefs, savePrefs, hasSavedPrefs, nextSort } from './logic/prefs.js';
import { createRefresher } from './logic/refresh.js';
import { selectorRegions, chosenRegions, guessRegion } from './logic/regions.js';
import { formatTimer } from './logic/timer.js';
import { viewServers, rowCells, sortIndicator, activeFilters, staleNotice } from './logic/view.js';

const $ = (sel) => document.querySelector(sel);
const typeNames = { o: 'Official', c: 'Community' };

const prefs = loadPrefs(localStorage);
if (!hasSavedPrefs(localStorage)) {
  // First visit: preselect the region nearest the visitor, from the time zone only.
  const guess = guessRegion(Intl.DateTimeFormat().resolvedOptions().timeZone);
  if (guess) prefs.regions = [guess];
}
let data = null;
let offset = 0;
// Join code of the selected row; in memory only, kept across data refreshes.
let selectedId = null;

const now = () => Date.now() + offset;
const panel = $('#list-panel');

// Timer cells currently on screen; only these are updated by the 1s ticker.
const visible = new Set();
const observer = new IntersectionObserver((entries) => {
  for (const e of entries) {
    if (e.isIntersecting) visible.add(e.target);
    else visible.delete(e.target);
  }
}, { root: panel });

function setPrefs(change) {
  Object.assign(prefs, change);
  savePrefs(localStorage, prefs);
  render();
}

let shownRegions = '';

// renderRegions rebuilds the region modal's list (every known region, then unknown IDs from the
// data) only when the data's region list changed.
function renderRegions() {
  const key = data.regions.join('\n');
  if (key === shownRegions) return;
  shownRegions = key;
  const rows = selectorRegions(data.regions).map(({ id, name }) => {
    const input = document.createElement('input');
    input.type = 'checkbox';
    input.value = id;
    const label = document.createElement('label');
    label.className = 'region-row';
    label.append(input, name);
    return label;
  });
  $('#region-list').replaceChildren(...rows);
}

function icon(id) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('class', 'icon');
  const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
  use.setAttribute('href', `#${id}`);
  svg.append(use);
  return svg;
}

function cell(className, ...content) {
  const td = document.createElement('td');
  if (className) td.className = className;
  td.append(...content);
  return td;
}

function renderRow(server, t) {
  const c = rowCells(server, t);
  const name = cell('name', c.name);
  if (c.rulesets.length > 0) {
    const chips = document.createElement('div');
    chips.className = 'chips';
    for (const rs of c.rulesets) {
      const chip = document.createElement('span');
      chip.className = 'chip';
      chip.textContent = rs;
      chips.append(chip);
    }
    name.append(chips);
  }
  const timer = cell(`timer ${c.timer.kind}`, c.timer.text);
  if (server.ms != null) timer.dataset.ms = server.ms;
  if (server.st != null) timer.dataset.st = server.st;
  const tr = document.createElement('tr');
  tr.classList.toggle('full', c.full);
  tr.classList.toggle('empty', c.empty);
  tr.classList.toggle('pw', c.password);
  // Only rows with a join code can be selected or get the copy-join-code button.
  const join = cell('join');
  if (server.id) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'join-btn';
    btn.textContent = 'Copy join code';
    btn.setAttribute('aria-label', `Copy join code for ${server.n}`);
    join.append(btn);
    tr.dataset.id = server.id;
    tr.tabIndex = 0;
  }
  tr.append(
    cell('password', ...(c.password ? [icon('icon-lock')] : [])),
    cell('official', ...(c.official ? [icon('icon-shield')] : [])),
    name,
    cell('mode', c.mode),
    cell('map', c.map),
    cell('players', c.players),
    timer,
    join,
  );
  if (server.id && server.id === selectedId) select(tr);
  return tr;
}

// select marks tr as the selected (gold) row; a null tr just clears the current selection.
function select(tr) {
  document.querySelector('#server-list tr[aria-selected="true"]')?.removeAttribute('aria-selected');
  selectedId = tr ? tr.dataset.id : null;
  if (tr) tr.setAttribute('aria-selected', 'true');
}

function renderStale() {
  const notice = $('#stale-notice');
  const text = data ? staleNotice(data, now()) : null;
  notice.hidden = text === null;
  if (text !== null && notice.textContent !== text) notice.textContent = text;
}

function renderHeaders() {
  for (const th of document.querySelectorAll('#server-list th[aria-sort]')) {
    const col = th.querySelector('button').dataset.sort;
    const arrow = sortIndicator(prefs, col);
    th.querySelector('.arrow').textContent = arrow;
    th.toggleAttribute('data-sorted', arrow !== '▶');
    th.setAttribute('aria-sort', arrow === '▼' ? 'ascending' : arrow === '▲' ? 'descending' : 'none');
  }
}

function renderRegionState() {
  const chosen = new Set(chosenRegions(prefs.regions, data.regions));
  for (const input of document.querySelectorAll('#region-list input')) input.checked = chosen.has(input.value);
  $('#region-count').textContent = chosen.size;
  $('#region-modal-count').textContent = chosen.size;
}

function render() {
  const picking = !prefs.type;
  $('#type-picker').hidden = !picking;
  $('#browser').hidden = picking || !data;
  $('#back').hidden = picking;
  $('#title').textContent = picking ? 'Deploy' : typeNames[prefs.type];
  $('#load-status').hidden = picking || Boolean(data);
  renderStale();
  if (picking || !data) return;

  $('#show-full').setAttribute('aria-pressed', String(prefs.showFull));
  $('#show-empty').setAttribute('aria-pressed', String(prefs.showEmpty));
  $('#fresh-starts').setAttribute('aria-pressed', String(prefs.freshStarts));
  renderHeaders();
  renderRegionState();

  const scroll = panel.scrollTop;
  const rows = viewServers(data.servers, prefs, data.regions);
  const t = now();
  observer.disconnect();
  visible.clear();
  // A selected server that is no longer shown loses its selection.
  if (!rows.some((s) => s.id && s.id === selectedId)) selectedId = null;
  $('#server-list tbody').replaceChildren(...rows.map((s) => renderRow(s, t)));
  for (const td of document.querySelectorAll('#server-list td.exact, #server-list td.estimate')) {
    observer.observe(td);
  }
  const empty = $('#empty');
  empty.hidden = rows.length > 0;
  if (rows.length === 0) {
    empty.textContent = `No servers match the current filters: ${activeFilters(prefs, data.regions).join(', ')}.`;
  }
  panel.scrollTop = scroll;
}

// tick updates the visible timer cells in place and the stale notice; rows never move here.
function tick() {
  const t = now();
  for (const td of visible) {
    td.textContent = formatTimer({ ms: Number(td.dataset.ms), st: td.dataset.st }, t).text;
  }
  renderStale();
}

function bind() {
  for (const btn of document.querySelectorAll('#type-picker button')) {
    btn.addEventListener('click', () => setPrefs({ type: btn.dataset.type }));
  }
  $('#back').addEventListener('click', () => setPrefs({ type: null }));
  $('#show-full').addEventListener('click', () => setPrefs({ showFull: !prefs.showFull }));
  $('#show-empty').addEventListener('click', () => setPrefs({ showEmpty: !prefs.showEmpty }));
  $('#fresh-starts').addEventListener('click', () => setPrefs({ freshStarts: true }));
  for (const btn of document.querySelectorAll('#server-list th button')) {
    btn.addEventListener('click', () => {
      // Leaving Fresh Starts via the saved sort's own column restores it as-is, not flipped.
      const col = btn.dataset.sort;
      const sort = prefs.freshStarts && prefs.sort.column === col ? prefs.sort : nextSort(prefs.sort, col);
      setPrefs({ freshStarts: false, sort });
    });
  }

  // Clicking a row selects it (no re-render); clicking the selected row again clears it.
  $('#server-list tbody').addEventListener('click', (e) => {
    if (e.target.closest('.join-btn')) return;
    const tr = e.target.closest('tr[data-id]');
    if (!tr) return;
    select(tr.dataset.id === selectedId ? null : tr);
  });
  $('#server-list tbody').addEventListener('keydown', (e) => {
    if ((e.key !== 'Enter' && e.key !== ' ') || e.target.tagName !== 'TR' || !e.target.dataset.id) return;
    e.preventDefault();
    select(e.target.dataset.id === selectedId ? null : e.target);
  });

  const modal = $('#region-modal');
  $('#region-open').addEventListener('click', () => modal.showModal());
  $('#region-close').addEventListener('click', () => modal.close());
  // A click on the backdrop lands on the dialog element itself.
  modal.addEventListener('click', (e) => {
    if (e.target === modal) modal.close();
  });
  // Each checkbox applies at once (no Confirm). Saved regions that aren't listed are kept.
  $('#region-list').addEventListener('change', (e) => {
    const id = e.target.value;
    const rest = prefs.regions.filter((r) => r !== id);
    setPrefs({ regions: e.target.checked ? [...rest, id] : rest });
  });
}

const refresher = createRefresher({
  url: '/data/servers.json',
  fetch: (url) => fetch(url),
  now,
  setTimeout: (fn, ms) => setTimeout(fn, ms),
  clearTimeout: (id) => clearTimeout(id),
  random: Math.random,
  onData(body, dateHeader) {
    offset = clockOffset(dateHeader, Date.now());
    data = { ...body, regions: body.regions || [], servers: body.servers || [] };
    renderRegions();
    render();
  },
  onError(err) {
    // With data on screen, keep showing it; the stale notice flags it once it ages out.
    if (!data) $('#load-status').textContent = `Could not load servers (${err.message}). Retrying…`;
  },
});

bind();
render();
refresher.start();
setInterval(tick, 1000);
