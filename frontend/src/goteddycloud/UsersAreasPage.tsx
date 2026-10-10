// SPDX-License-Identifier: GPL-2.0-only
import { useCallback, useEffect, useState } from "react";
import {
    Alert,
    Button,
    Card,
    Flex,
    Form,
    Input,
    Popconfirm,
    Select,
    Space,
    Switch,
    Table,
} from "antd";
import { useAuth } from "../provider/AuthProvider";
import { AccessLayout } from "./AccessLayout";
import {
    accessApi,
    managementApi,
    Area,
    Member,
    memberRole,
    Profile,
    User,
    switchArea,
} from "./api";
import { useGoText } from "./text";

type Box = { ID: string };
type AreaValues = { name: string; members: Member[]; boxes: string[] };
function AreaEditor({
    area,
    globalAdmin,
    boxes,
    areas,
    busy,
    onSave,
}: {
    area: Area;
    globalAdmin: boolean;
    boxes: Box[];
    areas: Area[];
    busy: boolean;
    onSave: (values: AreaValues) => void;
}) {
    const text = useGoText(),
        [form] = Form.useForm<AreaValues>();
    const roleOptions = (["admin", "editor", "reader"] as const).map((value) => ({
        value,
        label: text(value),
    }));
    useEffect(() => {
        form.setFieldsValue({ name: area.name, members: area.members, boxes: area.boxes });
    }, [area, form]);
    return (
        <Card title={`${text("manageArea")}: ${area.name}`}>
            <Form
                name={`area-${area.id}`}
                form={form}
                layout="vertical"
                disabled={busy}
                onFinish={onSave}
            >
                <Form.Item name="name" label={text("name")} rules={[{ required: true }]}>
                    <Input maxLength={64} />
                </Form.Item>
                <h3>{text("members")}</h3>
                <Form.List name="members">
                    {(fields, { add, remove }) => (
                        <>
                            {fields.map((field) => (
                                <Flex gap={8} wrap key={field.key}>
                                    <Form.Item
                                        name={[field.name, "username"]}
                                        rules={[{ required: true }]}
                                    >
                                        <Input
                                            aria-label={text("username")}
                                            placeholder={text("username")}
                                            maxLength={32}
                                        />
                                    </Form.Item>
                                    <Form.Item
                                        name={[field.name, "role"]}
                                        rules={[{ required: true }]}
                                    >
                                        <Select
                                            aria-label={text("role")}
                                            style={{ minWidth: 180 }}
                                            options={roleOptions}
                                        />
                                    </Form.Item>
                                    <Button disabled={busy} onClick={() => remove(field.name)}>
                                        {text("remove")}
                                    </Button>
                                </Flex>
                            ))}
                            <Button
                                disabled={busy}
                                onClick={() => add({ username: "", role: "reader" })}
                            >
                                {text("addMember")}
                            </Button>
                        </>
                    )}
                </Form.List>
                {globalAdmin && (
                    <Form.Item name="boxes" label={text("boxes")}>
                        <Select
                            mode="multiple"
                            options={Array.from(
                                new Set([...boxes.map((box) => box.ID), ...area.boxes]),
                            ).map((id) => ({
                                value: id,
                                label: id,
                                disabled: areas.some(
                                    (other) =>
                                        other.id !== area.id &&
                                        other.boxes.some(
                                            (box) => box.toLowerCase() === id.toLowerCase(),
                                        ),
                                ),
                            }))}
                        />
                    </Form.Item>
                )}
                <Button type="primary" htmlType="submit" loading={busy} style={{ marginTop: 16 }}>
                    {text("save")}
                </Button>
            </Form>
        </Card>
    );
}
export function UsersAreasPage() {
    const auth = useAuth(),
        text = useGoText();
    const globalAdmin = !auth.enabled || auth.role === "admin";
    const [areas, setAreas] = useState<Area[]>([]),
        [users, setUsers] = useState<User[]>([]),
        [boxes, setBoxes] = useState<Box[]>([]);
    const [profile, setProfile] = useState<Profile>(),
        [editing, setEditing] = useState<string>();
    const [revision, setRevision] = useState(0);
    const [busy, setBusy] = useState(false),
        [loading, setLoading] = useState(true),
        [error, setError] = useState(""),
        [notice, setNotice] = useState("");
    const [createForm] = Form.useForm(),
        [areaForm] = Form.useForm(),
        [passwordForm] = Form.useForm(),
        [resetForm] = Form.useForm();
    const roleOptions = (["admin", "editor", "reader"] as const).map((value) => ({
        value,
        label: text(value),
    }));
    const accountOptions = (["admin", "editor", "reader"] as const).map((value) => ({
        value,
        label: text(
            value === "admin"
                ? "accountAdmin"
                : value === "editor"
                  ? "accountEditor"
                  : "accountReader",
        ),
    }));
    const load = useCallback(
        async (signal?: AbortSignal) => {
            setLoading(true);
            try {
                const [areaData, profileData, userData, boxData] = await Promise.all([
                    accessApi<{ areas: Area[] }>("areas", undefined, signal),
                    auth.loggedIn
                        ? accessApi<Profile>("profile", undefined, signal)
                        : Promise.resolve(undefined),
                    globalAdmin
                        ? accessApi<{ users: User[] }>("users/details", undefined, signal)
                        : Promise.resolve({ users: [] }),
                    globalAdmin
                        ? managementApi<{ boxes: Box[] }>("/api/getBoxes?area=", undefined, signal)
                        : Promise.resolve({ boxes: [] }),
                ]);
                if (signal?.aborted) return;
                setAreas(areaData.areas);
                setProfile(profileData);
                setUsers(userData.users);
                setBoxes(boxData.boxes);
            } finally {
                if (!signal?.aborted) setLoading(false);
            }
        },
        [auth.username, auth.loggedIn, globalAdmin],
    );
    useEffect(() => {
        const controller = new AbortController();
        void load(controller.signal).catch((error) => {
            if (!controller.signal.aborted) setError(String(error));
        });
        return () => controller.abort();
    }, [load, revision]);
    const run = async (operation: () => Promise<unknown>) => {
        setBusy(true);
        setError("");
        setNotice("");
        try {
            await operation();
            await auth.refresh();
            setRevision((value) => value + 1);
            window.dispatchEvent(new Event("goteddycloud-access-changed"));
            setNotice(text("saved"));
        } catch (error) {
            setError(String(error));
        } finally {
            setBusy(false);
        }
    };
    const active = areas.find((area) => area.id === editing);
    return (
        <AccessLayout title={text("users")} section="settings">
            <Space orientation="vertical" size="large" style={{ width: "100%" }}>
                {error && <Alert type="error" showIcon title={error} />}
                {notice && <Alert type="success" title={notice} />}
                {auth.envOverride && <Alert type="warning" title={text("override")} />}
                {!auth.loggedIn && auth.userCount > 0 && (
                    <Card title={text("login")}>
                        <Form
                            name="manualLogin"
                            layout="vertical"
                            disabled={busy}
                            onFinish={(values) =>
                                void run(() => auth.login(values.username, values.password))
                            }
                        >
                            <Form.Item
                                name="username"
                                label={text("username")}
                                rules={[{ required: true }]}
                            >
                                <Input autoComplete="username" />
                            </Form.Item>
                            <Form.Item
                                name="password"
                                label={text("password")}
                                rules={[{ required: true }]}
                            >
                                <Input.Password autoComplete="current-password" />
                            </Form.Item>
                            <Button htmlType="submit">{text("login")}</Button>
                        </Form>
                    </Card>
                )}
                <Alert type="info" title={text("areasHelp")} />
                {profile && (
                    <Select
                        aria-label={text("area")}
                        value={profile.selectedArea || (globalAdmin ? "" : profile.defaultArea)}
                        disabled={busy}
                        style={{ width: "100%" }}
                        options={[
                            ...(globalAdmin ? [{ value: "", label: text("global") }] : []),
                            ...areas.map((area) => ({ value: area.id, label: area.name })),
                        ]}
                        onChange={(area) =>
                            void run(async () => {
                                await switchArea(area);
                            })
                        }
                    />
                )}
                <Table<Area>
                    rowKey="id"
                    dataSource={areas}
                    loading={loading}
                    pagination={false}
                    scroll={{ x: true }}
                    columns={[
                        { title: text("area"), dataIndex: "name" },
                        {
                            title: text("role"),
                            render: (_, area) =>
                                globalAdmin
                                    ? text("accountAdmin")
                                    : text(memberRole(area, auth.username) || "reader"),
                        },
                        {
                            title: text("members"),
                            render: (_, area) =>
                                area.members
                                    .map((member) => `${member.username}: ${text(member.role)}`)
                                    .join(", "),
                        },
                        {
                            title: "",
                            render: (_, area) => (
                                <Space>
                                    {(globalAdmin ||
                                        memberRole(area, auth.username) === "admin") && (
                                        <Button disabled={busy} onClick={() => setEditing(area.id)}>
                                            {text("edit")}
                                        </Button>
                                    )}
                                    {globalAdmin && (
                                        <Popconfirm
                                            title={text("removeArea")}
                                            onConfirm={() =>
                                                run(() =>
                                                    accessApi("areas/delete", { area: area.id }),
                                                )
                                            }
                                        >
                                            <Button
                                                danger
                                                disabled={
                                                    busy ||
                                                    users.some(
                                                        (user) => user.defaultArea === area.id,
                                                    )
                                                }
                                            >
                                                {text("remove")}
                                            </Button>
                                        </Popconfirm>
                                    )}
                                </Space>
                            ),
                        },
                    ]}
                />
                {active && (globalAdmin || memberRole(active, auth.username) === "admin") && (
                    <AreaEditor
                        area={active}
                        globalAdmin={globalAdmin}
                        boxes={boxes}
                        areas={areas}
                        busy={busy}
                        onSave={(values) =>
                            void run(() =>
                                accessApi("areas/update", {
                                    area: active.id,
                                    name: values.name,
                                    members: values.members || [],
                                    ...(globalAdmin ? { boxes: values.boxes || [] } : {}),
                                }),
                            )
                        }
                    />
                )}
                {globalAdmin && (
                    <>
                        <Card title={text("createArea")}>
                            <Form
                                name="createArea"
                                form={areaForm}
                                layout="inline"
                                disabled={busy}
                                onFinish={(values) =>
                                    void run(async () => {
                                        await accessApi("areas/create", values);
                                        areaForm.resetFields();
                                    })
                                }
                            >
                                <Form.Item
                                    name="name"
                                    label={text("name")}
                                    rules={[{ required: true }]}
                                >
                                    <Input maxLength={64} />
                                </Form.Item>
                                <Button type="primary" htmlType="submit">
                                    {text("create")}
                                </Button>
                            </Form>
                        </Card>
                        <Card title={text("accounts")}>
                            {!auth.userCount && <Alert type="info" title={text("setup")} />}
                            <Flex gap={12} align="center" style={{ marginBottom: 16 }}>
                                <Switch
                                    aria-label={text("enabled")}
                                    checked={auth.enabled}
                                    disabled={busy || auth.envOverride || !auth.userCount}
                                    onChange={(enabled) =>
                                        void run(() => accessApi("enabled", { enabled }))
                                    }
                                />
                                {text("enabled")}
                            </Flex>
                            <p>{text("accountHelp")}</p>
                            <Table<User>
                                rowKey="username"
                                dataSource={users}
                                pagination={false}
                                scroll={{ x: true }}
                                columns={[
                                    { title: text("username"), dataIndex: "username" },
                                    {
                                        title: text("role"),
                                        render: (_, user) => (
                                            <Select
                                                aria-label={`${text("role")}: ${user.username}`}
                                                style={{ minWidth: 220 }}
                                                value={user.role}
                                                options={accountOptions}
                                                disabled={busy}
                                                onChange={(role) =>
                                                    void run(() =>
                                                        accessApi("users/access", {
                                                            username: user.username,
                                                            role,
                                                        }),
                                                    )
                                                }
                                            />
                                        ),
                                    },
                                    {
                                        title: "",
                                        render: (_, user) => (
                                            <Popconfirm
                                                title={text("removeUser")}
                                                onConfirm={() =>
                                                    run(() =>
                                                        accessApi("users/delete", {
                                                            username: user.username,
                                                        }),
                                                    )
                                                }
                                            >
                                                <Button danger disabled={busy}>
                                                    {text("remove")}
                                                </Button>
                                            </Popconfirm>
                                        ),
                                    },
                                ]}
                            />
                        </Card>
                        <Card title={text("createUser")}>
                            <Form
                                name="createAccount"
                                form={createForm}
                                layout="vertical"
                                disabled={busy}
                                onFinish={(values) =>
                                    void run(async () => {
                                        await accessApi("users/create", {
                                            ...values,
                                            role: users.length ? values.role : "admin",
                                        });
                                        createForm.resetFields();
                                    })
                                }
                            >
                                <Form.Item
                                    name="username"
                                    label={text("username")}
                                    rules={[{ required: true }, { pattern: /^[A-Za-z0-9_.-]+$/ }]}
                                >
                                    <Input maxLength={32} autoComplete="off" />
                                </Form.Item>
                                <Form.Item
                                    name="password"
                                    label={text("password")}
                                    rules={[{ required: true, min: 4, max: 128 }]}
                                >
                                    <Input.Password autoComplete="new-password" />
                                </Form.Item>
                                <Form.Item name="role" label={text("role")} initialValue="editor">
                                    <Select
                                        disabled={!users.length || busy}
                                        options={
                                            users.length
                                                ? accountOptions
                                                : [{ value: "editor", label: text("accountAdmin") }]
                                        }
                                    />
                                </Form.Item>
                                <Button type="primary" htmlType="submit" loading={busy}>
                                    {text("create")}
                                </Button>
                            </Form>
                        </Card>
                        {!!users.length && (
                            <Card title={text("resetPassword")}>
                                <Form
                                    name="resetPassword"
                                    form={resetForm}
                                    layout="vertical"
                                    disabled={busy}
                                    onFinish={(values) =>
                                        void run(async () => {
                                            await accessApi("users/updatePassword", values);
                                            resetForm.resetFields();
                                        })
                                    }
                                >
                                    <Form.Item
                                        name="username"
                                        label={text("username")}
                                        rules={[{ required: true }]}
                                    >
                                        <Select
                                            options={users.map((user) => ({
                                                value: user.username,
                                                label: user.username,
                                            }))}
                                        />
                                    </Form.Item>
                                    <Form.Item
                                        name="password"
                                        label={text("newPassword")}
                                        rules={[{ required: true, min: 4, max: 128 }]}
                                    >
                                        <Input.Password autoComplete="new-password" />
                                    </Form.Item>
                                    <Button htmlType="submit" loading={busy}>
                                        {text("save")}
                                    </Button>
                                </Form>
                            </Card>
                        )}
                    </>
                )}
                {auth.loggedIn && (
                    <Card title={text("ownPassword")}>
                        <Form
                            name="ownPassword"
                            form={passwordForm}
                            layout="vertical"
                            disabled={busy}
                            onFinish={(values) =>
                                void run(async () => {
                                    await accessApi("password", values);
                                    passwordForm.resetFields();
                                    await auth.logout();
                                })
                            }
                        >
                            <Form.Item
                                name="currentPassword"
                                label={text("currentPassword")}
                                rules={[{ required: true }]}
                            >
                                <Input.Password autoComplete="current-password" />
                            </Form.Item>
                            <Form.Item
                                name="password"
                                label={text("newPassword")}
                                rules={[{ required: true, min: 4, max: 128 }]}
                            >
                                <Input.Password autoComplete="new-password" />
                            </Form.Item>
                            <Button htmlType="submit" loading={busy}>
                                {text("save")}
                            </Button>
                        </Form>
                    </Card>
                )}
            </Space>
        </AccessLayout>
    );
}
