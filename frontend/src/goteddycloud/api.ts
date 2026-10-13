// SPDX-License-Identifier: GPL-2.0-only
import { defaultAPIConfig } from "../config/defaultApiConfig";
import { userStorage } from "../utils/storage/userStorage";
import { backendUrl } from "../utils/basePath";

export type Role = "admin" | "editor" | "reader";
export type CloudStatus = { enabled: boolean; available: boolean };
export type Member = { username: string; role: Role };
export type Area = { id: string; name: string; members: Member[]; boxes: string[] };
export type User = { username: string; role: Role; defaultArea: string };
export type Profile = User & { selectedArea: string; areaRole: Role | ""; areas: Area[] };
export type CatalogItem = { root: "content" | "library"; path: string };
export type CatalogFile = CatalogItem & { size: number; title: string };
export type Recipient = { username: string; all: boolean; items: CatalogItem[] | null };
export type Catalog = { area: string; name: string; manage: boolean; recipients: Recipient[] };

// Reuse the frontend's bearer/cookie handling and runtime reverse-proxy prefix.
export async function managementApi<T>(
    path: string,
    payload?: unknown,
    signal?: AbortSignal,
): Promise<T> {
    const config = defaultAPIConfig();
    const fetchApi = config.fetchApi ?? fetch;
    const response = await fetchApi(backendUrl(path), {
        method: payload === undefined ? "GET" : "POST",
        signal,
        ...(payload === undefined
            ? {}
            : {
                  headers: { "Content-Type": "application/json" },
                  body: JSON.stringify(payload),
              }),
    });
    const text = await response.text();
    let data;
    try {
        data = text ? JSON.parse(text) : {};
    } catch {
        throw new Error(`HTTP ${response.status}: invalid server response`);
    }
    if (!response.ok) {
        if (response.status === 401) window.dispatchEvent(new Event("teddycloud-auth-required"));
        throw new Error(data.message || data.error || `HTTP ${response.status}`);
    }
    return data as T;
}
export function itemKey(item: CatalogItem): string {
    return JSON.stringify([item.root, item.path]);
}
export function downloadUrl(area: string, item: CatalogItem): string {
    return backendUrl(
        "/api/auth/catalog/download?" +
            new URLSearchParams({ area, root: item.root, path: item.path }),
    );
}
export function memberRole(area: Area, username: string): Role | undefined {
    return area.members.find((member) => member.username.toLowerCase() === username.toLowerCase())
        ?.role;
}

export function accessApi<T>(path: string, payload?: unknown, signal?: AbortSignal): Promise<T> {
    return managementApi<T>("/api/auth/" + path, payload, signal);
}
export function readCloudStatus(signal?: AbortSignal): Promise<CloudStatus> {
    return managementApi<CloudStatus>("/api/cloud/status", undefined, signal);
}

export function areaSwitchUrl(current: string): string {
    const url = new URL(current);
    url.searchParams.delete("area");
    url.searchParams.delete("overlay");
    return url.toString();
}
export async function switchArea(area: string): Promise<void> {
    await accessApi("area", { area });
    userStorage.removeItem("contentOverlay");
    window.location.assign(areaSwitchUrl(window.location.href));
}
