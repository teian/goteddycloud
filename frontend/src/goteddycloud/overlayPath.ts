// SPDX-License-Identifier: GPL-2.0-only

export function withOverlay(path: string, overlay?: string): string {
    if (!overlay) return path;
    const hashAt = path.indexOf("#");
    const hash = hashAt < 0 ? "" : path.slice(hashAt);
    const resource = hashAt < 0 ? path : path.slice(0, hashAt);
    const queryAt = resource.indexOf("?");
    const base = queryAt < 0 ? resource : resource.slice(0, queryAt);
    const query = new URLSearchParams(queryAt < 0 ? "" : resource.slice(queryAt + 1));
    query.set("overlay", overlay);
    return `${base}?${query.toString()}${hash}`;
}
