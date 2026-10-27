import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  Check,
  ChevronDown,
  CirclePlay,
  Clapperboard,
  Download,
  Film,
  ImagePlus,
  LoaderCircle,
  Pause,
  Plus,
  Settings2,
  Sparkles,
  WandSparkles,
  X,
} from "lucide-react";
import { api, media, money, statusLabel } from "./api";
import type { Project, Quote, Settings } from "./api";
import ServiceSettings from "./ServiceSettings";
import AssetWorkflow, { assetStep } from "./AssetWorkflow";
import { quoteStageLabels, quoteRequestLabel } from "./api";
import { BoardAssets, ShotInspector } from "./StoryboardReview";
const kindLabels: Record<string, string> = {
  text: "剧本规划",
  image: "分镜图片",
  video: "视频镜头",
  tts: "旁白配音",
  vision: "视觉审核",
};
const stages = [
  "故事想法",
  "角色主图",
  "参考素材",
  "分镜创作",
  "审核分镜",
  "镜头生成",
  "成片交付",
];
export default function App() {
  const cache = useQueryClient();
  const [page, setPage] = useState<"studio" | "settings">("studio");
  const [id, setID] = useState(localStorage.getItem("project") ?? "");
  const [idea, setIdea] = useState("");
  const [instruction, setInstruction] = useState("");
  const [styleChoice, setStyleChoice] = useState("");
  const [impact, setImpact] = useState<string[]>([]);
  const [styleEdit, setStyleEdit] = useState("");
  const [target, setTarget] = useState("");
  const [version, setVersion] = useState<number | null>(null);
  const [quote, setQuote] = useState<Quote | null>(null);
  const [unknown, setUnknown] = useState(false);
  const [error, setError] = useState("");
  const projects = useQuery({
    queryKey: ["projects"],
    queryFn: () => api<Project[]>("/projects"),
  });
  const current = useQuery({
    queryKey: ["project", id],
    queryFn: () => api<Project>("/projects/" + id),
    enabled: !!id,
  });
  const service = useQuery({
    queryKey: ["settings"],
    queryFn: () =>
      api<{ settings: Settings; credentials_present: Record<string, boolean> }>(
        "/settings",
      ),
  });
  useEffect(() => {
    if (!id) return;
    localStorage.setItem("project", id);
    const stream = new EventSource("/api/projects/" + id + "/events");
    stream.addEventListener("project", (event) => {
      const p = JSON.parse((event as MessageEvent).data) as Project;
      cache.setQueryData(["project", id], p);
      cache.invalidateQueries({ queryKey: ["projects"] });
    });
    return () => stream.close();
  }, [id, cache]);
  const action = useMutation({
    mutationFn: async (fn: () => Promise<unknown>) => fn(),
    onError: (e) => setError(e.message),
    onSuccess: () => {
      setError("");
      cache.invalidateQueries({ queryKey: ["project", id] });
      cache.invalidateQueries({ queryKey: ["projects"] });
    },
  });
  const execute = (fn: () => Promise<unknown>) => action.mutate(fn);
  const p = current.data;
  const latest = p?.boards?.at(-1);
  const b =
    version === null ? latest : p?.boards.find((x) => x.version === version);
  const historical = !!b && b.version !== latest?.version;
  const running = p?.status === "running";
  const isMock =
    service.data &&
    Object.values(service.data.settings.bindings).some(
      (binding) =>
        service.data!.settings.providers.find((p) => p.id === binding.provider)
          ?.adapter === "mock",
    );
  const referenceSet = p?.asset_sets?.at(-1);
  const stage =
    p?.status === "complete"
      ? 6
      : referenceSet && !referenceSet.locked
        ? assetStep(referenceSet)
        : !latest
          ? assetStep(referenceSet)
          : running
            ? latest.approved
              ? 5
              : 3
            : 4;
  async function requestQuote(phase: string, shotID = "") {
    const q = await api<Quote>(`/projects/${id}/quote`, {
      stage: phase,
      instruction:
        instruction || (shotID ? "重新制作此镜头，保留故事与其他镜头。" : ""),
      shot_id: shotID,
    });
    setUnknown(false);
    setQuote(q);
  }
  async function upload(file: File, kind: string) {
    const data = new FormData();
    data.append("file", file);
    data.append("kind", kind);
    const res = await fetch(`/api/projects/${id}/upload`, {
      method: "POST",
      body: data,
    });
    const out = await res.json();
    if (!res.ok) throw new Error(out.error);
  }
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <a className="brand" href="#" onClick={(e) => e.preventDefault()}>
          <span className="brand-symbol">
            <Film size={23} />
          </span>
          <span>
            映序<span className="brand-en">LYPIT STUDIO</span>
          </span>
        </a>
        <div className="workspace-label">
          个人工作空间 <span>LOCAL</span>
        </div>
        <button
          className="new-project"
          onClick={() => {
            setID("");
            setVersion(null);
            setPage("studio");
            localStorage.removeItem("project");
          }}
        >
          <Plus size={16} /> 新建短片
        </button>
        <div className="nav-caption">工作台</div>
        <button
          className={"nav-item " + (page === "studio" ? "selected" : "")}
          onClick={() => setPage("studio")}
        >
          <Clapperboard size={17} /> 我的创作{" "}
          <span>{projects.data?.length ?? 0}</span>
        </button>
        <button
          className={"nav-item " + (page === "settings" ? "selected" : "")}
          onClick={() => setPage("settings")}
        >
          <Settings2 size={17} /> 模型服务
        </button>
        <div className="nav-caption recent-heading">最近的项目</div>
        <div className="project-list">
          {projects.data?.map((item) => (
            <button
              key={item.id}
              className={"project-link " + (item.id === id ? "active" : "")}
              onClick={() => {
                setID(item.id);
                setVersion(null);
                setPage("studio");
              }}
            >
              <span className={"status-dot " + item.status} />
              <span>{item.boards?.at(-1)?.title ?? item.idea}</span>
            </button>
          ))}
        </div>
        <div className="sidebar-bottom">
          <span className="avatar">我</span>
          <div>
            本机创作者<small>文件与项目保存在本地</small>
          </div>
          <span className="online-dot" />
        </div>
      </aside>
      <main>
        <header className="topbar">
          <div>
            个人空间 <span>/</span>{" "}
            {page === "settings" ? "模型服务" : "创作工作台"}
          </div>
          <div className="local-badge">
            <span className="online-dot" /> 本地运行{" "}
            <span className="keycap">V 0.1</span>
          </div>
        </header>
        {
          <div
            className={
              "workspace " + (!p ? "workspace-home" : "workspace-project")
            }
          >
            <div className="page-heading">
              <div>
                <div className="eyebrow">AI FILM WORKSPACE</div>
                <h1>{p ? "分镜与制作" : "今天，想讲一个什么故事？"}</h1>
                <p>
                  {p
                    ? "在这里审核画面、调整镜头，让故事成为成片。"
                    : "从一句灵感开始，让 AI 完成分镜、配音和剪辑。"}
                </p>
              </div>
              <span className="spec-badge">
                9:16 竖屏 <i /> 约 60 秒 <i /> 中文旁白
              </span>
            </div>
            {isMock && (
              <div className="demo-banner">
                <Sparkles size={16} />
                <span>
                  模拟模式 ·
                  验证创作流程，图片为本地示意，旁白为静音占位，不产生模型费用。
                </span>
                <button onClick={() => setPage("settings")}>
                  配置真实模型 <ArrowRight size={14} />
                </button>
              </div>
            )}
            <div className="steps">
              {stages.map((s, i) => (
                <div
                  className={
                    "step " +
                    (i === stage ? "current" : i < stage ? "done" : "")
                  }
                  key={s}
                >
                  <span>
                    {i < stage ? (
                      <Check size={14} />
                    ) : (
                      String(i + 1).padStart(2, "0")
                    )}
                  </span>
                  {s}
                  {i < stages.length - 1 && <div className="step-line" />}
                </div>
              ))}
            </div>
            {!p ? (
              <div className="creation-layout">
                <section className="creation-panel">
                  <div className="panel-label">
                    <span>
                      <WandSparkles size={18} /> 描述你的故事
                    </span>
                    <small>灵感输入</small>
                  </div>
                  <textarea
                    aria-label="故事想法"
                    className="idea-input"
                    placeholder="描述你想讲的故事，例如：一只戴着绿色围巾的小猫，第一次独自去城市旅行，最后找到了属于自己的温暖……"
                    value={idea}
                    onChange={(e) => setIdea(e.target.value)}
                  />
                  <label className="style-choice">
                    视觉风格
                    <select
                      aria-label="新片风格"
                      value={styleChoice}
                      onChange={(e) => setStyleChoice(e.target.value)}
                    >
                      <option value="">写实电影感 · 尊重故事明确要求</option>
                      <option value="写实电影感">写实电影感</option>
                      <option value="精致三维动画">精致三维动画</option>
                      <option value="水彩插画">水彩插画</option>
                    </select>
                  </label>
                  <div className="idea-footer">
                    <span>写下角色、场景，或一个想传达的情绪。</span>
                    <button
                      className="primary"
                      disabled={!idea.trim() || action.isPending}
                      onClick={() =>
                        execute(async () => {
                          const project = await api<Project>("/projects", {
                            idea,
                            visual: { style: styleChoice },
                          });
                          setID(project.id);
                          setIdea("");
                        })
                      }
                    >
                      开始我的短片 <ArrowRight size={16} />
                    </button>
                  </div>
                  <div className="inspiration">
                    <span>试试这些灵感</span>
                    {[
                      "一只小猫的城市冒险",
                      "雨夜便利店的温暖相遇",
                      "机器人寻找春天",
                    ].map((x) => (
                      <button key={x} onClick={() => setIdea(x)}>
                        {x} <Plus size={12} />
                      </button>
                    ))}
                  </div>
                </section>
                <aside className="creative-guide">
                  <div className="guide-heading">
                    <span className="guide-icon">
                      <Clapperboard size={20} />
                    </span>
                    <span>
                      你的 AI 制片助理<small>从灵感到成片，一起完成</small>
                    </span>
                  </div>
                  <div className="storyboard-illustration" aria-hidden="true">
                    <div className="illustration-label">
                      <span /> STORYBOARD
                    </div>
                    <div className="illustration-frames">
                      {[1, 2, 3].map((n) => (
                        <div
                          className={"illustration-frame frame-" + n}
                          key={n}
                        >
                          <span className="frame-scene" />
                          <span className="frame-index">0{n}</span>
                        </div>
                      ))}
                    </div>
                    <div className="illustration-timeline">
                      <span />
                      <span />
                      <span />
                      <i />
                    </div>
                  </div>
                  <dl className="film-specs">
                    <div>
                      <dt>成片时长</dt>
                      <dd>约 60 秒</dd>
                    </div>
                    <div>
                      <dt>画面比例</dt>
                      <dd>9:16 竖屏</dd>
                    </div>
                    <div>
                      <dt>声音与字幕</dt>
                      <dd>中文旁白 · 自动字幕</dd>
                    </div>
                  </dl>
                  <div className="guide-note">
                    <Check size={15} />
                    <span>先看完整分镜，满意后再生成视频。</span>
                  </div>
                </aside>
              </div>
            ) : (
              <>
                <section className="project-summary">
                  <div>
                    <span className="eyebrow">YOUR STORY</span>
                    <h2>{latest?.title ?? "一个新故事，正在等待发生"}</h2>
                    <p>{p.idea}</p>
                  </div>
                  <span className={"pill " + p.status}>
                    {statusLabel[p.status] ?? p.status}
                  </span>
                </section>
                <div className="project-toolbar">
                  <div className="upload-tools">
                    <label
                      className={
                        "button secondary " + (running ? "disabled" : "")
                      }
                    >
                      <ImagePlus size={16} /> 上传已有参考图{" "}
                      {p.references?.length ?? 0}/3
                      <input
                        type="file"
                        accept="image/png,image/jpeg"
                        disabled={running}
                        onChange={(e) => {
                          const f = e.target.files?.[0];
                          if (f) execute(() => upload(f, "reference"));
                          e.target.value = "";
                        }}
                      />
                    </label>
                    <label
                      className={
                        "button secondary " + (running ? "disabled" : "")
                      }
                    >
                      ♫ {p.music ? "音乐已上传" : "背景音乐（可选）"}
                      <input
                        type="file"
                        accept="audio/*"
                        disabled={running}
                        onChange={(e) => {
                          const f = e.target.files?.[0];
                          if (f) execute(() => upload(f, "music"));
                          e.target.value = "";
                        }}
                      />
                    </label>
                    {p.references?.map((ref) => (
                      <img
                        className="reference-thumb"
                        key={ref}
                        src={media(ref)}
                        alt="已上传的参考图"
                      />
                    ))}
                  </div>
                  {running && (
                    <button
                      className="secondary"
                      onClick={() =>
                        execute(() => api(`/projects/${id}/cancel`, {}))
                      }
                    >
                      <Pause size={15} /> 暂停新增任务
                    </button>
                  )}
                  {["attention", "interrupted", "cancelled"].includes(
                    p.status,
                  ) && (
                    <button
                      className="secondary"
                      disabled={action.isPending}
                      onClick={() =>
                        execute(() => api(`/projects/${id}/resume`, {}))
                      }
                    >
                      恢复已确认任务
                    </button>
                  )}
                </div>
                {p.error && (
                  <div className="error-banner">
                    {p.error}
                    <small>
                      已成功的素材会保留。结果未知时先核对供应商记录，恢复不会盲目重提。
                    </small>
                  </div>
                )}
                {running && (
                  <div className="progress-banner">
                    <LoaderCircle className="spin" size={18} />
                    <div>
                      AI 正在制作你的故事
                      <small>
                        {p.tasks.filter((t) => t.state === "succeeded").length}{" "}
                        个步骤已完成 ·{" "}
                        {p.tasks
                          .filter((t) => t.state === "running")
                          .map((t) => t.message)
                          .join(" / ") || "正在准备"}
                      </small>
                    </div>
                    <span>可以离开页面，稍后回来查看</span>
                  </div>
                )}
                <AssetWorkflow
                  project={p}
                  disabled={action.isPending}
                  act={execute}
                  onQuote={requestQuote}
                />
                {b && (
                  <>
                    <div className="section-heading">
                      <div>
                        <h2>
                          故事分镜 <span>{b.shots.length} 个镜头</span>
                        </h2>
                        <p>{b.synopsis}</p>
                      </div>
                      <select
                        aria-label="分镜版本"
                        value={b.version}
                        onChange={(e) => setVersion(Number(e.target.value))}
                      >
                        {p.boards.map((x) => (
                          <option key={x.version} value={x.version}>
                            版本 {x.version}
                            {x.approved ? " · 已审核" : ""}
                          </option>
                        ))}
                      </select>
                    </div>
                    {!referenceSet && (
                      <div className="visual-edit">
                        <input
                          aria-label="项目视觉风格"
                          value={styleEdit}
                          onChange={(e) => setStyleEdit(e.target.value)}
                          placeholder={p.visual?.style ?? "写实电影感"}
                        />
                        <button
                          className="secondary"
                          disabled={
                            running ||
                            historical ||
                            action.isPending ||
                            !styleEdit.trim()
                          }
                          onClick={() =>
                            execute(async () => {
                              await api(
                                `/projects/${id}/visual`,
                                {
                                  revision: p.revision,
                                  visual: { ...p.visual, style: styleEdit },
                                },
                                "PUT",
                              );
                              setStyleEdit("");
                            })
                          }
                        >
                          更新风格 · 标记待生成
                        </button>
                      </div>
                    )}{" "}
                    {b.schema < 3 && (
                      <BoardAssets
                        onRegenerate={(assetID) =>
                          execute(() =>
                            requestQuote("fill", "asset:" + assetID),
                          )
                        }
                        project={p}
                        board={b}
                        disabled={!!running || historical || action.isPending}
                        act={execute}
                      />
                    )}
                    <div className="shot-grid">
                      {b.shots.map((shot, i) => (
                        <article className="shot-card" key={shot.id}>
                          <div className="shot-image">
                            {shot.video ? (
                              <video
                                src={media(shot.video)}
                                poster={
                                  shot.image ? media(shot.image) : undefined
                                }
                                controls
                                preload="none"
                              />
                            ) : shot.image ? (
                              <img src={media(shot.image)} alt={shot.title} />
                            ) : (
                              <div className="image-placeholder">
                                <Film size={30} />
                                <span>等待画面生成</span>
                              </div>
                            )}
                            <span className="shot-number">
                              {String(i + 1).padStart(2, "0")}
                            </span>
                            <span className="duration">{shot.duration}s</span>
                          </div>
                          <div className="shot-content">
                            <div className="shot-title">
                              <h3>{shot.title}</h3>
                              <span>
                                {shot.transition === "dissolve"
                                  ? "柔和叠化"
                                  : shot.transition === "match"
                                    ? "动作匹配"
                                    : "直接切换"}
                              </span>
                            </div>
                            <p className="narration">“{shot.narration}”</p>
                            <ShotInspector
                              onRedoVideo={() =>
                                execute(() => requestQuote("video", shot.id))
                              }
                              project={p}
                              board={b}
                              shot={shot}
                              disabled={
                                !!running || historical || action.isPending
                              }
                              act={execute}
                            />
                            <details>
                              <summary>
                                镜头提示词 <ChevronDown size={13} />
                              </summary>
                              <small>画面提示词</small>
                              <p>{shot.image_prompt}</p>
                              <small>视频提示词</small>
                              <p>{shot.video_prompt}</p>
                              <small>场景与动作</small>
                              <p>
                                {shot.scene} · {shot.action}
                              </p>
                            </details>
                            {shot.warning && (
                              <small className="warning">{shot.warning}</small>
                            )}
                            <button
                              className="text-button"
                              disabled={
                                running || historical || action.isPending
                              }
                              onClick={() => {
                                setTarget(shot.id);
                                document.getElementById("revision")?.focus();
                              }}
                            >
                              调整这个镜头 <ArrowRight size={13} />
                            </button>
                          </div>
                        </article>
                      ))}
                    </div>
                    {!historical && !running && (
                      <section className="review-panel">
                        <div>
                          <Sparkles size={20} />
                          <div>
                            <h3>
                              {b.approved
                                ? "分镜已审核，准备让画面动起来"
                                : "看看这个故事，是否符合你的想象？"}
                            </h3>
                            <p>
                              先比较候选和处理审核意见，再确认视频。评分仅用于相对比较。
                            </p>
                          </div>
                        </div>
                        <div className="revision-tools">
                          <button
                            className="text-button"
                            disabled={action.isPending}
                            onClick={() =>
                              execute(async () => {
                                const result = await api<{
                                  affected: string[];
                                }>(`/projects/${id}/impact`, {
                                  shot_id: target,
                                  instruction,
                                });
                                setImpact(result.affected);
                              })
                            }
                          >
                            预览修改影响范围
                          </button>
                          <button
                            className="text-button"
                            disabled={action.isPending || !instruction.trim()}
                            onClick={() =>
                              execute(async () => {
                                await api(`/projects/${id}/prompts`, {
                                  version: b.version,
                                  revision: p.revision,
                                  shot_id: target,
                                  instruction,
                                });
                                setInstruction("");
                                setImpact([]);
                              })
                            }
                          >
                            仅更新提示词 · 不生成图片
                          </button>
                          <button
                            className="secondary"
                            disabled={action.isPending}
                            onClick={() =>
                              execute(() => requestQuote("fill", target))
                            }
                          >
                            补生成 / 重做选中镜头 · 先报价
                          </button>
                        </div>
                        {impact.length > 0 && (
                          <p className="impact-notice">
                            预计涉及：
                            {impact
                              .map(
                                (shotID) =>
                                  b.shots.find((q) => q.id === shotID)?.title ??
                                  shotID,
                              )
                              .join("、")}
                            。未提交生成；全片修改范围按全部镜头预览。
                          </p>
                        )}
                        <div className="revision-row">
                          <input
                            id="revision"
                            value={instruction}
                            onChange={(e) => setInstruction(e.target.value)}
                            placeholder={
                              target
                                ? `修改 ${b.shots.find((q) => q.id === target)?.title}，例如：换成雨天`
                                : "例如：第二幕换成夜晚，结尾再温暖一点"
                            }
                          />
                          {target && (
                            <button
                              className="secondary"
                              onClick={() => setTarget("")}
                            >
                              取消选镜
                            </button>
                          )}
                          <button
                            className="secondary"
                            disabled={
                              action.isPending ||
                              (!instruction.trim() && !target)
                            }
                            onClick={() =>
                              execute(() => requestQuote("storyboard", target))
                            }
                          >
                            调整分镜
                          </button>
                          <button
                            className="primary"
                            disabled={
                              action.isPending ||
                              b.shots.some(
                                (q) =>
                                  !q.image ||
                                  q.stale ||
                                  (b.schema >= 2 &&
                                    !q.candidates?.some(
                                      (c) =>
                                        c.id === q.selected &&
                                        (c.review?.state === "passed" ||
                                          !!c.review?.accepted),
                                    )),
                              )
                            }
                            onClick={() =>
                              execute(async () => {
                                if (!b.approved)
                                  await api(`/projects/${id}/approve`, {
                                    version: b.version,
                                  });
                                await requestQuote("video");
                              })
                            }
                          >
                            <Check size={16} />
                            {b.approved
                              ? "查看视频生成报价"
                              : "确认分镜，制作成片"}
                          </button>
                        </div>
                      </section>
                    )}
                  </>
                )}
                {p.output && (
                  <section className="delivery">
                    <div>
                      <span className="eyebrow">IT’S A WRAP</span>
                      <h2>你的故事，现在可以播放了。</h2>
                      <p>视频、分镜和提示词，都为你保留。</p>
                      <a
                        className="button primary"
                        href={media(p.output)}
                        download
                      >
                        <Download size={16} /> 下载 MP4
                      </a>
                      <a
                        className="button secondary"
                        href={`/api/projects/${id}/export`}
                      >
                        <Download size={16} /> 导出制作素材
                      </a>
                    </div>
                    <video src={media(p.output)} controls preload="metadata" />
                  </section>
                )}
                {p.tasks.length > 0 && (
                  <details className="task-log">
                    <summary>
                      制作记录 · {p.tasks.length} 个步骤{" "}
                      <ChevronDown size={15} />
                    </summary>
                    {[...p.tasks].reverse().map((t) => (
                      <div key={t.id}>
                        <span className={"status-dot " + t.state} />
                        <span>{t.message}</span>
                        <small>{statusLabel[t.state] ?? t.state}</small>
                      </div>
                    ))}
                    <p>
                      实际用量：
                      {
                        Object.values(p.attempts).filter((a) => a.usage).length
                      }{" "}
                      个请求返回用量；未返回的费用不推算。供应商实际账单为准。
                    </p>
                    {Object.entries(p.attempts).map(([key, a]) => (
                      <details key={key} className="usage-row">
                        <summary>
                          {kindLabels[a.kind] ?? a.kind} ·{" "}
                          {statusLabel[a.state] ?? a.state} ·{" "}
                          {a.fee === null
                            ? "实际费用未知"
                            : `估算 ¥${a.fee.toFixed(4)}`}
                        </summary>
                        <p>{a.detail}</p>
                        <pre>
                          {JSON.stringify(
                            a.usage ?? { message: "用量未返回" },
                            null,
                            2,
                          )}
                        </pre>
                      </details>
                    ))}
                  </details>
                )}
              </>
            )}
            {!p && (
              <div className="feature-grid">
                {[
                  [
                    Sparkles,
                    "AI 负责创作",
                    "角色、分镜、提示词，自动成套生成。",
                  ],
                  [
                    CirclePlay,
                    "你来决定方向",
                    "先看完整分镜，满意后再生成视频。",
                  ],
                  [
                    Clapperboard,
                    "自动剪辑交付",
                    "旁白、字幕、镜头衔接，一起完成。",
                  ],
                ].map(([Icon, title, desc]) => {
                  const I = Icon as typeof Sparkles;
                  return (
                    <div className="feature" key={String(title)}>
                      <I size={21} />
                      <h3>{String(title)}</h3>
                      <p>{String(desc)}</p>
                    </div>
                  );
                })}
              </div>
            )}
            <footer>
              映序 LYPIT <span>让灵感，有迹可循。</span>
            </footer>
          </div>
        }
      </main>
      {(error || current.error || projects.error || service.error) && (
        <div className="toast" role="alert">
          {error ||
            current.error?.message ||
            projects.error?.message ||
            service.error?.message}
          <button aria-label="关闭提示" onClick={() => setError("")}>
            <X size={16} />
          </button>
        </div>
      )}
      {page === "settings" && (
        <ServiceSettings onError={setError} onClose={() => setPage("studio")} />
      )}
      {quote && (
        <div className="modal-backdrop">
          <section
            className="quote-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="quote-title"
          >
            <div className="modal-header">
              <div>
                <span className="eyebrow">REVIEW BEFORE GENERATION</span>
                <h2 id="quote-title">
                  {quoteStageLabels[quote.stage] ?? "确认本次制作"}
                </h2>
              </div>
              <button
                className="icon-button"
                aria-label="关闭报价"
                onClick={() => setQuote(null)}
              >
                <X size={22} />
              </button>
            </div>
            <div className="quote-total">
              <span>本批预估费用</span>
              <strong>{money(quote.estimate)}</strong>
              <small>最终按实际用量核算，估价不等于账户账单。</small>
            </div>
            <p className="quote-notice">{quote.notice}</p>
            <div className="quote-requests">
              {quote.requests.map((r, i) => (
                <details key={i} open={quote.requests.length < 4}>
                  <summary>
                    <span>
                      {quoteRequestLabel(quote.stage, r)} · {r.model}
                    </span>
                    <span>
                      {r.estimate === null ? "未知" : money(r.estimate)}
                    </span>
                  </summary>
                  <p>
                    供应商：{r.provider} · 时长：{r.duration || "不适用"} ·
                    数量：
                    {r.count ?? 1}
                    {r.conditional ? "（条件执行，未触发不提交）" : ""}
                  </p>
                  <div className="full-prompt">{r.prompt}</div>
                  <p>
                    实际 API 参考素材：
                    {r.kind === "vision" &&
                    ["character_masters", "reference_assets"].includes(
                      quote.stage,
                    )
                      ? "本阶段生成或已有的待审核图片1张；额外参考："
                      : ""}
                    {r.references?.length ? r.references.join("、") : "无"}
                  </p>
                  <pre>{JSON.stringify(r.parameters, null, 2)}</pre>
                </details>
              ))}
            </div>
            {quote.estimate === null && (
              <label className="unknown-check">
                <input
                  type="checkbox"
                  checked={unknown}
                  onChange={(e) => setUnknown(e.target.checked)}
                />{" "}
                我理解费用上界未知，同意本批按供应商实际用量计费。
              </label>
            )}
            <div className="modal-footer">
              <button className="secondary" onClick={() => setQuote(null)}>
                返回检查
              </button>
              <button
                className="primary"
                disabled={
                  action.isPending || (quote.estimate === null && !unknown)
                }
                onClick={() =>
                  execute(async () => {
                    await api(`/projects/${id}/confirm`, {
                      quote_id: quote.id,
                      allow_unknown: unknown,
                    });
                    setQuote(null);
                    setInstruction("");
                    setTarget("");
                    setVersion(null);
                  })
                }
              >
                {action.isPending ? (
                  <LoaderCircle className="spin" size={16} />
                ) : (
                  <Check size={16} />
                )}{" "}
                确认本批生成
              </button>
            </div>
          </section>
        </div>
      )}
    </div>
  );
}
