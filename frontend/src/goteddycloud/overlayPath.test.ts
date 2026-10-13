// SPDX-License-Identifier: GPL-2.0-only
// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { TeddyCloudApi } from "../api/apis/TeddyCloudApi";
import { Configuration } from "../api/runtime";
import { withOverlay } from "./overlayPath";

describe("box overlay request URLs", () => {
    const ruid = "97763c1a500304e0";
    const overlay = "6CC374F2CBD8";

    it("refreshes the actual tag card with two distinct query parameters", async () => {
        const tag = { ruid, tonieInfo: { model: "TEST" } };
        const fetchApi = vi.fn().mockResolvedValue(Response.json({ tagInfo: tag }));
        const api = new TeddyCloudApi(new Configuration({ basePath: "/proxy", fetchApi }));
        await expect(api.apiGetTagInfo(ruid, overlay)).resolves.toEqual(tag);
        expect(fetchApi).toHaveBeenCalledExactlyOnceWith(
            `/proxy/api/getTagInfo?ruid=${ruid}&overlay=${overlay}`,
            expect.objectContaining({ method: "GET" }),
        );
    });

    it("keeps POST parameters and body when changing cloud access", async () => {
        const fetchApi = vi.fn().mockResolvedValue(new Response("OK"));
        const api = new TeddyCloudApi(new Configuration({ basePath: "", fetchApi }));
        await api.apiPostTeddyCloudRaw(
            "/content/json/set/97763C1A/500304E0?mode=edit",
            "nocloud=false",
            overlay,
        );
        const [url, request] = fetchApi.mock.calls[0];
        expect(url).toBe(`/content/json/set/97763C1A/500304E0?mode=edit&overlay=${overlay}`);
        expect(request.method).toBe("POST");
        expect(request.body).toBeInstanceOf(Blob);
    });

    it("encodes values, replaces a stale overlay and preserves fragments", () => {
        const path = withOverlay("/api/getTagInfo?ruid=a%26b&overlay=old#section", "box & 1");
        const url = new URL(path, "https://example.test");
        expect(url.searchParams.get("ruid")).toBe("a&b");
        expect(url.searchParams.getAll("overlay")).toEqual(["box & 1"]);
        expect(url.hash).toBe("#section");
    });

    it.each([undefined, ""])("leaves the path unchanged without an overlay (%s)", (value) => {
        expect(withOverlay("/api/getTagInfo?ruid=a%20b", value)).toBe("/api/getTagInfo?ruid=a%20b");
    });

    it.each(["/api/getTagIndex", "/api/getTagIndex?"])("handles an empty query (%s)", (path) => {
        expect(withOverlay(path, overlay)).toBe(`/api/getTagIndex?overlay=${overlay}`);
    });
});
