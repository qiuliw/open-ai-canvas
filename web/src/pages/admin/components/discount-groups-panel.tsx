import { useEffect, useRef, useState } from "react";
import { App, Button, Drawer, Form, Input, InputNumber, Switch } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Edit3, Plus, RefreshCw, Trash2 } from "lucide-react";

import {
    createAdminDiscountGroup,
    deleteAdminDiscountGroup,
    listAdminDiscountGroups,
    updateAdminDiscountGroup,
    type DiscountGroup,
} from "@/services/api/wallet";
import { AdminDataTable, AdminRowActions, AdminStatusBadge, AdminTableEmpty, PaginationBar } from "./admin-ui";

type MultiplierRow = { model?: string; multiplier?: number };
type GroupFormValues = {
    name: string;
    description?: string;
    defaultMultiplier: number;
    modelMultipliers: MultiplierRow[];
    enabled: boolean;
};

type GroupDialog = { mode: "create" } | { mode: "edit"; group: DiscountGroup };

function formValuesFromGroup(group?: DiscountGroup): GroupFormValues {
    return {
        name: group?.name || "",
        description: group?.description || "",
        defaultMultiplier: (group?.defaultMultiplierBasisPoints ?? 10_000) / 10_000,
        modelMultipliers: Object.entries(group?.modelMultiplierBasisPoints || {}).map(([model, value]) => ({
            model,
            multiplier: value / 10_000,
        })),
        enabled: group?.enabled ?? true,
    };
}

function payloadFromForm(values: GroupFormValues) {
    const modelMultiplierBasisPoints: Record<string, number> = {};
    for (const row of values.modelMultipliers || []) {
        const model = row.model?.trim();
        if (!model || row.multiplier == null) continue;
        modelMultiplierBasisPoints[model] = Math.round(row.multiplier * 10_000);
    }
    return {
        name: values.name.trim(),
        description: values.description?.trim() || "",
        defaultMultiplierBasisPoints: Math.round(values.defaultMultiplier * 10_000),
        modelMultiplierBasisPoints,
        enabled: values.enabled,
    };
}

function formatMultiplier(basisPoints: number) {
    return `${(basisPoints / 10_000).toFixed(4).replace(/\.?0+$/, "")}x`;
}

