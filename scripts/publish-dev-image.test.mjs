// SPDX-License-Identifier: GPL-2.0-only
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { spawnSync } from 'node:child_process';
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { basename, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const script = fileURLToPath(new URL('../publish-dev-image.sh', import.meta.url));
const fakeTool = `#!${process.execPath}
import { appendFileSync, writeFileSync } from 'node:fs';
import { basename } from 'node:path';
const tool = basename(process.argv[1]);
const args = process.argv.slice(2);
appendFileSync(process.env.CALLS, JSON.stringify({tool, args}) + '\\n');
if (tool === 'git') console.log('123456789abcdef0123456789abcdef0123456789a');
if (tool === 'docker' && args[0] === 'version') console.log(process.env.TEST_PLATFORM || 'linux/amd64');
if (tool === 'docker' && args[0] === 'buildx' && args[1] === 'inspect' && !args.includes('--bootstrap')) process.exit(1);
const stage = tool === 'python3' ? basename(args[0]) :
    tool === 'docker' && args[1] === 'build' ? (args.includes('VERSION=dev-layercheck') ? 'variant' : 'build') :
    tool === 'skopeo' && args.includes('--all') ? 'publish' : '';
if (stage && stage === process.env.FAIL_STAGE) process.exit(17);
if (stage === 'publish') writeFileSync(args[args.indexOf('--digestfile') + 1], 'sha256:verified\\n');
`;

function run(failure = '', platform = 'linux/amd64') {
    const cwd = mkdtempSync(join(tmpdir(), 'publish image test-'));
    try {
        const bin = join(cwd, 'bin');
        mkdirSync(bin);
        mkdirSync(join(cwd, 'frontend'));
        writeFileSync(join(cwd, 'frontend/upstream-revision'), 'frontend-sha\n');
        copyFileSync(script, join(cwd, 'publish-dev-image.sh'));
        for (const name of ['docker', 'git', 'python3', 'skopeo']) {
            writeFileSync(join(bin, name), fakeTool, { mode: 0o755 });
        }
        const callsPath = join(cwd, 'calls.jsonl');
        const env = { ...process.env, PATH: `${bin}:${process.env.PATH}`, CALLS: callsPath, TMPDIR: cwd,
            FAIL_STAGE: failure, TEST_PLATFORM: platform };
        delete env.PUBLISH_BUILDER;
        const result = spawnSync('bash', [join(cwd, 'publish-dev-image.sh')], { cwd, env, encoding: 'utf8' });
        const calls = readFileSync(callsPath, 'utf8').trim().split('\n').map(line => JSON.parse(line));
        return { ...result, calls };
    } finally { rmSync(cwd, { recursive: true, force: true }); }
}

for (const platform of ['linux/amd64', 'linux/arm64', 'linux/arm']) {
    test(`publisher checks then copies the original multi-platform OCI layers on ${platform}`, () => {
        const result = run('', platform);
        assert.equal(result.status, 0, result.stderr);
        const builds = result.calls.filter(c => c.tool === 'docker' && c.args[1] === 'build');
        assert.equal(builds.length, 2, 'never rebuild for publication');
        const main = builds[0].args;
        assert.equal(main[main.indexOf('--platform') + 1], 'linux/amd64,linux/arm/v7,linux/arm64');
        assert.equal(main[main.indexOf('--target') + 1], 'ffmpeg');
        assert.ok(main.includes('--provenance=mode=max') && main.includes('--sbom=true'));
        assert.ok(main.includes('type=registry,ref=ghcr.io/teian/goteddycloud:buildcache'));
        assert.ok(main.includes('type=registry,ref=ghcr.io/teian/goteddycloud:dev-buildcache'));
        assert.equal(main[main.indexOf('--cache-to') + 1], 'type=registry,ref=ghcr.io/teian/goteddycloud:dev-buildcache,mode=max');
        const publishIndex = result.calls.findIndex(c => c.tool === 'skopeo' && c.args.includes('--all'));
        const publish = result.calls[publishIndex].args;
        assert.ok(publish.includes('--preserve-digests'));
        assert.equal(publish.at(-1), 'docker://ghcr.io/teian/goteddycloud:dev');
        const archive = main[main.indexOf('--output') + 1].slice('type=oci,dest='.length);
        assert.equal(publish.at(-2), `oci-archive:${archive}:dev`);
        for (const check of ['check-container-architectures.py', 'container-webui-smoke.py', 'check-container-layers.py']) {
            const index = result.calls.findIndex(c => c.tool === 'python3' && basename(c.args[0]) === check);
            assert.ok(index >= 0 && index < publishIndex, `${check} gates publication`);
        }
        const native = result.calls.find(c => c.tool === 'skopeo' && !c.args.includes('--all')).args;
        assert.ok(native.includes(`oci-archive:${archive}:dev`), 'native checks use the exact inspected build');
        assert.ok(result.calls.some(c => c.tool === 'docker' && c.args.includes('-docker-test')));
        assert.ok(result.calls.some(c => c.tool === 'docker' && c.args.includes('--entrypoint') && c.args.includes('ffmpeg')));
        assert.ok(result.calls.some(c => c.tool === 'docker' && c.args[1] === 'create' && c.args.includes('docker-container')));
        assert.ok(result.calls.some(c => c.tool === 'docker' && c.args[0] === 'image' && c.args[1] === 'rm'));
        assert.match(result.stdout, /Published .*sha256:verified/);
    });
}
for (const stage of ['build', 'check-container-architectures.py', 'container-webui-smoke.py', 'variant', 'check-container-layers.py']) {
    test(`publisher stops before changing the dev image when ${stage} fails`, () => {
        const result = run(stage);
        assert.equal(result.status, 17, result.stderr);
        assert.ok(!result.calls.some(c => c.tool === 'skopeo' && c.args.includes('--all')));
    });
}
test('publisher reports registry copy failures without claiming success', () => {
    const result = run('publish');
    assert.equal(result.status, 1);
    assert.match(result.stderr, /Push failed/);
    assert.doesNotMatch(result.stdout, /Published /);
});
test('help does not start a build', () => {
    const result = spawnSync('bash', [script, '--help'], { encoding: 'utf8' });
    assert.equal(result.status, 0);
    assert.match(result.stdout, /Skopeo/);
});
