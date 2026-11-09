import { useEffect, useState, useRef } from "react";
import {
  Alert,
  Input,
  InputNumber,
  Popconfirm,
  Select,
  Segmented,
  Tag,
} from "antd";
import {
  CheckCircle2,
  CircleAlert,
  Cloud,
  Eye,
  EyeOff,
  Plus,
  Settings2,
  Terminal,
} from "lucide-react";
import {
  api,
  type Endpoint,
  type Settings,
  type SettingsUpdate,
  type Vendor,
  type ModelCatalog,
} from "./api";
import { Badge, Button, Field, Modal } from "./ui";
import { ModelPriceDetails, modelPriceSummary } from "./ModelPricing";

type Capability = "text" | "image" | "video" | "voice" | "music";
type ToolSystem = {
  tools: Record<string, boolean>;
  installer?: {
    available: boolean;
    manager?: string;
    help_url?: string;
    command?: string;
    status: "idle" | "running" | "succeeded" | "failed";
    message: string;
  };
};
const capabilities: { key: Capability; name: string; description: string }[] = [
  { key: "text", name: "文本导演", description: "故事分析、剧本与分镜规划" },
  {
    key: "image",
    name: "图片制作",
    description: "角色、设定图、场景道具与分镜图",
  },
  { key: "video", name: "视频生成", description: "按已确认分镜生成镜头视频" },
  { key: "voice", name: "配音", description: "逐段台词配音与WAV音轨" },
  { key: "music", name: "音乐生成", description: "整集时间线纯音乐，预览报价后执行" },
];
export function vendorSupports(v: Vendor, key: Capability, supported?: Record<string, string[]>) {
  return supported?.[v.id]?.includes(key) ?? (v.type === "cli" && (key === "text" || (v.cli === "codex" && key === "image")));
}
const presets = [
 { value: "codex", label: "Codex CLI", type: "cli", cli: "codex" },
 { value: "claude", label: "Claude Code", type: "cli", cli: "claude" },
 { value: "ark", label: "火山方舟（豆包）", type: "api", protocol: "ark", base: "https://ark.cn-beijing.volces.com/api/v3" },
 { value: "bailian", label: "阿里百炼（千问）", type: "api", protocol: "openai", base: "https://dashscope.aliyuncs.com/compatible-mode/v1" },
 { value: "minimax", label: "MiniMax（音乐）", type: "api", protocol: "minimax", base: "https://api.minimax.io/v1" },
 { value: "custom", label: "自定义兼容 API", type: "api", protocol: "openai", base: "https://api.openai.com/v1" },
] as const;
const emptyVendor = (): Vendor => ({
  id: crypto.randomUUID(),
  name: "",
  kind: "custom",
  type: "api",
  protocol: "openai",
  base: "https://api.openai.com/v1",
  key_mode: "local",
});
const protocolName = (v: Vendor) => presets.find(p => p.value === (v.kind || (v.type === "cli" ? v.cli : v.protocol === "ark" ? "ark" : "custom")))?.label || "自定义兼容 API";
export default function ServiceSettings({
  settings,
  credentials,
  busy,
  save,
  page = "services",
  supported,
  onDirty,
  openVendors,
}: {
  settings: Settings;
  credentials: Record<string, boolean>;
  busy: boolean;
  save: (s: SettingsUpdate) => void;
  page?: "vendors" | "services";
  supported?: Record<string, string[]>;
  onDirty?: (dirty: boolean) => void;
  openVendors?: () => void;
}) {
  const [catalogs, setCatalogs] = useState<Record<string, ModelCatalog>>({});
  const [catalogErrors, setCatalogErrors] = useState<Record<string, string>>({});
  const [catalogLoading, setCatalogLoading] = useState<Record<string, boolean>>({});
  const [viewModels, setViewModels] = useState<string>();
  const [manual, setManual] = useState<Record<string, boolean>>({});
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  async function loadModels(id: string, refresh = false) {
    setCatalogLoading(p => ({...p, [id]: true}));
    setCatalogErrors(p => ({...p, [id]: ""}));
    try {
      const cat = await api<ModelCatalog>(`/vendors/${encodeURIComponent(id)}/models${refresh ? "?refresh=1" : ""}`);
      if (!Array.isArray(cat.models)) throw new Error("模型列表响应格式无效，请重试");
      if (alive.current) setCatalogs(p => ({...p, [id]: cat}));
    } catch (e) { if (alive.current) setCatalogErrors(p => ({...p, [id]: e instanceof Error ? e.message : "获取模型失败"})); }
    finally { if (alive.current) setCatalogLoading(p => ({...p, [id]: false})); }
  }
  useEffect(() => {
    if (page === "services") {
      const ids = new Set(capabilities.map(c => settings[c.key]?.vendor_id).filter(Boolean));
      for (const id of ids) if (id) void loadModels(id);
    }
  }, []);
  const [v, setV] = useState(settings);
  const [draft, setDraft] = useState<Vendor>();
  const [draftError, setDraftError] = useState("");
  const [apiKeys, setAPIKeys] = useState<Record<string, string>>({});
  const [draftKey, setDraftKey] = useState("");
  const [showKey, setShowKey] = useState(false);
  const [toolSystem, setToolSystem] = useState<ToolSystem>();
  const [toolError, setToolError] = useState("");
  const [checkingTools, setCheckingTools] = useState(true);
  const [startingInstall, setStartingInstall] = useState(false);
  const tools = toolSystem?.tools ?? {};
  const missingTools = !!toolSystem && (!tools.ffmpeg || !tools.ffprobe);
  const installingTools = startingInstall || toolSystem?.installer?.status === "running";
  async function refreshTools() {
    setCheckingTools(true);
    setToolError("");
    try { setToolSystem(await api<ToolSystem>("/system")); }
    catch (error) { setToolError(error instanceof Error ? error.message : "工具检测失败，请重新检测"); }
    finally { setCheckingTools(false); }
  }
  async function installTools() {
    setStartingInstall(true);
    setToolError("");
    try { setToolSystem(await api<ToolSystem>("/system/tools/install", {}, "POST")); }
    catch (error) { setToolError(error instanceof Error ? error.message : "安装未能启动，请重试"); }
    finally { setStartingInstall(false); }
  }
  useEffect(() => {
    if (page === "services") void refreshTools();
  }, []);
  useEffect(() => {
    if (toolSystem?.installer?.status !== "running") return;
    const timer = setInterval(() => {
      void api<ToolSystem>("/system").then(value => { setToolSystem(value); setToolError(""); }).catch(() => {
        setToolError("暂时无法查询安装状态，请重新检测；不会自动重复安装");
      });
    }, 2000);
    return () => clearInterval(timer);
  }, [toolSystem?.installer?.status]);
  const vendors = v.vendors ?? [];
  const dirty =
    JSON.stringify(v) !== JSON.stringify(settings) ||
    Object.keys(apiKeys).length > 0;
  const unsaved = dirty || !!draft;
  useEffect(() => { onDirty?.(unsaved); return () => onDirty?.(false); }, [unsaved, onDirty]);
  useEffect(() => {
    if (!unsaved) return;
    const guard = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [unsaved]);
  function saveConfiguration() {
    const keys = Object.fromEntries(
      Object.entries(apiKeys).filter(([id]) =>
        vendors.some(p => p.id === id && p.type === "api" && p.key_mode === "local"),
      ),
    );
    save({ ...v, image_concurrency: v.image_concurrency ?? 2, video_concurrency: v.video_concurrency ?? 2, api_keys: keys, section: page });
  }
  function closeVendor() {
    setDraft(undefined);
    setDraftKey(""); setShowKey(false);
  }
  function updateBinding(key: Capability, patch: Partial<Endpoint>) {
    setV({ ...v, [key]: { ...v[key], ...patch } });
  }
  function applyVendor() {
    if (!draft) return;
    const item = { ...draft, name: draft.name.trim() };
    if (!item.name) {
      setDraftError("请填写厂商名称");
      return;
    }
    if (item.type === "api" && (!item.base?.trim() || (item.key_mode !== "local" && !item.key_env?.trim()))) {
      setDraftError("请填写API地址；使用环境变量时还需填写变量名");
      return;
    }
    if (item.type === "api" && item.key_mode === "local" && !draftKey && !apiKeys[item.id] && !item.credential_id) {
      setDraftError("请填写 API Key，保存后会在本机加密存储");
      return;
    }
    if (draftKey && (draftKey.trim() !== draftKey || /[\r\n\x00]/.test(draftKey))) {
      setDraftError("API Key 不能包含首尾空格或换行");
      return;
    }
    const incompatible = capabilities.filter(
      (c) => v[c.key]?.vendor_id === item.id && item.type === "cli" && !vendorSupports(item, c.key),
    );
    if (incompatible.length) {
      setDraftError(
        `此厂商仍绑定${incompatible.map((c) => c.name).join("、")}，请先调整能力绑定再更改接入类型。`,
      );
      return;
    }
    setAPIKeys(previous => {
      const next = { ...previous };
      if (item.type !== "api" || item.key_mode !== "local") delete next[item.id];
      else if (draftKey) next[item.id] = draftKey;
      return next;
    });
    setDraftKey(""); setShowKey(false);
    setV({
      ...v,
      vendors: vendors.some((p) => p.id === item.id)
        ? vendors.map((p) => (p.id === item.id ? item : p))
        : [...vendors, item],
    });
    setDraft(undefined);
    setDraftError("");
  }
  const vendorPanel = (
    <>
      <div className="vendor-section-heading">
        <div>
          <h2>厂商管理</h2>
          <p>连接本机 CLI 或远程厂商，再到“服务配置”选择模型。</p>
        </div>
        <Button
          kind="primary"
          disabled={busy}
          onClick={() => {
            setDraft(emptyVendor());
            setDraftKey(""); setShowKey(false);
            setDraftError("");
          }}
        >
          <Plus size={16} />
          新增厂商
        </Button>
      </div>
      <div className="vendor-cards">
        {vendors.length === 0 && <p>还没有厂商，点击“新增厂商”连接本地 CLI 或远程 API。</p>}
        {vendors.map(p => {
          const changed = JSON.stringify(p) !== JSON.stringify(settings.vendors.find(old => old.id === p.id)) || !!apiKeys[p.id];
          const used = capabilities.filter(c => v[c.key]?.vendor_id === p.id);
          const cat = catalogs[p.id];
          return <article className="vendor-card" key={p.id}>
            <div className="vendor-card-heading"><div className="vendor-name">
              {p.type === "cli" ? <Terminal size={20} /> : <Cloud size={20} />}
              <div><strong>{p.name}</strong><small>{protocolName(p)} · {p.type === "cli" ? "本地 CLI" : "远程 API"}</small></div>
            </div><Badge green={!changed && !!credentials[p.id]}>{changed ? "待保存" : credentials[p.id] ? p.type === "cli" ? "已发现 CLI" : "凭据已配置" : "未就绪"}</Badge></div>
            <p className="vendor-address">{p.type === "cli" ? p.executable || `自动查找 ${p.cli}` : p.base}</p>
            <p>{p.type === "api" ? p.key_mode === "local" ? "凭据：本机加密保存" : `凭据环境变量：${p.key_env || "未设置"}` : "复用本机 CLI 登录"}</p>
            <p>使用中的服务：{used.map(c => c.name).join("、") || "尚未绑定"}</p>
            <p role="status">{catalogLoading[p.id] ? "正在获取模型…" : cat ? `${cat.models.length} 个模型 · ${new Date(cat.updated).toLocaleString()}${cat.stale ? " · 缓存已过期" : ""}` : "尚未获取模型目录"}</p>
            {(catalogErrors[p.id] || cat?.error) && <Alert type="warning" showIcon title={catalogErrors[p.id] || cat?.error} />}
            <div className="vendor-actions">
              <Button disabled={busy} onClick={() => { setDraft({...p, key_mode: p.key_mode || "env"}); setDraftKey(""); setShowKey(false); setDraftError(""); }}>编辑</Button>
              <>
                <Button disabled={busy || changed || catalogLoading[p.id]} onClick={() => void loadModels(p.id, true)}>{cat ? "刷新模型" : "获取模型"}</Button>
                <Button disabled={!cat} onClick={() => setViewModels(p.id)}>查看模型</Button>
              </>
              <Popconfirm disabled={busy || used.length > 0} title="移除此厂商？" description="历史任务会保留原配置快照。" onConfirm={() => setV({...v, vendors: vendors.filter(x => x.id !== p.id)})}><span><Button disabled={busy || used.length > 0}>移除</Button></span></Popconfirm>
            </div>
            {changed && <small>先保存厂商，再获取模型目录。</small>}
          </article>;
        })}
      </div>
      <p className="vendor-help">
        正在被制作能力使用的厂商不能移除。找到CLI或读取到凭据仅表示本机配置存在，不代表登录、模型权限或生成能力已经验证。
      </p>
    </>
  );
  const bindingsPanel = (
    <>
      <div className="vendor-section-heading">
        <div>
          <h2>制作能力</h2>
          <p>
            按制作环节绑定厂商和模型，同一厂商可用于多个环节。
          </p>
        </div>
      </div>
      <div className="settings-grid">
        {capabilities.map((cap) => {
          const binding = v[cap.key] || {model:""};
          const vendor = vendors.find((p) => p.id === binding.vendor_id);
          return (
            <section className="panel capability-card" key={cap.key}>
              <div className="panel-heading">
                <div>
                  <h2>{cap.name}</h2>
                  <p>{cap.description}</p>
                </div>
                {vendor && (
                  <Badge>
                    {vendor.type === "cli" ? "本地 CLI" : "远程 API"}
                  </Badge>
                )}
              </div>
              <Field label={`${cap.name}厂商`}>
                <Select
                  disabled={busy}
                  allowClear
                  placeholder="先新增厂商，再选择绑定"
                  value={cap.key === "voice" ? binding.vendor_id || "" : binding.vendor_id || undefined}
                  onChange={(id) =>
                    { updateBinding(cap.key, { vendor_id: id ?? "", model: "" }); setManual(p => ({...p, [cap.key]: false})); if (id) void loadModels(id); }
                  }
                  options={[...(cap.key === "voice" ? [{ value: "", label: "无（不使用统一旁白）" }] : []), ...vendors
                    .filter((p) => vendorSupports(p, cap.key, supported))
                    .map((p) => ({
                      value: p.id,
                      label: `${p.name} · ${p.type === "cli" ? "本地CLI" : "远程API"}`,
                    }))]}
                  virtual={false}
                />
              </Field>
              {(cap.key !== "voice" || vendor) && <Field label={`${cap.name}模型`}>
                <Select allowClear showSearch optionFilterProp="label" disabled={busy || !vendor || manual[cap.key]}
                  placeholder={vendor?.type === "cli" ? "沿用 CLI 默认模型" : "获取模型列表后选择"}
                  value={binding.model || undefined} loading={vendor ? catalogLoading[vendor.id] : false}
                  options={(vendor ? catalogs[vendor.id]?.models || [] : []).filter(m => m.services.includes(cap.key)).map(m => ({value: m.id, label: `${m.name} · ${m.id} · ${m.capabilities.join(" / ")}`, description: modelPriceSummary(m)}))}
                  optionRender={option => <div className="model-price-option"><div>{option.label}</div><small>{option.data.description}</small></div>}
                  onChange={model => updateBinding(cap.key, {model: model ?? ""})} virtual={false} />
              </Field>}
              {cap.key === "voice" && !vendor && <Alert type="info" showIcon title="未启用统一旁白，无需配置旁白模型和 API Key。" description="角色对白模式直接使用视频原声；若作品选择统一旁白模式，需要先启用此服务。" />}
              {vendor && <>
                <div className="vendor-actions">
                  <Button disabled={catalogLoading[vendor.id]} onClick={() => void loadModels(vendor.id, true)}>刷新模型</Button>
                  {vendor.type === "api" && <Button onClick={() => setManual(p => ({...p, [cap.key]: !p[cap.key]}))}>{manual[cap.key] ? "从列表选择" : "手动填写模型 ID"}</Button>}
                </div>
                {(vendor.type === "cli" || manual[cap.key]) && <Field label={`${cap.name}模型 ID`}><Input value={binding.model || ""} disabled={busy} onChange={e => updateBinding(cap.key, {model: e.target.value})} placeholder={vendor.type === "cli" ? "留空则沿用 CLI 默认模型" : "输入精确模型 ID（未验证）"} /></Field>}
                <>
                  {vendor.type === "cli" && catalogs[vendor.id] && <details className="service-catalog-note"><summary>模型目录说明</summary><p>来源：{catalogs[vendor.id].source}；清空模型则沿用 CLI 默认配置。列表不代表账户权限已验证。</p></details>}
                  {(catalogErrors[vendor.id] || catalogs[vendor.id]?.error) && <Alert type="warning" title={catalogErrors[vendor.id] || catalogs[vendor.id]?.error} />}
                  {catalogs[vendor.id]?.stale && <p>当前显示过期缓存，可重试刷新。</p>}
                  {!catalogLoading[vendor.id] && catalogs[vendor.id]?.models.filter(m => m.services.includes(cap.key)).length === 0 && <p>此目录暂无本服务可用模型，可手动填写模型 ID。</p>}
                  {binding.model && !catalogs[vendor.id]?.models.some(m => m.id === binding.model && m.services.includes(cap.key)) && <Tag color="gold">当前模型未验证</Tag>}
                </>
              </>}
              {vendor && <ModelPriceDetails vendor={vendor} model={catalogs[vendor.id]?.models.find(m => m.id === binding.model)} stale={catalogs[vendor.id]?.stale} />}
              {cap.key !== "voice" && vendors.filter(p => vendorSupports(p, cap.key, supported)).length === 0 && <Button onClick={openVendors}>前往添加厂商</Button>}

              {vendor?.type === "cli" && (
                <Alert
                  type="info"
                  showIcon
                  title={cap.key === "image" ? "通过 Codex 的 imagegen 技能调用内置生图工具，支持参考图。模型 ID 为 Codex 执行模型；费用未知，制作前需确认报价。" : "使用本机CLI账户执行文字任务；点击生成即调用一次，不走报价。"}
                />
              )}
            </section>
          );
        })}
      </div>
      <Alert
        type="info"
        showIcon
        title="厂商能力与当前调用适配共同决定可选模型；目录中待支持的模型可在厂商管理查看。"
      />
    </>
  );
  return (
    <div className={`service-settings-page service-settings-${page}`}>
      <div className="page-heading">
        <div>
          <h1>{page === "vendors" ? "厂商管理" : "服务配置"}</h1>
          <p>{page === "vendors" ? "管理本地 CLI 与远程 API 的连接和凭据。" : "为文字、图片、视频与声音选择制作服务。"}</p>
        </div>
        <div className="inline-actions">
          {dirty && <Tag color="gold">有未保存修改</Tag>}
          <Button
            kind="primary"
            disabled={busy || !dirty}
            onClick={saveConfiguration}
          >
            <Settings2 size={16} />
            保存配置
          </Button>
        </div>
      </div>
      {page === "services" && <section className="panel service-concurrency">
        <div className="service-section-intro"><h2>生成并发</h2>
        <p>每个作品同时生成的数量，按厂商限流和本机性能调整。</p></div>
        <div className="service-concurrency-fields">
          <Field label="图片并发数"><InputNumber aria-label="图片并发数" min={1} max={8} precision={0} disabled={busy} value={v.image_concurrency ?? 2} onChange={n => setV({...v, image_concurrency: n ?? 2})} /></Field>
          <Field label="视频并发数"><InputNumber aria-label="视频并发数" min={1} max={8} precision={0} disabled={busy} value={v.video_concurrency ?? 2} onChange={n => setV({...v, video_concurrency: n ?? 2})} /></Field>
        </div>
        <small>范围 1～8，默认 2。仅用于新确认的任务；已确认任务保留原配置。修改后需重新报价。</small>
      </section>}
      <section className="panel vendor-registry">
        {page === "vendors" ? vendorPanel : bindingsPanel}
      </section>
      <Alert
        className="vendor-notice"
        type="info"
        showIcon
        title="配置修改会使未确认报价失效；已确认任务保留原厂商快照。"
        description="API Key 可直接输入并在本机加密保存，编辑时留空保留原密钥。也支持环境变量。CLI复用本机账户；保存配置不会触发生成。"
      />
      {page === "services" && <UserDataPanel />}
      {page === "services" && <section className="panel tool-panel">
        <h2>本机制作工具</h2>
        <div>
          {["ffmpeg", "ffprobe"].map((name) => (
            <span key={name}>
              {tools[name] ? <CheckCircle2 size={15} /> : <CircleAlert size={15} />}
              {name}
              <small>{checkingTools && !toolSystem ? "检测中" : tools[name] ? "已安装" : "未发现"}</small>
            </span>
          ))}
        </div>
        <p>FFmpeg 和 ffprobe 一起安装，用于本地合成导出和成片检查。</p>
        <div>
          <Button disabled={checkingTools || startingInstall} onClick={() => void refreshTools()}>
            {checkingTools ? "检测中…" : "重新检测"}
          </Button>
          {(missingTools || installingTools) && (
            <Button kind="primary" disabled={installingTools || checkingTools || !toolSystem?.installer?.available} onClick={() => void installTools()}>
              {installingTools ? "正在安装…" : toolSystem?.installer?.status === "failed" ? "重试安装" : "安装缺少的工具"}
            </Button>
          )}
        </div>
        {missingTools && !toolSystem?.installer?.available && (
          <p>未找到 {toolSystem?.installer?.manager || "系统安装工具"}。请先<a href={toolSystem?.installer?.help_url || "https://ffmpeg.org/download.html"} target="_blank" rel="noreferrer">安装 {toolSystem?.installer?.manager || "FFmpeg"}</a>，再重新检测；也可以自行安装 FFmpeg 并加入 PATH。</p>
        )}
        {toolSystem?.installer?.message && <p role="status">{toolSystem.installer.message}</p>}
        {toolError && <p role="alert">{toolError}</p>}
      </section>}
      {viewModels && <Modal title="厂商模型目录" onClose={() => setViewModels(undefined)}><p>来源：{catalogs[viewModels]?.source}</p>{catalogs[viewModels]?.models.map(m => <article className="catalog-model" key={m.id}><strong>{m.name}</strong><code>{m.id}</code><div>{m.capabilities.join(" / ") || "能力未知"} · {m.services.length ? m.services.map(key => capabilities.find(c => c.key === key)?.name).join("、") : "待支持"}</div></article>)}</Modal>}
      {draft && (
        <Modal
          title={
            vendors.some((p) => p.id === draft.id) ? "编辑厂商" : "新增厂商"
          }
          onClose={closeVendor}
          footer={
            <div className="dialog-actions">
              <Button onClick={closeVendor}>取消</Button>
              <Button
                kind="primary"
                disabled={!draft.name.trim()}
                onClick={applyVendor}
              >
                保存到配置草稿
              </Button>
            </div>
          }
        >
          <p className="modal-intro">
            先选择厂商类型，再配置连接。保存到草稿后，点击页面“保存配置”生效。
          </p>
          {draftError && (
            <Alert
              type="error"
              showIcon
              title={draftError}
              style={{ marginBottom: 16 }}
            />
          )}
          <Field label="厂商类型">
            <Select value={draft.kind || (draft.type === "cli" ? draft.cli : draft.protocol === "ark" ? "ark" : "custom")} options={[...presets]} virtual={false} onChange={kind => {
              const preset = presets.find(p => p.value === kind)!;
              setDraft({...draft, kind, name: draft.name || preset.label, type: preset.type, cli: "cli" in preset ? preset.cli : undefined, protocol: "protocol" in preset ? preset.protocol : undefined, base: "base" in preset ? preset.base : undefined, region: kind === "bailian" || kind === "ark" ? "cn-beijing" : "", workspace: "", credential_id: undefined, key_mode: "local", key_env: ""});
              setDraftKey(""); setDraftError("");
            }} />
          </Field>
          <Field label="厂商名称">
            <Input
              autoFocus
              maxLength={60}
              value={draft.name}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              placeholder="例如：我的Codex、OpenAI、火山方舟"
            />
          </Field>
          {draft.type === "cli" ? (
            <>
              <Field label="可执行文件绝对路径（可选）">
                <Input
                  value={draft.executable || ""}
                  onChange={(e) =>
                    setDraft({ ...draft, executable: e.target.value })
                  }
                  placeholder="留空则从后端PATH查找，不填写命令参数"
                />
              </Field>
              <Alert
                type="info"
                showIcon
                title={draft.cli === "claude" ? "支持文本规划" : "支持文本规划与生图"}
                description="复用本机登录与CLI配置。Codex 生图需要可用的 imagegen 技能与内置生图工具，支持参考图；不会自动切换 API。保存不触发生成，文字点击生成即调用一次，媒体制作需确认报价。"
              />
            </>
          ) : (
            <>
              {draft.kind === "ark" && <Field label="区域"><Input value={draft.region || "cn-beijing"} onChange={e => setDraft({...draft, region: e.target.value})} placeholder="与下方 API 根地址保持一致" /></Field>}
              {draft.kind === "bailian" && <>
                <Field label="区域"><Select value={draft.region || "cn-beijing"} virtual={false} options={[{value:"cn-beijing",label:"中国北京"},{value:"ap-southeast-1",label:"新加坡"},{value:"us-east-1",label:"美国弗吉尼亚"},{value:"cn-hongkong",label:"中国香港"},{value:"eu-central-1",label:"德国法兰克福"},{value:"ap-northeast-1",label:"日本东京"}]} onChange={region => setDraft({...draft, region, base: draft.workspace ? `https://${draft.workspace}.${region}.maas.aliyuncs.com/compatible-mode/v1` : draft.base})} /></Field>
                <Field label="工作空间 ID"><Input value={draft.workspace || ""} onChange={e => setDraft({...draft, workspace: e.target.value, base: e.target.value ? `https://${e.target.value}.${draft.region || "cn-beijing"}.maas.aliyuncs.com/compatible-mode/v1` : draft.base})} placeholder="用于模型目录查询" /></Field>
              </>}
              <Field label="API根地址">
                <Input
                  value={draft.base || ""}
                  onChange={(e) => setDraft({ ...draft, base: e.target.value })}
                  placeholder="https://…/v1"
                />
              </Field>
              <Field label="密钥保存方式">
                <Segmented
                  block
                  value={draft.key_mode || "env"}
                  options={[{ label: "本机加密保存", value: "local" }, { label: "环境变量（高级）", value: "env" }]}
                  onChange={(key_mode) => { setDraft({...draft, key_mode: key_mode as "local" | "env"}); setDraftKey(""); setShowKey(false); }}
                />
              </Field>
              {draft.key_mode === "local" ? (<>
                <Field label="API Key">
                  <Input
                    type="text"
                    className={showKey ? "" : "api-key-masked"}
                    value={draftKey}
                    onChange={e => setDraftKey(e.target.value)}
                    autoComplete="off"
                    autoCorrect="off"
                    autoCapitalize="none"
                    spellCheck={false}
                    data-1p-ignore="true"
                    data-lpignore="true"
                    data-bwignore="true"
                    suffix={
                      <button
                        type="button"
                        className="icon-button"
                        aria-label={showKey ? "隐藏 API Key" : "显示 API Key"}
                        aria-pressed={showKey}
                        onClick={() => setShowKey(!showKey)}
                      >
                        {showKey ? <EyeOff size={16} /> : <Eye size={16} />}
                      </button>
                    }
                    maxLength={8192}
                    placeholder={apiKeys[draft.id] ? "新密钥待保存，再次输入可替换" : draft.credential_id ? "已保存，留空保留原密钥；输入新值可替换" : "粘贴厂商提供的 API Key"}
                  />
                </Field>
                <Alert type="info" showIcon title="密钥仅在本机加密保存，不会回显" description="留空保留原密钥。替换后新任务使用新密钥，已确认任务保留原密钥版本。" />
              </>) : (<Field label="凭据环境变量名">
                <Input
                  value={draft.key_env || ""}
                  onChange={(e) =>
                    setDraft({ ...draft, key_env: e.target.value })
                  }
                  autoComplete="off"
                  placeholder="例如 OPENAI_API_KEY，不填写密钥原文"
                />
              </Field>)}
            </>
          )}
        </Modal>
      )}
    </div>
  );
}


function UserDataPanel() {
  const [data, setData] = useState<{directory:string;backup_directory:string}>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [backup, setBackup] = useState("");
  useEffect(() => { let alive = true; api<{directory:string;backup_directory:string}>("/system/data").then(v => { if (alive) setData(v); }).catch(e => { if (alive) setError(String(e.message)); }); return () => { alive = false; }; }, []);
  async function action(kind: "open" | "backup") {
    setBusy(true); setError("");
    try {
      const result = await api<{directory?:string}>(`/system/data/${kind}`, {}, "POST", 30 * 60 * 1000);
      if (kind === "backup") setBackup(result.directory || "");
    } catch (e) { setError(e instanceof Error ? e.message : "操作失败"); }
    finally { setBusy(false); }
  }
  return <section className="panel">
    <h2>个人数据与更新</h2>
    <p>发行版将作品、素材、任务记录、服务配置和加密密钥独立保存。更新时关闭程序，解压新版并启动即可继续使用原数据。</p>
    {data && <><p>数据目录：<code>{data.directory}</code></p><p>备份目录：<code>{data.backup_directory}</code></p></>}
    <div className="inline-actions"><Button disabled={busy || !data} onClick={() => void action("open")}>打开数据目录</Button><Button disabled={busy || !data} onClick={() => void action("backup")}>{busy ? "处理中…" : "备份全部数据"}</Button></div>
    <p>备份前请等待制作任务结束；备份期间暂停编辑，大量素材可能需要较长时间。备份保存在本机，包含加密密钥，请妥善保管。</p>
    <p>恢复备份：先关闭程序，将当前数据目录改名保留，再把完整备份复制为原数据目录；勿将不同备份混合。请使用创建该数据的版本或兼容新版程序。</p>
    {backup && <Alert type="success" showIcon title="完整备份已保存" description={backup} />}
    {error && <Alert type="error" showIcon title={error} />}
  </section>;
}
