# Compatibility inventory

Reference: `8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1` ([official source](https://github.com/toniebox-reverse-engineering/teddycloud/tree/8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1)).

Generated with `python3 scripts/reference-inventory.py`. Raw C defaults retain compile-time expressions; these are not evaluated values. Statuses merge the scoped evidence in `docs/compatibility-evidence.json`; implemented does not mean complete route parity. Remaining checks are retained in JSON. `docs/reference-inventory.json` contains complete arguments and source hints.

## Submodules

| Path | Pinned commit |
| --- | --- |
| `cJSON` | `c859b25da02955fef659d658b8f324b5cde87be3` |
| `cyclone/common` | `7b402407038962c14ca65a124cea3bde72292170` |
| `cyclone/cyclone_crypto` | `8da34eef143813357bb066e60a252e99674aba43` |
| `cyclone/cyclone_ssl` | `a74d676237aa58806ffc2f69cdabe2255bccfe5f` |
| `cyclone/cyclone_tcp` | `06e2cf6fc1271a8c9854ea507ab4f280feec3cce` |
| `emsdk` | `d223ae73c6998296e3ab27cf81dc2c2c9fd383de` |
| `ogg` | `be05b13e98b048f0b5a0f5fa8ce514d56db5f822` |
| `opus` | `ddbe48383984d56acd9e1ab6a090c54ca6b735a6` |
| `teddycloud_web` | `636a8d65043f6c6e322f76bcb11e958725159e96` |

## Dispatch contracts

Routes use ordered **prefix** matching, case-insensitive GET/POST method comparison, and listener masks. ANY includes the binary transport dispatch. WEB means web HTTP/HTTPS; BOTH also includes box HTTPS. `src/server.c:328–635` performs dispatch, optional box access enforcement, error mapping and static fallback. Handler source links below define payload/response and mutation details; no route parity is established by this inventory alone.

| Method | Prefix | Listener | Handler source | Milestone / acceptance | Status |
| --- | --- | --- | --- | --- | --- |
| ANY | `*binary` | BOTH | `handleRtnl` (src/handler_rtnl.c:23) | M2: response/files/listener | verified |
| ANY | `/reverseGeneric` | WEB | `handleReverseGeneric` (src/handler_reverse.c:115) | M4: response/files/listener | implemented |
| ANY | `/reverse` | WEB | `handleReverseCloudGet` (src/handler_reverse.c:21) | M4: response/files/listener | implemented |
| GET | `/cache/` | WEB | `handleCacheDownload` (src/server.c:193) | M4: response/files/listener | verified |
| GET | `/content/download/` | WEB | `handleApiContentDownload` (src/handler_api.c:2380) | M4: response/files/listener | verified |
| GET | `/content/json/get/` | WEB | `handleApiContentJsonGet` (src/handler_api.c:4767) | M4: response/files/listener | verified |
| POST | `/content/json/set/` | WEB | `handleApiContentJsonSet` (src/handler_api.c:4784) | M4: response/files/listener | verified |
| GET | `/content/json/` | WEB | `handleApiContentJson` (src/handler_api.c:4734) | M4: response/files/listener | verified |
| GET | `/content/` | WEB | `handleApiContent` (src/handler_api.c:2213) | M4: response/files/listener | verified |
| POST | `/api/auth/login` | WEB | `handleApiAuthLogin` (src/handler_web_auth.c:750) | M4: response/files/listener | implemented |
| GET | `/api/auth/logout` | WEB | `handleApiAuthLogout` (src/handler_web_auth.c:815) | M4: response/files/listener | implemented |
| POST | `/api/auth/refresh-token` | WEB | `handleApiAuthRefreshToken` (src/handler_web_auth.c:847) | M4: response/files/listener | implemented |
| GET | `/api/auth/status` | WEB | `handleApiAuthStatus` (src/handler_web_auth.c:727) | M4: response/files/listener | implemented |
| GET | `/api/auth/users/get` | WEB | `handleApiAuthUsersGet` (src/handler_web_auth.c:863) | M4: response/files/listener | implemented |
| POST | `/api/auth/users/create` | WEB | `handleApiAuthUsersCreate` (src/handler_web_auth.c:883) | M4: response/files/listener | implemented |
| POST | `/api/auth/users/delete` | WEB | `handleApiAuthUsersDelete` (src/handler_web_auth.c:928) | M4: response/files/listener | implemented |
| POST | `/api/auth/users/updatePassword` | WEB | `handleApiAuthUsersPassword` (src/handler_web_auth.c:982) | M4: response/files/listener | implemented |
| POST | `/api/auth/enabled` | WEB | `handleApiAuthEnabled` (src/handler_web_auth.c:1030) | M4: response/files/listener | implemented |
| GET | `/api/plugins/get` | WEB | `handleApiPluginsGet` (src/handler_api.c:5275) | M4: response/files/listener | verified |
| POST | `/api/fileDelete` | WEB | `handleApiFileDelete` (src/handler_api.c:2953) | M4: response/files/listener | implemented |
| POST | `/api/fileMove` | WEB | `handleApiFileMove` (src/handler_api.c:3001) | M4: response/files/listener | implemented |
| POST | `/api/dirDelete` | WEB | `handleApiDirectoryDelete` (src/handler_api.c:2904) | M4: response/files/listener | implemented |
| POST | `/api/dirCreate` | WEB | `handleApiDirectoryCreate` (src/handler_api.c:2855) | M4: response/files/listener | implemented |
| POST | `/api/uploadCert` | WEB | `handleApiUploadCert` (src/handler_api.c:1233) | M4: response/files/listener | verified |
| GET | `/api/getFile/ca.der` | WEB | `handleApiGetCaDer` (src/handler_api.c:1359) | M4: response/files/listener | verified |
| GET | `/api/getFile/c2.der` | WEB | `handleApiGetCaDer` (src/handler_api.c:1359) | M4: response/files/listener | verified |
| POST | `/api/esp32/uploadFirmware` | WEB | `handleApiESP32UploadFirmware` (src/handler_api.c:1369) | M4: response/files/listener | verified |
| POST | `/api/esp32/extractCerts` | WEB | `handleApiESP32ExtractCerts` (src/handler_api.c:1457) | M4: response/files/listener | verified |
| GET | `/api/esp32/patchFirmware` | WEB | `handleApiESP32PatchFirmware` (src/handler_api.c:1602) | M4: response/files/listener | verified |
| POST | `/api/fileUpload` | WEB | `handleApiFileUpload` (src/handler_api.c:1821) | M4: response/files/listener | verified |
| POST | `/api/fileEncode` | WEB | `handleApiEncodeFile` (src/handler_api.c:2534) | M4: response/files/listener | implemented |
| POST | `/api/pcmUpload` | WEB | `handleApiPcmUpload` (src/handler_api.c:2639) | M4: response/files/listener | implemented |
| POST | `/api/tafUpload` | WEB | `handleApiTafUpload` (src/handler_api.c:2752) | M4: response/files/listener | implemented |
| GET | `/api/fileIndexV2` | WEB | `handleApiFileIndexV2` (src/handler_api.c:646) | M4: response/files/listener | verified |
| POST | `/api/fileSetListened` | WEB | `handleApiFileSetListened` (src/handler_api.c:850) | M4: response/files/listener | implemented |
| GET | `/api/fileIndex` | WEB | `handleApiFileIndex` (src/handler_api.c:909) | M4: response/files/listener | verified |
| GET | `/api/stats` | WEB | `handleApiStats` (src/handler_api.c:1075) | M4: response/files/listener | verified |
| GET | `/api/toniesJsonSearch` | WEB | `handleApiToniesJsonSearch` (src/handler_api.c:4703) | M4: response/files/listener | implemented |
| GET | `/api/toniesJsonUpdate` | WEB | `handleApiToniesJsonUpdate` (src/handler_api.c:3081) | M4: response/files/listener | implemented |
| GET | `/api/toniesJsonReload` | WEB | `handleApiToniesJsonReload` (src/handler_api.c:3066) | M4: response/files/listener | implemented |
| GET | `/api/toniesJson` | WEB | `handleApiToniesJson` (src/handler_api.c:3073) | M4: response/files/listener | implemented |
| GET | `/api/toniesCustomJson` | WEB | `handleApiToniesCustomJson` (src/handler_api.c:3091) | M4: response/files/listener | verified |
| POST | `/api/toniesCustomJsonUpsert` | WEB | `handleApiToniesCustomJsonUpsert` (src/handler_api.c:4105) | M4: response/files/listener | verified |
| POST | `/api/toniesCustomJsonDelete` | WEB | `handleApiToniesCustomJsonDelete` (src/handler_api.c:4215) | M4: response/files/listener | verified |
| POST | `/api/toniesCustomJsonRename` | WEB | `handleApiToniesCustomJsonRename` (src/handler_api.c:4412) | M4: response/files/listener | verified |
| GET | `/api/tonieboxesJson` | WEB | `handleApiTonieboxJson` (src/handler_api.c:4549) | M4: response/files/listener | verified |
| GET | `/api/tonieboxesCustomJson` | WEB | `handleApiTonieboxCustomJson` (src/handler_api.c:4687) | M4: response/files/listener | verified |
| GET | `/api/trigger` | WEB | `handleApiTrigger` (src/handler_api.c:450) | M4: response/files/listener | verified |
| GET | `/api/getTagIndex` | WEB | `handleApiTagIndex` (src/handler_api.c:5065) | M4: response/files/listener | verified |
| GET | `/api/getTagInfo` | WEB | `handleApiTagInfo` (src/handler_api.c:5032) | M4: response/files/listener | verified |
| GET | `/api/getBoxes` | WEB | `handleApiGetBoxes` (src/handler_api.c:418) | M4: response/files/listener | verified |
| POST | `/api/assignUnknown` | WEB | `handleApiAssignUnknown` (src/handler_api.c:255) | M4: response/files/listener | verified |
| GET | `/api/settings/getIndex` | WEB | `handleApiGetIndex` (src/handler_api.c:289) | M4: response/files/listener | implemented |
| GET | `/api/settings/get/` | WEB | `handleApiSettingsGet` (src/handler_api.c:491) | M4: response/files/listener | implemented |
| POST | `/api/settings/set/` | WEB | `handleApiSettingsSet` (src/handler_api.c:544) | M4: response/files/listener | implemented |
| POST | `/api/settings/reset/` | WEB | `handleApiSettingsReset` (src/handler_api.c:597) | M4: response/files/listener | implemented |
| POST | `/api/settings/removeOverlay` | WEB | `handleDeleteOverlay` (src/handler_api.c:5218) | M4: response/files/listener | implemented |
| POST | `/api/migrateContent2Lib` | WEB | `handleApiMigrateContent2Lib` (src/handler_api.c:5160) | M4: response/files/listener | implemented |
| POST | `/api/cacheFlush` | WEB | `handleApiCacheFlush` (src/handler_api.c:5239) | M4: response/files/listener | verified |
| GET | `/api/cacheStats` | WEB | `handleApiCacheStats` (src/handler_api.c:5251) | M4: response/files/listener | verified |
| GET | `/api/sse` | WEB | `handleApiSse` (src/handler_sse.c:9) | M4: response/files/listener | implemented |
| GET | `/robots.txt` | WEB | `handleSecMitRobotsTxt` (src/handler_security_mit.c:167) | M4: response/files/listener | implemented |
| GET | `/v1/time` | BOTH | `handleCloudTime` (src/handler_cloud.c:42) | M2: response/files/listener | implemented |
| GET | `/v1/ota` | BOTH | `handleCloudOTA` (src/handler_cloud.c:74) | M3: response/files/listener | implemented |
| GET | `/v1/claim` | BOTH | `handleCloudClaim` (src/handler_cloud.c:361) | M2: response/files/listener | implemented |
| GET | `/v1/content` | BOTH | `handleCloudContentV1` (src/handler_cloud.c:897) | M2: response/files/listener | implemented |
| GET | `/v2/content` | BOTH | `handleCloudContentV2` (src/handler_cloud.c:902) | M2: response/files/listener | implemented |
| POST | `/v1/freshness-check` | BOTH | `handleCloudFreshnessCheck` (src/handler_cloud.c:1064) | M2: response/files/listener | implemented |
| POST | `/v1/log` | BOTH | `handleCloudLog` (src/handler_cloud.c:331) | M2: response/files/listener | implemented |
| POST | `/v1/cloud-reset` | BOTH | `handleCloudReset` (src/handler_cloud.c:1724) | M2: response/files/listener | implemented |
| POST | `/v3/freshness-check` | BOTH | `handleCloudFreshnessCheckV3` (src/handler_cloud.c:1142) | M3: response/files/listener | implemented |
| POST | `/v3/check-ota` | BOTH | `handleCloudCheckOtaV3` (src/handler_cloud.c:1319) | M3: response/files/listener | implemented |
| GET | `/v3/ota` | BOTH | `handleCloudOtaV3` (src/handler_cloud.c:1598) | M3: response/files/listener | implemented |
| POST | `/v3/setup-status` | BOTH | `handleCloudSetupStatusV3` (src/handler_cloud.c:1368) | M3: response/files/listener | implemented |
| GET | `/v3/chapter` | BOTH | `handleCloudChapterV3` (src/handler_cloud.c:1554) | M3: response/files/listener | implemented |
| GET | `/v3/content-meta` | BOTH | `handleCloudContentMetaV3` (src/handler_cloud.c:1415) | M3: response/files/listener | implemented |

## Non-table routing

| Contract | Source | Milestone / acceptance | Status |
| --- | --- | --- | --- |
| Root/index.shtm always redirects to /web | src/server.c:607 | M4 | implemented |
| Web SPA fallback, assets, MIME and errors | src/server.c:617 | M4 | implemented |
| Box listener never serves web static files | src/server.c:578 | M2 | implemented |
| 00 00 + big endian nonzero length <=65535 across reads | src/cyclone/cyclone_tcp/http/http_server.c:475 | M2 | implemented |
| No SNI TB1, any SNI TB2, optional certificates, separate policies | src/server.c:767-875 | M2 | implemented |

## Settings

All OPTION entries, including internal state and UI tree nodes, are retained. Complete macro arguments, behavior classifications, evidence and outstanding acceptance are in JSON and [settings audit](settings-audit.md). Implemented means configuration support and the stated consumer; it does not establish hardware or frontend acceptance.

| Key | Type | Default C expression | Source line | Milestone | Status |
| --- | --- | --- | --- | --- | --- |
| `configVersion` | INTERNAL_UNSIGNED | `0` | src/settings.c:135 | M2 | documented deviation |
| `commonName` | INTERNAL_STRING | `"default"` | src/settings.c:137 | M2 | implemented |
| `boxName` | INTERNAL_STRING | `"Toniebox"` | src/settings.c:138 | M2 | implemented |
| `boxModel` | INTERNAL_STRING | `""` | src/settings.c:139 | M2 | implemented |
| `log` | TREE_DESC | `—` | src/settings.c:140 | M2 | implemented |
| `log.level` | UNSIGNED | `4` | src/settings.c:142 | M2 | implemented |
| `log.color` | BOOL | `TRUE` | src/settings.c:143 | M2 | documented deviation |
| `log.logFullAuth` | BOOL | `FALSE` | src/settings.c:144 | M2 | documented deviation |
| `core.server` | TREE_DESC | `—` | src/settings.c:147 | M2 | implemented |
| `core.server.http_port` | UNSIGNED | `80` | src/settings.c:148 | M2 | implemented |
| `core.server.https_web_port` | UNSIGNED | `8443` | src/settings.c:149 | M2 | implemented |
| `core.server.https_api_port` | UNSIGNED | `443` | src/settings.c:150 | M2 | implemented |
| `core.server.bind_ip` | STRING | `""` | src/settings.c:151 | M2 | implemented |
| `core` | TREE_DESC | `—` | src/settings.c:152 | M2 | implemented |
| `core.host_url` | STRING | `"http://localhost"` | src/settings.c:154 | M2 | implemented |
| `core.certdir` | STRING | `"certs/client"` | src/settings.c:155 | M2 | implemented |
| `core.configdir` | INTERNAL_STRING | `CONFIG_BASE_PATH` | src/settings.c:156 | M2 | implemented |
| `core.contentdir` | STRING | `"default"` | src/settings.c:157 | M2 | implemented |
| `core.librarydir` | STRING | `"library"` | src/settings.c:158 | M2 | implemented |
| `core.datadir` | STRING | `"data"` | src/settings.c:159 | M2 | implemented |
| `core.wwwdir` | STRING | `"www"` | src/settings.c:160 | M2 | implemented |
| `core.pluginsdir` | STRING | `"plugins"` | src/settings.c:161 | M2 | implemented |
| `core.firmwaredir` | STRING | `"firmware"` | src/settings.c:162 | M2 | implemented |
| `core.cachedir` | STRING | `"cache"` | src/settings.c:163 | M2 | implemented |
| `core.sslkeylogfile` | STRING | `""` | src/settings.c:164 | M2 | implemented |
| `core.server.http_client_timeout` | UNSIGNED | `2000` | src/settings.c:165 | M2 | implemented |
| `core.file_upload_timeout_ms` | UNSIGNED | `120000` | src/settings.c:166 | M2 | implemented |
| `core.server_cert` | TREE_DESC | `—` | src/settings.c:167 | M2 | implemented |
| `core.client_cert.file` | TREE_DESC | `—` | src/settings.c:168 | M2 | implemented |
| `core.server_cert.file.ca` | STRING | `"certs/server/ca-root.pem"` | src/settings.c:169 | M2 | implemented |
| `core.server_cert.file.ca_der` | STRING | `"certs/server/ca.der"` | src/settings.c:170 | M2 | implemented |
| `core.server_cert.file.ca_key` | STRING | `"certs/server/ca-key.pem"` | src/settings.c:171 | M2 | implemented |
| `core.server_cert.file.crt` | STRING | `"certs/server/teddy-cert.pem"` | src/settings.c:172 | M2 | implemented |
| `core.server_cert.file.key` | STRING | `"certs/server/teddy-key.pem"` | src/settings.c:173 | M2 | implemented |
| `core.server_cert.data` | TREE_DESC | `—` | src/settings.c:174 | M2 | implemented |
| `core.server_cert.data.ca` | STRING | `""` | src/settings.c:175 | M2 | implemented |
| `core.server_cert.data.ca_key` | INTERNAL_STRING | `""` | src/settings.c:176 | M2 | implemented |
| `core.server_cert.data.crt` | INTERNAL_STRING | `""` | src/settings.c:177 | M2 | implemented |
| `core.server_cert.data.key` | INTERNAL_STRING | `""` | src/settings.c:178 | M2 | implemented |
| `core.server_cert_tb2` | TREE_DESC | `—` | src/settings.c:179 | M2 | implemented |
| `core.server_cert_tb2.file` | TREE_DESC | `—` | src/settings.c:181 | M2 | implemented |
| `core.server_cert_tb2.file.ca` | STRING | `"certs/server_tb2/ca-root.pem"` | src/settings.c:182 | M2 | implemented |
| `core.server_cert_tb2.file.ca_der` | STRING | `"certs/server_tb2/ca.der"` | src/settings.c:183 | M2 | implemented |
| `core.server_cert_tb2.file.ca_key` | STRING | `"certs/server_tb2/ca-key.pem"` | src/settings.c:184 | M2 | implemented |
| `core.server_cert_tb2.file.crt` | STRING | `"certs/server_tb2/teddy-cert.pem"` | src/settings.c:185 | M2 | implemented |
| `core.server_cert_tb2.file.key` | STRING | `"certs/server_tb2/teddy-key.pem"` | src/settings.c:186 | M2 | implemented |
| `core.server_cert_tb2.data` | TREE_DESC | `—` | src/settings.c:187 | M2 | implemented |
| `core.server_cert_tb2.data.ca` | STRING | `""` | src/settings.c:188 | M2 | implemented |
| `core.server_cert_tb2.data.ca_key` | INTERNAL_STRING | `""` | src/settings.c:189 | M2 | implemented |
| `core.server_cert_tb2.data.crt` | INTERNAL_STRING | `""` | src/settings.c:190 | M2 | implemented |
| `core.server_cert_tb2.data.key` | INTERNAL_STRING | `""` | src/settings.c:191 | M2 | implemented |
| `core.client_cert` | TREE_DESC | `—` | src/settings.c:194 | M2 | implemented |
| `core.client_cert.file` | TREE_DESC | `—` | src/settings.c:195 | M2 | implemented |
| `core.client_cert.file.ca` | STRING | `"certs/client/ca.der"` | src/settings.c:196 | M2 | implemented |
| `core.client_cert.file.crt` | STRING | `"certs/client/client.der"` | src/settings.c:197 | M2 | implemented |
| `core.client_cert.file.key` | STRING | `"certs/client/private.der"` | src/settings.c:198 | M2 | implemented |
| `core.client_cert.data` | TREE_DESC | `—` | src/settings.c:199 | M2 | implemented |
| `core.client_cert.data.ca` | INTERNAL_STRING | `""` | src/settings.c:200 | M2 | implemented |
| `core.client_cert.data.crt` | INTERNAL_STRING | `""` | src/settings.c:201 | M2 | implemented |
| `core.client_cert.data.key` | INTERNAL_STRING | `""` | src/settings.c:202 | M2 | implemented |
| `core.client_cert_fake` | TREE_DESC | `—` | src/settings.c:205 | M2 | implemented |
| `core.client_cert_fake.file` | TREE_DESC | `—` | src/settings.c:206 | M2 | implemented |
| `core.client_cert_fake.file.ca` | STRING | `"certs/client/ca.fake.der"` | src/settings.c:207 | M2 | implemented |
| `core.client_cert_fake.file.crt` | STRING | `"certs/client/client.fake.der"` | src/settings.c:208 | M2 | implemented |
| `core.client_cert_fake.file.key` | STRING | `"certs/client/private.fake.der"` | src/settings.c:209 | M2 | implemented |
| `core.client_cert_fake.data` | TREE_DESC | `—` | src/settings.c:210 | M2 | implemented |
| `core.client_cert_fake.data.ca` | INTERNAL_STRING | `""` | src/settings.c:211 | M2 | implemented |
| `core.client_cert_fake.data.crt` | INTERNAL_STRING | `""` | src/settings.c:212 | M2 | implemented |
| `core.client_cert_fake.data.key` | INTERNAL_STRING | `""` | src/settings.c:213 | M2 | implemented |
| `core.allowOrigin` | STRING | `""` | src/settings.c:214 | M2 | implemented |
| `core.boxCertAuth` | BOOL | `TRUE` | src/settings.c:216 | M2 | implemented |
| `core.allowPrivateFetch` | BOOL | `FALSE` | src/settings.c:217 | M2 | implemented |
| `core.allowNewBox` | BOOL | `TRUE` | src/settings.c:218 | M2 | implemented |
| `core.flex_enabled` | BOOL | `TRUE` | src/settings.c:219 | M2 | implemented |
| `core.flex_uid` | STRING | `""` | src/settings.c:221 | M2 | implemented |
| `core.settings_level` | UNSIGNED | `1` | src/settings.c:222 | M2 | implemented |
| `core.tonies_json_auto_update` | BOOL | `TRUE` | src/settings.c:223 | M2 | implemented |
| `core.full_taf_validation` | BOOL | `FALSE` | src/settings.c:224 | M2 | implemented |
| `core.tap_taf_validation` | BOOL | `FALSE` | src/settings.c:225 | M2 | documented deviation |
| `core.track_pos_taf_validation` | BOOL | `TRUE` | src/settings.c:226 | M2 | documented deviation |
| `security_mit` | TREE_DESC | `—` | src/settings.c:227 | M2 | implemented |
| `security_mit.warnAccess` | BOOL | `TRUE` | src/settings.c:229 | M2 | implemented |
| `security_mit.lockAccess` | BOOL | `TRUE` | src/settings.c:230 | M2 | implemented |
| `security_mit.httpsOnly` | BOOL | `FALSE` | src/settings.c:231 | M2 | implemented |
| `security_mit.onBlacklistDomain` | BOOL | `TRUE` | src/settings.c:232 | M2 | implemented |
| `security_mit.onCrawler` | BOOL | `TRUE` | src/settings.c:233 | M2 | implemented |
| `security_mit.onRobotsTxt` | BOOL | `TRUE` | src/settings.c:235 | M2 | implemented |
| `security_mit.hardLock` | BOOL | `FALSE` | src/settings.c:236 | M2 | implemented |
| `internal` | TREE_DESC | `—` | src/settings.c:237 | M2 | implemented |
| `internal.server.ca` | INTERNAL_STRING | `""` | src/settings.c:239 | M2 | documented deviation |
| `internal.server.ca_key` | INTERNAL_STRING | `""` | src/settings.c:240 | M2 | documented deviation |
| `internal.server.crt` | INTERNAL_STRING | `""` | src/settings.c:241 | M2 | documented deviation |
| `internal.server.key` | INTERNAL_STRING | `""` | src/settings.c:242 | M2 | documented deviation |
| `internal.server.cert_chain` | INTERNAL_STRING | `""` | src/settings.c:243 | M2 | documented deviation |
| `internal.server_tb2.ca` | INTERNAL_STRING | `""` | src/settings.c:244 | M2 | documented deviation |
| `internal.server_tb2.ca_key` | INTERNAL_STRING | `""` | src/settings.c:245 | M2 | documented deviation |
| `internal.server_tb2.crt` | INTERNAL_STRING | `""` | src/settings.c:246 | M2 | documented deviation |
| `internal.server_tb2.key` | INTERNAL_STRING | `""` | src/settings.c:247 | M2 | documented deviation |
| `internal.server_tb2.cert_chain` | INTERNAL_STRING | `""` | src/settings.c:248 | M2 | documented deviation |
| `internal.client.ca` | INTERNAL_STRING | `""` | src/settings.c:249 | M2 | documented deviation |
| `internal.client.crt` | INTERNAL_STRING | `""` | src/settings.c:250 | M2 | documented deviation |
| `internal.client.key` | INTERNAL_STRING | `""` | src/settings.c:251 | M2 | documented deviation |
| `internal.autogen_certs` | INTERNAL_BOOL | `TRUE` | src/settings.c:252 | M2 | implemented |
| `internal.exit` | INTERNAL_BOOL | `FALSE` | src/settings.c:253 | M2 | documented deviation |
| `internal.returncode` | INTERNAL_SIGNED | `0` | src/settings.c:255 | M2 | documented deviation |
| `internal.config_init` | INTERNAL_BOOL | `FALSE` | src/settings.c:256 | M2 | documented deviation |
| `internal.config_used` | INTERNAL_BOOL | `FALSE` | src/settings.c:257 | M2 | documented deviation |
| `internal.config_changed` | INTERNAL_BOOL | `FALSE` | src/settings.c:258 | M2 | documented deviation |
| `internal.logColorSupport` | INTERNAL_BOOL | `FALSE` | src/settings.c:259 | M2 | documented deviation |
| `internal.basedir` | INTERNAL_STRING | `BASE_PATH` | src/settings.c:260 | M2 | implemented |
| `internal.basedirfull` | INTERNAL_STRING | `""` | src/settings.c:261 | M2 | implemented |
| `internal.cwd` | INTERNAL_STRING | `""` | src/settings.c:262 | M2 | documented deviation |
| `internal.certdirfull` | INTERNAL_STRING | `""` | src/settings.c:263 | M2 | implemented |
| `internal.configdirfull` | INTERNAL_STRING | `""` | src/settings.c:264 | M2 | implemented |
| `internal.contentdirrel` | INTERNAL_STRING | `""` | src/settings.c:265 | M2 | implemented |
| `internal.contentdirfull` | INTERNAL_STRING | `""` | src/settings.c:266 | M2 | implemented |
| `internal.librarydirfull` | INTERNAL_STRING | `""` | src/settings.c:267 | M2 | implemented |
| `internal.datadirfull` | INTERNAL_STRING | `""` | src/settings.c:268 | M2 | implemented |
| `internal.wwwdirfull` | INTERNAL_STRING | `""` | src/settings.c:269 | M2 | implemented |
| `internal.pluginsdirfull` | INTERNAL_STRING | `""` | src/settings.c:270 | M2 | implemented |
| `internal.firmwaredirfull` | INTERNAL_STRING | `""` | src/settings.c:271 | M2 | implemented |
| `internal.cachedirfull` | INTERNAL_STRING | `""` | src/settings.c:272 | M2 | implemented |
| `internal.overlayUniqueId` | INTERNAL_STRING | `""` | src/settings.c:273 | M2 | documented deviation |
| `internal.overlayNumber` | INTERNAL_UNSIGNED | `0` | src/settings.c:274 | M2 | documented deviation |
| `internal.assign_unknown` | INTERNAL_STRING | `""` | src/settings.c:275 | M2 | implemented |
| `internal.rtnl.lastEarId` | INTERNAL_UNSIGNED | `EAR_NONE` | src/settings.c:276 | M2 | documented deviation |
| `internal.rtnl.lastEarpress` | INTERNAL_UNSIGNED | `0` | src/settings.c:278 | M2 | documented deviation |
| `internal.rtnl.wasDoubleEarpress` | INTERNAL_BOOL | `FALSE` | src/settings.c:279 | M2 | documented deviation |
| `internal.rtnl.multipressTime` | INTERNAL_UNSIGNED | `300` | src/settings.c:280 | M2 | implemented |
| `internal.rtnl.prodDomain` | INTERNAL_STRING | `""` | src/settings.c:281 | M2 | implemented |
| `internal.rtnl.rtnlDomain` | INTERNAL_STRING | `""` | src/settings.c:282 | M2 | implemented |
| `internal.version.id` | INTERNAL_STRING | `""` | src/settings.c:283 | M2 | documented deviation |
| `internal.version.git_sha_short` | INTERNAL_STRING | `""` | src/settings.c:285 | M2 | documented deviation |
| `internal.version.git_sha` | INTERNAL_STRING | `""` | src/settings.c:286 | M2 | documented deviation |
| `internal.version.dirty` | INTERNAL_BOOL | `FALSE` | src/settings.c:287 | M2 | documented deviation |
| `internal.version.datetime` | INTERNAL_STRING | `""` | src/settings.c:288 | M2 | documented deviation |
| `internal.version.platform` | INTERNAL_STRING | `""` | src/settings.c:289 | M2 | documented deviation |
| `internal.version.os` | INTERNAL_STRING | `""` | src/settings.c:290 | M2 | documented deviation |
| `internal.version.architecture` | INTERNAL_STRING | `""` | src/settings.c:291 | M2 | documented deviation |
| `internal.version.v_short` | INTERNAL_STRING | `""` | src/settings.c:292 | M2 | documented deviation |
| `internal.version.v_long` | INTERNAL_STRING | `""` | src/settings.c:293 | M2 | documented deviation |
| `internal.version.v_full` | INTERNAL_STRING | `""` | src/settings.c:294 | M2 | documented deviation |
| `internal.version_web.id` | INTERNAL_STRING | `""` | src/settings.c:295 | M2 | documented deviation |
| `internal.version_web.git_sha_short` | INTERNAL_STRING | `""` | src/settings.c:297 | M2 | documented deviation |
| `internal.version_web.git_sha` | INTERNAL_STRING | `""` | src/settings.c:298 | M2 | documented deviation |
| `internal.version_web.dirty` | INTERNAL_BOOL | `FALSE` | src/settings.c:299 | M2 | documented deviation |
| `internal.version_web.datetime` | INTERNAL_STRING | `""` | src/settings.c:300 | M2 | documented deviation |
| `internal.version_web.v_short` | INTERNAL_STRING | `""` | src/settings.c:301 | M2 | documented deviation |
| `internal.version_web.v_long` | INTERNAL_STRING | `""` | src/settings.c:302 | M2 | documented deviation |
| `internal.version_web.v_full` | INTERNAL_STRING | `""` | src/settings.c:303 | M2 | documented deviation |
| `internal.toniebox_firmware.boxIC` | INTERNAL_UNSIGNED | `0` | src/settings.c:304 | M2 | implemented |
| `internal.toniebox_firmware.uaVersionFirmware` | INTERNAL_UNSIGNED | `0` | src/settings.c:306 | M2 | implemented |
| `internal.toniebox_firmware.uaVersionServicePack` | INTERNAL_UNSIGNED | `0` | src/settings.c:307 | M2 | implemented |
| `internal.toniebox_firmware.uaVersionHardware` | INTERNAL_UNSIGNED | `0` | src/settings.c:308 | M2 | implemented |
| `internal.toniebox_firmware.uaEsp32Firmware` | INTERNAL_STRING | `""` | src/settings.c:309 | M2 | implemented |
| `internal.toniebox_firmware.rtnlVersion` | INTERNAL_STRING | `""` | src/settings.c:310 | M2 | implemented |
| `internal.toniebox_firmware.rtnlFullVersion` | INTERNAL_STRING | `""` | src/settings.c:311 | M2 | implemented |
| `internal.toniebox_firmware.rtnlDetail` | INTERNAL_STRING | `""` | src/settings.c:312 | M2 | implemented |
| `internal.toniebox_firmware.rtnlRegion` | INTERNAL_STRING | `""` | src/settings.c:313 | M2 | implemented |
| `internal.toniebox_firmware.otaVersionSfx` | INTERNAL_UNSIGNED | `0` | src/settings.c:314 | M2 | implemented |
| `internal.toniebox_firmware.otaVersionServicePack` | INTERNAL_UNSIGNED | `0` | src/settings.c:315 | M2 | implemented |
| `internal.toniebox_firmware.otaVersionHtml` | INTERNAL_UNSIGNED | `0` | src/settings.c:316 | M2 | implemented |
| `internal.toniebox_firmware.otaVersionEu` | INTERNAL_UNSIGNED | `0` | src/settings.c:317 | M2 | implemented |
| `internal.toniebox_firmware.otaVersionPd` | INTERNAL_UNSIGNED | `0` | src/settings.c:318 | M2 | implemented |
| `internal.freshnessCache` | INTERNAL_U64_ARRAY | `0` | src/settings.c:319 | M2 | implemented |
| `internal.freshnessCacheChanged` | INTERNAL_BOOL | `FALSE` | src/settings.c:321 | M2 | documented deviation |
| `internal.last_connection` | INTERNAL_UNSIGNED | `0` | src/settings.c:322 | M2 | documented deviation |
| `internal.last_ruid` | INTERNAL_STRING | `"ffffffffffffffff"` | src/settings.c:324 | M2 | documented deviation |
| `internal.last_ruid_time` | INTERNAL_UNSIGNED | `0` | src/settings.c:325 | M2 | documented deviation |
| `internal.ip` | INTERNAL_STRING | `""` | src/settings.c:326 | M2 | documented deviation |
| `internal.online` | INTERNAL_BOOL | `FALSE` | src/settings.c:327 | M2 | documented deviation |
| `internal.security_mit.incident` | INTERNAL_BOOL | `FALSE` | src/settings.c:328 | M2 | implemented |
| `internal.security_mit.blacklisted_domain_access` | INTERNAL_UNSIGNED | `0` | src/settings.c:330 | M2 | implemented |
| `internal.security_mit.crawler_access` | INTERNAL_UNSIGNED | `0` | src/settings.c:331 | M2 | implemented |
| `internal.security_mit.external_access` | INTERNAL_UNSIGNED | `0` | src/settings.c:332 | M2 | documented deviation |
| `internal.security_mit.robots_txt_access` | INTERNAL_UNSIGNED | `0` | src/settings.c:333 | M2 | implemented |
| `cloud` | TREE_DESC | `—` | src/settings.c:334 | M2 | implemented |
| `cloud.enabled` | BOOL | `FALSE` | src/settings.c:336 | M2 | implemented |
| `cloud.remote_hostname` | STRING | `"prod.de.tbs.toys"` | src/settings.c:337 | M2 | implemented |
| `cloud.remote_hostname_tb2` | STRING | `"tbs2.tonie.cloud"` | src/settings.c:338 | M2 | implemented |
| `cloud.remote_port` | UNSIGNED | `443` | src/settings.c:339 | M2 | implemented |
| `cloud.remote_port_tb2` | UNSIGNED | `443` | src/settings.c:340 | M2 | implemented |
| `cloud.enableV1Claim` | BOOL | `TRUE` | src/settings.c:341 | M2 | implemented |
| `cloud.enableV1CloudReset` | BOOL | `FALSE` | src/settings.c:342 | M2 | implemented |
| `cloud.enableV1FreshnessCheck` | BOOL | `TRUE` | src/settings.c:343 | M2 | implemented |
| `cloud.enableV3FreshnessCheck` | BOOL | `TRUE` | src/settings.c:344 | M2 | implemented |
| `cloud.enableV1Log` | BOOL | `FALSE` | src/settings.c:345 | M2 | implemented |
| `cloud.enableV1Time` | BOOL | `FALSE` | src/settings.c:346 | M2 | implemented |
| `cloud.enableV1Ota` | BOOL | `FALSE` | src/settings.c:347 | M2 | implemented |
| `cloud.enableV3Ota` | BOOL | `TRUE` | src/settings.c:348 | M2 | implemented |
| `cloud.enableV2Content` | BOOL | `TRUE` | src/settings.c:349 | M2 | implemented |
| `cloud.enableV3SetupStatus` | BOOL | `TRUE` | src/settings.c:350 | M2 | implemented |
| `cloud.enableV3ContentMeta` | BOOL | `TRUE` | src/settings.c:351 | M2 | implemented |
| `cloud.enableV3Chapter` | BOOL | `TRUE` | src/settings.c:352 | M2 | implemented |
| `cloud.cacheOta` | BOOL | `TRUE` | src/settings.c:353 | M2 | implemented |
| `cloud.localOta` | BOOL | `FALSE` | src/settings.c:354 | M2 | implemented |
| `cloud.cacheContent` | BOOL | `TRUE` | src/settings.c:355 | M2 | implemented |
| `cloud.cacheToLibrary` | BOOL | `TRUE` | src/settings.c:356 | M2 | implemented |
| `cloud.markCustomTagByPass` | BOOL | `TRUE` | src/settings.c:357 | M2 | implemented |
| `cloud.markCustomTagByUid` | BOOL | `TRUE` | src/settings.c:358 | M2 | implemented |
| `cloud.prioCustomContent` | BOOL | `TRUE` | src/settings.c:359 | M2 | implemented |
| `cloud.updateOnLowerAudioId` | BOOL | `TRUE` | src/settings.c:360 | M2 | implemented |
| `cloud.dumpRuidAuthContentJson` | BOOL | `TRUE` | src/settings.c:361 | M2 | implemented |
| `cloud.autoMarkListenedOnSync` | BOOL | `TRUE` | src/settings.c:362 | M2 | implemented |
| `encode` | TREE_DESC | `—` | src/settings.c:363 | M2 | implemented |
| `encode.bitrate` | UNSIGNED | `96` | src/settings.c:365 | M2 | implemented |
| `encode.ffmpeg_stream_buffer_ms` | UNSIGNED | `2000` | src/settings.c:366 | M2 | implemented |
| `encode.ffmpeg_stream_restart` | BOOL | `FALSE` | src/settings.c:367 | M2 | implemented |
| `encode.ffmpeg_sweep_startup_buffer` | BOOL | `TRUE` | src/settings.c:368 | M2 | implemented |
| `encode.ffmpeg_sweep_delay_ms` | UNSIGNED | `2000` | src/settings.c:369 | M2 | implemented |
| `encode.stream_max_size` | UNSIGNED | `1024 * 1024 * 40 * 6 - 1` | src/settings.c:370 | M2 | implemented |
| `encode.use_frontend` | BOOL | `TRUE` | src/settings.c:371 | M2 | implemented |
| `frontend` | TREE_DESC | `—` | src/settings.c:372 | M2 | implemented |
| `frontend.split_model_content` | BOOL | `TRUE` | src/settings.c:374 | M2 | implemented |
| `frontend.ignore_web_version_mismatch` | BOOL | `FALSE` | src/settings.c:375 | M2 | implemented |
| `frontend.confirm_audioplayer_close` | BOOL | `TRUE` | src/settings.c:376 | M2 | implemented |
| `frontend.check_cc3200_cfw` | BOOL | `FALSE` | src/settings.c:377 | M2 | implemented |
| `frontend.web_auth_enabled` | BOOL | `FALSE` | src/settings.c:378 | M2 | implemented |
| `toniebox` | TREE_DESC | `—` | src/settings.c:379 | M2 | implemented |
| `toniebox.api_access` | BOOL | `TRUE` | src/settings.c:381 | M2 | implemented |
| `toniebox.overrideCloud` | BOOL | `TRUE` | src/settings.c:382 | M2 | implemented |
| `toniebox.boxGeneration` | UNSIGNED | `GENERATION_UNKNOWN` | src/settings.c:383 | M2 | implemented |
| `toniebox.max_vol_spk` | UNSIGNED | `3` | src/settings.c:385 | M2 | implemented |
| `toniebox.max_vol_hdp` | UNSIGNED | `3` | src/settings.c:386 | M2 | implemented |
| `toniebox.slap_enabled` | BOOL | `TRUE` | src/settings.c:387 | M2 | implemented |
| `toniebox.slap_back_left` | BOOL | `FALSE` | src/settings.c:388 | M2 | implemented |
| `toniebox.led` | UNSIGNED | `0` | src/settings.c:389 | M2 | implemented |
| `toniebox.overrideFields` | BOOL | `FALSE` | src/settings.c:390 | M2 | implemented |
| `toniebox.field2` | UNSIGNED | `7` | src/settings.c:391 | M2 | implemented |
| `toniebox.field6` | UNSIGNED | `1` | src/settings.c:392 | M2 | implemented |
| `toniebox2` | TREE_DESC | `—` | src/settings.c:393 | M2 | implemented |
| `toniebox2.baby_mode` | BOOL | `FALSE` | src/settings.c:395 | M2 | implemented |
| `toniebox2.lightring_brightness` | UNSIGNED | `100` | src/settings.c:396 | M2 | implemented |
| `rtnl` | TREE_DESC | `—` | src/settings.c:397 | M2 | implemented |
| `rtnl.logRaw` | BOOL | `FALSE` | src/settings.c:399 | M2 | implemented |
| `rtnl.logHuman` | BOOL | `FALSE` | src/settings.c:400 | M2 | implemented |
| `rtnl.logRawFile` | STRING | `"config/rtnl.bin"` | src/settings.c:401 | M2 | implemented |
| `rtnl.logHumanFile` | STRING | `"config/rtnl.csv"` | src/settings.c:402 | M2 | implemented |
| `pcap` | TREE_DESC | `—` | src/settings.c:403 | M2 | implemented |
| `pcap.enabled` | BOOL | `FALSE` | src/settings.c:405 | M2 | implemented |
| `pcap.filename` | STRING | `"config/traffic.pcap"` | src/settings.c:406 | M2 | implemented |
| `mqtt` | TREE_DESC | `—` | src/settings.c:407 | M5 | implemented |
| `mqtt.enabled` | BOOL | `FALSE` | src/settings.c:409 | M5 | implemented |
| `mqtt.hostname` | STRING | `""` | src/settings.c:410 | M5 | implemented |
| `mqtt.port` | UNSIGNED | `1883` | src/settings.c:411 | M5 | implemented |
| `mqtt.username` | STRING | `""` | src/settings.c:412 | M5 | implemented |
| `mqtt.password` | STRING | `""` | src/settings.c:413 | M5 | implemented |
| `mqtt.identification` | STRING | `""` | src/settings.c:414 | M5 | implemented |
| `mqtt.topic` | STRING | `"teddyCloud"` | src/settings.c:415 | M5 | implemented |
| `mqtt.qosLevel` | UNSIGNED | `0` | src/settings.c:416 | M5 | implemented |
| `mqtt.retain_will` | BOOL | `TRUE` | src/settings.c:417 | M5 | implemented |
| `mqtt.disable_on_error` | BOOL | `FALSE` | src/settings.c:418 | M5 | implemented |
| `mqtt.tls_enabled` | BOOL | `FALSE` | src/settings.c:419 | M5 | implemented |
| `mqtt.tls_ca_file` | STRING | `""` | src/settings.c:420 | M5 | implemented |
| `mqtt.tls_insecure` | BOOL | `FALSE` | src/settings.c:421 | M5 | implemented |
| `mqtt_server` | TREE_DESC | `—` | src/settings.c:422 | M5 | implemented |
| `mqtt_server.enabled` | BOOL | `FALSE` | src/settings.c:424 | M5 | implemented |
| `mqtt_server.port` | UNSIGNED | `8883` | src/settings.c:425 | M5 | implemented |
| `mqtt_server.cert.crt` | STRING | `"certs/server_tb2/ici.pem"` | src/settings.c:426 | M5 | implemented |
| `mqtt_server.cert.key` | STRING | `"certs/server_tb2/ici.key"` | src/settings.c:427 | M5 | implemented |
| `hass` | TREE_DESC | `—` | src/settings.c:428 | M5 | implemented |
| `hass.name` | STRING | `"teddyCloud - Server"` | src/settings.c:430 | M5 | implemented |
| `hass.id` | STRING | `"teddyCloud_Server"` | src/settings.c:431 | M5 | implemented |
| `tonie_json` | TREE_DESC | `—` | src/settings.c:432 | M2 | implemented |
| `tonie_json.cache_images` | BOOL | `FALSE` | src/settings.c:434 | M2 | implemented |
| `tonie_json.cache_preload` | BOOL | `FALSE` | src/settings.c:435 | M2 | implemented |
| `tonie_json.custom_backup_keep` | UNSIGNED | `10` | src/settings.c:436 | M2 | implemented |
| `debug` | TREE_DESC | `—` | src/settings.c:437 | M2 | implemented |
| `debug.web.pcm_encode_console_url` | BOOL | `FALSE` | src/settings.c:439 | M2 | implemented |

## CLI modes

The option spelling comes from getopt, including `encode_test` (the usage text differs). Auxiliary flags inherit the invoked operation. Test modes must use a mock cloud/localhost target.

| Option | Argument | Source | Milestone / acceptance | Status |
| --- | --- | --- | --- | --- |
| `--base_path` | required_argument | src/main.c:287 | M2: exit/output/files | implemented |
| `--source` | required_argument | src/main.c:288 | M2: exit/output/files | implemented |
| `--destination` | required_argument | src/main.c:289 | M2: exit/output/files | implemented |
| `--generate-server-certs` | no_argument | src/main.c:290 | M4: exit/output/files | implemented |
| `--generate-client-cert` | required_argument | src/main.c:291 | M4: exit/output/files | implemented |
| `--generate-client-cert-tb2` | required_argument | src/main.c:292 | M4: exit/output/files | implemented |
| `--add-to-settings` | no_argument | src/main.c:293 | M2: exit/output/files | implemented |
| `--encode` | required_argument | src/main.c:294 | M3: exit/output/files | implemented |
| `--encode_test` | required_argument | src/main.c:295 | M3: exit/output/files | implemented |
| `--skip-seconds` | required_argument | src/main.c:296 | M3: exit/output/files | implemented |
| `--esp32-hostpatch` | required_argument | src/main.c:297 | M4: exit/output/files | implemented |
| `--esp32-portpatch` | required_argument | src/main.c:298 | M4: exit/output/files | implemented |
| `--port` | required_argument | src/main.c:299 | M2: exit/output/files | implemented |
| `--oldrtnlhost` | required_argument | src/main.c:300 | M4: exit/output/files | implemented |
| `--oldapihost` | required_argument | src/main.c:301 | M4: exit/output/files | implemented |
| `--esp32-fixup` | required_argument | src/main.c:302 | M4: exit/output/files | implemented |
| `--esp32-inject` | required_argument | src/main.c:303 | M4: exit/output/files | implemented |
| `--esp32-extract` | required_argument | src/main.c:304 | M4: exit/output/files | implemented |
| `--docker-test` | no_argument | src/main.c:305 | M2: exit/output/files | implemented |
| `--url-test` | required_argument | src/main.c:306 | M2: exit/output/files | implemented |
| `--cloud-test` | required_argument | src/main.c:307 | M2: exit/output/files | implemented |
| `--hash` | required_argument | src/main.c:308 | M2: exit/output/files | implemented |
| `--hostname` | required_argument | src/main.c:309 | M2: exit/output/files | implemented |
| `--config-set` | required_argument | src/main.c:310 | M2: exit/output/files | implemented |
| `--help` | no_argument | src/main.c:311 | M2: exit/output/files | implemented |

## Persisted formats and integrations

| Area | Contract/source | Milestone / acceptance | Status |
| --- | --- | --- | --- |
| INI | config/config.ini and config.overlay.ini; flat key=value, overlay.<CN>.<key>; src/settings.c settings_load_ovl/settings_save_ovl | M2: roundtrip/defaults/unknown fields | implemented |
| Certificates | configured PEM/DER server TB1/TB2 and client identities; src/cert.c + settings paths | M2/4: preserve hashes/one-time generation/import/download | implemented |
| Content | UID path resolution, content.json, .nocloud/.live; src/contentJson.c + src/handler_cloud.c | M2/3: resolve fixtures/metadata/cloud policy | implemented |
| Library and playlists | configured librarydir, src/tonie_audio_playlist.c + handler_api.c | M3/4: playlist/seek/migration effects | implemented |
| TAF | proto/toniebox.pb.taf-header.proto + src/toniefile.c; payload SHA1/Ogg 4096-byte alignment | M1/3: generated multi-track integrity/playback | implemented |
| Tonies JSON | tonies.json, tonies.custom.json, box metadata, content links; src/toniesJson.c | M4: CRUD/search/reload/normalization | implemented |
| Custom backups | tonie_json.custom_backup_keep default 10; timestamped backups and linked rename | M4: retention/recovery/collision | implemented |
| OTA and cache | core.firmwaredir/core.cachedir generation-specific layouts; src/handler.c + handler_cloud.c + cache.c | M3: partial/concurrent downloads | implemented |
| RTNL logs | proto/toniebox.pb.rtnl.proto + src/handler_rtnl.c; configured raw/log/dump output | M2: decoded state/log effects | implemented |
| SSE/stats | src/handler_sse.c + src/stats.c | M4: event names/payloads/slow consumer | implemented |
| MQTT client/HA | src/mqtt.c + src/home_assistant.c + MQTT_CONTROL.md | M5: topics/discovery/5-to-60s reconnect/commands | implemented |
| MQTT server | src/mqtt_server.c; TB2 server certificate defaults, enabled=false | M5: connect/topic/listener fixture | implemented |
| Firmware | src/esp32.c; host patch/fixup/inject/extract using copied images | M4: checksums/layout/original preservation | implemented |
| Reverse proxies | src/handler_reverse.c; configured generic target mappings/cloud auth | M4: path/headers/auth/error comparison | implemented |
| Security/auth | handler_web_auth.c users/sessions + handler_security_mit.c; compare configured behavior | M4: sessions/lock/threat fixture | implemented |
| PCAP/TLS diagnostics | src/pcaplog.c + pcap_dump.c + tls_adapter.c; configured logs | M4: sanitized outputs/no private fixture inclusion | implemented |

## Evidence policy

See `docs/verification.md` for commands/results. Hardware acceptance stays pending without a recorded physical chip/firmware. No release parity is claimed while pending rows remain. Reference backend C dependencies are oracle-only; the Go backend will not link them. WASM/emsdk are recorded for provenance but excluded from the port.
