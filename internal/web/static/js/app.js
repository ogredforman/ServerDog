// Page wiring: reads prefs, fetches servers.json once and renders the list (SPEC §7.5
// steps 1–3, §11). Logic lives in ./logic/; this file only touches the DOM.
// The 1s ticker, Find Fresh Starts, scheduled refresh and stale notice come in PLAN 1.12.

import { clockOffset } from './logic/clock.js';
import { loadPrefs, savePrefs, nextSort } from './logic/prefs.js';
import { viewServers, rowCells, activeFilters } from './logic/view.js';

const $ = (sel) => document.querySelector(sel);
const typeNames = { o: 'Official', c: 'Community' };

const prefs = loadPrefs(localStorage);
let data = null;
let offset = 0;

const now = () => Date.now() + offset;

function setPrefs(change) {
  Object.assign(prefs, change);
  savePrefs(localStorage, prefs);
  render();
}

function renderRegions() {
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

function render() {
  const picking = !prefs.type;
  $('#type-picker').hidden = !picking;
  $('#browser').hidden = picking || !data;
  if (picking || !data) return;

  $('#change-type').textContent = `${typeNames[prefs.type]} servers (change)`;
  $('#show-full').checked = prefs.showFull;
  $('#show-empty').checked = prefs.showEmpty;
  for (const th of document.querySelectorAll('#server-list th')) {
    const col = th.querySelector('button').dataset.sort;
    th.setAttribute('aria-sort', col !== prefs.sort.column ? 'none' : prefs.sort.dir === 'asc' ? 'ascending' : 'descending');
  }

  const scroll = window.scrollY;
  const rows = viewServers(data.servers, prefs, data.regions);
  const t = now();
  $('#server-list tbody').replaceChildren(...rows.map((s) => renderRow(s, t)));
  const empty = $('#empty');
  empty.hidden = rows.length > 0;
  if (rows.length === 0) {
    empty.textContent = `No servers match the current filters: ${activeFilters(prefs, data.regions).join(', ')}.`;
  }
  window.scrollTo(window.scrollX, scroll);
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
  for (const btn of document.querySelectorAll('#server-list th button')) {
    btn.addEventListener('click', () => setPrefs({ sort: nextSort(prefs.sort, btn.dataset.sort) }));
  }
}

async function load() {
  const status = $('#load-status');
  try {
    const res = await fetch('/data/servers.json');
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    offset = clockOffset(res.headers.get('Date'), Date.now());
    const body = await res.json();
    data = { ...body, regions: body.regions || [], servers: body.servers || [] };
    status.hidden = true;
    renderRegions();
  } catch (err) {
    status.textContent = `Could not load servers (${err.message}).`;
  }
  render();
}

bind();
render();
load();
