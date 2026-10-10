// SPDX-License-Identifier: GPL-2.0-only
// USB/Promise glue only. The ROM protocol and file operations run in Go/WASM.
let modulePromise;
export function loadCC3200Go() {
  if (!modulePromise) modulePromise = (async () => {
    if (!globalThis.Go) {
      const script = document.createElement('script');
      script.src = new URL('./cc3200-wasm-exec.js', import.meta.url).href;
      await new Promise((resolve, reject) => { script.onload = resolve; script.onerror = reject; document.head.appendChild(script); });
    }
    const runtime = new globalThis.Go();
    const response = await fetch(new URL('./cc3200-tool.wasm', import.meta.url));
    if (!response.ok) throw new Error(`CC3200 Go module unavailable: HTTP ${response.status}`);
    const result = await WebAssembly.instantiateStreaming(response, runtime.importObject);
    // The module installs its factory before blocking for JS callbacks.
    void runtime.run(result.instance);
    if (typeof globalThis.cc3200Go !== 'function') throw new Error('CC3200 Go module did not initialize');
  })().catch(error => { modulePromise = null; throw error; });
  return modulePromise;
}
export class GoROM {
  constructor(gateway) {
    this.gateway = gateway;
    this.client = null;
  }
  async prepare(loader) {
    await loadCC3200Go();
    const gateway = this.gateway;
    this.client = globalThis.cc3200Go({
      read: (count, timeout) => gateway.uart.take(count, timeout),
      write: data => gateway.packet(0, data),
      flush: async () => gateway.uart.clear(),
      break: () => gateway.control('B:200'),
      reset: async boot => {
        await gateway.control(boot ? 'C:1' : 'C:0');
        await gateway.control('R:0'); await new Promise(resolve => setTimeout(resolve, 100));
        await gateway.control('R:1'); await new Promise(resolve => setTimeout(resolve, 1000));
      },
    });
    try { await this.client.prepare(loader); } catch (error) { await this.close(); throw error; }
  }
  rawFlash(progress) { return this.client.rawFlash(progress); }
  readFile(name) { return this.client.readFile(name); }
  fileInfo(name) { return this.client.fileInfo(name); }
  writeFile(name, data) { return this.client.writeFile(name, data); }
  async close() { if (this.client) { const old = this.client; this.client = null; await old.close(); } }
}
