// Page wiring: reads prefs, keeps servers.json fresh and renders the list (SPEC §7.5, §11).
// Logic lives in ./logic/; this file only touches the DOM.

import { clockOffset } from './logic/clock.js';
import { loadPrefs, savePrefs, nextSort } from './logic/prefs.js';
import { createRefresher } from './logic/refresh.js';
import { formatTimer } from './logic/timer.js';
import { viewServers, rowCells, activeFilters, staleNotice } from './logic/view.js';

const $ = (sel) => document.querySelector(sel);
const typeNames = { o: 'Official', c: 'Community' };

const prefs = loadPrefs(localStorage);
let data = null;
let offset = 0;

const now = () => Date.now() + offset;

// Timer cells currently on screen; only these are updated by the 1s ticker.
const visible = new Set();
const observer = new IntersectionObserver((entries) => {
  for (const e of entries) {
    if (e.isIntersecting) visible.add(e.target);
    else visible.delete(e.target);
  }
});

function setPrefs(change) {
  Object.assign(prefs, change);
  savePrefs(localStorage, prefs);
  render();
}

let shownRegions = '';

// renderRegions rebuilds the region checkboxes only when the data's region list changed.
function renderRegions() {
  const key = data.regions.join('\n');
  if (key === shownRegions) return;
  shownRegions = key;
  const box = $('#regions');
  const chosen = new Set(prefs.regions);
  const inputs = data.regions.map((r) => {
    const input = document.createElement('input');
    input.type = 'checkbox';
    input.value = r;
    input.checked = chosen.has(r);
    const label = document.createElement('label');
    label.append(input, ` ${r}`);
    return label;
  });
  box.replaceChildren(box.querySelector('legend'), ...inputs);
}

function renderRow(server, t) {
  const cells = rowCells(server, t);
  const tr = document.createElement('tr');
  for (const key of ['name', 'players', 'map', 'rulesets', 'password']) {
    const td = document.createElement('td');
    td.textContent = cells[key];
    tr.append(td);
  }
  const timer = document.createElement('td');
  timer.textContent = cells.timer.text;
  timer.className = `timer ${cells.timer.kind}`;
  if (server.ms != null) timer.dataset.ms = server.ms;
  if (server.st != null) timer.dataset.st = server.st;
  tr.append(timer);
  return tr;
}

function renderStale() {
  const notice = $('#stale-notice');
  const text = data ? staleNotice(data, now()) : null;
  notice.hidden = text === null;
  if (text !== null && notice.textContent !== text) notice.textContent = text;
}

function render() {
  const picking = !prefs.type;
  $('#type-picker').hidden = !picking;
  $('#browser').hidden = picking || !data;
  renderStale();
  if (picking || !data) return;

  $('#change-type').textContent = `${typeNames[prefs.type]} servers (change)`;
  $('#show-full').checked = prefs.showFull;
  $('#show-empty').checked = prefs.showEmpty;
  $('#fresh-starts').setAttribute('aria-pressed', String(prefs.freshStarts));
  for (const th of document.querySelectorAll('#server-list th')) {
    const col = th.querySelector('button').dataset.sort;
    const active = !prefs.freshStarts && col === prefs.sort.column;
    th.setAttribute('aria-sort', !active ? 'none' : prefs.sort.dir === 'asc' ? 'ascending' : 'descending');
  }

  const scroll = window.scrollY;
  const rows = viewServers(data.servers, prefs, data.regions);
  const t = now();
  observer.disconnect();
  visible.clear();
  $('#server-list tbody').replaceChildren(...rows.map((s) => renderRow(s, t)));
  for (const cell of document.querySelectorAll('#server-list td.exact, #server-list td.estimate')) {
    observer.observe(cell);
  }
  const empty = $('#empty');
  empty.hidden = rows.length > 0;
  if (rows.length === 0) {
    empty.textContent = `No servers match the current filters: ${activeFilters(prefs, data.regions).join(', ')}.`;
  }
  window.scrollTo(window.scrollX, scroll);
}

// tick updates the visible timer cells in place and the stale notice; rows never move here.
function tick() {
  const t = now();
  for (const cell of visible) {
    cell.textContent = formatTimer({ ms: Number(cell.dataset.ms), st: cell.dataset.st }, t).text;
  }
  renderStale();
}

function bind() {
  for (const btn of document.querySelectorAll('#type-picker button')) {
    btn.addEventListener('click', () => setPrefs({ type: btn.dataset.type }));
  }
  $('#change-type').addEventListener('click', () => setPrefs({ type: null }));
  $('#regions').addEventListener('change', () => {
    const regions = [...document.querySelectorAll('#regions input:checked')].map((i) => i.value);
    setPrefs({ regions });
  });
  $('#show-full').addEventListener('change', (e) => setPrefs({ showFull: e.target.checked }));
  $('#show-empty').addEventListener('change', (e) => setPrefs({ showEmpty: e.target.checked }));
  $('#fresh-starts').addEventListener('click', () => setPrefs({ freshStarts: true }));
  for (const btn of document.querySelectorAll('#server-list th button')) {
    btn.addEventListener('click', () => {
      // Leaving Fresh Starts via the saved sort's own column restores it as-is, not flipped.
      const col = btn.dataset.sort;
      const sort = prefs.freshStarts && prefs.sort.column === col ? prefs.sort : nextSort(prefs.sort, col);
      setPrefs({ freshStarts: false, sort });
    });
  }
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
    $('#load-status').hidden = true;
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
