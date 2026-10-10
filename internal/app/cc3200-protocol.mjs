// SPDX-License-Identifier: GPL-2.0-only
// Independent implementation of the documented UARTGWEX and gateway wire format. CC3200 ROM operations are implemented in Go.
export const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
export const concat = (...parts) => {
  const result = new Uint8Array(parts.reduce((n, part) => n + part.length, 0));
  let offset = 0; for (const part of parts) { result.set(part, offset); offset += part.length; }
  return result;
};
const text = value => new TextEncoder().encode(value);
export const be32 = value => { const b = new Uint8Array(4); new DataView(b.buffer).setUint32(0, value); return b; };
export const digest = async bytes => Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', bytes)), b => b.toString(16).padStart(2, '0')).join('');
export const equal = (a, b) => a.length === b.length && a.every((v, i) => v === b[i]);

export class ByteQueue {
  constructor() { this.bytes = new Uint8Array(); this.waiter = null; this.error = null; }
  clear() { if (this.waiter) throw new Error("Cannot clear a pending serial read"); this.bytes = new Uint8Array(); this.error = null; }
  push(data) {
    if (this.error) return;
    if (this.bytes.length + data.length > 1 << 20) { this.fail(new Error('Serial receive buffer exceeded')); return; }
    this.bytes = concat(this.bytes, data); this.wake();
  }
  fail(error) { this.error = error; this.wake(); }
  wake() { if (this.waiter) this.waiter(); }
  async take(size, timeout = 5000) {
    if (this.waiter) throw new Error('Concurrent serial reads are not allowed');
    if (this.error) throw this.error;
    if (this.bytes.length < size) {
      await new Promise((resolve, reject) => {
        const finish = error => { clearTimeout(timer); this.waiter = null; error ? reject(error) : resolve(); };
        const timer = setTimeout(() => finish(new Error('Serial response timed out; reconnect before retrying')), timeout);
        this.waiter = () => { if (this.error) finish(this.error); else if (this.bytes.length >= size) finish(); };
        this.wake();
      });
    }
    const result = this.bytes.slice(0, size); this.bytes = this.bytes.slice(size); return result;
  }
}

export class Gateway {
  constructor(port) { this.port = port; this.uart = new ByteQueue(); this.config = new ByteQueue(); this.frames = new Uint8Array(); this.closed = false; }
  async open(pins) {
    await this.port.open({ baudRate: 921600 });
    // Native USB implementations may not expose modem signals. The config
    // handshake below still has to succeed before any target operations.
    if (this.port.setSignals) await this.port.setSignals({ requestToSend: false, dataTerminalReady: false }).catch(() => {});
    this.writer = this.port.writable.getWriter(); this.reader = this.port.readable.getReader();
    this.readTask = this.readLoop();
    await sleep(300); this.frames = new Uint8Array();
    await this.packet(10, text('UARTGWEX'));
    const query = new Uint8Array(12); await this.packet(1, query); await this.config.take(12);
    const config = new Uint8Array(12); new DataView(config.buffer).setUint32(0, 921600, true);
    config.set(pins, 4); config[11] = 1;
    await this.packet(1, config);
    const actual = await this.config.take(12);
    if (!equal(actual.slice(0, 9), config.slice(0, 9))) throw new Error('Gateway did not accept the selected pins/baud');
  }
  async readLoop() {
    try {
      while (!this.closed) {
        const { value, done } = await this.reader.read(); if (done) throw new Error('USB disconnected');
        this.feed(value);
      }
    } catch (error) { this.uart.fail(error); this.config.fail(error); }
  }
  feed(value) {
    this.frames = concat(this.frames, value);
    if (this.frames.length > 1 << 20) throw new Error('Gateway receive buffer exceeded');
    while (this.frames.length >= 4) {
      const view = new DataView(this.frames.buffer, this.frames.byteOffset);
      const length = view.getUint16(0, true), type = view.getUint16(2, true);
      // Firmware startup text can precede extended-mode frames. Discard only
      // impossible gateway headers; ROM packets are parsed strictly below.
      if (length < 4 || type > 10) { this.frames = this.frames.slice(1); continue; }
      if (this.frames.length < length) return;
      const payload = this.frames.slice(4, length); this.frames = this.frames.slice(length);
      if (type === 0) this.uart.push(payload);
      if (type === 1) { if (payload.length !== 12) throw new Error('Invalid gateway configuration response'); this.config.push(payload); }
    }
  }
  async packet(type, data) {
    if (this.closed || !this.writer) throw new Error('Gateway is disconnected');
    if (data.length > 65531) throw new Error('Gateway packet too large');
    const header = new Uint8Array(4), view = new DataView(header.buffer);
    view.setUint16(0, data.length + 4, true); view.setUint16(2, type, true);
    await this.writer.write(concat(header, data));
  }
  async control(command) { const payload = new Uint8Array(16); payload.set(text(command)); await this.packet(2, payload); }
  async close() {
    if (this.closed) return; this.closed = true;
    const error = new Error('Gateway disconnected; reconnect and make a new backup before flashing');
    this.uart.fail(error); this.config.fail(error);
    if (this.reader) { await this.reader.cancel().catch(() => {}); await this.readTask; this.reader.releaseLock(); }
    if (this.writer) this.writer.releaseLock();
    await this.port.close().catch(() => {});
  }
}

