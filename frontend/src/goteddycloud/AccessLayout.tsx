// SPDX-License-Identifier: GPL-2.0-only
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { SettingsSubNav } from "../components/settings/SettingsSubNav";
import { ToniesSubNav } from "../components/tonies/ToniesSubNav";
import BreadcrumbWrapper, {
    StyledContent,
    StyledLayout,
    StyledSider,
} from "../components/common/StyledComponents";
export function AccessLayout({
    title,
    section,
    children,
}: {
    title: string;
    section: "settings" | "tonies";
    children: ReactNode;
}) {
    return (
        <>
            <StyledSider>
                {section === "settings" ? <SettingsSubNav /> : <ToniesSubNav />}
            </StyledSider>
            <StyledLayout>
                <BreadcrumbWrapper items={[{ title: <Link to="/">TeddyCloud</Link> }, { title }]} />
                <StyledContent>
                    <h1>{title}</h1>
                    {children}
                </StyledContent>
            </StyledLayout>
        </>
    );
}
