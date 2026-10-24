// SPDX-License-Identifier: GPL-2.0-only
// Run after scripts/frontend.sh build; uses only generated local identities.
import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';
import tls from 'node:tls';

const { chromium } = await import(pathToFileURL(resolve(
    process.env.PLAYWRIGHT_MODULE || '.cache/browser-tools/node_modules/playwright/index.mjs',
)));
const root = await mkdtemp(resolve('.cache/webui-live-'));
const fixture = join(root, 'fixture');
const binary = join(root, 'goteddycloud');
execFileSync('go', ['build', '-buildvcs=false', '-o', binary, './cmd/goteddycloud']);
execFileSync('go', ['run', '-buildvcs=false', './cmd/fixturegen', '-output', fixture]);
execFileSync('bash', ['scripts/frontend.sh', 'install-fixture', fixture]);
const server = spawn(binary, ['-base_path', fixture, '-http', '127.0.0.1:0',
    '-httpsecure', 'off', '-box', '127.0.0.1:0']);
let logs = '';
server.stdout.on('data', data => { logs += data; });
server.stderr.on('data', data => { logs += data; });
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
let browser;
let page, url;
const sockets = new Set();
try {
    let http, box;
    for (let i = 0; i < 100; i++) {
        http = logs.match(/HTTP: (127\.0\.0\.1:\d+)/)?.[1];
        box = logs.match(/Box API: (127\.0\.0\.1:\d+)/)?.[1];
        if (http && box) break;
        if (server.exitCode !== null) throw new Error(logs);
        await pause(100);
    }
    assert(http && box, logs);
    url = 'http://' + http;
    const id = '001122334455';
    const credentials = {
        cert: await readFile(join(fixture, 'certs/client', id, 'teddy-cert.pem')),
        key: await readFile(join(fixture, 'certs/client', id, 'teddy-key.pem')),
        ca: await readFile(join(fixture, 'certs/server_tb2/teddy-cert.pem')),
    };
    function varint(value) {
        const out = [];
        do { out.push((value & 127) | (value > 127 ? 128 : 0)); value >>>= 7; } while (value);
        return Buffer.from(out);
    }
    async function present(ruid) {
        // Encode the CC3200 TagValid RTNL function using the checked-in schema.
        const uid = Buffer.from(ruid, 'hex').readBigUInt64LE();
        const payload = Buffer.alloc(8);
        payload.writeUInt32BE(Number(uid & 0xffffffffn));
        payload.writeUInt32BE(Number(uid >> 32n), 4);
        const log = Buffer.concat([
            Buffer.from([9, 1, 0, 0, 0, 0, 0, 0, 0, 16, 1, 24, 0, 32, 15, 40]),
            varint(8627), Buffer.from([50, 8]), payload,
        ]);
        const rpc = Buffer.concat([Buffer.from([18]), varint(log.length), log]);
        const length = Buffer.alloc(4);
        length.writeUInt32BE(rpc.length);
        const socket = tls.connect({
            host: '127.0.0.1', port: Number(box.split(':')[1]), servername: 'box.test',
            maxVersion: 'TLSv1.2', ...credentials,
        });
        sockets.add(socket);
        await new Promise((resolve, reject) => {
            socket.once('error', reject);
            socket.once('secureConnect', () => socket.write(Buffer.concat([length, rpc]), resolve));
        });
        return socket;
    }
    browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE_PATH
        ? { executablePath: process.env.BROWSER_EXECUTABLE_PATH } : {}) });
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    await context.addInitScript(() => localStorage.setItem('theme', 'dark'));
    page = await context.newPage();
    const errors = [];
    let streams = 0;
    page.on('pageerror', error => errors.push(String(error)));
    page.on('request', request => { if (request.url().endsWith('/api/sse')) streams++; });
    const ready = page.waitForResponse(response => response.url().endsWith('/api/sse') && response.status() === 200);
    await page.goto(url + '/web/tonies');
    await ready;

    const first = '11000000500304e0';
    const second = '22000000500304e0';
    await present(first);
    const firstCard = page.locator('[data-ruid="' + first + '"]');
    await page.waitForSelector('[data-ruid="' + first + '"][data-presented="true"]');
    await firstCard.evaluate(element => element.setAttribute("data-preserved", "true"));
    await present(second);
    await page.waitForSelector('[data-ruid="' + second + '"][data-presented="true"]');
    assert.equal(await firstCard.getAttribute('data-presented'), null);
    assert.equal(await firstCard.getAttribute('data-preserved'), 'true', 'inventory refresh remounted an existing card');
    assert.notEqual(await firstCard.evaluate(element => getComputedStyle(element).borderTopWidth), '3px', 'previous tag retained its blue border');
    const secondCard = page.locator('[data-ruid="' + second + '"]');
    const border = await secondCard.evaluate(element => ({
        width: getComputedStyle(element).borderRightWidth,
        color: getComputedStyle(element).borderRightColor,
    }));
    assert.deepEqual(border, { width: '3px', color: 'rgb(22, 119, 255)' });
    await page.getByText(/^(Tag presented|Tag aufgelegt)$/).first().waitFor();
    const edit = await fetch(url + '/content/json/set/' + second + '?overlay=' + id, {
        method: 'POST', body: 'tonie_model=LIVE_EDITED', headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    });
    assert.equal(edit.status, 200);
    await secondCard.getByText('LIVE_EDITED', { exact: true }).waitFor();
    await page.screenshot({ path: resolve('.cache/webui-live-tags.png'), fullPage: true });
    // The 15-second heartbeat must survive the shorter per-write deadline.
    await pause(16000);
    assert.equal(streams, 1, 'SSE reconnected during the heartbeat interval');
    await present(first);
    await page.waitForSelector('[data-ruid="' + first + '"][data-presented="true"]');
    await page.reload();
    await page.waitForSelector('[data-ruid="' + first + '"][data-presented="true"]');
    const beforeRtnl = streams;
    await page.evaluate(() => {
        history.pushState({}, '', '/web/settings/rtnl');
        dispatchEvent(new PopStateEvent('popstate'));
    });
    await page.waitForURL('**/web/settings/rtnl');
    await page.locator('.rtnl-log-container').waitFor();
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    await present(second);
    await page.getByText('Raw', { exact: true }).click();
    await page.getByText(/Raw2 \|/).first().waitFor();
    assert.equal(streams, beforeRtnl, 'RTNL opened a second SSE connection');
    const snapshotReady = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Snapshot (JSON)', exact: true }).click();
    const snapshot = await snapshotReady;
    await snapshot.saveAs(resolve('.cache/webui-rtnl-snapshot.json'));
    const exported = JSON.parse(await readFile(resolve('.cache/webui-rtnl-snapshot.json'), 'utf8'));
    assert.equal(exported.mappingSchemaVersion, 1);
    assert(/^sha256:[a-f0-9]{64}$/.test(exported.mappingRevision));
    assert(exported.entries.some(entry => entry.raw.type === 'rtnl-raw-log2' && entry.box === id));
    assert.deepEqual(errors, []);
    console.log('PASS: real TLS RTNL → SSE → new unknown cards, blue border, switching tags, live metadata, notifications, snapshots and heartbeat');
} catch (error) {
    if (page) {
        await page.screenshot({ path: resolve('.cache/webui-live-failure.png'), fullPage: true }).catch(() => {});
        console.error(await page.locator('[data-ruid]').evaluateAll(elements =>
            elements.map(element => ({ ruid: element.getAttribute('data-ruid'), presented: element.getAttribute('data-presented') }))));
    }
    if (url) console.error('Tags:', await (await fetch(url + '/api/getTagIndex')).text());
    console.error(logs);
    throw error;
} finally {
    for (const socket of sockets) socket.destroy();
    if (browser) await browser.close();
    server.kill('SIGTERM');
    await new Promise(resolve => server.exitCode !== null ? resolve() : server.once('exit', resolve));
    await rm(root, { recursive: true, force: true });
}