/** 嵌入积分策略抽屉的「用户倍率分组」区块。 */
export default function DiscountGroupsPanel({
    active,
    onGroupsChanged,
}: {
    active: boolean;
    onGroupsChanged?: () => void;
}) {
    const { message } = App.useApp();
    const [groups, setGroups] = useState<DiscountGroup[]>([]);
    const [loading, setLoading] = useState(false);
    const [saving, setSaving] = useState(false);
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(10);
    const [total, setTotal] = useState(0);
    const [dialog, setDialog] = useState<GroupDialog | null>(null);
    const [form] = Form.useForm<GroupFormValues>();
    const listRequestRef = useRef(0);

    const reload = async (targetPage = page, targetPageSize = pageSize) => {
        const requestId = ++listRequestRef.current;
        setLoading(true);
        try {
            const result = await listAdminDiscountGroups({ page: targetPage, pageSize: targetPageSize });
            if (requestId !== listRequestRef.current) return;
            if (targetPage > 1 && result.total > 0 && result.groups.length === 0) {
                setPage(1);
                return;
            }
            setGroups(result.groups);
            setTotal(result.total);
        } catch (error) {
            if (requestId === listRequestRef.current) message.error(error instanceof Error ? error.message : "读取用户倍率分组失败");
        } finally {
            if (requestId === listRequestRef.current) setLoading(false);
        }
    };

    useEffect(() => {
        if (!active) return;
        void reload(page, pageSize);
    }, [active, page, pageSize]);

    useEffect(() => {
        if (!dialog) {
            form.resetFields();
            return;
        }
        form.setFieldsValue(formValuesFromGroup(dialog.mode === "edit" ? dialog.group : undefined));
    }, [dialog, form]);

    const save = async () => {
        if (!dialog) return;
        const values = await form.validateFields();
        const payload = payloadFromForm(values);
        setSaving(true);
        try {
            if (dialog.mode === "create") {
                await createAdminDiscountGroup(payload);
                message.success("用户倍率分组已创建");
            } else {
                await updateAdminDiscountGroup(dialog.group.id, payload);
                message.success("用户倍率分组已保存");
            }
            setDialog(null);
            setPage(1);
            await reload(1, pageSize);
            onGroupsChanged?.();
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存用户倍率分组失败");
        } finally {
            setSaving(false);
        }
    };

    const remove = async (group: DiscountGroup) => {
        try {
            await deleteAdminDiscountGroup(group.id);
            message.success("用户倍率分组已删除");
            await reload(page, pageSize);
            onGroupsChanged?.();
        } catch (error) {
            message.error(error instanceof Error ? error.message : "删除用户倍率分组失败");
        }
    };

    const columns: ColumnsType<DiscountGroup> = [
        {
            title: "分组",
            dataIndex: "name",
            render: (_, group) => (
                <div>
                    <div className="font-medium">{group.name}</div>
                    <div className="text-xs text-foreground/45">{group.description || "无说明"}</div>
                </div>
            ),
        },
        {
            title: "默认倍率",
            dataIndex: "defaultMultiplierBasisPoints",
            width: 96,
            align: "center",
            render: (value: number) => <span className="tabular-nums">{formatMultiplier(value)}</span>,
        },
        {
            title: "成员",
            dataIndex: "memberCount",
            width: 72,
            align: "center",
            render: (value) => <span className="tabular-nums">{value ?? 0}</span>,
        },
        {
            title: "状态",
            dataIndex: "enabled",
            width: 88,
            align: "center",
            render: (enabled: boolean) => <AdminStatusBadge label={enabled ? "已启用" : "已停用"} tone={enabled ? "success" : "neutral"} />,
        },
        {
            title: "操作",
            width: 140,
            align: "center",
            render: (_, group) => (
                <AdminRowActions
                    primary={{ label: "编辑", icon: <Edit3 className="size-3.5" />, onClick: () => setDialog({ mode: "edit", group }) }}
                    visibleActionCount={1}
                    actions={[
                        {
                            key: "delete",
                            label: "删除分组",
                            icon: <Trash2 className="size-3.5" />,
                            danger: true,
                            confirm: {
                                title: "删除这个用户倍率分组？",
                                description: "删除后会清除已分配用户的分组，历史账单倍率不受影响。",
                                okText: "确认删除",
                            },
                            onClick: () => void remove(group),
                        },
                    ]}
                />
            ),
        },
    ];

    return (
        <section className="admin-credit-drawer-section" aria-label="用户倍率分组">
            <div className="admin-credit-drawer-section-heading admin-credit-drawer-section-heading-with-action">
                <div>
                    <h3>用户倍率分组</h3>
                    <p>最终计费再乘本分组倍率，在用户管理中分配。</p>
                </div>
                <div className="flex items-center gap-1">
                    <Button type="text" size="small" icon={<RefreshCw className="size-3.5" />} loading={loading} onClick={() => void reload(page, pageSize)}>
                        刷新
                    </Button>
                    <Button type="text" size="small" icon={<Plus className="size-3.5" />} onClick={() => setDialog({ mode: "create" })}>
                        添加分组
                    </Button>
                </div>
            </div>

            <AdminDataTable
                table={{
                    className: "app-data-table",
                    rowKey: "id",
                    size: "small",
                    loading,
                    pagination: false,
                    columns,
                    dataSource: groups,
                }}
                empty={<AdminTableEmpty title="还没有用户倍率分组" description="创建后可在用户管理中分配。" />}
                footer={
                    <PaginationBar
                        current={page}
                        pageSize={pageSize}
                        total={total}
                        onChange={(nextPage, nextSize) => {
                            setPage(nextPage);
                            setPageSize(nextSize);
                        }}
                    />
                }
            />

            <Drawer
                title={dialog?.mode === "edit" ? `编辑倍率分组 · ${dialog.group.name}` : "新建用户倍率分组"}
                open={Boolean(dialog)}
                size="min(520px, 100vw)"
                onClose={() => {
                    if (!saving) setDialog(null);
                }}
                mask={{ closable: !saving }}
                destroyOnHidden
                rootClassName="admin-drawer admin-credit-drawer"
                footer={
                    <div className="flex justify-end gap-2">
                        <Button disabled={saving} onClick={() => setDialog(null)}>
                            取消
                        </Button>
                        <Button type="primary" loading={saving} onClick={() => void save()}>
                            保存分组
                        </Button>
                    </div>
                }
            >
                <Form form={form} layout="vertical" requiredMark={false} initialValues={formValuesFromGroup()}>
                    <Form.Item name="name" label="分组名称" rules={[{ required: true, whitespace: true, message: "请填写分组名称" }]}>
                        <Input maxLength={80} placeholder="例如 合作伙伴、内测用户" />
                    </Form.Item>
                    <Form.Item name="description" label="说明">
                        <Input.TextArea rows={2} maxLength={500} placeholder="仅管理员可见" />
                    </Form.Item>
                    <Form.Item
                        name="defaultMultiplier"
                        label="分组默认倍率"
                        extra="与全局策略倍率相乘。例如全局 1.2x、分组 0.8x，最终 0.96x。"
                        rules={[
                            { required: true, message: "请填写默认倍率" },
                            { type: "number", min: 0.0001, max: 100, message: "请输入 0.0001–100" },
                        ]}
                    >
                        <InputNumber className="w-full" min={0.0001} max={100} precision={4} />
                    </Form.Item>
                    <Form.Item name="enabled" label="启用状态" valuePropName="checked">
                        <Switch checkedChildren="启用" unCheckedChildren="停用" />
                    </Form.Item>
                    <Form.List name="modelMultipliers">
                        {(fields, { add, remove: removeRow }) => (
                            <>
                                <div className="mb-2 flex items-center justify-between">
                                    <div className="text-sm font-medium">分组内模型倍率</div>
                                    <Button type="text" size="small" icon={<Plus className="size-3.5" />} onClick={() => add({ model: "", multiplier: 1 })}>
                                        添加模型
                                    </Button>
                                </div>
                                {fields.length > 0 ? (
                                    <div className="admin-credit-multiplier-list">
                                        <div className="admin-credit-multiplier-header" aria-hidden="true">
                                            <span>模型标识</span>
                                            <span>倍率</span>
                                            <span>操作</span>
                                        </div>
                                        {fields.map((field, index) => (
                                            <div className="admin-credit-multiplier-row" key={field.key}>
                                                <Form.Item name={[field.name, "model"]} rules={[{ required: true, whitespace: true, message: "请填写模型标识" }]}>
                                                    <Input aria-label={`第 ${index + 1} 行模型标识`} placeholder="例如 gpt-image-1" />
                                                </Form.Item>
                                                <Form.Item
                                                    name={[field.name, "multiplier"]}
                                                    rules={[
                                                        { required: true, message: "请填写倍率" },
                                                        { type: "number", min: 0.0001, max: 100, message: "请输入 0.0001–100" },
                                                    ]}
                                                >
                                                    <InputNumber aria-label={`第 ${index + 1} 行倍率`} className="w-full" min={0.0001} max={100} precision={4} />
                                                </Form.Item>
                                                <Button type="text" danger className="admin-credit-multiplier-remove" icon={<Trash2 className="size-4" />} aria-label={`删除第 ${index + 1} 条模型倍率`} onClick={() => removeRow(field.name)} />
                                            </div>
                                        ))}
                                    </div>
                                ) : (
                                    <div className="admin-credit-multiplier-empty">暂无模型独立倍率，分组内所有模型使用分组默认倍率。</div>
                                )}
                            </>
                        )}
                    </Form.List>
                </Form>
            </Drawer>
        </section>
    );
}
