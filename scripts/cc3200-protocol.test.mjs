// SPDX-License-Identifier: GPL-2.0-only
import test from 'node:test';
import assert from 'node:assert/strict';
import { ByteQueue, Gateway, concat, digest, installPlan, zipFiles } from '../internal/app/cc3200-protocol.mjs';

import '../internal/app/cc3200-wasm-exec.js';
import { GoROM, loadCC3200Go } from '../internal/app/cc3200-go.mjs';
import { readFile } from 'node:fs/promises';
const originalFetch = globalThis.fetch;
globalThis.fetch = async url => new Response(await readFile(url), { headers: { 'content-type': 'application/wasm' } });
await loadCC3200Go();
globalThis.fetch = originalFetch;

const response = data => { const h = new Uint8Array(3); new DataView(h.buffer).setUint16(0, data.length + 2); h[2] = data.reduce((n, v) => n + v, 0) & 255; return concat(h, data); };
class Device {
  constructor() { this.uart = new ByteQueue(); this.files = new Map(); this.commands = []; this.corrupt = false; this.target = ''; this.staged = null; }
  async control(command) { if (command === "B:200") this.uart.push(new Uint8Array([0, 0xcc])); }
  async packet(type, bytes) {
    assert.equal(type, 0);
    if (bytes.length === 2) { assert.deepEqual([...bytes], [0, 0xcc]); return; }
    const view = new DataView(bytes.buffer, bytes.byteOffset), data = bytes.slice(3), opcode = data[0], body = data.slice(1), fields = new DataView(body.buffer);
    assert.equal(view.getUint16(0), data.length + 2); assert.equal(bytes[2], data.reduce((n, v) => n + v, 0) & 255);
    this.commands.push(opcode); let result = null, raw = false;
    switch (opcode) {
      case 0x2f: result = new Uint8Array(28); result[1] = 4; break;
      case 0x2a: { const name = new TextDecoder().decode(body.slice(4)); const content = this.files.get(name); result = new Uint8Array(8); if (content) { result[0] = 1; new DataView(result.buffer).setUint32(4, content.length); } break; }
      case 0x21: this.target = new TextDecoder().decode(body.slice(8, -2)); this.staged = fields.getUint32(0) === 0 ? null : []; result = new Uint8Array(4); raw = true; break;
      case 0x22: if (this.staged) this.files.set(this.target, concat(...this.staged)); break;
      case 0x23: result = new Uint8Array([0x40]); break;
      case 0x24: assert.equal(fields.getUint32(0), this.staged.reduce((n, b) => n + b.length, 0)); this.staged.push(body.slice(4)); break;
      case 0x2b: result = this.files.get(this.target).slice(fields.getUint32(0), fields.getUint32(0) + fields.getUint32(4)); if (this.corrupt) result[0] ^= 1; break;
      case 0x2e: this.files.delete(new TextDecoder().decode(body.slice(4, -1))); break;
      default: throw new Error(`Unexpected opcode ${opcode}`);
    }
    const wire = concat(new Uint8Array([0, 0xcc]), result ? raw ? result : response(result) : new Uint8Array());
    // Byte-level fragmentation exercises buffering across all ROM header fields.
    for (const byte of wire) this.uart.push(new Uint8Array([byte]));
  }
}
test('gateway parses fragmented and coalesced extended frames', async () => {
  const gateway = new Gateway({}); const packet = (type, data) => { const h = new Uint8Array(4), v = new DataView(h.buffer); v.setUint16(0, data.length + 4, true); v.setUint16(2, type, true); return concat(h, data); };
  const frames = concat(packet(3, new Uint8Array([7, 8])), packet(0, new Uint8Array([0, 0xcc])), packet(1, new Uint8Array(12)));
  for (const byte of frames) gateway.feed(new Uint8Array([byte]));
  assert.deepEqual([...await gateway.uart.take(2)], [0, 0xcc]); assert.equal((await gateway.config.take(12)).length, 12);
});
test('timeout, concurrent read and disconnect reject pending operations', async () => {
  const q = new ByteQueue(); const read = q.take(2, 1000); await assert.rejects(q.take(1), /Concurrent/); q.fail(new Error('USB removed')); await assert.rejects(read, /USB removed/);
  await assert.rejects(new ByteQueue().take(1, 5), /timed out/);
});

