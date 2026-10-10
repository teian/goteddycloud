// SPDX-License-Identifier: GPL-2.0-only
import { useEffect, useState } from "react";
import { Alert, Select } from "antd";
import { useAuth } from "../provider/AuthProvider";
import { accessApi, Profile, switchArea } from "./api";
import { useGoText } from "./text";
export function AreaSelector() {
    const auth = useAuth(),
        text = useGoText();
    const [revision, setRevision] = useState(0);
    useEffect(() => {
        const update = () => setRevision((value) => value + 1);
        window.addEventListener("goteddycloud-access-changed", update);
        return () => window.removeEventListener("goteddycloud-access-changed", update);
    }, []);
    const [profile, setProfile] = useState<Profile>();
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    useEffect(() => {
        setProfile(undefined);
        if (!auth.loggedIn) return;
        const controller = new AbortController();
        accessApi<Profile>("profile", undefined, controller.signal)
            .then((value) => {
                if (!controller.signal.aborted) setProfile(value);
            })
            .catch((error) => {
                if (!controller.signal.aborted) setError(String(error));
            });
        return () => controller.abort();
    }, [auth.username, auth.loggedIn, auth.role, revision]);
    if (!profile) return null;
    return (
        <span style={{ marginInline: 8, maxWidth: "100%" }}>
            <Select
                aria-label={text("area")}
                size="small"
                style={{ width: 170, maxWidth: "100%" }}
                value={
                    profile.selectedArea || (profile.role === "admin" ? "" : profile.defaultArea)
                }
                loading={busy}
                disabled={busy}
                options={[
                    ...(profile.role === "admin" ? [{ label: text("global"), value: "" }] : []),
                    ...profile.areas.map((area) => ({ label: area.name, value: area.id })),
                ]}
                onChange={async (area) => {
                    setBusy(true);
                    setError("");
                    try {
                        await switchArea(area);
                    } catch (error) {
                        setError(String(error));
                        setBusy(false);
                    }
                }}
            />
            {error && <Alert type="error" title={error} closable onClose={() => setError("")} />}
        </span>
    );
}
