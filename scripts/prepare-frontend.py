#!/usr/bin/env python3
"""Apply the owned Go UI overlay to an isolated copy of the pinned frontend."""
from pathlib import Path
import shutil
import sys

root = Path(__file__).resolve().parent.parent
source = Path(sys.argv[1]).resolve()
stage = root / '.cache/goteddycloud-web'
marker = stage / '.goteddycloud-overlay'
if stage.exists():
    if not marker.is_file():
        raise SystemExit('Refusing to replace an unmarked frontend stage')
    shutil.rmtree(stage)
shutil.copytree(source, stage, ignore=shutil.ignore_patterns('.git', 'node_modules', 'dist'))
marker.write_text('Generated from the pinned upstream frontend; edit frontend/ instead.\n')
shutil.copytree(root / 'frontend/src/goteddycloud', stage / 'src/goteddycloud')

def edit(name, before, after):
    path = stage / 'src' / name
    text = path.read_text()
    if text.count(before) != 1:
        raise SystemExit(f'Frontend integration anchor changed in {name}: {before!r}')
    path.write_text(text.replace(before, after, 1))

edit('App.tsx', 'import "./styles/matrix/matrix.css";', '''import { UsersAreasPage } from "./goteddycloud/UsersAreasPage";
import { SharedCatalogsPage } from "./goteddycloud/SharedCatalogsPage";
import "./styles/matrix/matrix.css";''')
edit('App.tsx', 'const { loading, error, statusAvailable, needsLogin, refresh } = useAuth();', 'const { loading, error, statusAvailable, needsLogin, refresh, enabled, role } = useAuth();')
edit('App.tsx', '<Route path="/settings" element={<SettingsPage />} />', '<Route path="/settings" element={enabled && role !== "admin" ? <Navigate to="/settings/users" replace /> : <SettingsPage />} />')
edit('App.tsx', '<Route path="/settings/webauth" element={<WebAuthPage />} />', '''<Route path="/settings/webauth" element={<Navigate to="/settings/users" replace />} />
                                <Route path="/settings/users" element={<UsersAreasPage />} />
                                <Route path="/tonies/shared" element={<SharedCatalogsPage />} />''')
edit('components/common/header/StyledHeader.tsx', 'import logoImg', 'import { AreaSelector } from "../../../goteddycloud/AreaSelector";\nimport logoImg')
edit('components/common/header/StyledHeader.tsx', '<StyledRightPart>','<StyledRightPart>\n                {!isMobile && <AreaSelector />}')
edit('components/common/header/StyledHeader.tsx', 'calc(100vw - 510px)', 'calc(100vw - 710px)')
edit('components/common/header/StyledHeader.tsx', '                        <StyledMenu', '                        {isMobile && <AreaSelector />}\n                        <StyledMenu')
edit('components/common/header/StyledHeader.tsx', 'const { enabled: authEnabled, logout } = useAuth();', 'const { loggedIn, logout } = useAuth();')
edit('components/common/header/StyledHeader.tsx', '{authEnabled ? (', '{loggedIn ? (')
edit('utils/auth/webAuthApi.ts', 'export type AuthStatus = {', 'export type AuthStatus = {\n    role?: "admin" | "editor" | "reader";')
edit('utils/auth/webAuthApi.ts', '        userCount: Number(data.userCount || 0),', '        userCount: Number(data.userCount || 0),\n        ...(data.role ? { role: data.role } : {}),')

for name, path, label in [('settings/SettingsSubNav.tsx', '/settings/users', 'users'), ('tonies/ToniesSubNav.tsx', '/tonies/shared', 'shares')]:
    name = 'components/' + name
    edit(name, 'import React,', 'import { useGoText } from "../../goteddycloud/text";\nimport { useAuth } from "../../provider/AuthProvider";\nimport React,')
    edit(name, '    const { t', '    const text = useGoText();\n    const auth = useAuth();\n    const { t')
    edit(name, '    const subnav: MenuProps["items"] = [', f'''    const subnav: MenuProps["items"] = [
        {{ key: "{label}", label: <Link to="{path}" onClick={{() => {{ setNavOpen(false); setSubNavOpen(false); }}}}>{{text("{label}")}}</Link>, title: text("{label}") }},''')
    if label == 'users':
        edit(name, '            items={subnav}', '            items={!auth.enabled || auth.role === "admin" ? subnav : subnav.filter(item => item?.key === "users")}')
        # Replace the old login-settings submenu with the native users entry above.
        file = stage / 'src' / name
        data = file.read_text()
        start = data.index('                {\n                    key: "webauth",')
        end = data.index('                },', start) + len('                },\n')
        file.write_text(data[:start] + data[end:])
