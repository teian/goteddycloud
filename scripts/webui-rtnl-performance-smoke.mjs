// SPDX-License-Identifier: GPL-2.0-only
// Run against built UI assets; uses a disposable fixture and a synthetic SSE feed.
import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import { createServer, request } from 'node:http';
import { mkdtemp, readFile, rm, cp } from 'node:fs/promises';
import { resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';

const { chromium } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE || '.cache/browser-tools/node_modules/playwright/index.mjs')));
const root = await mkdtemp(resolve('.cache/rtnl-performance-'));
const fixture = join(root, 'fixture');
execFileSync('go', ['run', '-buildvcs=false', './cmd/fixturegen', '-output', fixture]);
await cp(resolve(process.env.RTNL_PERF_DIST || '.cache/goteddycloud-web/dist'), join(fixture, 'data/www/web'), { recursive: true });
const binary = join(root, 'goteddycloud');
execFileSync('go', ['build', '-buildvcs=false', '-o', binary, './cmd/goteddycloud']);
const backend = spawn(binary, ['-base_path', fixture, '-http', '127.0.0.1:0', '-httpsecure', 'off', '-box', 'off']);
let logs = '';
backend.stdout.on('data', data => logs += data);
backend.stderr.on('data', data => logs += data);
const pause = ms => new Promise(done => setTimeout(done, ms));
let browser, proxy, page;
const streams = new Set();
try {
    let address;
    for (let i = 0; i < 100; i++) {
        address = logs.match(/HTTP: (127\.0\.0\.1:\d+)/)?.[1];
        if (address) break;
        if (backend.exitCode !== null) throw Error(logs);
        await pause(100);
    }
    assert(address, logs);
    const raw = sequence => ({ type: 'rtnl-raw-log2', box: '6CC374F2CBD8', data: { sequence, uptime: sequence, function_group: 24, function: 8427, field6: 'E01700001B0000000000000000000000000000001B000000' } });
    const wire = event => `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`;
    const events = Array.from({ length: 2000 }, (_, i) => raw(i));
    events.push({ type: 'audio-sync', box: '6CC374F2CBD8', data: { phase: 'freshness-response', status: 200, requested: 1, serverVersion: 'performance-fixture', markedRuids: ['be3cea0e500304e0'], tags: [{ ruid: 'be3cea0e500304e0', source: 'lib://by/audioID/1748955785.taf', boxAudioId: 1634291675, localAudioId: 1748955785, localState: 'ready', marked: true }] } });
    let sent = false;
    proxy = createServer((req, res) => {
        if (req.url === '/api/sse') {
            res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' });
            streams.add(res);
            res.on('close', () => streams.delete(res));
            res.write(wire({ type: 'keep-alive', data: '' }));
            if (!sent) { sent = true; res.write(events.map(wire).join('')); }
            return;
        }
        const upstream = request('http://' + address + req.url, { method: req.method, headers: req.headers }, response => {
            res.writeHead(response.statusCode, response.headers);
            response.pipe(res);
        });
        upstream.on('error', error => res.destroy(error));
        req.pipe(upstream);
    });
    await new Promise(done => proxy.listen(0, '127.0.0.1', done));
    browser = await chromium.launch({ headless: true });
    page = await browser.newPage({ acceptDownloads: true });
    const cdp = await page.context().newCDPSession(page);
    await cdp.send('Emulation.setCPUThrottlingRate', { rate: 4 });
    await page.addInitScript(() => {
        localStorage.setItem('i18nextLng', 'de');
        window.longTasks = [];
        new PerformanceObserver(list => window.longTasks.push(...list.getEntries().map(e => e.duration))).observe({ type: 'longtask' });
    });
    const errors = [];
    page.on('pageerror', error => errors.push(String(error)));
    const ready = page.waitForResponse(response => response.url().endsWith('/api/sse'));
    await page.goto('http://127.0.0.1:' + proxy.address().port + '/web/tonies');
    await ready;
    await page.locator('.ant-layout').first().waitFor();
    await page.waitForTimeout(500);
    await page.evaluate(() => {
        window.longTasks = [];
        window.consoleStarted = performance.now();
        history.pushState({}, '', '/web/settings/rtnl');
        dispatchEvent(new PopStateEvent('popstate'));
    });
    await page.locator('.rtnl-log-container').waitFor();
    const elapsed = await page.evaluate(() => new Promise(done => requestAnimationFrame(() => requestAnimationFrame(() => done(performance.now() - window.consoleStarted)))));
    const metrics = await page.locator('.rtnl-log-container').evaluate(element => ({
        characters: element.textContent.length, elements: element.querySelectorAll('*').length,
        longestTaskMs: Math.max(0, ...window.longTasks),
    }));
    console.log(JSON.stringify({ routeToPaintMs: Math.round(elapsed), cpuSlowdown: 4, ...metrics }));
    if (!process.env.RTNL_PERF_BASELINE) {
        assert(elapsed < 1000, 'console route blocked for a second');
        assert(metrics.characters <= 65536, 'unbounded console text');
        assert(metrics.elements <= 2, 'per-token DOM expansion returned');
        await page.getByRole('button', { name: 'Ältere Einträge', exact: true }).click();
        assert(!(await page.locator('.rtnl-log-container').innerText()).includes('performance-fixture'));
        await page.getByRole('button', { name: 'Neueste Einträge', exact: true }).click();
        assert((await page.locator('.rtnl-log-container').innerText()).includes('performance-fixture'));
        const ready = page.waitForEvent('download');
        await page.getByRole('button', { name: 'Snapshot (JSON)', exact: true }).click();
        const snapshot = JSON.parse(await readFile(await (await ready).path(), 'utf8'));
        assert.equal(snapshot.entries.length, 2000);
        assert.equal(snapshot.droppedEntries, 1);
        assert(snapshot.entries.some(entry => entry.raw.type === 'audio-sync' && entry.fields.tags[0].localAudioId === 1748955785));
        assert(snapshot.entries.some(entry => entry.raw.data.sequence === 1));
        assert.deepEqual(errors, []);
        console.log('PASS: full buffer with 4× CPU slowdown, bounded display, history navigation and complete diagnostic export');
    }
} catch (error) {
    if (page) console.error('Fixture page:', page.url(), (await page.locator('body').innerText()).slice(0, 1500));
    throw error;
} finally {
    if (browser) await browser.close();
    for (const stream of streams) stream.end();
    if (proxy) await new Promise(done => proxy.close(done));
    backend.kill('SIGTERM');
    await new Promise(done => backend.exitCode !== null ? done() : backend.once('exit', done));
    await rm(root, { recursive: true, force: true });
}
