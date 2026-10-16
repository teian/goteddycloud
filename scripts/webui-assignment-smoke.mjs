// SPDX-License-Identifier: GPL-2.0-only
// Verify assignment using an isolated box library and synthetic catalog/audio.
import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import { mkdtemp, readFile, writeFile, mkdir, cp, rm } from 'node:fs/promises';
import { resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';
const { chromium } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE || '.cache/browser-tools/node_modules/playwright/index.mjs')));
const root = await mkdtemp(resolve('.cache/webui-assignment-'));
const fixture = join(root, 'fixture');
const binary = join(root, 'goteddycloud');
const id = '001122334455';
const ruid = '11000000500304e0';
let server, browser;
let logs = '';
try {
    execFileSync('go', ['build', '-o', binary, './cmd/goteddycloud']);
    execFileSync('go', ['run', './cmd/fixturegen', '-output', fixture]);
    const overlayPath = join(fixture, 'config/config.overlay.ini');
    await writeFile(overlayPath, await readFile(overlayPath, 'utf8') + `\noverlay.${id}.core.librarydir=box-library\noverlay.${id}.core.contentdir=box-content\n`);
    const library = join(fixture, 'data/box-library');
    await mkdir(library, { recursive: true });
    const cloudTagDir = join(fixture, 'data/content/box-content/72A80421');
    await mkdir(cloudTagDir, { recursive: true });
    await writeFile(join(cloudTagDir, '500304E0.json'), JSON.stringify({
        nocloud: false, cloud_auth: 'AB'.repeat(32), cloud_ruid: '72a80421500304e0',
    }));
    execFileSync(binary, ['-base_path', fixture, '-encode', join(library, 'matched.taf'), join(fixture, 'tone1.wav')]);
    await rm(join(fixture, 'data/www/web'), { recursive: true });
    await cp(resolve(process.env.FRONTEND_DIST || '.cache/goteddycloud-web/dist'), join(fixture, 'data/www/web'), { recursive: true });
    server = spawn(binary, ['-base_path', fixture, '-http', '127.0.0.1:0', '-httpsecure', 'off', '-box', 'off']);
    server.stdout.on('data', data => { logs += data; });
    server.stderr.on('data', data => { logs += data; });
    let http;
    for (let i = 0; i < 100; i++) {
        http = logs.match(/http:(127\.0\.0\.1:\d+)/)?.[1];
        if (http) break;
        if (server.exitCode !== null) throw new Error(logs);
        await new Promise(resolve => setTimeout(resolve, 100));
    }
    assert(http, logs);
    const url = 'http://' + http;
    const index = await (await fetch(`${url}/api/fileIndexV2?special=library&overlay=${id}`)).json();
    const header = index.files.find(file => file.name === 'matched.taf').tafHeader;
    assert(header?.valid, JSON.stringify(index));
    assert.deepEqual((await (await fetch(`${url}/api/fileIndexV2?special=library`)).json()).files, []);
    const entry = {
        model: 'ASSIGN_TEST', series: 'Synthetic Assignment', episodes: 'Matching episode',
        pic: '/img_unknown.png', language: 'en', tracks: ['Synthetic tone'],
        audio_id: ['999', String(header.audioId)], hash: ['00'.repeat(20), header.sha1Hash],
    };
    await writeFile(join(fixture, 'config/tonies.json'), JSON.stringify([entry, { model: 'META_ONLY', series: 'Metadata without audio', episodes: 'No local audio', pic: '/img_unknown.png' }]));
    assert.equal((await fetch(url + '/api/toniesJsonReload')).status, 200);
    assert.equal((await fetch(`${url}/content/json/set/${ruid}?overlay=${id}`, { method: 'POST', body: 'tonie_model=&source=&hide=false' })).status, 200);
    browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE_PATH ? { executablePath: process.env.BROWSER_EXECUTABLE_PATH } : {}) });
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    await context.addInitScript(() => {
        localStorage.setItem('contentOverlay', '001122334455');
        localStorage.setItem('i18nextLng', 'en');
        localStorage.setItem('theme', 'dark');
    });
    const page = await context.newPage();
    await page.addInitScript(() => {
        window.cloudDownloadAuthRequests = 0;
        window.addEventListener('teddycloud-auth-required', () => window.cloudDownloadAuthRequests++);
    });
    await page.route('**/api/cloud/status', route => route.fulfill({ json: { enabled: true, available: false } }));
    const errors = [];
    const requests = [];
    page.on('pageerror', error => errors.push(String(error)));
    page.on('request', request => { if (request.url().includes('/api/fileIndexV2')) requests.push(request.url()); });
    await page.goto(url + '/web/tonies');
    const card = page.locator(`[data-ruid="${ruid}"]`);
    await card.locator('[aria-label="edit"]').click();
    await page.locator('[aria-label="folder-open"]').click();
    const chooser = page.getByRole('dialog', { name: 'Select file' });
    await chooser.getByText('matched.taf', { exact: true }).filter({ visible: true }).first().waitFor();
    assert(requests.some(request => new URL(request).searchParams.get('overlay') === id));
    await chooser.getByRole('tab', { name: 'tonies.json' }).click();
    await chooser.getByPlaceholder('Search for a model').fill('ASSIGN_TEST');
    await page.getByText('[ASSIGN_TEST] Synthetic Assignment - Matching episode', { exact: true }).click();
    await chooser.getByRole('button', { name: 'OK', exact: true }).click();
    await page.getByRole('button', { name: /Save$/ }).click();
    await card.getByText('Synthetic Assignment', { exact: true }).waitFor();
    const tag = (await (await fetch(`${url}/api/getTagInfo?ruid=${ruid}&overlay=${id}`)).json()).tagInfo;
    assert.equal(tag.source, 'lib://matched.taf');
    assert.equal(tag.tonieInfo.model, 'ASSIGN_TEST');
    assert.equal(tag.valid, true);
    assert.equal(tag.nocloud, true);
    await page.screenshot({ path: resolve('.cache/webui-assignment.png'), fullPage: true });
    // Metadata assignment also works when no corresponding audio is in the library.
    await card.locator('[aria-label="edit"]').click();
    await page.locator('[aria-label="folder-open"]').click();
    await chooser.getByRole('tab', { name: 'tonies.json' }).click();
    await chooser.getByPlaceholder('Search for a model').fill('META_ONLY');
    await page.getByText('[META_ONLY] Metadata without audio - No local audio', { exact: true }).click();
    await chooser.getByRole('button', { name: 'OK', exact: true }).click();
    await page.getByRole('button', { name: /Save$/ }).click();
    await card.getByText('Metadata without audio', { exact: true }).waitFor();
    const updated = (await (await fetch(`${url}/api/getTagInfo?ruid=${ruid}&overlay=${id}`)).json()).tagInfo;
    assert.equal(updated.tonieInfo.model, 'META_ONLY');
    assert.equal(updated.source, 'lib://matched.taf');
    // A global offline probe must not disable downloads using a box identity.
    const cloudDownloadPath = '**/content/download/72A80421/500304E0?*';
    await page.route(cloudDownloadPath, route => route.fulfill({
        status: 401, headers: { 'X-TeddyCloud-Error-Origin': 'tonies-cloud' }, body: '',
    }));
    const [response] = await Promise.all([
        page.waitForResponse(response => response.url().includes('/content/download/72A80421/500304E0'), { timeout: 10000 }),
        page.locator('[data-ruid="72a80421500304e0"] [aria-label="download"]').click({ timeout: 10000 }),
    ]);
    assert.equal(new URL(response.url()).searchParams.get('overlay'), id);
    assert.equal(response.status(), 401);
    await page.getByText(/HTTP 401/).filter({ visible: true }).first().waitFor();
    assert.equal(await page.evaluate(() => window.cloudDownloadAuthRequests), 0);
    assert.deepEqual(errors, []);
    console.log('PASS selected box library, tonies.json assignment, later audio revision, persisted model/source and metadata-only assignment');
    console.log('PASS cloud download remains clickable with a failed global status probe, uses the selected box, reports HTTP 401 and preserves the web session');
} catch (error) {
    console.error(logs);
    if (browser) for (const context of browser.contexts()) for (const page of context.pages()) {
        await page.screenshot({ path: resolve('.cache/webui-assignment-failure.png'), fullPage: true }).catch(() => {});
    }
    throw error;
} finally {
    if (browser) await browser.close();
    if (server) { server.kill('SIGTERM'); await new Promise(resolve => server.once('exit', resolve)); }
    await rm(root, { recursive: true, force: true });
}