async function planFixture() {
  const original = new Uint8Array([1, 2, 3]);
  const previous = new Map([['/sys/mcuimg.bin', original], ['/cert/client.der', new Uint8Array([4])], ['/cert/private.der', new Uint8Array([5])]]);
  const writes = [];
  for (const [path, bytes] of [['/sys/pre-img.bin', original], ['/cert/c2.der', new Uint8Array([6])], ['/sys/mcuimg.bin', new Uint8Array([7])]]) {
    writes.push({ path, bytes, sha256: await digest(bytes) });
  }
  return { previous, writes };
}
test('installation preflights every destination before writes and stops on failure', async () => {
  const { previous, writes } = await planFixture(); const events = [], target = { readFile: async name => { events.push(`read ${name}`); return previous.get(name) ?? null; }, writeFile: async name => { events.push(`write ${name}`); if (name === '/cert/c2.der') throw new Error('failed readback'); } };
  await assert.rejects(installPlan(target, writes, previous), /failed readback/);
  assert.equal(events.filter(e => e.startsWith('read ')).length, 5); assert.deepEqual(events.slice(-2), ['write /sys/pre-img.bin', 'write /cert/c2.der']); assert(!events.includes('write /sys/mcuimg.bin'));
});
test('changed identity and existing differing original prevent all writes', async () => {
  const { previous, writes } = await planFixture(); let count = 0;
  const target = { readFile: async name => name === '/cert/private.der' ? new Uint8Array([99]) : previous.get(name) ?? null, writeFile: async () => count++ };
  await assert.rejects(installPlan(target, writes, previous), /contents changed/); assert.equal(count, 0);
  previous.set('/sys/pre-img.bin', new Uint8Array([99])); target.readFile = async name => previous.get(name) ?? null;
  await assert.rejects(installPlan(target, writes, previous), /original bootloader differs/); assert.equal(count, 0);
});
test('successful update preserves the existing original bootloader', async () => {
  const { previous, writes } = await planFixture(); previous.set('/sys/pre-img.bin', writes[0].bytes); const changed = [];
  await installPlan({ readFile: async name => previous.get(name) ?? null, writeFile: async name => changed.push(name) }, writes, previous);
  assert.deepEqual(changed, ['/cert/c2.der', '/sys/mcuimg.bin']);
});
test('ZIP STORE headers include UTF8 paths, payload CRC and central directory', () => {
  const archive = zipFiles(new Map([['/cert/client.der', new TextEncoder().encode('123456789')]])), view = new DataView(archive.buffer);
  assert.equal(view.getUint32(0, true), 0x04034b50); assert.equal(view.getUint32(14, true), 0xcbf43926); assert.equal(view.getUint16(6, true), 0x800); assert.equal(view.getUint32(archive.length - 22, true), 0x06054b50); assert.equal(view.getUint16(archive.length - 14, true), 1);
});


test('Go WASM ROM file upload, readback and corruption detection', async () => {
  const device = new Device(), rom = new GoROM(device);
  await rom.prepare(new Uint8Array());
  try {
    const data = new Uint8Array(9107).map((_, i) => i % 251);
    device.files.set('/cert/c2.der', new Uint8Array([9]));
    await rom.writeFile('/cert/c2.der', data);
    assert.deepEqual(await rom.readFile('/cert/c2.der'), data);
    assert.equal(await rom.readFile('/missing'), null);
    device.corrupt = true;
    await assert.rejects(rom.writeFile('/sys/mcuimg.bin', data), /read-back verification failed/);
  } finally { await rom.close(); }
});
test('Go WASM ROM disconnect fails closed', async () => {
  const device = new Device(), rom = new GoROM(device);
  await rom.prepare(new Uint8Array());
  try {
    device.uart.fail(new Error('USB removed'));
    await assert.rejects(rom.readFile('/cert/client.der'), /USB removed/);
    const count = device.commands.length;
    await assert.rejects(rom.readFile('/cert/client.der'), /reconnect/);
    assert.equal(device.commands.length, count);
  } finally { await rom.close(); }
});