// A small ZIP STORE writer keeps file backups independent of third-party scripts.
export function zipFiles(files) {
  const local = [], directory = []; let offset = 0;
  const crc32 = bytes => { let crc = 0xffffffff; for (const byte of bytes) { crc ^= byte; for (let bit = 0; bit < 8; bit++) crc = crc >>> 1 ^ (crc & 1 ? 0xedb88320 : 0); } return (crc ^ 0xffffffff) >>> 0; };
  for (const [name, data] of files) {
    const encoded = text(name.replace(/^\//, '')), crc = crc32(data), header = new Uint8Array(30), h = new DataView(header.buffer);
    h.setUint32(0, 0x04034b50, true); h.setUint16(4, 20, true); h.setUint16(6, 0x800, true); h.setUint32(14, crc, true);
    h.setUint32(18, data.length, true); h.setUint32(22, data.length, true); h.setUint16(26, encoded.length, true);
    local.push(header, encoded, data);
    const central = new Uint8Array(46), c = new DataView(central.buffer);
    c.setUint32(0, 0x02014b50, true); c.setUint16(4, 20, true); c.setUint16(6, 20, true); c.setUint16(8, 0x800, true);
    c.setUint32(16, crc, true); c.setUint32(20, data.length, true); c.setUint32(24, data.length, true); c.setUint16(28, encoded.length, true); c.setUint32(42, offset, true);
    directory.push(central, encoded); offset += header.length + encoded.length + data.length;
  }
  const central = concat(...directory), end = new Uint8Array(22), e = new DataView(end.buffer);
  e.setUint32(0, 0x06054b50, true); e.setUint16(8, files.size, true); e.setUint16(10, files.size, true); e.setUint32(12, central.length, true); e.setUint32(16, offset, true);
  return concat(...local, central, end);
}

// Preflight every destination and the device identity before the first write.
// Preserve a previously installed original; never overwrite it during updates.
export async function installPlan(rom, writes, backupFiles, progress = () => {}) {
  if (writes.length !== 3 || writes.map(write => write.path).join('|') !== '/sys/pre-img.bin|/cert/c2.der|/sys/mcuimg.bin') throw new Error('Unexpected installation plan');
  for (const write of writes) { if (await digest(write.bytes) !== write.sha256) throw new Error('Installation artifact hash mismatch'); }
  for (const name of new Set([...writes.map(write => write.path), '/cert/client.der', '/cert/private.der'])) {
    const actual = await rom.readFile(name), previous = backupFiles.get(name);
    if (previous ? !actual || !equal(previous, actual) : actual !== null) throw new Error(`Box contents changed since backup: ${name}`);
  }
  for (const write of writes) {
    if (write.path === '/sys/pre-img.bin' && backupFiles.has(write.path)) {
      if (!equal(backupFiles.get(write.path), write.bytes)) throw new Error('Existing original bootloader differs');
      progress('Existing original bootloader preserved.'); continue;
    }
    progress(`Installing and verifying ${write.path}…`); await rom.writeFile(write.path, write.bytes);
  }
}
