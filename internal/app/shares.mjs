// SPDX-License-Identifier: GPL-2.0-only
const $ = id => document.getElementById(id);
let catalogs = [], items = [], catalogRequest = 0;
async function api(path, payload) {
  const response = await fetch('/api/auth/catalog/' + path, {credentials: 'same-origin', cache: 'no-store', ...(payload === undefined ? {} : {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(payload)})});
  const result = await response.json();
  if (!response.ok) throw new Error(result.message || result.error || `HTTP ${response.status}`);
  return result;
}
function report(error) { $('message').textContent = error.message; }
function action(text, run) {
  const button = document.createElement('button'); button.type = 'button'; button.textContent = text;
  button.addEventListener('click', async () => { button.disabled = true; try { await run(); } catch (error) { report(error); } finally { button.disabled = false; } });
  return button;
}
async function showCatalog() {
  const request = ++catalogRequest;
  const catalog = catalogs.find(item => item.area === $('catalog').value);
  $('items').replaceChildren(); $('recipients').replaceChildren(); $('manage').hidden = true;
  $('grant').reset(); items = [];
  if (!catalog) return;
  const result = await api('items?' + new URLSearchParams({area: catalog.area}));
  if (request !== catalogRequest) return;
  items = result.items; $('manage').hidden = !catalog.manage;
  for (const [index, item] of items.entries()) {
    const row = document.createElement('label');
    if (catalog.manage) { const check = document.createElement('input'); check.type = 'checkbox'; check.value = index; check.setAttribute('aria-label', `Teilen: ${item.path}`); row.append(check); }
    const download = document.createElement('a');
    download.textContent = `${item.title.trim() || item.path} (${item.root}/${item.path}, ${item.size} Bytes)`;
    download.href = '/api/auth/catalog/download?' + new URLSearchParams({area: catalog.area, root: item.root, path: item.path});
    row.append(download); $('items').append(row);
  }
  if (!items.length) $('items').textContent = 'Keine gespeicherten Audiodateien verfügbar.';
  for (const recipient of catalog.recipients) {
    const row = document.createElement('p');
    row.append(document.createTextNode(`${recipient.username} · ${recipient.all ? 'Gesamter Katalog' : `${recipient.items.length} ausgewählte Inhalte`} `));
    row.append(action('Bearbeiten', () => {
      $('grant').elements.username.value = recipient.username; $('grant').elements.all.checked = recipient.all;
      for (const input of $('items').querySelectorAll('input')) { const item = items[Number(input.value)]; input.checked = recipient.items?.some(selected => selected.root === item.root && selected.path === item.path) || false; }
    }));
    row.append(action('Widerrufen', async () => { await api('revoke', {area: catalog.area, username: recipient.username}); await load(); $('message').textContent = 'Freigabe widerrufen.'; }));
    $('recipients').append(row);
  }
}
async function load() {
  const status = await (await fetch('/api/auth/status', {credentials: 'same-origin', cache: 'no-store'})).json();
  $('login').hidden = Boolean(status.username); $('catalogs').hidden = !status.username; $('manage').hidden = true;
  if (!status.username) return;
  const selected = $('catalog').value;
  catalogs = (await api('shares')).catalogs;
  $('catalog').replaceChildren();
  for (const catalog of catalogs) $('catalog').add(new Option(`${catalog.name}${catalog.manage ? ' (eigener Bereich)' : ''}`, catalog.area));
  if (catalogs.some(item => item.area === selected)) $('catalog').value = selected;
  await showCatalog();
}
$('catalog').addEventListener('change', () => showCatalog().catch(report));
$('reload').addEventListener('click', () => load().catch(report));
$('grant').addEventListener('submit', async event => {
  event.preventDefault(); const form = event.currentTarget; const button = form.querySelector('button'); button.disabled = true;
  try {
    const all = form.elements.all.checked;
    const selected = all ? [] : Array.from($('items').querySelectorAll('input:checked'), input => ({root: items[Number(input.value)].root, path: items[Number(input.value)].path}));
    if (!all && !selected.length) throw new Error('Mindestens einen Inhalt oder den gesamten Katalog auswählen.');
    await api('grant', {area: $('catalog').value, username: form.elements.username.value, all, items: selected});
    await load(); $('message').textContent = 'Freigabe gespeichert.';
  } catch (error) { report(error); } finally { button.disabled = false; }
});
load().catch(report);
