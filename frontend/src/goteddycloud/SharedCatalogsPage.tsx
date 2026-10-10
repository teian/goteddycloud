// SPDX-License-Identifier: GPL-2.0-only
import { useEffect, useState } from "react";
import { Alert, Button, Card, Checkbox, Flex, Form, Input, Select, Space, Table } from "antd";
import type { Key } from "react";
import { Link } from "react-router-dom";
import { useAuth } from "../provider/AuthProvider";
import { AccessLayout } from "./AccessLayout";
import { accessApi, Catalog, CatalogFile, downloadUrl, itemKey, Recipient } from "./api";
import { useGoText } from "./text";
export function SharedCatalogsPage() {
    const auth = useAuth(),
        text = useGoText();
    const [catalogs, setCatalogs] = useState<Catalog[]>([]),
        [area, setArea] = useState("");
    const [items, setItems] = useState<CatalogFile[]>([]),
        [selected, setSelected] = useState<Key[]>([]);
    const [revision, setRevision] = useState(0),
        [loading, setLoading] = useState(false),
        [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [form] = Form.useForm<{ username: string; all: boolean }>();
    const catalog = catalogs.find((item) => item.area === area);
    useEffect(() => {
        if (!auth.loggedIn) return;
        const controller = new AbortController();
        accessApi<{ catalogs: Catalog[] }>("catalog/shares", undefined, controller.signal)
            .then((data) => {
                setCatalogs(data.catalogs);
                setArea((current) =>
                    data.catalogs.some((item) => item.area === current)
                        ? current
                        : data.catalogs[0]?.area || "",
                );
            })
            .catch((error) => {
                if (!controller.signal.aborted) setError(String(error));
            });
        return () => controller.abort();
    }, [auth.username, auth.loggedIn, revision]);
    useEffect(() => {
        setItems([]);
        setSelected([]);
        form.resetFields();
        setError("");
        if (!area) return;
        const controller = new AbortController();
        setLoading(true);
        accessApi<{ items: CatalogFile[] }>(
            "catalog/items?" + new URLSearchParams({ area }),
            undefined,
            controller.signal,
        )
            .then((data) => {
                if (!controller.signal.aborted) setItems(data.items);
            })
            .catch((error) => {
                if (!controller.signal.aborted) setError(String(error));
            })
            .finally(() => {
                if (!controller.signal.aborted) setLoading(false);
            });
        return () => controller.abort();
    }, [area, revision, form]);
    const run = async (operation: () => Promise<unknown>) => {
        setBusy(true);
        setError("");
        try {
            await operation();
            setRevision((value) => value + 1);
        } catch (error) {
            setError(String(error));
        } finally {
            setBusy(false);
        }
    };
    const edit = (recipient: Recipient) => {
        form.setFieldsValue({ username: recipient.username, all: recipient.all });
        setSelected((recipient.items || []).map(itemKey));
    };
    return (
        <AccessLayout title={text("shares")} section="tonies">
            {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
            {!auth.loggedIn ? (
                <Alert
                    type="info"
                    title={text("loginNeeded")}
                    action={<Link to="/settings/users">{text("login")}</Link>}
                />
            ) : (
                <Space orientation="vertical" size="large" style={{ width: "100%" }}>
                    <Alert type="info" showIcon title={text("readOnly")} />
                    <Flex wrap gap={12}>
                        <Select
                            aria-label={text("catalog")}
                            style={{ minWidth: 240 }}
                            value={area || undefined}
                            disabled={busy}
                            options={catalogs.map((item) => ({
                                label: item.name,
                                value: item.area,
                            }))}
                            onChange={setArea}
                        />
                        <Button
                            disabled={busy || loading}
                            onClick={() => setRevision((value) => value + 1)}
                        >
                            {text("refresh")}
                        </Button>
                    </Flex>
                    <Table<CatalogFile>
                        rowKey={itemKey}
                        loading={loading}
                        dataSource={items}
                        scroll={{ x: true }}
                        locale={{ emptyText: text("empty") }}
                        rowSelection={
                            catalog?.manage
                                ? {
                                      selectedRowKeys: selected,
                                      onChange: setSelected,
                                      preserveSelectedRowKeys: true,
                                      getCheckboxProps: () => ({ disabled: busy }),
                                  }
                                : undefined
                        }
                        columns={[
                            { title: text("name"), render: (_, item) => item.title || item.path },
                            {
                                title: text("catalog"),
                                render: (_, item) => `${item.root}/${item.path}`,
                            },
                            { title: "Bytes", dataIndex: "size" },
                            {
                                title: text("download"),
                                fixed: "right",
                                width: 120,
                                render: (_, item) => (
                                    <a href={downloadUrl(area, item)}>{text("download")}</a>
                                ),
                            },
                        ]}
                    />
                    {catalog?.manage && (
                        <Card title={text("grant")}>
                            <Form
                                name="catalogGrant"
                                form={form}
                                layout="vertical"
                                initialValues={{ all: false }}
                                disabled={busy || loading}
                                onFinish={(values) => {
                                    const files = items
                                        .filter((item) => selected.includes(itemKey(item)))
                                        .map(({ root, path }) => ({ root, path }));
                                    if (!values.all && !files.length) {
                                        setError(text("selectFiles"));
                                        return;
                                    }
                                    void run(() =>
                                        accessApi("catalog/grant", {
                                            area,
                                            username: values.username,
                                            all: !!values.all,
                                            items: values.all ? [] : files,
                                        }),
                                    );
                                }}
                            >
                                <Form.Item
                                    name="username"
                                    label={text("recipient")}
                                    rules={[{ required: true }]}
                                >
                                    <Input maxLength={32} autoComplete="off" />
                                </Form.Item>
                                <Form.Item name="all" valuePropName="checked">
                                    <Checkbox>{text("all")}</Checkbox>
                                </Form.Item>
                                <Button type="primary" htmlType="submit" loading={busy}>
                                    {text("grant")}
                                </Button>
                            </Form>
                            <h2>{text("outgoing")}</h2>
                            <Table<Recipient>
                                rowKey="username"
                                dataSource={catalog.recipients}
                                pagination={false}
                                scroll={{ x: true }}
                                columns={[
                                    { title: text("username"), dataIndex: "username" },
                                    {
                                        title: text("catalog"),
                                        render: (_, recipient) =>
                                            recipient.all
                                                ? text("all")
                                                : `${text("selected")}: ${recipient.items?.length || 0}`,
                                    },
                                    {
                                        title: "",
                                        render: (_, recipient) => (
                                            <Space>
                                                <Button
                                                    disabled={busy || loading}
                                                    onClick={() => edit(recipient)}
                                                >
                                                    {text("edit")}
                                                </Button>
                                                <Button
                                                    danger
                                                    disabled={busy}
                                                    onClick={() =>
                                                        void run(() =>
                                                            accessApi("catalog/revoke", {
                                                                area,
                                                                username: recipient.username,
                                                            }),
                                                        )
                                                    }
                                                >
                                                    {text("revoke")}
                                                </Button>
                                            </Space>
                                        ),
                                    },
                                ]}
                            />
                        </Card>
                    )}
                </Space>
            )}
        </AccessLayout>
    );
}
