import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  ChevronRight,
  Cpu,
  Eye,
  EyeOff,
  FlaskConical,
  Globe,
  Image,
  KeyRound,
  LoaderCircle,
  Plus,
  RefreshCw,
  Save,
  Settings2,
  ShieldCheck,
  SlidersHorizontal,
  Terminal,
  X,
} from "lucide-react";
import { api, media } from "./api";
import type {
  CLICatalog,
  CLIStatus,
  ImageProbe,
  Model,
  Provider,
  Settings,
} from "./api";
import ModelEditor from "./ModelEditor";
import CLIModelSelect from "./CLIModelSelect";
import {
  bindingOptions,
  cliNames,
  codexImageModel,
  imageVerified,
  newModel,
  protocols,
  roles,
} from "./settings-utils";
import "./model-settings.css";
type SettingsResponse = {
  settings: Settings;
  credentials_present: Record<string, boolean>;
  credential_errors: Record<string, string>;
};
export default function ServiceSettings({
  onError,
  onClose,
}: {
  onError: (e: string) => void;
  onClose: () => void;
}) {
  const cache = useQueryClient();
  const query = useQuery({
    queryKey: ["settings"],
    queryFn: () => api<SettingsResponse>("/settings"),
  });
  const [form, setForm] = useState<Settings | null>(null);
  const [saved, setSaved] = useState<Settings | null>(null);
  const [section, setSection] = useState("connections");
  const [mode, setMode] = useState("cli");
  const [selected, setSelected] = useState("codex");
  const [protocol, setProtocol] = useState("openai");
  const [keys, setKeys] = useState<Record<string, string>>({});
  const [visible, setVisible] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [closing, setClosing] = useState(false);
  const [statuses, setStatuses] = useState<CLIStatus[]>([]);
  const [models, setModels] = useState<Record<string, string[]>>({});
  const [catalogs, setCatalogs] = useState<Record<string, CLICatalog>>({});
  const [catalogLoading, setCatalogLoading] = useState(false);
  const catalogRequest = useRef(0);
  const catalogIdentityRef = useRef("");
  const [probe, setProbe] = useState<ImageProbe | null>(null);
  const [history, setHistory] = useState<ImageProbe[]>([]);
  const [accepted, setAccepted] = useState(false);
  const dialog = useRef<HTMLDialogElement>(null);
  const dirty =
    !!form &&
    !!saved &&
    (JSON.stringify(form) !== JSON.stringify(saved) ||
      Object.entries(keys).some(
        ([id, key]) =>
          !!key &&
          form.providers.some(
            (p) => p.id === id && p.credential_source === "file",
          ),
      ));
  useEffect(() => {
    if (query.data && !form) {
      setForm(structuredClone(query.data.settings));
      setSaved(structuredClone(query.data.settings));
    }
  }, [query.data, form]);
  useEffect(() => {
    const d = dialog.current;
    const previous = document.activeElement as HTMLElement | null;
    d?.showModal();
    return () => {
      d?.close();
      previous?.focus();
    };
  }, []);
  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = "";
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);
  useEffect(() => {
    let active = true;
    api<CLIStatus[]>("/cli/status")
      .then((x) => {
        if (active) setStatuses(x);
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, []);
  useEffect(() => {
    if (!probe || probe.state !== "running") return;
    const timer = window.setInterval(() => {
      api<ImageProbe>("/image-verifications/" + probe.id)
        .then(async (result) => {
          setProbe(result);
          if (result.state === "succeeded") {
            const fresh = await api<SettingsResponse>("/settings");
            setForm(fresh.settings);
            setSaved(fresh.settings);
            await cache.invalidateQueries({ queryKey: ["settings"] });
          }
        })
        .catch((e) => setError(e.message));
    }, 1500);
    return () => clearInterval(timer);
  }, [probe?.id, probe?.state, cache]);
  useEffect(() => {
    if (!closing && !probe) return;
    const frame = requestAnimationFrame(() => {
      dialog.current
        ?.querySelector<HTMLElement>(".ms-overlay input, .ms-overlay button")
        ?.focus();
    });
    return () => cancelAnimationFrame(frame);
  }, [closing, probe?.id, probe?.state]);
  function close() {
    if (busy || probe?.state === "running") return;
    if (dirty) setClosing(true);
    else onClose();
  }
  function change(fn: (s: Settings) => void) {
    if (!form) return;
    const next = structuredClone(form);
    fn(next);
    setForm(next);
    setMessage("");
    setError("");
  }
  const provider = form?.providers.find((p) =>
    mode === "cli"
      ? p.adapter === "cli" && p.cli?.kind === selected
      : p.id === selected,
  );
  const catalogIdentity =
    provider?.adapter === "cli"
      ? JSON.stringify([provider.id, provider.cli?.kind, provider.cli?.path])
      : "";
  catalogIdentityRef.current = catalogIdentity;
  const catalog = catalogs[catalogIdentity];
  async function loadCLIModels(force = false) {
    if (!provider?.cli || provider.cli.kind !== "codex") return;
    const identity = catalogIdentity;
    const request = ++catalogRequest.current;
    setCatalogLoading(true);
    try {
      const result = await api<CLICatalog>("/cli/models", {
        kind: provider.cli.kind,
        path: provider.cli.path,
        refresh: force,
      });
      if (
        catalogIdentityRef.current === identity &&
        catalogRequest.current === request
      ) {
        setCatalogs((old) => ({ ...old, [identity]: result }));
      }
    } catch {
      if (
        catalogIdentityRef.current === identity &&
        catalogRequest.current === request
      ) {
        setCatalogs((old) => ({
          ...old,
          [identity]: old[identity]?.options.length
            ? {
                ...old[identity],
                source: "cache",
                stale: true,
                error: "读取失败，显示上次成功目录，请重新刷新。",
              }
            : {
                models: [],
                options: [],
                source: "unavailable",
                stale: false,
                error: "模型目录读取失败，请检查 CLI 路径后重试。",
              },
        }));
      }
    } finally {
      if (
        catalogIdentityRef.current === identity &&
        catalogRequest.current === request
      )
        setCatalogLoading(false);
    }
  }
  useEffect(() => {
    ++catalogRequest.current;
    setCatalogLoading(false);
    if (provider?.cli?.kind !== "codex") return;
    const timer = window.setTimeout(() => {
      void loadCLIModels();
    }, 250);
    return () => {
      window.clearTimeout(timer);
      ++catalogRequest.current;
    };
  }, [catalogIdentity]);
  useEffect(() => {
    let active = true;
    setHistory([]);
    if (
      provider?.adapter === "cli" &&
      saved?.providers.some((p) => p.id === provider.id)
    ) {
      api<ImageProbe[]>(`/providers/${provider.id}/image-verifications`)
        .then((items) => {
          if (active) setHistory(items);
        })
        .catch(() => {});
    }
    return () => {
      active = false;
    };
  }, [provider?.id, probe?.state, saved]);
  function patch(patch: Partial<Provider>) {
    if (!provider) return;
    change((s) =>
      Object.assign(
        s.providers.find((p) => p.id === provider.id)!,
        patch,
      ),
    );
  }
  function patchCLI(patchCLI: Partial<NonNullable<Provider["cli"]>>) {
    if (provider?.cli) patch({ cli: { ...provider.cli, ...patchCLI } });
  }
  function patchModel(i: number, patchM: Partial<Model>) {
    if (!provider) return;
    change((s) => {
      const p = s.providers.find((p) => p.id === provider.id)!;
      const oldID = p.models[i].id;
      p.models[i] = { ...p.models[i], ...patchM };
      if (patchM.id !== undefined && patchM.id !== oldID) {
        for (const b of Object.values(s.bindings))
          if (b.provider === p.id && b.model === oldID) b.model = patchM.id;
      }
    });
  }
  function chooseCLIModel(id: string) {
    const old = provider?.models[0]?.cli_options;
    const option = catalog?.options.find((m) => m.id === id);
    const next = {
      reasoning:
        old?.reasoning && option?.reasoning.some((o) => o.id === old.reasoning)
          ? old.reasoning
          : undefined,
      service_tier:
        old?.service_tier &&
        option?.service_tiers.some((o) => o.id === old.service_tier)
          ? old.service_tier
          : undefined,
    };
    patchModel(0, {
      id,
      ...(old
        ? {
            cli_options: next.reasoning || next.service_tier ? next : undefined,
          }
        : {}),
    });
    if (
      old &&
      (old.reasoning !== next.reasoning ||
        old.service_tier !== next.service_tier)
    )
      setMessage("已将新模型不支持的参数恢复为沿用 CLI 配置。");
  }
  function ensureCLI(kind: string) {
    setMode("cli");
    setSelected(kind);
    setMessage("");
    if (
      !form?.providers.some((p) => p.adapter === "cli" && p.cli?.kind === kind)
    ) {
      change((s) =>
        s.providers.push({
          id: "cli-" + kind,
          name: cliNames[kind],
          adapter: "cli",
          base_url: "",
          key_env: "",
          cli: {
            ...(kind === "codex" ? { image_mode: "codex" as const } : {}),
            kind: kind as "codex" | "claude" | "gemini",
            path: "",
            image_instructions: "",
            skill_path: "",
            timeout_seconds: 600,
          },
          models: [
            kind === "codex"
              ? codexImageModel(newModel("default"))
              : newModel("default"),
          ],
        }),
      );
    }
  }
  function chooseImageMode(image_mode: "codex" | "tool") {
    if (!provider?.cli) return;
    patch({
      cli: { ...provider.cli, image_mode },
      ...(image_mode === "codex"
        ? {
            models: provider.models.map((m, i) =>
              i === 0 ? codexImageModel(m) : m,
            ),
          }
        : {}),
    });
  }
  function chooseProtocol(type: string) {
    setProtocol(type);
    setMode("remote");
    const existing = form?.providers.find((p) => p.adapter === type);
    if (existing) {
      setSelected(existing.id);
      return;
    }
    addRemote(type);
  }
  function addRemote(type = protocol) {
    const id = "provider-" + Date.now();
    const m = newModel();
    if (type === "seedance") {
      m.capabilities = ["video"];
      m.async = true;
      m.sizes = ["480p"];
      m.reference_images = true;
      m.max_references = 6;
    }
    change((s) =>
      s.providers.push({
        id,
        name: protocols[type].name,
        adapter: type,
        base_url: protocols[type].url,
        key_env: "",
        credential_source: "file",
        models: [m],
      }),
    );
    setSelected(id);
  }
  async function act(fn: () => Promise<void>) {
    if (busy) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      await fn();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function saveAll() {
    if (!form) throw new Error("配置尚未加载");
    let next = await api<Settings>("/settings", form, "PUT");
    setSaved(structuredClone(next));
    setForm(structuredClone(next));
    for (const p of next.providers) {
      const key = keys[p.id];
      if (!key || p.credential_source !== "file") continue;
      await api(
        `/providers/${p.id}/credential`,
        { key, version: p.credential_version ?? "" },
        "PUT",
      );
      setKeys((current) => ({ ...current, [p.id]: "" }));
      const fresh = await api<SettingsResponse>("/settings");
      next = fresh.settings;
      setForm(structuredClone(next));
      setSaved(structuredClone(next));
    }
    await cache.invalidateQueries({ queryKey: ["settings"] });
    setMessage("配置已保存，旧报价自动失效。");
    return next;
  }
  async function fetchModels() {
    if (!provider) return;
    if (provider.adapter === "cli") {
      await loadCLIModels(true);
      return;
    }
    await saveAll();
    const result = await api<{ models: string[] }>(
      `/providers/${provider.id}/models`,
    );
    setModels((old) => ({ ...old, [provider.id]: result.models }));
    setMessage(
      result.models.length
        ? `读取到 ${result.models.length} 个模型，可在模型 ID 中选择。`
        : "当前 CLI 没有可读取的模型目录，请手动填写 ID 或使用 default。",
    );
  }
  async function test() {
    if (!provider) return;
    await saveAll();
    const result = await api<{
      message: string;
      cli?: CLIStatus;
      models?: string[];
    }>(`/providers/${provider.id}/test`, {});
    setMessage(result.message);
    if (result.cli)
      setStatuses((old) => [
        ...old.filter((x) => x.kind !== result.cli!.kind),
        result.cli!,
      ]);
    if (result.models)
      setModels((old) => ({ ...old, [provider.id]: result.models! }));
    if (provider.cli?.kind === "codex") await loadCLIModels(true);
  }
  const disabled = busy || probe?.state === "running";
  return (
    <dialog
      ref={dialog}
      className="model-settings-dialog"
      aria-labelledby="model-settings-title"
      onCancel={(e) => {
        e.preventDefault();
        if (busy) return;
        if (probe) {
          if (probe.state !== "running") setProbe(null);
        } else if (closing) setClosing(false);
        else close();
      }}
    >
      <div className="ms-shell" inert={closing || !!probe}>
        <aside className="ms-nav">
          <div className="ms-brand">
            <Settings2 size={21} />
            <strong>设置</strong>
            <span>映序</span>
          </div>
          <div className="ms-nav-caption">创作工作空间</div>
          {[
            ["connections", "模型接入", Globe, "本地工具与远程服务"],
            ["bindings", "制作能力", Cpu, "为每一步选择模型"],
            ["advanced", "高级配置", SlidersHorizontal, "能力、参数与计价"],
          ].map(([id, label, Icon, desc]) => {
            const I = Icon as typeof Globe;
            return (
              <button
                key={String(id)}
                className={"ms-nav-item " + (section === id ? "active" : "")}
                onClick={() => setSection(String(id))}
              >
                <I size={18} />
                <span>
                  <strong>{String(label)}</strong>
                  <small>{String(desc)}</small>
                </span>
                <ChevronRight size={14} />
              </button>
            );
          })}
          <div className="ms-local-note">
            <ShieldCheck size={17} />
            <span>
              本机私人工作空间<small>配置由本地后端管理</small>
            </span>
          </div>
        </aside>
        <div className="ms-main">
          <select
            className="ms-mobile-nav"
            aria-label="设置分区"
            value={section}
            onChange={(e) => setSection(e.target.value)}
          >
            <option value="connections">模型接入</option>
            <option value="bindings">制作能力</option>
            <option value="advanced">高级配置</option>
          </select>
          <header className="ms-header">
            <div>
              <span className="eyebrow">MODEL SERVICES</span>
              <h1 id="model-settings-title">
                {section === "connections"
                  ? "模型接入"
                  : section === "bindings"
                    ? "制作能力"
                    : "高级配置"}
              </h1>
            </div>
            <button
              className="icon-button"
              aria-label="关闭模型设置"
              disabled={disabled}
              onClick={close}
            >
              <X size={21} />
            </button>
          </header>
          <fieldset className="ms-content ms-fields" disabled={disabled}>
            {query.isPending || !form || !saved ? (
              <div className="ms-empty">
                {query.error ? (
                  <>
                    <p role="alert">{query.error.message}</p>
                    <button onClick={() => query.refetch()}>重新加载</button>
                  </>
                ) : (
                  <>
                    <LoaderCircle className="spin" size={20} /> 正在读取配置…
                  </>
                )}
              </div>
            ) : (
              <>
                {error && (
                  <div className="ms-alert error" role="alert">
                    {error}
                  </div>
                )}
                {message && (
                  <div className="ms-alert success" role="status">
                    <Check size={16} />
                    {message}
                  </div>
                )}
                {section === "connections" && (
                  <>
                    <p className="ms-intro">
                      连接你习惯的工具，让合适的模型参与创作。
                    </p>
                    <div
                      className="ms-segment"
                      role="tablist"
                      aria-label="模型接入方式"
                    >
                      <button
                        role="tab"
                        aria-selected={mode === "cli"}
                        className={mode === "cli" ? "active" : ""}
                        onClick={() => {
                          setMode("cli");
                          setSelected("codex");
                        }}
                      >
                        <Terminal size={18} />
                        <span>
                          本地 CLI<small>复用本机工具和登录状态</small>
                        </span>
                      </button>
                      <button
                        role="tab"
                        aria-selected={mode === "remote"}
                        className={mode === "remote" ? "active" : ""}
                        onClick={() => {
                          setMode("remote");
                          const p = form.providers.find(
                            (p) => p.adapter === protocol,
                          );
                          setSelected(p?.id ?? "");
                        }}
                      >
                        <Globe size={18} />
                        <span>
                          远程 API<small>使用自己的 API Key</small>
                        </span>
                      </button>
                    </div>
                    {mode === "cli" ? (
                      <>
                        <div className="ms-section-heading">
                          <h2>本地创作工具</h2>
                          <button
                            className="text-button"
                            disabled={disabled}
                            onClick={() =>
                              act(async () =>
                                setStatuses(
                                  await api<CLIStatus[]>("/cli/status"),
                                ),
                              )
                            }
                          >
                            <RefreshCw size={13} /> 重新检测
                          </button>
                        </div>
                        <div className="ms-cli-list">
                          {Object.entries(cliNames).map(([kind, name]) => {
                            const status = statuses.find(
                              (x) => x.kind === kind,
                            );
                            const p = form.providers.find(
                              (x) =>
                                x.adapter === "cli" && x.cli?.kind === kind,
                            );
                            return (
                              <button
                                key={kind}
                                className={
                                  "ms-cli-card " +
                                  (selected === kind ? "selected" : "")
                                }
                                onClick={() => ensureCLI(kind)}
                              >
                                <span className={"ms-cli-icon " + kind}>
                                  {kind === "codex"
                                    ? "C"
                                    : kind === "claude"
                                      ? "✳"
                                      : "✧"}
                                </span>
                                <span className="ms-cli-info">
                                  <strong>
                                    {name}
                                    <span className="ms-subtle">
                                      {kind === "codex"
                                        ? "OpenAI"
                                        : kind === "claude"
                                          ? "Anthropic"
                                          : "Google"}
                                    </span>
                                  </strong>
                                  <small>
                                    {status
                                      ? status.installed
                                        ? status.version || "已检测到本机工具"
                                        : "未安装 · 可配置可执行路径"
                                      : "检测中…"}
                                  </small>
                                  {p && (
                                    <small>
                                      {p.models.map((m) => m.id).join(" · ")}
                                    </small>
                                  )}
                                </span>
                                <span
                                  className={
                                    "ms-state " +
                                    (status?.installed ? "ready" : "")
                                  }
                                >
                                  {status?.installed
                                    ? "已安装"
                                    : status
                                      ? "未安装"
                                      : "检测中"}
                                </span>
                                <span className="ms-radio">
                                  {selected === kind && <span />}
                                </span>
                              </button>
                            );
                          })}
                        </div>
                        {!provider ? (
                          <div className="ms-empty ms-soft">
                            <Terminal size={24} />
                            <h3>选择一个本地 CLI</h3>
                            <p>
                              点击上方卡片配置模型。检测不会安装工具或发起生成。
                            </p>
                            <button
                              className="secondary"
                              onClick={() => ensureCLI(selected)}
                            >
                              配置 {cliNames[selected]}
                            </button>
                          </div>
                        ) : (
                          <section className="ms-card">
                            <div className="ms-section-heading">
                              <div>
                                <h2>{provider.name}</h2>
                                <p>结构化规划与已有生图工具</p>
                              </div>
                              <button
                                className="secondary"
                                disabled={disabled}
                                onClick={() => act(test)}
                              >
                                保存并检测
                              </button>
                            </div>
                            <div className="ms-form-grid">
                              <label className="wide">
                                可执行文件路径
                                <input
                                  value={provider.cli?.path ?? ""}
                                  placeholder="留空从 PATH 检测；也可填写绝对路径"
                                  onChange={(e) =>
                                    patchCLI({ path: e.target.value })
                                  }
                                />
                                <small>
                                  {statuses.find((x) => x.kind === selected)
                                    ?.path ||
                                    "未检测到时，请先在本机安装并登录 CLI，然后重新检测。"}
                                </small>
                              </label>
                              <div>
                                <CLIModelSelect
                                  key={provider.id}
                                  value={provider.models[0]?.id ?? ""}
                                  models={catalog?.models ?? []}
                                  options={catalog?.options}
                                  cliOptions={provider.models[0]?.cli_options}
                                  onOptionsChange={
                                    provider.cli?.kind === "codex"
                                      ? (cli_options) =>
                                          patchModel(0, {
                                            cli_options:
                                              cli_options.reasoning ||
                                              cli_options.service_tier
                                                ? cli_options
                                                : undefined,
                                          })
                                      : undefined
                                  }
                                  onChange={chooseCLIModel}
                                />
                                <small role="status">
                                  {catalogLoading
                                    ? "正在读取 CLI 模型目录…"
                                    : catalog
                                      ? `${catalog.source === "live" ? "来自 Codex CLI" : catalog.source === "cache" ? "已缓存的 CLI 目录" : "目录不可用"}${catalog.stale ? " · 已过期" : ""}${catalog.fetched_at ? ` · ${catalog.fetched_at}` : ""}`
                                      : "尚未读取模型目录"}
                                </small>
                                {catalog?.error && (
                                  <small role="alert">{catalog.error}</small>
                                )}
                                <button
                                  className="text-button"
                                  disabled={
                                    disabled ||
                                    catalogLoading ||
                                    provider.cli?.kind !== "codex"
                                  }
                                  onClick={() => act(fetchModels)}
                                >
                                  <RefreshCw size={13} /> 刷新模型列表
                                </button>
                              </div>
                              <div className="ms-auth-status">
                                <span className="ms-label">登录状态</span>
                                <span>
                                  {statuses.find((x) => x.kind === selected)
                                    ?.auth === "ready"
                                    ? "本机状态检测通过"
                                    : statuses.find((x) => x.kind === selected)
                                          ?.auth === "unavailable"
                                      ? "未登录或状态不可用"
                                      : "尚未核实"}
                                </span>
                                <small>
                                  剧本与分镜规划无需配置下方生图工具。
                                </small>
                              </div>
                              <div className="wide ms-subtle">
                                <strong>使用 CLI 生成图片（可选）</strong>
                                <p>
                                  用于角色主图、其他视角和分镜画面。只做剧本规划无需验证；生图须先确认一次测试报价，成功后才能绑定图片制作。
                                </p>
                              </div>
                              {provider.cli?.kind === "codex" && (
                                <label className="wide">
                                  生图方式
                                  <select
                                    value={provider.cli.image_mode || "tool"}
                                    onChange={(e) =>
                                      chooseImageMode(
                                        e.target.value as "codex" | "tool",
                                      )
                                    }
                                  >
                                    <option value="codex">
                                      Codex 生图 · 自动调用 imagegen
                                    </option>
                                    <option value="tool">
                                      已有工具或 Skill · 手动配置
                                    </option>
                                  </select>
                                </label>
                              )}
                              {provider.cli?.image_mode === "codex" ? (
                                <>
                                  <div className="wide ms-subtle">
                                    <p>
                                      自动通过 Codex 的 $imagegen
                                      调用生图工具、传入参考图并收取本次任务图片，无需填写工具说明或
                                      Skill
                                      路径。上方模型负责组织调用，实际图片模型由
                                      Codex 生图工具决定。能否使用取决于当前
                                      CLI、Skill 与账户权限。
                                    </p>
                                  </div>
                                  <label>
                                    输出尺寸（像素）
                                    <input
                                      value={
                                        provider.models[0]?.sizes.join(", ") ||
                                        ""
                                      }
                                      onChange={(e) =>
                                        patchModel(0, {
                                          sizes: e.target.value
                                            .split(",")
                                            .map((v) => v.trim())
                                            .filter(Boolean),
                                        })
                                      }
                                      placeholder="例如 1024x1536"
                                    />
                                  </label>
                                  <label>
                                    最多参考图数量
                                    <input
                                      type="number"
                                      min={1}
                                      max={9}
                                      value={
                                        provider.models[0]?.max_references ?? 6
                                      }
                                      onChange={(e) =>
                                        patchModel(0, {
                                          max_references: Number(
                                            e.target.value,
                                          ),
                                        })
                                      }
                                    />
                                  </label>
                                  <small className="wide">
                                    以上为待验证的图片规格；仅返回预览、提示词或尺寸不符的图片不会通过验证。
                                  </small>
                                </>
                              ) : (
                                <>
                                  <label className="wide">
                                    生图工具或 Skill 说明（可选）
                                    <textarea
                                      value={
                                        provider.cli?.image_instructions ?? ""
                                      }
                                      onChange={(e) =>
                                        patchCLI({
                                          image_instructions: e.target.value,
                                        })
                                      }
                                      placeholder="填写已有生图工具的名称、调用方式与必要限制。CLI 需生成真实图片并返回文件路径。"
                                    />
                                  </label>
                                  <label className="wide">
                                    Skill 文件路径{" "}
                                    <span className="ms-subtle">可选</span>
                                    <input
                                      value={provider.cli?.skill_path ?? ""}
                                      onChange={(e) =>
                                        patchCLI({ skill_path: e.target.value })
                                      }
                                      placeholder="本机 SKILL.md 的绝对路径，只读使用"
                                    />
                                  </label>
                                </>
                              )}
                            </div>
                            <div className="ms-verification">
                              <Image size={18} />
                              <div>
                                <strong>
                                  {provider.models[0] &&
                                  imageVerified(
                                    provider,
                                    provider.models[0],
                                    saved,
                                  )
                                    ? "生图能力已验证"
                                    : "尚未验证 CLI 能否生成图片"}
                                </strong>
                                <p>
                                  {provider.cli?.image_mode === "codex"
                                    ? "查看报价后，明确确认一次真实单张生图测试。系统校验本次任务的文件、格式和尺寸；成功后才能绑定角色图与分镜图。不会自动重试，内部调用次数和费用可能未知。"
                                    : "先在高级配置声明图片能力、参考图数量与尺寸，再查看报价并确认生成一张测试图。验证成功后才能绑定图片制作；内部调用次数和费用可能未知。"}
                                </p>
                              </div>
                              <button
                                className="secondary"
                                disabled={
                                  disabled ||
                                  (provider.cli?.image_mode !== "codex" &&
                                    !provider.cli?.image_instructions) ||
                                  !provider.models[0]?.capabilities.includes(
                                    "image",
                                  )
                                }
                                onClick={() =>
                                  act(async () => {
                                    await saveAll();
                                    const result = await api<ImageProbe>(
                                      `/providers/${provider.id}/image-verification/quote`,
                                      { model: provider.models[0].id },
                                    );
                                    setProbe(result);
                                    setAccepted(false);
                                  })
                                }
                              >
                                查看验证报价
                              </button>
                            </div>
                          </section>
                        )}
                      </>
                    ) : (
                      <>
                        <div
                          className="ms-protocols"
                          role="tablist"
                          aria-label="远程模型协议"
                        >
                          {Object.entries(protocols).map(([id, p]) => (
                            <button
                              key={id}
                              role="tab"
                              aria-selected={protocol === id}
                              className={protocol === id ? "active" : ""}
                              onClick={() => chooseProtocol(id)}
                            >
                              {p.name}
                            </button>
                          ))}
                        </div>
                        <div className="ms-section-heading">
                          <h2>服务配置</h2>
                          <button
                            className="text-button"
                            disabled={disabled}
                            onClick={() => addRemote()}
                          >
                            <Plus size={14} /> 添加服务
                          </button>
                        </div>
                        {form.providers.some((p) => p.adapter === protocol) && (
                          <div className="ms-provider-chips">
                            {form.providers
                              .filter((p) => p.adapter === protocol)
                              .map((p) => (
                                <button
                                  key={p.id}
                                  className={selected === p.id ? "active" : ""}
                                  onClick={() => setSelected(p.id)}
                                >
                                  {p.name}
                                </button>
                              ))}
                          </div>
                        )}
                        {!provider ? (
                          <div className="ms-empty ms-soft">
                            <KeyRound size={25} />
                            <h3>连接 {protocols[protocol].name}</h3>
                            <p>填写服务地址和自己的 Key，选择需要的模型。</p>
                            <button
                              className="secondary"
                              onClick={() => addRemote()}
                            >
                              添加服务配置
                            </button>
                          </div>
                        ) : (
                          <section className="ms-card">
                            <div className="ms-section-heading">
                              <div>
                                <h2>
                                  {protocols[provider.adapter]?.name ??
                                    provider.name}
                                </h2>
                                <p>凭据仅交给你指定的服务地址</p>
                              </div>
                              <span
                                className={
                                  "ms-state " +
                                  (query.data?.credentials_present[provider.id]
                                    ? "ready"
                                    : "")
                                }
                              >
                                {query.data?.credentials_present[provider.id]
                                  ? "凭据已配置"
                                  : "待配置"}
                              </span>
                            </div>
                            <div className="ms-form-grid">
                              <label className="wide">
                                服务名称
                                <input
                                  value={provider.name}
                                  onChange={(e) =>
                                    patch({ name: e.target.value })
                                  }
                                />
                              </label>
                              <label className="wide">
                                API 地址
                                <input
                                  value={provider.base_url}
                                  onChange={(e) =>
                                    patch({ base_url: e.target.value })
                                  }
                                  disabled={provider.adapter === "seedance"}
                                />
                              </label>
                              <label className="wide">
                                凭据来源
                                <select
                                  value={provider.credential_source ?? "env"}
                                  onChange={(e) =>
                                    patch({
                                      credential_source: e.target.value as
                                        "env" | "file",
                                    })
                                  }
                                >
                                  <option value="file">
                                    页面填写 · 本地加密保存
                                  </option>
                                  <option value="env">后端环境变量</option>
                                </select>
                              </label>
                              {provider.credential_source === "file" ? (
                                <label className="wide">
                                  API Key
                                  <div className="ms-key-input">
                                    <input
                                      autoComplete="off"
                                      type={visible ? "text" : "password"}
                                      value={keys[provider.id] ?? ""}
                                      placeholder={
                                        provider.credential_version
                                          ? "已保存 · 留空保留现有 Key"
                                          : "填写 API Key"
                                      }
                                      onChange={(e) =>
                                        setKeys((old) => ({
                                          ...old,
                                          [provider.id]: e.target.value,
                                        }))
                                      }
                                    />
                                    <button
                                      aria-label={
                                        visible
                                          ? "隐藏 API Key"
                                          : "显示 API Key"
                                      }
                                      onClick={() => setVisible(!visible)}
                                    >
                                      {visible ? (
                                        <EyeOff size={16} />
                                      ) : (
                                        <Eye size={16} />
                                      )}
                                    </button>
                                  </div>
                                  <small>
                                    保存后清空输入，不回显原文，不写入浏览器存储。
                                  </small>
                                  {provider.credential_version && (
                                    <button
                                      className="text-button ms-danger"
                                      disabled={disabled}
                                      onClick={() =>
                                        act(async () => {
                                          await api(
                                            `/providers/${provider.id}/credential`,
                                            {
                                              version:
                                                provider.credential_version,
                                            },
                                            "DELETE",
                                          );
                                          const fresh =
                                            await api<SettingsResponse>(
                                              "/settings",
                                            );
                                          const current =
                                            fresh.settings.providers.find(
                                              (p) => p.id === provider.id,
                                            )!;
                                          const refresh = (
                                            settings: Settings | null,
                                          ) =>
                                            settings && {
                                              ...settings,
                                              providers: settings.providers.map(
                                                (p) =>
                                                  p.id === provider.id
                                                    ? {
                                                        ...p,
                                                        credential_version:
                                                          current.credential_version,
                                                      }
                                                    : p,
                                              ),
                                            };
                                          setForm(refresh);
                                          setSaved(refresh);
                                          setKeys((old) => ({
                                            ...old,
                                            [provider.id]: "",
                                          }));
                                          await cache.invalidateQueries({
                                            queryKey: ["settings"],
                                          });
                                          setMessage(
                                            "已解除当前 Key 绑定，历史密文仍保留。",
                                          );
                                        })
                                      }
                                    >
                                      清除当前 Key 绑定
                                    </button>
                                  )}
                                </label>
                              ) : (
                                <label className="wide">
                                  环境变量名称
                                  <input
                                    value={provider.key_env}
                                    onChange={(e) =>
                                      patch({ key_env: e.target.value })
                                    }
                                    placeholder="例如 IMAGE_API_KEY"
                                  />
                                  <small>
                                    请在后端进程环境中设置，配置后重启后端。
                                  </small>
                                </label>
                              )}
                              <label className="wide">
                                模型 ID
                                <input
                                  list={"models-" + provider.id}
                                  value={provider.models[0]?.id ?? ""}
                                  onChange={(e) =>
                                    patchModel(0, { id: e.target.value })
                                  }
                                  placeholder="手动填写，或从读取到的目录选择"
                                />
                              </label>
                            </div>
                            <div className="ms-test-row">
                              <span>
                                {query.data?.credential_errors?.[provider.id] ||
                                  "连接检测仅读取模型目录，不发起生成。"}
                              </span>
                              <button
                                className="secondary"
                                disabled={disabled}
                                onClick={() => act(test)}
                              >
                                保存并检测
                              </button>
                            </div>
                          </section>
                        )}
                      </>
                    )}
                    <div className="ms-footnote">
                      <ShieldCheck size={15} />
                      <p>
                        本地 CLI 与远程 API
                        可以混合使用。配置接入后，在“制作能力”选择每一步使用的模型。
                      </p>
                    </div>
                  </>
                )}
                {section === "bindings" && (
                  <>
                    <p className="ms-intro">
                      每一步独立选择服务。视频使用
                      Seedance，旁白与视觉审核单独配置。
                    </p>
                    <section className="ms-card ms-binding-list">
                      {Object.entries(roles).map(([role, label]) => (
                        <label key={role}>
                          <span>
                            <strong>{label}</strong>
                            <small>
                              {role.endsWith("_image")
                                ? "可继承通用图片生成模型"
                                : role === "image"
                                  ? "CLI 生图通过验证后才可选"
                                  : "仅展示具备对应能力的模型"}
                            </small>
                          </span>
                          <select
                            aria-label={label}
                            value={`${form.bindings[role]?.provider ?? ""}/${form.bindings[role]?.model ?? ""}`}
                            onChange={(e) => {
                              const [id, ...model] = e.target.value.split("/");
                              change((s) => {
                                if (id)
                                  s.bindings[role] = {
                                    provider: id,
                                    model: model.join("/"),
                                  };
                                else delete s.bindings[role];
                              });
                            }}
                          >
                            <option value="/">
                              {role.endsWith("_image")
                                ? "继承通用图片模型"
                                : "选择模型"}
                            </option>
                            {bindingOptions(form, saved, role).map((o) => (
                              <option key={o.value} value={o.value}>
                                {o.label}
                              </option>
                            ))}
                          </select>
                        </label>
                      ))}
                    </section>
                    <div className="ms-footnote">
                      <FlaskConical size={16} />
                      <p>
                        模拟服务仍可用于免费验证流程；切换接入方式不会自动更改制作绑定。
                      </p>
                    </div>
                  </>
                )}
                {section === "advanced" && (
                  <>
                    <p className="ms-intro">
                      只声明服务真正支持的能力。价格未知时保持留空，实际任务仍需确认报价。
                    </p>
                    <label className="ms-provider-select">
                      服务
                      <select
                        value={provider?.id ?? ""}
                        onChange={(e) => {
                          const p = form.providers.find(
                            (x) => x.id === e.target.value,
                          )!;
                          setMode(p.adapter === "cli" ? "cli" : "remote");
                          setSelected(p.cli?.kind ?? p.id);
                          if (protocols[p.adapter]) setProtocol(p.adapter);
                        }}
                      >
                        <option value="" disabled>
                          选择服务
                        </option>
                        {form.providers.map((p) => (
                          <option value={p.id} key={p.id}>
                            {p.name}
                          </option>
                        ))}
                      </select>
                    </label>
                    {provider && (
                      <section className="ms-card">
                        <div className="ms-section-heading">
                          <h2>模型能力与参数</h2>
                          <button
                            className="text-button"
                            onClick={() => {
                              const m = newModel();
                              m.capabilities =
                                provider.adapter === "seedance"
                                  ? ["video"]
                                  : ["text"];
                              if (provider.adapter === "seedance") {
                                m.async = true;
                                m.sizes = ["480p"];
                              }
                              patch({ models: [...provider.models, m] });
                            }}
                          >
                            <Plus size={14} /> 添加模型
                          </button>
                        </div>
                        {provider.adapter === "cli" && (
                          <label className="ms-timeout">
                            任务超时（秒，0 使用 600 秒）
                            <input
                              type="number"
                              min="0"
                              max="1800"
                              value={provider.cli?.timeout_seconds ?? 600}
                              onChange={(e) =>
                                patchCLI({
                                  timeout_seconds: Number(e.target.value),
                                })
                              }
                            />
                          </label>
                        )}
                        <ModelEditor
                          provider={provider}
                          patchModel={patchModel}
                        />
                      </section>
                    )}
                    <div className="ms-footnote">
                      <KeyRound size={16} />
                      <p>
                        Key 使用 AES-GCM
                        加密，主密钥和密文分文件保存在私有数据目录。恢复凭据需要同时备份两者；本地加密不能抵御整个用户目录被读取。
                      </p>
                    </div>
                  </>
                )}
                {provider &&
                  section === "connections" &&
                  provider.adapter !== "mock" && (
                    <button
                      className="text-button"
                      disabled={disabled}
                      onClick={() => act(fetchModels)}
                    >
                      <RefreshCw size={13} />{" "}
                      {provider?.adapter === "cli"
                        ? "刷新 CLI 模型目录"
                        : "保存并读取模型目录"}
                    </button>
                  )}
                {provider &&
                  provider.adapter === "cli" &&
                  section === "connections" &&
                  history.length > 0 && (
                    <div className="ms-history">
                      <h3>最近的生图验证</h3>
                      {history.slice(0, 3).map((item) => (
                        <button
                          key={item.id}
                          className="text-button"
                          onClick={() => {
                            setProbe(item);
                            setAccepted(false);
                          }}
                        >
                          {item.quote.created} ·{" "}
                          {item.state === "succeeded"
                            ? "已通过"
                            : item.state === "running"
                              ? "进行中"
                              : item.state === "pending"
                                ? "等待确认"
                                : "需要处理"}{" "}
                          · 查看记录
                        </button>
                      ))}
                    </div>
                  )}
                {provider && (
                  <datalist id={"models-" + provider.id}>
                    {(models[provider.id] ?? []).map((id) => (
                      <option key={id} value={id} />
                    ))}
                  </datalist>
                )}
              </>
            )}
          </fieldset>
          <footer className="ms-footer">
            <span>{dirty ? "有未保存的更改" : "配置保存在本机"}</span>
            <button className="secondary" disabled={disabled} onClick={close}>
              关闭
            </button>
            <button
              className="primary"
              disabled={disabled || !form}
              onClick={() =>
                act(async () => {
                  await saveAll();
                })
              }
            >
              {busy ? (
                <LoaderCircle size={15} className="spin" />
              ) : (
                <Save size={15} />
              )}{" "}
              保存配置
            </button>
          </footer>
        </div>
      </div>
      {closing && (
        <div className="ms-overlay">
          <section
            className="ms-confirm"
            role="alertdialog"
            aria-labelledby="discard-title"
          >
            <h2 id="discard-title">保留这次配置修改？</h2>
            <p>尚未保存的字段和 Key 仅保存在当前窗口。</p>
            <div>
              <button className="secondary" onClick={() => setClosing(false)}>
                继续编辑
              </button>
              <button
                className="secondary"
                disabled={busy}
                onClick={() => {
                  setKeys({});
                  onClose();
                }}
              >
                放弃并关闭
              </button>
              <button
                className="primary"
                disabled={busy}
                onClick={() =>
                  act(async () => {
                    await saveAll();
                    onClose();
                  })
                }
              >
                保存并关闭
              </button>
            </div>
          </section>
        </div>
      )}
      {probe && (
        <div className="ms-overlay">
          <section
            className="ms-confirm"
            role="dialog"
            aria-labelledby="probe-title"
          >
            <div className="ms-section-heading">
              <h2 id="probe-title">单张生图能力验证</h2>
              <button
                aria-label="关闭验证"
                className="icon-button"
                disabled={probe.state === "running" || busy}
                onClick={() => setProbe(null)}
              >
                <X size={18} />
              </button>
            </div>
            <p>{probe.quote.notice}</p>
            <div className="ms-probe-detail">
              <strong>
                {probe.quote.requests[0].provider} /{" "}
                {probe.quote.requests[0].model}
              </strong>
              <p>{probe.quote.requests[0].prompt}</p>
              <p>数量：1 张 · 费用上界未知</p>
              <pre>
                {JSON.stringify(probe.quote.requests[0].parameters, null, 2)}
              </pre>
            </div>
            {probe.state === "pending" ? (
              <>
                <label className="ms-accept">
                  <input
                    type="checkbox"
                    checked={accepted}
                    onChange={(e) => setAccepted(e.target.checked)}
                  />
                  我接受本次验证费用未知，按实际用量计费。
                </label>
                <button
                  className="primary"
                  disabled={!accepted || busy}
                  onClick={() =>
                    act(async () =>
                      setProbe(
                        await api<ImageProbe>(
                          `/image-verifications/${probe.id}/confirm`,
                          { allow_unknown: true },
                        ),
                      ),
                    )
                  }
                >
                  确认这一次验证
                </button>
              </>
            ) : probe.state === "running" ? (
              <p role="status">
                <LoaderCircle size={17} className="spin" />{" "}
                正在验证，失败不会自动重试…
              </p>
            ) : probe.state === "succeeded" ? (
              <>
                <p className="ms-alert success">
                  已验证真实图片产物，可在制作能力中绑定。
                </p>
                <img
                  className="ms-probe-image"
                  src={media(probe.image)}
                  alt="CLI 生图验证产物"
                />
              </>
            ) : (
              <>
                <p role="alert" className="ms-alert error">
                  {probe.error}
                </p>
                <button
                  className="secondary"
                  disabled={busy}
                  onClick={() =>
                    act(async () =>
                      setProbe(
                        await api<ImageProbe>(
                          `/image-verifications/${probe.id}/resume`,
                          {},
                        ),
                      ),
                    )
                  }
                >
                  仅恢复已保存响应
                </button>
              </>
            )}
          </section>
        </div>
      )}
    </dialog>
  );
}
