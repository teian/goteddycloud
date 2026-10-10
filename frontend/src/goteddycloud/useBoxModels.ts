// SPDX-License-Identifier: GPL-2.0-only
import { useEffect, useState } from "react";
import { TeddyCloudApi } from "../api";
import { defaultAPIConfig } from "../config/defaultApiConfig";
import type { TonieboxImage } from "../types/tonieboxTypes";

const api = new TeddyCloudApi(defaultAPIConfig());
type Models = { boxModelImages: TonieboxImage[]; error: boolean };

// Empty catalogs are valid. Preserve successful sources when another source fails.
export async function loadBoxModels(
    fetchCatalog: (path: string) => Promise<Response>,
): Promise<Models> {
    const results = await Promise.allSettled(
        ["/api/tonieboxesJson", "/api/tonieboxesCustomJson"].map(async (path) => {
            const response = await fetchCatalog(path);
            if (!response.ok) throw new Error("Model catalog unavailable");
            const rows: unknown = await response.json();
            if (
                !Array.isArray(rows) ||
                rows.some(
                    (row) =>
                        !row ||
                        typeof row.id !== "string" ||
                        typeof row.name !== "string" ||
                        typeof row.img_src !== "string",
                )
            )
                throw new Error("Invalid model catalog");
            return rows as TonieboxImage[];
        }),
    );
    return {
        boxModelImages: results.flatMap((result) =>
            result.status === "fulfilled" ? result.value : [],
        ),
        error: results.some((result) => result.status === "rejected"),
    };
}

export function useBoxModelImages() {
    const [state, setState] = useState<Models & { loading: boolean }>({
        boxModelImages: [],
        error: false,
        loading: true,
    });
    useEffect(() => {
        const controller = new AbortController();
        void loadBoxModels((path) =>
            api.apiGetTeddyCloudApiRaw(path, undefined, {
                signal: controller.signal,
            }),
        ).then((result) => {
            if (!controller.signal.aborted) setState({ ...result, loading: false });
        });
        return () => controller.abort();
    }, []);
    return state;
}
