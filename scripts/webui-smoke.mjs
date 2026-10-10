#!/usr/bin/env node
// Build the integrated frontend first. Playwright is an optional local QA tool.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { spawn, execFileSync } from 'node:child_process';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';
const { chromium } = await import(pathToFileURL(resolve(process.env.PLAYWRIGHT_MODULE || '.cache/browser-tools/node_modules/playwright/index.mjs')));
const root = await mkdtemp(resolve('.cache/webui-smoke-'));
const fixture = join(root, 'fixture');
execFileSync('go', ['run', '-buildvcs=false', './cmd/fixturegen', '-output', fixture]);
execFileSync('bash', ['scripts/frontend.sh', 'install-fixture', fixture]);
const server = spawn(resolve('bin/goteddycloud'), ['-base_path', fixture, '-http', '127.0.0.1:0', '-httpsecure', 'off', '-box', 'off']);
let logs = '';
server.stderr.on('data', data => { logs += data; });
server.stdout.on('data', data => { logs += data; });
let browser;
try {
    let address;
    for (let i = 0; i < 100; i++) {
        address = logs.match(/http:(127\.0\.0\.1:\d+)/)?.[1];
        if (address) break;
        if (server.exitCode !== null) throw new Error(logs);
        await new Promise(resolve => setTimeout(resolve, 100));
    }
    assert(address, logs);
    const url = 'http://' + address;
    const clock = await fetch(url + '/api/time');
    assert.equal(clock.status, 200);
    assert.match(await clock.text(), /^\d+$/);
    const plugin = await fetch(url + '/plugins/synthetic/plugin.json');
    assert.equal(plugin.status, 200);
    assert.equal((await plugin.json()).pluginName, 'Synthetic fixture');
    browser = await chromium.launch({ headless: true, ...(process.env.BROWSER_EXECUTABLE_PATH ? { executablePath: process.env.BROWSER_EXECUTABLE_PATH } : {}) });
    const exceptions = [];
    async function contextPage(locale = 'en-US') {
        const context = await browser.newContext({ locale, viewport: { width: 1440, height: 1000 } });
        const page = await context.newPage();
        page.on('pageerror', error => exceptions.push(String(error)));
        return page;
    }
    const admin = await contextPage();
    // Load the actual embedded Go module under the tool page's CSP without USB.
    await admin.goto(url + '/tools/cc3200');
    await admin.locator('#connect').waitFor();
    await admin.evaluate(async () => {
        const module = await import('/tools/cc3200-go.mjs');
        await module.loadCC3200Go();
        if (typeof globalThis.cc3200Go !== 'function') throw new Error('Go CC3200 core missing');
    });
    const missingAssets = [];
    admin.on('response', response => {
        if (/\/assets\/.*\.(js|css)$/.test(response.url()) && response.status() !== 200) {
            missingAssets.push(response.url());
        }
    });
    for (const path of ['/web', '/web/', '/web/settings/users']) {
        await admin.goto(url + path);
        await admin.locator('#teddycloud-header').waitFor();
    }
    assert.deepEqual(missingAssets, []);
    const wasmWarnings = [];
    admin.on('console', message => {
        if (/wasm streaming compile failed|falling back to ArrayBuffer/i.test(message.text())) {
            wasmWarnings.push(message.text());
        }
    });
    await admin.evaluate(async () => {
        const response = await fetch(new URL('wasm/taf_encoder.wasm', document.baseURI));
        if (!response.ok || response.headers.get('content-type') !== 'application/wasm') {
            throw new Error('Incorrect WASM response: ' + response.status + ' ' + response.headers.get('content-type'));
        }
        await WebAssembly.compileStreaming(response);
        await new Promise((resolve, reject) => {
            const script = document.createElement('script');
            script.src = new URL('wasm/taf_encoder.js', document.baseURI).href;
            script.onload = resolve;
            script.onerror = reject;
            document.head.appendChild(script);
        });
        await window.createTafEncoder();
    });
    assert.deepEqual(wasmWarnings, []);
    await admin.goto(url + '/web/tonieboxes');
    await admin.getByText('Synthetic Box One', { exact: true }).waitFor();
    assert.equal(await admin.getByText('Error fetching Tonieboxes Models', { exact: true }).count(), 0);
    const guideProbes = [];
    admin.on('request', request => {
        if (request.url().startsWith('https://forum.revvox.de/') && ['fetch', 'xhr'].includes(request.resourceType())) {
            guideProbes.push(request.url());
        }
    });
    await admin.goto(url + '/web/tonieboxes/boxsetup');
    for (const name of ['CC3200', 'CC3235', 'ESP32']) {
        await admin.getByRole('link', { name: new RegExp('TeddyCloud ' + name + ' Newbie') }).waitFor();
    }
    assert.deepEqual(guideProbes, []);
    const failurePage = await contextPage();
    await failurePage.route('**/api/tonieboxesJson', route => route.fulfill({ status: 500, body: 'unavailable' }));
    await failurePage.goto(url + '/web/tonieboxes');
    await failurePage.getByText('Error fetching Tonieboxes Models', { exact: true }).waitFor();
    await failurePage.context().close();
    const missingPreviews = [];
    admin.on('response', response => {
        if (response.url().endsWith('/preview.png') && response.status() !== 200) {
            missingPreviews.push(response.url());
        }
    });
    await admin.goto(url + '/web/community/tcplugins');
    for (const name of ['CC3200 flashing', 'Synthetic fixture']) {
        const preview = admin.getByRole('img', { name: name + ' preview', exact: true });
        await preview.waitFor();
        await preview.evaluate(img => {
            if (img.complete) {
                if (!img.naturalWidth) throw new Error('Preview failed: ' + img.src);
                return;
            }
            return new Promise((resolve, reject) => {
                img.onload = resolve;
                img.onerror = () => reject(new Error('Preview failed: ' + img.src));
            });
        });
    }
    assert.deepEqual(missingPreviews, []);
    assert.equal(await admin.getByText('Benutzer und Bereiche', { exact: true }).count(), 0);
    const plugins = await fetch(url + '/api/plugins/get');
    assert(!(await plugins.json()).includes('goteddycloud-users'));
    const legacy = await fetch(url + '/tools/users', { redirect: 'manual' });
    assert.equal(legacy.status, 302);
    assert.equal(legacy.headers.get('location'), '../web/settings/users');
    await admin.goto(url + '/web/settings/users');
    await admin.getByRole('heading', { name: 'Users and areas', exact: true }).waitFor();
    async function createAccount(name, reader = false) {
        const form = admin.locator('#createAccount');
        await form.getByLabel('Username', { exact: true }).fill(name);
        await form.getByLabel('Password', { exact: true }).fill('test-secret');
        if (reader) {
            await form.getByRole('combobox').click();
            await admin.getByText('User (default: reader)', { exact: true }).last().click();
        }
        await form.locator('button[type=submit]').click();
        await form.getByLabel('Username', { exact: true }).waitFor();
        await admin.waitForFunction(name => Array.from(document.querySelectorAll('.ant-table-row')).some(row => row.textContent.includes(name)), name);
        await admin.getByRole('button', { name: 'Create', exact: true }).last().waitFor({ state: 'visible' });
    }
    await createAccount('admin');
    await admin.getByRole('switch', { name: 'Require web login' }).click();
    async function login(page, username) {
        await page.locator('input[autocomplete="username"]').fill(username);
        await page.locator('input[autocomplete="current-password"]').fill('test-secret');
        await page.locator('form button[type=submit]').click();
        await page.locator('#teddycloud-header').waitFor();
    }
    await login(admin, 'admin');
    await createAccount('alice');
    await createAccount('bob', true);
    const newArea = admin.locator('#createArea');
    await newArea.getByLabel('Name', { exact: true }).fill('Family');
    await newArea.locator('button[type=submit]').click();
    const familyRow = admin.locator('.ant-table-row').filter({ has: admin.getByText('Family', { exact: true }) });
    await familyRow.getByRole('button', { name: 'Edit', exact: true }).click();
    const editor = admin.locator('form').filter({ has: admin.getByRole('button', { name: 'Add existing user' }) });
    await editor.getByRole('button', { name: 'Add existing user' }).click();
    await editor.getByRole('textbox', { name: 'Username', exact: true }).last().fill('alice');
    await editor.getByRole('combobox', { name: 'Role', exact: true }).last().click();
    await admin.getByText('Area administrator', { exact: true }).last().click();
    await editor.getByRole('button', { name: 'Add existing user' }).click();
    await editor.getByRole('textbox', { name: 'Username', exact: true }).last().fill('bob');
    await editor.locator('button[type=submit]').click();
    await admin.waitForFunction(() => Array.from(document.querySelectorAll('.ant-table-row')).some(row => row.textContent.includes('Family') && row.textContent.includes('alice: Area administrator') && row.textContent.includes('bob: Reader')));
    await admin.screenshot({ path: '.cache/webui-users.png', fullPage: true, animations: 'disabled' });
    const areas = (await (await admin.request.get(url + '/api/auth/areas')).json()).areas;
    const aliceArea = areas.find(area => area.name === 'alice');
    const library = join(fixture, 'users', createHash('sha256').update(aliceArea.id).digest('hex'), 'data/library');
    await mkdir(library, { recursive: true });
    const wav = Buffer.alloc(44 + 4800); wav.write('RIFF'); wav.writeUInt32LE(wav.length - 8, 4); wav.write('WAVEfmt ', 8); wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22); wav.writeUInt32LE(48000, 24); wav.writeUInt32LE(96000, 28); wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34); wav.write('data', 36); wav.writeUInt32LE(4800, 40);
    await writeFile(join(library, 'shared.wav'), wav); await writeFile(join(library, 'private.wav'), wav);
    const alice = await contextPage();
    await alice.goto(url + '/web/tonies/shared'); await login(alice, 'alice');
    await alice.locator('.ant-table-row').filter({ has: alice.getByText('shared.wav', { exact: true }) }).getByRole('checkbox').check();
    const grant = alice.locator('#catalogGrant');
    await grant.getByLabel('Recipient (existing username)', { exact: true }).fill('bob');
    await grant.locator('button[type=submit]').click();
    await alice.getByText('Selected files: 1', { exact: true }).waitFor();
    const bob = await contextPage();
    await bob.goto(url + '/web/tonies/shared'); await login(bob, 'bob');
    // Bob's own catalog is empty. Select Alice's received catalog.
    await bob.getByRole('combobox', { name: 'Catalog', exact: true }).click();
    await bob.getByText('alice', { exact: true }).last().click();
    await bob.getByText('shared.wav', { exact: true }).waitFor();
    assert.equal(await bob.getByText('private.wav', { exact: true }).count(), 0);
    assert.equal(await bob.locator('#catalogGrant').count(), 0);
    const row = bob.locator('.ant-table-row').filter({ has: bob.getByText('shared.wav', { exact: true }) });
    const link = await row.getByRole('link', { name: 'Download' }).getAttribute('href');
    assert.equal((await bob.request.get(url + link)).status(), 200);
    assert.equal((await bob.request.get(url + link.replace('shared.wav', 'private.wav'))).status(), 403);
    await bob.getByRole('combobox', { name: 'Catalog', exact: true }).press('Escape');
    await bob.getByRole('heading', { name: 'Shared catalogs', exact: true }).click();
    await bob.screenshot({ path: '.cache/webui-shared.png', fullPage: true, animations: 'disabled' });
    await bob.setViewportSize({ width: 390, height: 844 });
    await bob.screenshot({ path: '.cache/webui-shared-mobile.png', fullPage: true, animations: 'disabled' });
    await bob.locator('#teddycloud-header button:has([aria-label="menu"])').click();
    await bob.locator('.ant-drawer').getByRole('combobox', { name: 'Area', exact: true }).waitFor();
    await bob.locator('.ant-drawer-close').first().click();
    await grant.getByLabel('Recipient (existing username)', { exact: true }).fill('bob');
    await grant.getByLabel('Entire catalog, including future content').check();
    await grant.locator('button[type=submit]').click();
    await alice.getByRole('cell', { name: 'Entire catalog, including future content', exact: true }).waitFor();
    await bob.getByRole('button', { name: 'Refresh', exact: true }).click();
    await bob.getByText('private.wav', { exact: true }).waitFor();
    await alice.getByRole('button', { name: 'Revoke', exact: true }).click();
    await alice.getByText('bob', { exact: true }).waitFor({ state: 'hidden' });
    await bob.getByRole('button', { name: 'Refresh', exact: true }).click();
    await bob.getByText('shared.wav', { exact: true }).waitFor({ state: 'hidden' });
    await bob.goto(url + '/web/settings');
    await bob.waitForURL('**/web/settings/users');
    await bob.getByRole('heading', { name: 'Users and areas', exact: true }).waitFor();
    await bob.getByText('Family', { exact: true }).waitFor();
    assert.equal(await bob.locator('#createAccount').count(), 0);
    assert.equal(await bob.getByRole('button', { name: 'Edit', exact: true }).count(), 0);
    await alice.goto(url + '/web/settings/users?overlay=old&area=previous');
    await alice.getByRole('heading', { name: 'Users and areas', exact: true }).waitFor();
    const family = alice.locator('.ant-table-row').filter({ has: alice.getByText('Family', { exact: true }) });
    await family.getByRole('button', { name: 'Edit', exact: true }).waitFor();
    assert.equal(await family.getByRole('button', { name: 'Edit', exact: true }).count(), 1);
    assert.equal(await alice.locator('#createAccount').count(), 0);
    await alice.locator('#teddycloud-header').getByRole('combobox', { name: 'Area', exact: true }).click();
    await alice.getByText('Family', { exact: true }).last().click();
    await alice.waitForFunction(() => document.querySelector('#teddycloud-header')?.textContent.includes('Family'));
    const status = await (await alice.request.get(url + '/api/auth/status')).json();
    assert.equal(status.selectedArea, areas.find(area => area.name === 'Family').id);
    assert.equal(status.areaRole, 'admin');
    assert.equal(new URL(alice.url()).searchParams.has('overlay'), false);
    assert.equal(new URL(alice.url()).searchParams.has('area'), false);
    await bob.evaluate(() => localStorage.setItem('user:bob:theme', 'dark'));
    await bob.goto(url + '/web/tonies/shared');
    await bob.getByRole('heading', { name: 'Shared catalogs', exact: true }).waitFor();
    await bob.screenshot({ path: '.cache/webui-shared-dark.png', fullPage: true, animations: 'disabled' });
    const german = await contextPage('de-DE');
    await german.goto(url + '/web/settings/users'); await login(german, 'alice');
    await german.getByRole('heading', { name: 'Benutzer und Bereiche', exact: true }).waitFor();
    await german.screenshot({ path: '.cache/webui-users-de.png', fullPage: true, animations: 'disabled' });
    assert.deepEqual(exceptions, []);
    console.log('PASS native Web UI account setup/login, shared areas and member roles, read-only partial/full catalog grants, downloads, revocation, role-aware controls, header area switch and responsive rendering');
} catch (error) {
    if (browser) for (const context of browser.contexts()) for (const page of context.pages()) {
        await page.screenshot({ path: `.cache/webui-failure-${browser.contexts().indexOf(context)}.png`, fullPage: true }).catch(() => {});
        await writeFile(`.cache/webui-failure-${browser.contexts().indexOf(context)}.html`, await page.content());
    }
    console.error(logs); throw error;
} finally {
    await browser?.close();
    server.kill('SIGTERM');
    await new Promise(resolve => { if (server.exitCode !== null) resolve(); else server.once('exit', resolve); });
    await rm(root, { recursive: true, force: true });
}
