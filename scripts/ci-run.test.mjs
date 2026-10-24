// SPDX-License-Identifier: GPL-2.0-only
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const runner = fileURLToPath(new URL('./ci-run.sh', import.meta.url));
for (const status of [0, 7]) {
    test(`CI logger preserves command exit ${status} and both output streams`, () => {
        const cwd = mkdtempSync(join(tmpdir(), 'goteddycloud-ci-log-'));
        try {
            const result = spawnSync('bash', [runner, 'example', 'bash', '-c', `echo stdout; echo stderr >&2; exit ${status}`], { cwd, encoding: 'utf8' });
            assert.equal(result.status, status);
            const saved = readFileSync(join(cwd, '.cache/ci-checks/example.log'), 'utf8');
            assert.equal(saved, 'stdout\nstderr\n');
            assert.equal(result.stdout, saved);
        } finally { rmSync(cwd, { recursive: true, force: true }); }
    });
}
test('CI logger rejects a path outside its log directory', () => {
    const result = spawnSync('bash', [runner, '../escape', 'true'], { encoding: 'utf8' });
    assert.equal(result.status, 2);
});
