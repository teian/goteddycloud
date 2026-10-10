// SPDX-License-Identifier: GPL-2.0-only
import { Gateway, digest, equal, zipFiles, installPlan } from './cc3200-protocol.mjs';
import { GoROM } from './cc3200-go.mjs';
const $ = id => document.getElementById(id);
let gateway = null, rom = null, backup = null, files = new Map(), plan = null, busy = false, abort = null;
const log = message => { $('status').textContent = message; $('log').textContent += `${message}\n`; $('log').scrollTop = $('log').scrollHeight; };
function buttons() {
  for (const id of ['tx', 'rx', 'reset', 'control', 'led', 'loader']) $(id).disabled = busy || !!gateway;
  for (const id of ['saved', 'sdReady', 'preloader', 'originalHash', 'mac']) $(id).disabled = busy;
  $('connect').disabled = busy || !!gateway || !navigator.serial || !globalThis.isSecureContext;
  $('disconnect').disabled = !gateway && !busy;
  $('backup').disabled = busy || !rom;
  $('download').disabled = busy || !backup;
  $('import').disabled = busy || !backup || !/^[a-f\d]{12}$/i.test($('mac').value);
  $('plan').disabled = busy || !backup || !!backup.missing.length || !rom || !$('saved').checked || !$('sdReady').checked || !/^[a-f\d]{64}$/i.test($('originalHash').value) || !$('preloader').files.length;
  $('install').disabled = busy || !plan || !rom || !$('saved').checked || !$('sdReady').checked;
}
function invalidatePlan() { plan = null; $('planDetails').textContent = 'No installation plan prepared.'; buttons(); }
async function api(path, options = {}) {
  const response = await fetch(`/api/cc3200/${path}`, { ...options, signal: abort?.signal, credentials: 'same-origin', cache: 'no-store' });
  if (!response.ok) throw new Error((await response.text()).slice(0, 500) || `HTTP ${response.status}`);
  return response;
}
async function disconnect() {
  abort?.abort(); const old = gateway, oldROM = rom; gateway = null; rom = null; backup = null; files = new Map(); invalidatePlan();
  if (old) await old.close(); if (oldROM) await oldROM.close(); buttons();
}
async function run(work) {
  if (busy) return; busy = true; abort = new AbortController(); buttons();
  try { await work(); } catch (error) { log(error.message); await disconnect(); }
  finally { busy = false; abort = null; buttons(); }
}
const readUpload = async (id, limit) => {
  const file = $(id).files[0]; if (!file || file.size > limit || !file.size) throw new Error(`Select a valid ${id} file`);
  return new Uint8Array(await file.arrayBuffer());
};
$('portPatch').onclick = async () => {
  const port = Number($('connectionPort').value);
  if (!Number.isInteger(port) || port < 1 || port > 65535) { log('Choose a port from 1 to 65535.'); return; }
  try {
    const response = await api(`altPort?port=${port}`), link = document.createElement('a');
    const url = URL.createObjectURL(await response.blob());
    link.href = url; link.download = 'altPort.json'; link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    log(`SD port patch generated for port ${port}. Copy it to the microSD and enable altPort in ngCfg.json.`);
  } catch (error) { log(error.message); }
};
$('connect').onclick = () => run(async () => {
  const pins = ['tx', 'rx', 'reset', 'control', 'led'].map(id => Number($(id).value));
  if (pins.some(pin => !Number.isInteger(pin) || pin < 0 || pin > 21 || pin === 18 || pin === 19) || new Set(pins).size !== pins.length) throw new Error('Choose distinct valid C3 GPIOs; native USB uses GPIO18/19');
  // requestPort must remain directly in the user gesture before any awaits.
  const port = await navigator.serial.requestPort();
  const loader = await readUpload('loader', 128 * 1024);
  gateway = new Gateway(port); log('Configuring gateway at 921600 baud…'); await gateway.open(pins);
  rom = new GoROM(gateway); log('Preparing CC3200 network processor…'); await rom.prepare(loader); log('CC3200 ready. Make a new backup before installation.');
});
$('disconnect').onclick = async () => { await disconnect(); log('Disconnected.'); };
$('backup').onclick = () => run(async () => {
  backup = null; files = new Map(); invalidatePlan(); $('saved').checked = false;
  const progress = (done, total) => { $('progress').value = done / total; };
  log('Reading complete serial flash, pass 1…'); const flash = await rom.rawFlash(progress);
  log('Reading complete serial flash, pass 2…'); const second = await rom.rawFlash(progress);
  if (!equal(flash, second)) throw new Error('Flash backup verification failed. Nothing will be installed.');
  const manifest = await (await api('inspect', { method: 'POST', body: flash })).json();
  const names = new Set(manifest.entries.map(entry => entry.name).filter(Boolean));
  for (const name of ['/cert/ca.der', '/cert/client.der', '/cert/private.der', '/cert/c2.der', '/sys/mcuimg.bin', '/sys/pre-img.bin', '/sys/mcubootinfo.bin', '/sys/mcuimg1.bin', '/sys/mcuimg2.bin', '/sys/mcuimg3.bin']) names.add(name);
  let total = 0;
  for (const name of names) {
    log(`Reading ${name}…`); const data = await rom.readFile(name);
    if (data === null) { if (manifest.entries.some(entry => entry.name === name)) throw new Error(`Named FAT file disappeared: ${name}`); continue; }
    const verify = await rom.readFile(name); if (!verify || !equal(data, verify)) throw new Error(`File backup verification failed: ${name}`);
    total += data.length; if (total > 16 * 1024 * 1024) throw new Error('Named files exceed the backup limit'); files.set(name, data);
  }
  const form = new FormData(); form.set('flash', new Blob([flash]), 'flash.bin'); form.set('files', new Blob([zipFiles(files)]), 'files.zip');
  log('Publishing verified backup to teddyCloud storage…'); backup = await (await api('backup', { method: 'POST', body: form })).json();
  log(`Backup saved: ${backup.id}`);
  for (const name of ['/sys/mcuimg.bin', '/sys/pre-img.bin']) { const data = files.get(name); if (data) log(`${name} SHA-256: ${await digest(data)}`); }
  if (backup.missing.length) log(`Missing important files: ${backup.missing.join(', ')}. Installation is blocked.`);
  else log('Download the ZIP and preserve an independent copy before continuing.');
});
$('download').onclick = () => run(async () => {
  const response = await api(`download?backup=${backup.id}`), blob = await response.blob(), link = document.createElement('a');
  const url = URL.createObjectURL(blob); link.href = url; link.download = `cc3200-${backup.id}.zip`; link.click(); setTimeout(() => URL.revokeObjectURL(url), 60000);
});
$('import').onclick = () => run(async () => {
  await api(`import?backup=${backup.id}&mac=${$('mac').value.toLowerCase()}`, { method: 'POST' }); log('Client certificates imported; existing different identities are preserved.');
});
$('plan').onclick = () => run(async () => {
  invalidatePlan(); const preloader = await readUpload('preloader', 0x3c000), form = new FormData(); form.set('preloader', new Blob([preloader]), 'mcuimg.bin');
  const result = await (await api(`plan?backup=${backup.id}&originalSHA256=${$('originalHash').value.toLowerCase()}`, { method: 'POST', body: form })).json();
  if (result.writes.length !== 3 || result.writes.map(write => write.path).join('|') !== '/sys/pre-img.bin|/cert/c2.der|/sys/mcuimg.bin') throw new Error('Unexpected installation plan');
  for (const write of result.writes) { write.bytes = Uint8Array.from(atob(write.data), c => c.charCodeAt(0)); if (await digest(write.bytes) !== write.sha256) throw new Error('Installation artifact hash mismatch'); }
  plan = result.writes; $('planDetails').textContent = plan.map(write => `${write.path}\n${write.bytes.length} bytes\nSHA-256 ${write.sha256}`).join('\n\n'); log('Installation plan ready for review. No flash files have been changed.');
});
$('install').onclick = () => run(async () => {
  const selected = plan; invalidatePlan();
  await installPlan(rom, selected, files, log);
  log('All three files verified. Disconnect, remove programming wiring and finish the SD configuration.');
  // Keep the backup download available, but require reconnect/backup for another install.
  const completed = rom; rom = null; await completed.close();
});
for (const id of ['preloader', 'originalHash', 'saved', 'sdReady']) $(id).addEventListener('input', invalidatePlan);
$('mac').addEventListener('input', buttons);
if (!navigator.serial || !globalThis.isSecureContext) log('Open this page in a Web Serial browser over trusted HTTPS or localhost.');
buttons();
