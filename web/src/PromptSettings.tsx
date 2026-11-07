import { useEffect, useRef, useState } from "react";
import { Alert, App, Button, Collapse, Empty, Input, Select, Skeleton, Tag } from "antd";
import { api } from "./api";
import "./prompt-settings.css";

export type PromptItem = {
  id: string;
  name: string;
  group: string;
  description: string;
  template: string;
  default_template: string;
  variables: { name: string; label: string; fixed: string }[];
};
export type PromptLibrary = { version: number; max_bytes: number; items: PromptItem[] };

export default function PromptSettings({ onDirty, onSaved }: { onDirty?: (dirty: boolean) => void; onSaved?: () => void }) {
  const { modal, message } = App.useApp();
  const [library, setLibrary] = useState<PromptLibrary>();
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [selected, setSelected] = useState("analyze");
  const [search, setSearch] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const changed = library?.items.filter(item => drafts[item.id] !== item.template) ?? [];
  const dirty = changed.length > 0;
  useEffect(() => { onDirty?.(dirty); return () => onDirty?.(false); }, [dirty, onDirty]);
  useEffect(() => {
    if (!dirty) return;
    const leave = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
    window.addEventListener("beforeunload", leave);
    return () => window.removeEventListener("beforeunload", leave);
  }, [dirty]);
  useEffect(() => {
    let live = true;
    setLoading(true);
    setError("");
    api<PromptLibrary>("/prompts").then(value => {
      if (!Array.isArray(value.items) || value.items.length !== 10) throw new Error("提示词列表不完整，请检查后台版本后重新加载。");
      if (!live) return;
      setLibrary(value);
      setDrafts(Object.fromEntries(value.items.map(item => [item.id, item.template])));
    }).catch(e => { if (live) setError(e instanceof Error ? e.message : "读取提示词失败"); })
      .finally(() => { if (live) setLoading(false); });
    return () => { live = false; };
  }, [reload]);

  const current = library?.items.find(item => item.id === selected) ?? library?.items[0];
  const text = current ? drafts[current.id] ?? "" : "";
  const byteLength = new TextEncoder().encode(text).length;
  function reloadLibrary() {
    if (!dirty) { setReload(n => n + 1); return; }
    modal.confirm({ title: "重新加载提示词？", content: "这会放弃所有未保存修改，读取本机已保存的版本。", okText: "放弃修改并加载", cancelText: "继续编辑", onOk: () => setReload(n => n + 1) });
  }
  function restore() {
    if (!current) return;
    const item = current;
    modal.confirm({ title: `恢复「${item.name}」默认值？`, content: "只替换当前编辑内容，点击保存后才会生效，其他模板保持不变。", okText: "恢复默认值", cancelText: "取消", onOk: () => setDrafts(old => ({ ...old, [item.id]: item.default_template })) });
  }
  async function save() {
    if (!library || !dirty || saving) return;
    setSaving(true);
    setError("");
    try {
      const next = await api<PromptLibrary>("/prompts", { version: library.version, templates: drafts }, "PUT");
      if (!alive.current) return;
      setLibrary(next);
      setDrafts(Object.fromEntries(next.items.map(item => [item.id, item.template])));
      void message.success("提示词已保存，新任务使用新版本；旧任务保持原快照。");
      onDirty?.(false);
      onSaved?.();
    } catch (e) { if (alive.current) setError(e instanceof Error ? e.message : "保存失败，修改仍保留，请核对后重试"); }
    finally { if (alive.current) setSaving(false); }
  }

  return <div className="prompt-settings">
    <div className="page-heading">
      <div><h1>提示词管理</h1><p>统一查看和编辑制作流程中的 10 类提示词。保存后需要重新报价，已确认任务保留原版本。</p></div>
      <div className="prompt-actions">
        {dirty && <Tag color="gold">{changed.length} 项未保存</Tag>}
        <Button onClick={reloadLibrary} disabled={saving || loading}>重新加载</Button>
        <Button type="primary" onClick={() => void save()} loading={saving} disabled={loading || !dirty}>保存全部修改</Button>
      </div>
    </div>
    {error && <Alert type="error" showIcon title={error} description="保存失败不会清空编辑内容；配置版本变化时请先复制需要保留的内容，再重新加载。" />}
    {loading ? <Skeleton active paragraph={{ rows: 10 }} /> : !library ? <Empty description="提示词尚未加载"><Button onClick={reloadLibrary}>重试</Button></Empty> : <div className="prompt-layout">
      <div className="prompt-mobile-picker"><Select aria-label="选择提示词类型" showSearch={{ optionFilterProp: "label" }} value={current?.id} disabled={saving} onChange={setSelected} options={["文字", "图片", "视频"].map(group => ({ label: group, options: library.items.filter(item => item.group === group).map(item => ({ value: item.id, label: item.name+(drafts[item.id] !== item.template ? " · 未保存" : "") })) }))} /></div>
      <aside className="panel prompt-list" aria-label="提示词类型">
        <Input.Search aria-label="搜索提示词" placeholder="搜索名称或用途" value={search} onChange={event => setSearch(event.target.value)} allowClear />
        {["文字", "图片", "视频"].map(group => {
          const items = library.items.filter(item => item.group === group && `${item.name} ${item.description}`.includes(search.trim()));
          return items.length > 0 && <div className="prompt-group" key={group}><h2>{group}</h2>{items.map(item => <Button key={item.id} aria-label={item.name} aria-pressed={item.id === current?.id} type={item.id === current?.id ? "primary" : "text"} block disabled={saving} onClick={() => setSelected(item.id)}>
            <span>{item.name}</span>{drafts[item.id] !== item.template ? <span className="prompt-dot" aria-label="未保存" /> : item.template !== item.default_template ? <span className="prompt-custom">自定义</span> : null}
          </Button>)}</div>;
        })}
        {library.items.every(item => !`${item.name} ${item.description}`.includes(search.trim())) && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有匹配的提示词" />}
      </aside>
      {current && <section className="panel prompt-editor" aria-label="提示词编辑区">
        <div className="prompt-editor-heading"><div><h2>{current.name}</h2><p>{current.description}</p></div><Button onClick={restore} disabled={saving || text === current.default_template}>恢复当前默认值</Button></div>
        <p className="prompt-note">保留所有 <code>{"{{变量名}}"}</code>，生成时自动填入作品内容。修改措辞不会直接调用模型。</p>
        <Input.TextArea aria-label="提示词内容" value={text} onChange={event => setDrafts(old => ({ ...old, [current.id]: event.target.value }))} disabled={saving} autoSize={{ minRows: 18, maxRows: 32 }} spellCheck={false} />
        <div className="prompt-editor-meta"><span>{text.length} 字符 · {byteLength.toLocaleString()} / {library.max_bytes.toLocaleString()} 字节</span><Tag color={text !== current.default_template ? "blue" : undefined}>{text !== current.default_template ? "自定义内容" : "默认内容"}</Tag></div>
        <Collapse items={[
          { key: "variables", label: "变量说明与流程约束", children: <><p>这些变量都必须保留。系统同时检查输出格式、生成数量、时长及人工确认要求。</p><dl className="prompt-variables">{current.variables.map(variable => <div key={variable.name}><dt><code>{`{{${variable.name}}}`}</code></dt><dd>{variable.label}{variable.fixed && <pre>{variable.fixed}</pre>}</dd></div>)}</dl></> },
          { key: "default", label: "查看默认版本", children: <Input.TextArea aria-label="默认提示词" value={current.default_template} readOnly autoSize={{ minRows: 5, maxRows: 20 }} /> },
        ]} />
      </section>}
    </div>}
  </div>;
}
