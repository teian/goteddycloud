# Copied-data migration and rollback

This rewrite is incomplete. Use these steps for an isolated evaluation; do not
switch production routing until the compatibility and hardware gates pass.
Never run C and Go writers against the same deployment tree.

1. Stop the source writer before taking a consistent snapshot. Record the C
   version, configuration, certificates and hashes of the snapshot. Include
   configured absolute content/library paths, not only the deployment directory.
2. Copy the snapshot into a separate directory. Keep the original snapshot
   immutable. Set permissions so server keys are readable only by the service user.
3. Run `goteddycloud -base_path /path/to/copy -validate`. This reads existing
   configuration, validates types/ranges and imported server certificate pairs,
   and checks configured directories. It creates no files, opens no listeners,
   generates no keys and contacts no cloud. Missing or invalid input fails clearly.
4. For isolated evaluation, use `-http 127.0.0.1:18081 -httpsecure 127.0.0.1:19443
   -box 127.0.0.1:19444`. Disable cloud/MQTT in the copied configuration before
   starting tests. Exercise fixture comparisons and device tests on isolated routing.
5. After acceptance, stop both evaluation and original writers, take a fresh
   snapshot, repeat validation, and switch routing to the chosen Go data copy.
6. To roll back, stop Go first, restore the complete pre-cutover snapshot and
   route devices back to the pinned C deployment. Include certificates and all
   externally configured data roots; do not merge files from different snapshots.

`python3 scripts/migration-rehearsal.py --binary bin/goteddycloud` generates private
synthetic fixtures, verifies that read-only validation preserves every file hash,
changes a setting, and restores the exact snapshot including private key bytes.
It never reads a real deployment. This is a local migration/rollback rehearsal,
not a physical-device cutover test or proof of full replacement compatibility.

## Official develop migration

The current source target is `8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1`. Preserve `config/web_users.json` with owner-only permissions, deploy the pinned official frontend, and review the new private-download opt-in and box certificate directories. See [development port](develop-port.md) for setup, recovery and deliberate validation differences. Sessions reset at restart.
