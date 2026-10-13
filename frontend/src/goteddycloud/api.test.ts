// SPDX-License-Identifier: GPL-2.0-only
// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { accessApi, downloadUrl, itemKey, memberRole, areaSwitchUrl, readCloudStatus } from "./api";
import { resetBasePathCache } from "../utils/basePath";

describe("Go access API integration", () => {
    it("clears explicit old area and device selectors while preserving other query parameters", () => {
        expect(
            areaSwitchUrl(
                "https://example.test/proxy/web/tonies/content?overlay=old&area=previous&view=library",
            ),
        ).toBe("https://example.test/proxy/web/tonies/content?view=library");
    });
    beforeEach(() => {
        vi.restoreAllMocks();
        sessionStorage.clear();
        document.head.innerHTML = "";
        resetBasePathCache();
    });
    it("reads disabled or unavailable cloud status as ordinary data through the proxy prefix", async () => {
        document.head.innerHTML = '<base href="https://example.test/proxy/web/">';
        const fetch = vi.spyOn(globalThis, "fetch");
        for (const enabled of [false, true]) {
            fetch.mockResolvedValueOnce(Response.json({ enabled, available: false }));
            await expect(readCloudStatus()).resolves.toEqual({ enabled, available: false });
        }
        expect(fetch).toHaveBeenCalledTimes(2);
        for (const [url] of fetch.mock.calls) {
            expect(new URL(String(url), "https://example.test").pathname).toBe("/proxy/api/cloud/status");
        }
    });
    it("uses the existing session and reverse-proxy prefix", async () => {
        document.head.innerHTML = '<base href="https://example.test/ingress/test/web/">';
        sessionStorage.setItem("teddycloud_web_token", "token");
        const fetch = vi
            .spyOn(globalThis, "fetch")
            .mockResolvedValue(new Response('{"shared":true}'));
        await expect(
            accessApi("catalog/grant", { area: "a", username: "bob", all: true }),
        ).resolves.toEqual({ shared: true });
        const [url, init] = fetch.mock.calls[0];
        expect(new URL(String(url), "https://example.test").pathname).toBe(
            "/ingress/test/api/auth/catalog/grant",
        );
        expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer token");
        expect(init?.credentials).toBe("same-origin");
        expect(init?.method).toBe("POST");
        expect(JSON.parse(String(init?.body))).toEqual({ area: "a", username: "bob", all: true });
    });
    it("reports denied grants and reauthenticates expired sessions", async () => {
        const listener = vi.fn();
        window.addEventListener("teddycloud-auth-required", listener);
        const fetch = vi.spyOn(globalThis, "fetch");
        fetch.mockResolvedValueOnce(
            new Response('{"message":"Catalog not shared"}', { status: 403 }),
        );
        await expect(accessApi("catalog/items?area=a")).rejects.toThrow("Catalog not shared");
        expect(listener).not.toHaveBeenCalled();
        fetch.mockResolvedValueOnce(new Response('{"message":"Login required"}', { status: 401 }));
        await expect(accessApi("profile")).rejects.toThrow("Login required");
        expect(listener).toHaveBeenCalledOnce();
        window.removeEventListener("teddycloud-auth-required", listener);
    });
    it("encodes download paths and keeps root/path selections distinct", () => {
        document.head.innerHTML = '<base href="https://example.test/proxy/web/">';
        const item = { root: "library" as const, path: "folder/a & b.mp3" };
        const url = new URL(downloadUrl("area", item), "https://example.test");
        expect(url.pathname).toBe("/proxy/api/auth/catalog/download");
        expect(url.searchParams.get("path")).toBe(item.path);
        expect(itemKey(item)).not.toBe(itemKey({ ...item, root: "content" }));
    });
    it("uses area membership roles independently of account defaults", () => {
        expect(
            memberRole(
                {
                    id: "a",
                    name: "Family",
                    boxes: [],
                    members: [{ username: "Alice", role: "reader" }],
                },
                "alice",
            ),
        ).toBe("reader");
    });
});
