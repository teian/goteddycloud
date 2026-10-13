// SPDX-License-Identifier: GPL-2.0-only
// @vitest-environment jsdom
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ServerStatus } from "../components/common/header/ServerStatus";
import { resetBasePathCache } from "../utils/basePath";

const { setAvailable } = vi.hoisted(() => {
    // Generated API clients capture the page prefix when the module loads.
    document.head.innerHTML = '<base href="https://example.test/web/">';
    return { setAvailable: vi.fn() };
});
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("../provider/TeddyCloudProvider", () => ({
    useTeddyCloud: () => ({ fetchCloudStatus: false, setToniesCloudAvailable: setAvailable }),
}));
vi.mock("antd", () => {
    const Wrapper = ({ children }: { children: React.ReactNode }) => <span>{children}</span>;
    return { Space: Wrapper, Tag: Wrapper, Tooltip: Wrapper, theme: { useToken: () => ({ token: {} }) } };
});
vi.mock("@ant-design/icons", () => ({
    CheckCircleOutlined: () => null,
    CloseCircleOutlined: () => null,
    LockOutlined: () => null,
}));
vi.mock("../components/common/StyledComponents", () => {
    const Wrapper = ({ children }: { children: React.ReactNode }) => <span>{children}</span>;
    return { HiddenDesktop: Wrapper, HiddenMobile: Wrapper };
});

describe("header cloud availability", () => {
    let container: HTMLDivElement;
    let root: ReturnType<typeof createRoot>;
    beforeEach(() => {
        vi.clearAllMocks();
        sessionStorage.clear();
        document.head.innerHTML = '<base href="https://example.test/web/">';
        resetBasePathCache();
        Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
        container = document.createElement("div");
        document.body.appendChild(container);
        root = createRoot(container);
    });
    afterEach(async () => {
        await act(async () => root.unmount());
        container.remove();
        vi.restoreAllMocks();
    });
    for (const status of [
        { enabled: false, available: false },
        { enabled: true, available: false },
        { enabled: true, available: true },
    ]) {
        it(`reads status once without reverse-proxy requests: ${JSON.stringify(status)}`, async () => {
            const paths: string[] = [];
            vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
                const path = new URL(String(url), "https://example.test").pathname;
                paths.push(path);
                if (path === "/api/time") return new Response("1800000000");
                if (path === "/api/cloud/status") return Response.json(status);
                throw new Error(`Unexpected request: ${path}`);
            });
            await act(async () => root.render(<ServerStatus />));
            expect(paths.sort()).toEqual(["/api/cloud/status", "/api/time"]);
            expect(setAvailable).toHaveBeenLastCalledWith(status.available);
        });
    }
});
