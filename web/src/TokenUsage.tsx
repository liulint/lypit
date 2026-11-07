import { useEffect, useState } from "react";
import { Alert, Button, Input, Table, Tag } from "antd";
import { api, stageNames, statusNames, type Project, type Task, type UsageRecord } from "./api";

const tokens = (n?: number) => n == null ? "未返回 / 未记录" : n.toLocaleString("zh-CN");

export function TokenUsage({ projects, tasks }: { projects: Project[]; tasks: Task[] }) {
  const [records, setRecords] = useState<UsageRecord[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [revision, setRevision] = useState(0);
  const changes = tasks.map(t => `${t.id}:${t.updated}:${t.status}:${t.video_usage?.length}`).join("|");
  useEffect(() => {
    let active = true;
    setLoading(true);
    api<UsageRecord[]>("/usage").then(data => {
      if (active) { setRecords(data); setError(""); }
    }).catch(e => { if (active) setError(e instanceof Error ? e.message : "读取用量失败"); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [changes, revision]);
  const title = (r: UsageRecord) => projects.find(p => p.id === r.project_id)?.title || "历史作品";
  const query = search.trim().toLowerCase();
  const filtered = records.filter(r => [title(r), r.vendor, r.model, r.title, r.task_id].join(" ").toLowerCase().includes(query));
  return <>
    <div className="page-heading"><div><h1>Token 用量</h1><p>按每次模型调用记录厂商、模型与用量，保留原任务记录。</p></div>
      <Button onClick={() => setRevision(n => n + 1)} loading={loading}>刷新用量</Button></div>
    <Alert type="info" showIcon title="未返回的用量和未知费用不会按 0 计算。视频 Token 单独列出；参考折算费用以厂商账单为准。CLI Token 可能不含生图等工具费用，本地模拟不产生模型调用记录。" />
    {error && <Alert type="error" showIcon title={error} />}
    <div style={{ margin: "20px 0" }}><Input.Search aria-label="搜索用量" placeholder="搜索作品、厂商或模型" value={search} onChange={e => setSearch(e.target.value)} style={{ maxWidth: 360 }} /></div>
    <Table<UsageRecord> rowKey="id" loading={loading} dataSource={filtered} scroll={{ x: 1500 }}
      pagination={{ pageSize: 20, showSizeChanger: false, showTotal: n => `共 ${n} 次调用` }}
      locale={{ emptyText: "暂无模型调用记录" }}
      expandable={{ expandedRowRender: r => <div><p>任务：{r.task_id} · {statusNames[r.status] || r.status}</p>
        <p>{r.cost?.basis || r.reason}</p>{r.cost && <p><a href={r.cost.url} target="_blank" rel="noreferrer">{r.cost.source}</a> · 核对日期 {r.cost.checked}</p>}</div> }}
      columns={[
        { title: "时间", dataIndex: "recorded", width: 180, render: v => v ? new Date(v).toLocaleString("zh-CN") : "未记录" },
        { title: "作品 / 阶段", width: 210, render: (_, r) => <><strong>{title(r)}</strong><div>{stageNames[r.stage] || r.stage}{r.title && ` · ${r.title}`}</div></> },
        { title: "厂商", dataIndex: "vendor", width: 160 },
        { title: "模型", dataIndex: "model", width: 220, render: (v, r) => <>{v || "默认模型（未返回实际名称）"}<div><Tag>{r.transport === "cli" ? "本地 CLI" : "远程 API"}</Tag></div></> },
        { title: "输入 Token", dataIndex: "input_tokens", render: tokens },
        { title: "缓存输入 Token", dataIndex: "cached_input_tokens", render: tokens },
        { title: "输出 Token", dataIndex: "output_tokens", render: tokens },
        { title: "视频生成 Token", dataIndex: "video_tokens", render: tokens },
        { title: "总 Token", dataIndex: "total_tokens", render: tokens },
        { title: "参考费用", width: 160, render: (_, r) => r.cost ? `${r.cost.currency} ${r.cost.amount}（估算）` : "未知" },
      ]} />
  </>;
}
