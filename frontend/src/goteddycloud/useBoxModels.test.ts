// SPDX-License-Identifier: GPL-2.0-only
import { describe, expect, it } from "vitest";
import { loadBoxModels } from "./useBoxModels";

describe("box model catalogs", () => {
    it("accepts successful empty catalogs without an error", async () => {
        expect(await loadBoxModels(async () => new Response("[]"))).toEqual({
            boxModelImages: [],
            error: false,
        });
    });
    it("retains valid models if the other source fails", async () => {
        const model = { id: "test", name: "Test", img_src: "/test.png" };
        expect(
            await loadBoxModels(async (path) =>
                path.endsWith("CustomJson")
                    ? new Response("", { status: 500 })
                    : Response.json([model]),
            ),
        ).toEqual({
            boxModelImages: [model],
            error: true,
        });
    });
    it("reports network failures and invalid catalog data", async () => {
        expect(
            await loadBoxModels(async (path) => {
                if (path.endsWith("CustomJson")) throw new Error("offline");
                return Response.json({ unexpected: true });
            }),
        ).toEqual({ boxModelImages: [], error: true });
    });
});