# Keep the UI clock on the management listener; /v1/time belongs to devices.
edit('api/apis/BoxineApi.ts', 'path: `/v1/time`,', 'path: `/api/time`,')

# A missing cloud identity cannot recover during a burst of ten retries. Keep
# the cloud status probe separate from the working local management clock.
edit('components/common/header/ServerStatus.tsx',
     'const delay = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));\n', '')
edit('components/common/header/ServerStatus.tsx', '''    const fetchBoxineStatusWithRetries = useCallback(async () => {
        const maxRetries = 10;

        for (let attempt = 0; attempt < maxRetries; attempt++) {
            try {
                const timeRequest = (await boxineForcedApi.reverseV1TimeGet()) as string;
                if (timeRequest.length === 10) {
                    setBoxineStatus(true);
                    return;
                }
            } catch {
                setBoxineStatus(false);
                if (attempt < maxRetries - 1) {
                    await delay(500);
                }
            }
        }
    }, []);''', '''    const fetchBoxineStatus = useCallback(async () => {
        try {
            const timeRequest = (await boxineForcedApi.reverseV1TimeGet()) as string;
            setBoxineStatus(timeRequest.length === 10);
        } catch {
            setBoxineStatus(false);
        }
    }, []);''')
edit('components/common/header/ServerStatus.tsx',
     'await fetchBoxineStatusWithRetries();', 'await fetchBoxineStatus();')
edit('components/common/header/ServerStatus.tsx',
     '[fetchBoxineEnabledStatus, fetchTeddyStatus, fetchBoxineStatusWithRetries]',
     '[fetchBoxineEnabledStatus, fetchTeddyStatus, fetchBoxineStatus]')

edit('provider/TeddyCloudProvider.tsx', 'from "../hooks/useBoxModels";', 'from "../goteddycloud/useBoxModels";')
edit('provider/TeddyCloudProvider.tsx', '    boxModelImagesLoading: boolean;', '    boxModelImagesLoading: boolean;\n    boxModelImagesError: boolean;')
edit('provider/TeddyCloudProvider.tsx', '    boxModelImagesLoading: false,', '    boxModelImagesLoading: false,\n    boxModelImagesError: false,')
edit('provider/TeddyCloudProvider.tsx', 'const { boxModelImages, loading: boxModelImagesLoading }', 'const { boxModelImages, loading: boxModelImagesLoading, error: boxModelImagesError }')
# Both the memoized value and its dependency list need the error state.
provider = stage / 'src/provider/TeddyCloudProvider.tsx'
data = provider.read_text()
assert data.count('            boxModelImagesLoading,') == 2
provider.write_text(data.replace('            boxModelImagesLoading,', '            boxModelImagesLoading,\n            boxModelImagesError,'))
edit('components/tonieboxes/tonieboxeslist/TonieboxesList.tsx',
     'addNotification, boxModelImages, boxModelImagesLoading',
     'addNotification, boxModelImages, boxModelImagesLoading, boxModelImagesError')
edit('components/tonieboxes/tonieboxeslist/TonieboxesList.tsx',
     '!boxModelImagesLoading && boxModelImages.length === 0',
     '!boxModelImagesLoading && boxModelImagesError')
edit('components/tonieboxes/tonieboxeslist/TonieboxesList.tsx',
     '[boxModelImagesLoading, boxModelImages.length, addNotification, t]',
     '[boxModelImagesLoading, boxModelImagesError, addNotification, t]')
# External help links are ordinary links; cross-origin probes cannot establish
# their availability and generate CORS errors on otherwise working pages.
for name in [
    'components/tonieboxes/boxsetup/boxsetupoverview/hooks/useReachableNewbieGuideUrls.ts',
    'components/tonieboxes/boxsetup/esp32/flashing/hooks/useReachableBackupUrls.ts',
]:
    edit(name, 'return useUrlsReachable(urls);', 'return urls;')
    file = stage / 'src' / name
    file.write_text(file.read_text().replace('useUrlsReachable, UrlToTest', 'UrlToTest').replace('UrlToTest, useUrlsReachable', 'UrlToTest'))

print(stage)
