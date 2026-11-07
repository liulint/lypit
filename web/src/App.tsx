import {ReferenceImages, ReferencePlanImages} from "./ReferenceImages";
import { BoardPlanReview } from "./BoardPlanReview";
import { ZoomImage } from "./ZoomImage";
import { VideoUsageDetails } from "./VideoUsage";
import { TokenUsage } from "./TokenUsage";
import { TopNotifications } from "./TopNotifications";
import {ShotContentDialog} from "./ShotContentDialog";
import { ShotDurationDialog } from "./ShotDurationDialog";
import { TaskActions, type TaskAction } from "./TaskActions";
import { EditImageDialog, ImageHistoryDialog, type EditImage } from "./ShotImageDialogs";
import { videoModeNames, videoCurrent, type VideoMode } from "./api";
import {
  useEffect,
  useRef,
  useState,
  useId,
  lazy,
  Suspense,
  type ReactNode,
} from "react";
import {
  Alert,
  Button as AntButton,
  Dropdown,
  Popover,
  Tag,
  Tooltip,
  Input,
  InputNumber,
  Select,
  Checkbox,
  Progress,
  Radio,
  Form,
  App as AntApp,
  Modal as AntModal,
} from "antd";
import { Button, Badge, Modal, Field } from "./ui";
import { videoSpec, type AspectRatio } from "./video-spec";
import {
  Aperture,
  ArrowDownToLine,
  ArrowLeft,
  BookOpen,
  Check,
  CheckCircle2,
  ChevronRight,
  Clapperboard,
  Clock3,
  Coins,
  Film,
  FolderOpen,
  Layers3,
  Loader2,
  LockKeyhole,
  Monitor,
  MoreHorizontal,
  Play,
  Pencil,
  Plus,
  Search,
  Settings2,
  Sparkles,
  X,
} from "lucide-react";
import {
  api,
  blocked,
  media,
  stageNames,
  statusNames,
  type Asset,
  type Project,
  type Quote,
  type Settings,
  type Shot,
  type Task,
  type BoardPreview,
  type ShotDurationPolicy,
} from "./api";

const ServiceSettings = lazy(() => import("./ServiceSettings"));
const PromptSettings = lazy(() => import("./PromptSettings"));

type Page = "projects" | "workspace" | "tasks" | "usage" | "settings" | "vendors" | "prompts";
const styles = [
  "温暖电影感",
  "二维手绘动画",
  "真人写实",
  "国风水墨",
  "三维动画",
  "黏土定格",
];
const tabs = [
  { name: "故事与剧本", icon: BookOpen },
  { name: "参考素材", icon: Layers3 },
  { name: "分镜审片", icon: Clapperboard },
  { name: "视频与导出", icon: Film },
];
const errorText = (e: unknown) =>
  e instanceof Error ? e.message : "操作失败，请重试";
function Empty({
  icon,
  title,
  description,
  children,
}: {
  icon: ReactNode;
  title: string;
  description: string;
  children?: ReactNode;
}) {
  return (
    <div className="empty">
      <div className="empty-icon">{icon}</div>
      <h2>{title}</h2>
      <p>{description}</p>
      {children}
    </div>
  );
}
function projectStep(p: Project) {
  return p.output
    ? 4
    : p.boards_locked
      ? 3
      : p.assets_locked
        ? 2
        : p.analyzed
          ? 1
          : 0;
}
export default function App() {
  const { modal: confirmModal, message } = AntApp.useApp();
  const [page, setPageRaw] = useState<Page>("projects");
  const settingsDirty = useRef(false);
  function setPage(next: Page) {
    if (next === page) return;
    if (settingsDirty.current) {
      confirmModal.confirm({title: "有未保存的配置", content: "离开将放弃当前修改。", okText: "放弃并离开", cancelText: "继续编辑", onOk: () => { settingsDirty.current = false; setPageRaw(next); }});
    } else setPageRaw(next);
  }
  const [id, setID] = useState("");
  const [tab, setTab] = useState(0);
  const [projects, setProjects] = useState<Project[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [settings, setSettings] = useState<Settings>();
  const [vendorCapabilities, setVendorCapabilities] = useState<Record<string, string[]>>({});
  const [credentials, setCredentials] = useState<Record<string, boolean>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const [showNew, setShowNew] = useState(false);
  const [quote, setQuote] = useState<Quote>();
  const [taskPanelOpen,setTaskPanelOpen]=useState(false);
  const [taskShotID,setTaskShotID]=useState("");
  const [contentShot,setContentShot]=useState<{id:string;trial:boolean;duration?:number;taskID?:string}>();
  const [durationShot,setDurationShot]=useState<{id:string;trial:boolean}>();
  const [durationPolicy,setDurationPolicy]=useState<ShotDurationPolicy>();
  const [unknown, setUnknown] = useState(false);
  const [replacementAccepted, setReplacementAccepted] = useState(false);
  const [search, setSearch] = useState("");
  const [archived, setArchived] = useState(false);
  const requestSeq = useRef(0);
  const mounted = useRef(true);
  async function refresh() {
    const seq = ++requestSeq.current;
    const [ps, ts, cfg] = await Promise.all([
      api<Project[]>("/projects"),
      api<Task[]>("/tasks"),
      api<{ settings: Settings; credentials: Record<string, boolean>; vendor_capabilities?: Record<string, string[]>;video_duration_policy?:ShotDurationPolicy }>(
        "/settings",
      ),
    ]);
    if (mounted.current && seq === requestSeq.current) {
      setProjects(ps);
      setTasks(ts);
      // Polling must not discard an in-progress configuration draft.
      if (!settingsDirty.current) {
        setSettings(cfg.settings);
        setDurationPolicy(cfg.video_duration_policy);
        setCredentials(cfg.credentials);
        setVendorCapabilities(cfg.vendor_capabilities || {});
      }
      setLoading(false);
    }
  }
  useEffect(() => {
    mounted.current = true;
    let timer: ReturnType<typeof setTimeout>;
    let stop = false;
    async function poll() {
      try {
        await refresh();
      } catch (e) {
        if (!stop) {
          setError(errorText(e));
          setLoading(false);
        }
      }
      if (!stop) timer = setTimeout(poll, 2500);
    }
    void poll();
    return () => {
      mounted.current = false;
      stop = true;
      clearTimeout(timer);
    };
  }, []);
  async function act(fn: () => Promise<unknown>) {
    if (pending) return;
    setPending(true);
    setError("");
    try {
      await fn();
      await refresh();
    } catch (e) {
      setError(errorText(e));
      void message.error(errorText(e));
    } finally {
      setPending(false);
    }
  }
  function handleTaskAction(task:Task,action:TaskAction) {
    return act(()=>api(`/tasks/${task.id}/${action}`,{}));
  }
  function handleTasks(shotID="") { setTaskShotID(shotID); setTaskPanelOpen(true); }
  const project = projects.find((p) => p.id === id);
  const active = tasks.filter(blocked);
  const currentTasks = tasks.filter((t) => t.project_id === id);
  const busy = currentTasks.some(blocked) || pending || !!project?.archived;
  function openProject(p: Project) {
    setTaskPanelOpen(false);
    setDurationShot(undefined);
    setContentShot(undefined);
    setID(p.id);
    setPage("workspace");
    setTab(Math.min(3, projectStep(p)));
    setError("");
  }
  async function requestQuote(stage: string, retry?: {shot_id: string; feedback: string; video_mode?: VideoMode; end_shot_id?: string; replaces_task_id?: string; reference_ids?: string[]; preserve?:string; plan_trial?:boolean;target_duration?:number}, savedProject?:Project) {
    const quoteProject = savedProject ?? project;
    if (!quoteProject) return false;
    let quoted=false;
    await act(async () => {
      const q = await api<Quote>(`/projects/${quoteProject.id}/quotes`, {
        stage,
        ...retry,
        revision: quoteProject.revision,
      });
      if (q.mode==="remote" && ["boards","board_retry"].includes(q.stage) && (q.reference_policy_version!==4 || (q.stage==="board_retry" && q.image_reference_plan?.version!==4) || (q.stage==="boards" && !q.shot_reference_plans))) {
        throw new Error("后端尚未提供有效的参考板报价方案，请更新后端后重新报价；未提交生成。");
      }
      if(q.stage==="board_edit"){
        const target=quoteProject.shots.find(s=>s.id===retry?.shot_id);const base=target?.candidates[target.selected];
        if(!q.image_edit || q.reference_policy_version!==3 || q.image_reference_plan?.version!==3 || q.image_reference_plan.boards[0]?.kind!=="edit_base" || q.image_reference_plan.boards[0]?.cells.length!==1 || q.image_reference_plan.boards.length>5 || q.image_edit.base.file!==base?.file || q.image_edit.base.hash!==base?.hash){throw new Error("编辑报价未绑定当前独立底图，请刷新后重新报价；未调用模型。");}
      }
      if (retry?.reference_ids && JSON.stringify(q.image_edit ? q.image_edit.auxiliary_ids : q.reference_ids)!==JSON.stringify(retry.reference_ids)) {
        throw new Error("报价未采用你选择的参考素材，请刷新后重新报价；未提交生成。");
      }
      if (!!q.plan_trial !== !!retry?.plan_trial) throw new Error("报价的单镜试做范围不一致，请更新后端并重新报价；未提交生成。");
      setQuote(q);
      setUnknown(false);
      setReplacementAccepted(false);
      quoted=true;
    });
    return quoted;
  }
  const stats = [
    {
      label: "全部作品",
      value: projects.filter((p) => !p.archived).length,
      icon: FolderOpen,
    },
    {
      label: "制作中",
      value: tasks.filter((t) => ["running", "queued"].includes(t.status))
        .length,
      icon: Clapperboard,
    },
    {
      label: "已完成",
      value: projects.filter((p) => !!p.output && !p.archived).length,
      icon: CheckCircle2,
    },
  ];
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <a
          className="brand"
          href="#"
          onClick={(e) => {
            e.preventDefault();
            setPage("projects");
          }}
        >
          <span className="brand-mark">
            <Aperture size={27} />
          </span>
          <span>
            映序<small>LYPIT STUDIO</small>
          </span>
        </a>
        <nav>
          <button
            className={
              page === "projects" || page === "workspace" ? "active" : ""
            }
            onClick={() => setPage("projects")}
          >
            <FolderOpen size={18} />
            我的作品<span>{projects.filter((p) => !p.archived).length}</span>
          </button>
          <button
            className={page === "tasks" ? "active" : ""}
            onClick={() => setPage("tasks")}
          >
            <Clock3 size={18} />
            任务中心{active.length > 0 && <span>{active.length}</span>}
          </button>
          <button className={page === "usage" ? "active" : ""} onClick={() => setPage("usage")}><Coins size={18} />Token 用量</button>
          <button className={page === "prompts" ? "active" : ""} onClick={() => setPage("prompts")}><BookOpen size={18} />提示词管理</button>
          <button className={page === "vendors" ? "active" : ""} onClick={() => setPage("vendors")}>
            <Settings2 size={18} />厂商管理
          </button>
          <button
            className={page === "settings" ? "active" : ""}
            onClick={() => setPage("settings")}
          >
            <Settings2 size={18} />
            服务配置
          </button>
        </nav>
        <div className="sidebar-projects">
          <div className="sidebar-heading">
            最近的作品
            <button aria-label="新建作品" onClick={() => setShowNew(true)}>
              <Plus size={16} />
            </button>
          </div>
          {projects
            .filter((p) => !p.archived)
            .slice(0, 7)
            .map((p, i) => (
              <button
                key={p.id}
                className={
                  page === "workspace" && id === p.id ? "selected" : ""
                }
                onClick={() => openProject(p)}
              >
                <span className={`project-dot dot-${i % 3}`} />
                <span>{p.title}</span>
              </button>
            ))}
          {projects.length === 0 && (
            <p className="sidebar-hint">你的第一个故事，从这里开始。</p>
          )}
        </div>
        <div className="sidebar-footer">
          <div className="profile">
            <div>映</div>
            <span>
              创作者<small>个人工作空间</small>
            </span>
            <Monitor size={17} />
          </div>
        </div>
      </aside>
      <div className="main-shell">
        <header className={`topbar ${page === "workspace" && project ? "topbar-workspace" : ""}`}>
          <div className="topbar-context">
            {page === "workspace" && project ? (
              <>
                <AntButton type="text" size="small" className="project-back" onClick={() => setPage("projects")} icon={<ArrowLeft size={14} />}>
                  全部作品
                </AntButton>
                <ChevronRight size={14} className="project-context-divider" />
                <h1 className="topbar-project-title" title={project.title}>{project.title}</h1>
                <Badge green={!!project.output}>
                  {project.archived ? "已归档" : project.output ? "成片已就绪" : `第 ${project.revision} 版`}
                </Badge>
                <span className="topbar-project-spec">
                  {project.style} · {videoSpec(project).label} · {videoSpec(project).duration} 秒 · {project.mode === "mock" ? "本地模拟素材" : "真实模型素材"}
                </span>
              </>
            ) : (
              <>
                <span>工作空间</span>
                <ChevronRight size={14} />
                <strong>{page === "prompts" ? "提示词管理" : page === "settings" ? "服务配置" : page === "vendors" ? "厂商管理" : page === "tasks" ? "任务中心" : page === "usage" ? "Token 用量" : "我的作品"}</strong>
              </>
            )}
          </div>
          <div className="topbar-tools">
            {page === "workspace" && project && (
              <div className="inline-actions topbar-project-actions">
                <a
                  className="button"
                  href={`/api/projects/${project.id}/export`}
                >
                  <ArrowDownToLine size={15} />
                  导出项目
                </a>
                <Button
                  disabled={pending || currentTasks.some(blocked)}
                  onClick={() =>
                    void act(() =>
                      api(
                        `/projects/${project.id}`,
                        { ...project, archived: !project.archived },
                        "PUT",
                      ),
                    )
                  }
                >
                  {project.archived ? "恢复作品" : "归档"}
                </Button>
              </div>
            )}
            <TopNotifications tasks={tasks} projects={projects} onTasks={() => setPage("tasks")} />
          </div>
        </header>
        <main className={page === "workspace" && project ? "project-workspace bounded-workspace" : page === "projects" ? "projects-page" : undefined}>
          {error && (
            <div className="error" role="alert">
              <span>{error}</span>
              <button aria-label="关闭提示" onClick={() => setError("")}>
                <X size={17} />
              </button>
            </div>
          )}
          {loading ? (
            <Empty
              icon={<Loader2 className="spin" />}
              title="正在打开你的工作台"
              description="读取本机项目与任务记录…"
            />
          ) : (
            <>
              {page === "projects" && (
                <>
                  <div className="stats">
                    {stats.map((st) => (
                      <div key={st.label}>
                        <span className="stat-icon">
                          <st.icon size={20} />
                        </span>
                        <div>
                          <span>{st.label}</span>
                          <strong>
                            {st.value}
                            <small> 部</small>
                          </strong>
                        </div>
                      </div>
                    ))}
                  </div>
                  <section>
                    <div className="section-title">
                      <h2>
                        {archived ? "已归档作品" : "我的作品"}{" "}
                        <span>
                          {
                            projects.filter((p) => p.archived === archived)
                              .length
                          }
                        </span>
                      </h2>
                      <div>
                        <button
                          className="text-button"
                          onClick={() => setArchived(!archived)}
                        >
                          {archived ? "返回作品" : "查看归档"}
                        </button>
                        <div className="search">
                          <Search size={15} />
                          <Input
                            placeholder="搜索作品…"
                            aria-label="搜索作品"
                            value={search}
                            onChange={(e) => setSearch(e.target.value)}
                          />
                        </div>
                      </div>
                    </div>
                    <div className="project-grid">
                      {projects
                        .filter(
                          (p) =>
                            p.archived === archived && p.title.includes(search),
                        )
                        .map((p, i) => (
                          <button
                            className="project-card"
                            key={p.id}
                            onClick={() => openProject(p)}
                          >
                            <div className={`project-cover cover-${i % 3}`}>
                              {p.shots[0]?.candidates[0] ? (
                                <img
                                  src={media(p.shots[0].candidates[0].file)}
                                  alt="分镜预览"
                                />
                              ) : (
                                <div className="cover-illustration">
                                  <div />
                                  <span />
                                  <i />
                                </div>
                              )}
                              <span className="cover-tag">
                                {p.mode === "mock" ? "模拟素材" : "真实服务"}
                              </span>
                              <span className="cover-play">
                                <Play size={18} />
                              </span>
                              <span className="cover-format">
                                {videoSpec(p).aspect} · {videoSpec(p).duration}s
                              </span>
                            </div>
                            <div className="project-card-body">
                              <div>
                                <h3>{p.title}</h3>
                                <MoreHorizontal size={18} />
                              </div>
                              <p>{p.idea}</p>
                              <div className="project-card-footer">
                                <Badge green={!!p.output}>
                                  {p.output
                                    ? "已完成"
                                    : tabs[Math.min(3, projectStep(p))].name}
                                </Badge>
                                <span>
                                  {new Date(p.updated).toLocaleDateString(
                                    "zh-CN",
                                  )}
                                </span>
                              </div>
                            </div>
                          </button>
                        ))}
                      {!archived && !search && (
                        <button
                          className="new-card"
                          onClick={() => setShowNew(true)}
                        >
                          <span>
                            <Plus size={24} />
                          </span>
                          <h3>开始新的创作</h3>
                          <p>把脑海中的故事，变成画面</p>
                        </button>
                      )}
                    </div>
                    {(archived || search) &&
                      projects.filter(
                        (p) =>
                          p.archived === archived && p.title.includes(search),
                      ).length === 0 && (
                        <Empty
                          icon={<FolderOpen />}
                          title="这里还没有作品"
                          description="换个关键词，或开始一个新故事。"
                        />
                      )}
                  </section>
                </>
              )}
              {page === "workspace" && project && (
                <>
                  <div className="stepper">
                    {tabs.map((t, i) => (
                      <button
                        key={t.name}
                        aria-current={tab === i ? "step" : undefined}
                        className={`${tab === i ? "selected" : ""} ${projectStep(project) > i ? "done" : ""}`}
                        onClick={() => setTab(i)}
                      >
                        <span>
                          {projectStep(project) > i ? (
                            <Check size={16} />
                          ) : (
                            <t.icon size={17} />
                          )}
                        </span>
                        <div>
                          <small>STEP 0{i + 1}</small>
                          <strong>{t.name}</strong>
                        </div>
                        {i < 3 && <ChevronRight size={15} />}
                      </button>
                    ))}
                  </div>
                  {project.archived && (
                    <div className="notice">
                      作品已归档，恢复后可以继续编辑和制作。
                    </div>
                  )}
                  <div className="workspace-stage">
                  {tab === 0 && (
                    <Story
                      key={`${project.id}:${project.revision}`}
                      p={project}
                      busy={busy}
                      onSave={(v) =>
                        void act(() => api(`/projects/${project.id}`, v, "PUT"))
                      }
                      onAnalyze={() => void requestQuote("analyze")}
                    />
                  )}
                  {tab === 1 && (
                    <Assets
                      key={`${project.id}:${project.revision}`}
                      p={project}
                      busy={busy}
                      quote={(s) => void requestQuote(s)}
                      save={(v) =>
                        void act(() => api(`/projects/${project.id}`, v, "PUT"))
                      }
                      approve={(a, part) =>
                        void act(() =>
                          api(
                            `/projects/${project.id}/assets/${a.id}/approve`,
                            { revision: project.revision, part },
                          ),
                        )
                      }
                      lock={() =>
                        void act(() =>
                          api(`/projects/${project.id}/assets/lock`, {
                            revision: project.revision,
                          }),
                        )
                      }
                    />
                  )}
                  {tab===2 && currentTasks.filter(t=>t.stage==="shot_rewrite"&&t.status==="completed"&&!t.rewrite?.applied_revision).map(t=><Alert key={t.id} type="info" showIcon title="单镜改写建议已生成，当前分镜尚未改变" action={<AntButton onClick={()=>setContentShot({id:t.shot_id!,trial:!!t.plan_trial,taskID:t.id})}>对比并确认改写</AntButton>}/>)}
                  {tab === 2 && (
                    <Boards
                      task={currentTasks.find(t => ["boards","board_retry"].includes(t.stage) && !t.plan_trial && blocked(t)) ?? currentTasks.find(blocked)}
                      p={project}
                      busy={busy}
                      openTasks={()=>handleTasks()}
                      handleTasks={currentTasks.some(blocked)?handleTasks:undefined}
                      durationPolicy={durationPolicy}
                      editContent={(q,trial)=>setContentShot({id:q.id,trial:!!trial})}
                      adjustDuration={(q,trial)=>setDurationShot({id:q.id,trial:!!trial})}
                      videoBusy={(shotID,trial)=>pending || !!project.archived || videoTaskBusy(currentTasks,shotID,trial,settings?.video_concurrency ?? 2)}
                      imageBusy={(shotID,trial)=>pending || !!project.archived || imageTaskBusy(currentTasks,shotID,trial,settings?.image_concurrency ?? 2)}
                      reviewBusy={pending || !!project.archived || currentTasks.some(t => blocked(t) && !(t.stage === "boards" && ["running", "queued"].includes(t.status)))}
                      quote={() => void requestQuote("board_plan")}
                      imageQuote={() => void requestQuote("boards")}
                      allImages={async(shots)=>{
                        let saved:Project|undefined;
                        await act(async()=>{saved=await api<Project>(`/projects/${project.id}/board-plan`,{revision:project.revision,shots,approve:true},"PUT");});
                        if(saved) await requestQuote("boards",undefined,saved);
                      }}
                      savePlan={(shots,approve)=>void act(()=>api(`/projects/${project.id}/board-plan`,{revision:project.revision,shots,approve},"PUT"))}
                      trialImage={async(shots,q)=>{
                        if(currentTasks.some(blocked) && JSON.stringify(shots)===JSON.stringify(project.planned_shots??[])) {
                          await requestQuote("board_retry",{shot_id:q.id,feedback:"",plan_trial:true});
                          return;
                        }
                        let saved:Project|undefined;
                        await act(async()=>{saved=await api<Project>(`/projects/${project.id}/board-plan`,{revision:project.revision,shots,approve:false},"PUT");});
                        if(saved) await requestQuote("board_retry",{shot_id:q.id,feedback:"",plan_trial:true},saved);
                      }}
                      trialRetry={(q,feedback,reference_ids)=>requestQuote("board_retry",{shot_id:q.id,feedback,reference_ids,plan_trial:true})}
                      trialVideo={(q,feedback,video_mode,end_shot_id)=>requestQuote("shot_video",{shot_id:q.id,feedback,video_mode,end_shot_id,plan_trial:true})}
                      trialChoose={(q,selected,approved,narration)=>void act(()=>api(`/projects/${project.id}/shots/${q.id}`,{revision:project.revision,selected,approved,narration,plan_trial:true},"PUT"))}
                      replacementBusy={pending || !!project.archived || currentTasks.some(t=>blocked(t) && !(["boards","board_retry"].includes(t.stage) && !t.plan_trial && ["failed","interrupted","cancelled"].includes(t.status)))}
                      retry={(q, feedback, reference_ids) => requestQuote("board_retry", {shot_id:q.id, feedback, reference_ids, replaces_task_id:currentTasks.find(t=>["boards","board_retry"].includes(t.stage) && !t.plan_trial && ["failed","interrupted","cancelled"].includes(t.status))?.id})}
                      edit={(q,feedback,preserve,reference_ids)=>requestQuote("board_edit",{shot_id:q.id,feedback,preserve,reference_ids})}
                      video={(q, feedback, video_mode, end_shot_id) => requestQuote("shot_video", {shot_id:q.id, feedback, video_mode, end_shot_id})}
                      choose={(q, selected, approved, narration) =>
                        void act(() =>
                          api(
                            `/projects/${project.id}/shots/${q.id}`,
                            {
                              revision: project.revision,
                              selected,
                              approved,
                              narration,
                            },
                            "PUT",
                          ),
                        )
                      }
                      lock={() =>
                        void act(() =>
                          api(`/projects/${project.id}/boards/lock`, {
                            revision: project.revision,
                          }),
                        )
                      }
                    />
                  )}
                  {tab === 3 && (
                    <Production
                      p={project}
                      busy={busy}
                      quote={(s) => void requestQuote(s)}
                    />
                  )}
                  </div>
                </>
              )}
              {page === "usage" && <TokenUsage projects={projects} tasks={tasks} />}
              {page === "tasks" && (
                <>
                  <div className="page-heading">
                    <div>
                      <div className="eyebrow">PRODUCTION LOG</div>
                      <h1>任务中心</h1>
                      <p>进度与提交记录保存在本机，关闭页面也不会丢失。</p>
                    </div>
                    <Badge>{active.length} 个任务待处理</Badge>
                  </div>
                  {tasks.length === 0 ? (
                    <Empty
                      icon={<Clock3 />}
                      title="暂时没有制作任务"
                      description="在作品中确认报价后，任务将在这里显示。"
                    />
                  ) : (
                    <div className="task-list">
                      {tasks.map((t) => (
                        <article className="task-row" key={t.id}>
                          <div
                            className={`task-status ${t.status === "completed" ? "success" : ""}`}
                          >
                            {t.status === "completed" ? (
                              <Check size={20} />
                            ) : (
                              <Clock3 size={20} />
                            )}
                          </div>
                          <div className="task-details">
                            <div>
                              <strong>{stageNames[t.stage]}</strong>
                              <Badge green={t.status === "completed"}>
                                {statusNames[t.status]}
                              </Badge>
                              <small>
                                {t.mode === "mock" ? "本地模拟" : "真实服务"}
                              </small>
                            </div>
                            <button
                              className="text-button"
                              onClick={() => {
                                const p = projects.find(
                                  (p) => p.id === t.project_id,
                                );
                                if (p) openProject(p);
                              }}
                            >
                              {projects.find((p) => p.id === t.project_id)
                                ?.title || "作品"}
                            </button>
                            <p>{t.message}</p>
                            {t.stage==="shot_rewrite"&&t.status==="completed"&&!t.rewrite?.applied_revision&&<AntButton onClick={()=>{const p=projects.find(p=>p.id===t.project_id);if(p){openProject(p);setTab(2);setContentShot({id:t.shot_id!,trial:!!t.plan_trial,taskID:t.id});}}}>对比并确认改写</AntButton>}
                            <VideoUsageDetails task={t} />
                            {["failed","interrupted","cancelled"].includes(t.status) && <small>{t.stage==="board_edit" ? "原图仍保留。恢复只收集本次编辑的已有结果；需要重做时先处理原任务，再重新报价，原CLI或接口用量可能已计费。" : "恢复只核对已有回执，不会重复提交旧请求。缺图需到分镜页重新报价，确认后才开始新生成。"}</small>}
                            <small>
                              {new Date(t.created).toLocaleString("zh-CN")} ·{" "}
                              {t.id.slice(0, 8)}
                            </small>
                            {["running", "queued"].includes(t.status) && (
                              <Progress
                                percent={t.progress}
                                showInfo={false}
                                size="small"
                              />
                            )}
                          </div>
                          <div className="inline-actions">
                            <TaskActions task={t} pending={pending} onAction={handleTaskAction}/>
                          </div>
                        </article>
                      ))}
                    </div>
                  )}
                </>
              )}
              {page === "prompts" && <Suspense fallback={<Empty icon={<BookOpen />} title="正在打开提示词" description="加载提示词管理…" />}><PromptSettings onDirty={value => { settingsDirty.current = value; }} onSaved={() => { void refresh().catch(e => setError(errorText(e))); }} /></Suspense>}
              {(page === "settings" || page === "vendors") && settings && (
                <Suspense
                  fallback={
                    <Empty
                      icon={<Settings2 />}
                      title="正在打开配置"
                      description="加载厂商与服务配置…"
                    />
                  }
                >
                  <ServiceSettings
                    key={`${page}-${settings.version}`}
                    settings={settings}
                    page={page === "vendors" ? "vendors" : "services"}
                    supported={vendorCapabilities}
                    onDirty={value => { settingsDirty.current = value; }}
                    openVendors={() => setPage("vendors")}
                    credentials={credentials}
                    busy={pending}
                    save={(v) => void act(async () => { await api("/settings", v, "PUT"); settingsDirty.current = false; })}
                  />
                </Suspense>
              )}
            </>
          )}
        </main>
        <footer className="main-footer">
          <span>让灵感有序，让故事发生。</span>
        </footer>
      </div>
      {showNew && (
        <NewProject
          busy={pending}
          close={() => setShowNew(false)}
          create={(v) =>
            void act(async () => {
              const p = await api<Project>("/projects", v);
              setShowNew(false);
              setID(p.id);
              setPage("workspace");
              setTab(0);
            })
          }
        />
      )}
      {contentShot&&project&&(()=>{const q=(contentShot.trial?project.planned_shots??[]:project.shots).find(s=>s.id===contentShot.id);return q&&<ShotContentDialog key={`${project.id}:${q.id}:${contentShot.taskID??"manual"}`} p={project} q={q} trial={contentShot.trial} duration={contentShot.duration} taskID={contentShot.taskID} busy={pending} close={()=>setContentShot(undefined)} save={async(content,duration,settingsVersion)=>{let saved=false;let failure:unknown;await act(async()=>{try{await api(contentShot.taskID?`/tasks/${contentShot.taskID}/rewrite/apply`:`/projects/${project.id}/shots/${q.id}/content`,{...content,revision:project.revision,duration,settings_version:settingsVersion,plan_trial:contentShot.trial},contentShot.taskID?"POST":"PUT");saved=true;}catch(e){failure=e;throw e;}});if(failure)throw failure;return saved;}}/>;})()}
      {durationShot&&project&&(()=>{const q=(durationShot.trial?project.planned_shots??[]:project.shots).find(s=>s.id===durationShot.id);return q&&<ShotDurationDialog key={`${project.id}:${q.id}`} p={project} q={q} trial={durationShot.trial} busy={pending} editContent={duration=>{setContentShot({...durationShot,duration});setDurationShot(undefined);}} rewrite={duration=>requestQuote("shot_rewrite",{shot_id:q.id,feedback:"",plan_trial:durationShot.trial,target_duration:duration})} close={()=>setDurationShot(undefined)} save={async(duration,settingsVersion,finishRejected)=>{let saved=false;await act(async()=>{await api(`/projects/${project.id}/shots/${q.id}/duration`,{revision:project.revision,duration,plan_trial:durationShot.trial,settings_version:settingsVersion,finish_rejected:finishRejected},"PUT");saved=true;});return saved;}}/>;})()}
      {taskPanelOpen && project && <AntModal open centered aria-label="处理当前作品任务" title="处理当前作品任务" footer={null} width={640} onCancel={()=>{if(!pending)setTaskPanelOpen(false);}}>
        <p className="inline-task-intro">{taskShotID ? `当前镜头：${[...project.shots,...(project.planned_shots??[])].find(q=>q.id===taskShotID)?.title||"所选镜头"}。` : ""}在此处理尚未结束的任务，完成后继续制作，无需离开分镜页。</p>
        <p className="inline-task-help">恢复只查询和收集原任务已有结果，不重发结果未知的请求。停止或放弃可能无法停止供应商继续计费；新生成仍需重新报价并确认。</p>
        {currentTasks.filter(blocked).length ? <div className="inline-task-list">{[...currentTasks.filter(blocked)].sort((a,b)=>Number(b.shot_id===taskShotID)-Number(a.shot_id===taskShotID)).map(t=><article className="inline-task-item" key={t.id}>
          <div className="inline-task-heading"><strong>{stageNames[t.stage]||t.stage}</strong><Badge>{statusNames[t.status]||t.status}</Badge></div>
          <small>{t.shot_id ? [...project.shots,...(project.planned_shots??[])].find(q=>q.id===t.shot_id)?.title||"单镜任务" : "整批 / 作品任务"} · {t.plan_trial?"规划试做 · ":""}{t.id.slice(0,8)}</small>
          <p>{t.message}</p>
          {["running","queued"].includes(t.status)&&<Progress percent={t.progress} size="small"/>}
          <TaskActions task={t} pending={pending} onAction={handleTaskAction}/>
        </article>)}</div> : <Alert type="success" showIcon title="当前没有待处理任务，可以关闭面板继续制作。"/>}
      </AntModal>}
      {quote && (
        <Modal
          title="确认本次制作"
          size="large"
          footer={
            <div className="dialog-actions">
              <Button disabled={pending} onClick={() => setQuote(undefined)}>
                暂不制作
              </Button>
              <Button
                kind="primary"
                disabled={pending || (quote.unknown && !unknown) || (!!quote.replaces_task_id && !replacementAccepted)}
                onClick={() =>
                  void act(async () => {
                    await api(`/quotes/${quote.id}/confirm`, {
                      allow_unknown: unknown,
                      ...(quote.replaces_task_id ? {allow_replacement: replacementAccepted} : {}),
                    });
                    setQuote(undefined);
                  })
                }
              >
                {pending ? (
                  <Loader2 className="spin" size={16} />
                ) : (
                  <Play size={15} />
                )}
                确认并开始
              </Button>
            </div>
          }
          onClose={() => {
            if (!pending) setQuote(undefined);
          }}
        >
          <div className="quote-summary">
            {quote.plan_trial && <Alert type="info" showIcon title="规划单镜试做" description="本次只制作已审核的当前镜头；其余镜头不生成，整份规划仍等待审核。每一步确认本次报价后才调用模型。" />}
            <span className="quote-icon">
              <Clapperboard size={25} />
            </span>
            <h3>{stageNames[quote.stage]}</h3>
            <p>
              {quote.stage==="shot_rewrite" ? `${videoSpec(quote).label} · 仅当前镜头 ${quote.target_duration} 秒文字改写` : ["board_retry","board_edit"].includes(quote.stage) ? `${videoSpec(quote).label} · 仅当前镜头 1 张图片` : `${videoSpec(quote).label} · ${videoSpec(quote).duration} 秒`}<br />
              报价绑定当前项目第 {quote.revision} 版与模型配置第{" "}
              {quote.settings_version} 版
            </p>
          </div>
          {quote.replaces_task_id && <Alert type="warning" showIcon title="仅补生成缺失的这一张图片" description="恢复原任务只收集已有结果；本次是一次新的模型调用。其他已生成图片与原提交记录保留。原请求费用仍未知，新生成将另行计费；确认后原任务停止恢复，避免旧结果覆盖新图。" />}
          {quote.replaces_task_id && <Checkbox checked={replacementAccepted} onChange={e=>setReplacementAccepted(e.target.checked)}>我确认仅为当前缺失镜头新生成一张图片，并知晓原请求可能已计费。</Checkbox>}
          {quote.image_edit && <div className="notice"><strong>独立编辑底图 · 版本{quote.image_edit.base_version}</strong><ZoomImage src={media(quote.image_edit.base.file)} alt="已报价的独立编辑底图" style={{display:"block",width:"100%",maxHeight:260,objectFit:"contain"}} /><p><strong>修改内容：</strong>{quote.image_edit.changes}</p><p><strong>必须保留：</strong>{quote.image_edit.preserve}</p><small>底图文件与哈希已绑定报价，底图改变须重新报价。只修改1张，原图及其他镜头保留。</small></div>}
          {quote.feedback && !quote.image_edit && <div className="notice"><strong>本次修改建议</strong><p>{quote.feedback}</p><small>{quote.stage === "shot_video" ? "仅重新生成当前镜头的视频，其他镜头不重做。完成后请预览检查。" : "仅重新生成当前镜头的一张图，新图完成后需重新确认。"}</small></div>}
          {quote.image_reference_plan && <div className="notice"><strong>本次实际参考图片</strong><ReferencePlanImages plan={quote.image_reference_plan}/></div>}
          {quote.shot_reference_plans && Object.entries(quote.shot_reference_plans).map(([key,plan])=><details className="notice" key={key}><summary>{project?.shots.find(s=>key===`shot-${s.id}-0`)?.title || key} · 实际参考图片</summary><ReferencePlanImages plan={plan}/></details>)}
          {quote.stage==="boards" && quote.reference_policy_version===4 && <p className="notice">仅按已审核规划，每镜生成1张图片。各镜引用会按角色、场景、道具合成最多5张参考板；装不下时会在提交生图前停止，保留规划并提示选择素材，不自动增加付费次数。</p>}
          {quote.stage === "shot_rewrite" && <p className="notice">本次仅生成当前镜头的文字建议，不生图、不生成视频。完成后须对比并确认应用，原分镜在应用前保留。</p>}
          {quote.stage === "shot_video" && <p className="notice">本次仅制作当前镜头的 {quote.duration} 秒视频，不生成其他镜头，也不单独生成统一旁白。</p>}
          {!!quote.video_inputs?.length && <div className="video-input-summary"><h4>本次视频输入</h4>{quote.video_inputs.map(input=><div key={input.shot_id}><strong>{videoModeNames[input.mode]}</strong><div className="video-input-images">{input.images.map((ref,i)=><figure key={`${ref.role}:${ref.image.file}`}><ZoomImage src={media(ref.image.file)} alt={ref.name} /><figcaption>{i+1}. {ref.name} · {ref.role === "first_frame" ? "严格首帧" : ref.role === "last_frame" ? "严格尾帧" : "参考图"}</figcaption></figure>)}</div>{input.source_video && <><video controls playsInline preload="none" aria-label="本次报价的原视频参考" src={media(input.source_video)} style={{display:"block",width:240,maxWidth:"100%",maxHeight:200}}/><small>原视频哈希：{input.source_hash}</small><p>{input.mode==="edit_video"?"将引用当前镜头这条原视频进行修改，不是延续上一镜。":"将引用本项目上一镜已生成的视频进行续接。"}来源及文件哈希已绑定当前报价，不上传其他本地视频。输入视频计费用量，按供应商实际规则收费。</p></>}</div>)}<p className="muted">自定义模型 ID 的能力未实测，请确认模型支持所选模式；一致性和续接效果仍需预览检查。</p></div>}
          <div className="quote-items">
            {quote.items.map((item) => (
              <div key={item.label}>
                <span>{item.label}</span>
                <strong>{item.count} 次</strong>
              </div>
            ))}
          </div>
          <div className="quote-price">
            <span>
              {quote.mode === "mock"
                ? "本地模拟 · 模型费用"
                : quote.stage === "render"
                  ? "本地处理 · 模型费用"
                  : "真实服务 · 费用估算"}
            </span>
            <strong>{quote.estimate ? `约 ¥ ${quote.estimate.amount}` : quote.unknown ? "金额未知" : "¥ 0.00"}</strong>
          </div>
          {quote.estimate && <div className="notice"><strong>{quote.estimate.model}</strong><p>{quote.estimate.basis}</p><small><a href={quote.estimate.url} target="_blank" rel="noreferrer">{quote.estimate.source}</a> · 核对日期 {quote.estimate.checked}</small></div>}
          {quote.estimate_reason && <p className="muted">{quote.estimate_reason}</p>}
          {quote.unknown ? (
            <Checkbox
              className="consent"
              checked={unknown}
              onChange={(e) => setUnknown(e.target.checked)}
            >
              <span>
                {quote.estimate ? "我知晓以上为参考估算，接受供应商按实际用量结算，最终金额可能与估算不同；提交后取消可能仍计费。" : "我接受由已配置供应商按实际用量收费。本次金额没有可靠上界；提交后取消可能仍计费。"}
              </span>
            </Checkbox>
          ) : (
            <p className="notice">
              {quote.stage === "render"
                ? "仅运行本机 FFmpeg 合成导出与成片检查，不提交模型生成请求。"
                : "使用程序生成示意图、模板文本和静音视频，仅验证流程，不调用真实模型。"}
            </p>
          )}
          <p className="muted">
            有效至 {new Date(quote.expires).toLocaleTimeString("zh-CN")}
            。修改内容或模型配置后须重新报价。
          </p>
        </Modal>
      )}
    </div>
  );
}

function VideoSpecFields({
  aspect,
  duration,
  disabled = false,
  onAspect,
  onDuration,
}: {
  aspect: AspectRatio;
  duration: number;
  disabled?: boolean;
  onAspect: (v: AspectRatio) => void;
  onDuration: (v: number) => void;
}) {
  return (
    <div className="video-spec-fields">
      <Field label="画幅">
        <Select<AspectRatio>
          disabled={disabled}
          value={aspect}
          onChange={onAspect}
          options={[
            { value: "9:16", label: "9:16 竖屏" },
            { value: "16:9", label: "16:9 横屏" },
          ]}
          virtual={false}
        />
      </Field>
      <Field label="目标时长（秒）">
        <InputNumber
          min={10}
          max={180}
          precision={0}
          step={1}
          disabled={disabled}
          value={duration || null}
          onChange={(value) => onDuration(value ?? 0)}
          suffix="秒"
          style={{ width: "100%" }}
        />
        <small>10～180秒，默认60秒</small>
      </Field>
    </div>
  );
}
function AudioModeField({ value, onChange, disabled }: {
  value: "dialogue" | "narration";
  onChange: (v: "dialogue" | "narration") => void;
  disabled: boolean;
}) {
  return (
    <Field label="声音方式">
      <Select
        value={value}
        disabled={disabled}
        onChange={onChange}
        virtual={false}
        options={[
          { value: "dialogue", label: "角色对白（动物自己说话）" },
          { value: "narration", label: "旁白讲述" },
        ]}
      />
      <small>{value === "dialogue"
        ? "角色在画面中直接说话，不加旁白。视频模型需支持同步音频；模拟模式为静音。"
        : "由统一旁白讲述故事，角色不说话。"}</small>
    </Field>
  );
}
function NewProject({
  busy,
  close,
  create,
}: {
  busy: boolean;
  close: () => void;
  create: (v: unknown) => void;
}) {
  const formID = useId();
  const [title, setTitle] = useState("");
  const [idea, setIdea] = useState("");
  const [style, setStyle] = useState(styles[0]);
  const [aspect, setAspect] = useState<AspectRatio>("9:16");
  const [duration, setDuration] = useState(60);
  const [audioMode, setAudioMode] = useState<"dialogue" | "narration">("dialogue");
  return (
    <Modal
      title="开始一个新故事"
      onClose={() => {
        if (!busy) close();
      }}
      footer={
        <div className="dialog-actions">
          <Button onClick={close} disabled={busy}>
            取消
          </Button>
          <Button
            type="submit"
            form={formID}
            kind="primary"
            disabled={
              busy ||
              !title.trim() ||
              !idea.trim() ||
              !Number.isInteger(duration) ||
              duration < 10 ||
              duration > 180
            }
          >
            <Plus size={15} />
            创建作品
          </Button>
        </div>
      }
    >
      <p className="modal-intro">
        先写下一个想法。你可以在制作前继续调整剧本和角色。
      </p>
      <Form
        id={formID}
        layout="vertical"
        requiredMark={false}
        onFinish={() => {
          create({ title, idea, style, aspect_ratio: aspect, duration, audio_mode: audioMode });
        }}
      >
        <Field label="作品名称">
          <Input
            autoFocus
            maxLength={80}
            required
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="为你的故事起个名字"
          />
        </Field>
        <Field label="故事想法">
          <Input.TextArea
            required
            rows={5}
            maxLength={6000}
            value={idea}
            onChange={(e) => setIdea(e.target.value)}
            placeholder="谁，在什么地方，经历了什么？你希望观众感受到什么？"
          />
        </Field>
        <Field label="视觉风格">
          <Select
            value={style}
            onChange={setStyle}
            options={styles.map((value) => ({ value, label: value }))}
            virtual={false}
          />
        </Field>
        <AudioModeField value={audioMode} onChange={setAudioMode} disabled={busy} />
        <VideoSpecFields
          aspect={aspect}
          duration={duration}
          onAspect={setAspect}
          onDuration={setDuration}
          disabled={busy}
        />
        <div className="creation-spec">
          <span>{videoSpec({ aspect_ratio: aspect, duration }).label}</span>
          <span>{duration || "—"} 秒</span>
          <span>分阶段确认</span>
        </div>
      </Form>
    </Modal>
  );
}
export function Story({
  p,
  busy,
  onSave,
  onAnalyze,
}: {
  p: Project;
  busy: boolean;
  onSave: (p: Project) => void;
  onAnalyze: () => void;
}) {
  const [title, setTitle] = useState(p.title);
  const [idea, setIdea] = useState(p.idea);
  const [script, setScript] = useState(p.script);
  const [style, setStyle] = useState(p.style);
  const [aspect, setAspect] = useState<AspectRatio>(videoSpec(p).aspect);
  const [duration, setDuration] = useState(videoSpec(p).duration);
  const [audioMode, setAudioMode] = useState<"dialogue" | "narration">(p.audio_mode || "narration");
  const [history, setHistory] = useState<Project[]>();
  const [historyError, setHistoryError] = useState("");
  const dirty =
    audioMode !== (p.audio_mode || "narration") ||
    title !== p.title ||
    idea !== p.idea ||
    script !== p.script ||
    style !== p.style ||
    aspect !== videoSpec(p).aspect ||
    duration !== videoSpec(p).duration;
  return (
    <div className="story-layout">
      <section className="panel story-panel">
        <div className="panel-heading">
          <p>先把故事讲清楚，再决定画面的样子。</p>
          <Badge green={p.analyzed}>{p.analyzed ? "已分析" : "草稿"}</Badge>
        </div>
        <div className="story-fields" tabIndex={0} role="region" aria-label="故事与剧本表单">
        <Field label="作品名称">
          <Input
            disabled={busy}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            maxLength={80}
          />
        </Field>
        <Field label="故事想法">
          <Input.TextArea
            rows={6}
            disabled={busy}
            value={idea}
            onChange={(e) => setIdea(e.target.value)}
            maxLength={6000}
          />
        </Field>
        <Field label="视觉风格">
          <Select
            disabled={busy}
            value={style}
            onChange={setStyle}
            options={styles.map((value) => ({ value, label: value }))}
            virtual={false}
          />
        </Field>
        <AudioModeField value={audioMode} onChange={setAudioMode} disabled={busy} />
        <VideoSpecFields
          aspect={aspect}
          duration={duration}
          onAspect={setAspect}
          onDuration={setDuration}
          disabled={busy}
        />
        {p.analyzed && (
          <Field label="完整剧本">
            <Input.TextArea
              rows={12}
              disabled={busy}
              value={script}
              onChange={(e) => setScript(e.target.value)}
              maxLength={15000}
            />
          </Field>
        )}
        {dirty && (
          <div className="notice">
            仅切换声音方式会保留图片、清空镜头台词与视频并取消分镜确认，请核对剧本并重新填写台词。修改故事想法、画幅或时长会重置分析与资产清单；修改风格会使图片失效；修改剧本会使分镜和成片失效。历史版本保留，继续制作前需要重新报价。
          </div>
        )}
        </div>
        <div className="panel-actions">
          <Button
            disabled={
              busy ||
              !dirty ||
              !title.trim() ||
              !idea.trim() ||
              !Number.isInteger(duration) ||
              duration < 10 ||
              duration > 180
            }
            onClick={() =>
              onSave({
                ...p,
                title,
                idea,
                script,
                style,
                audio_mode: audioMode,
                aspect_ratio: aspect,
                duration,
              })
            }
          >
            保存修改
          </Button>
          {!p.analyzed && (
            <Button kind="primary" disabled={busy || dirty} onClick={onAnalyze}>
              <Sparkles size={16} />
              分析故事 · 查看报价
            </Button>
          )}
        </div>
      </section>
      <aside className="guidance">
        <span className="guidance-icon">
          <BookOpen size={22} />
        </span>
        <h3>好故事，从一个具体的瞬间开始。</h3>
        <p>一个人、一个场景、一个改变。无需写得完美，先让想法落在纸上。</p>
        <ol>
          <li>描述主人公与愿望</li>
          <li>给故事一个小小的转折</li>
          <li>留下你希望观众记住的情绪</li>
        </ol>
        <hr />
        <h4>你的创作始终由你决定</h4>
        <p>故事分析只生成剧本与素材清单。图片、分镜和视频将在之后分别确认。</p>
        <button
          className="text-button"
          onClick={() =>
            void api<Project[]>(`/projects/${p.id}/history`)
              .then(setHistory)
              .catch((e) => setHistoryError(errorText(e)))
          }
        >
          <Clock3 size={14} />
          查看历史版本
        </button>
        {historyError && <p role="alert">{historyError}</p>}
      </aside>
      {history && (
        <Modal title="历史版本（只读）" onClose={() => setHistory(undefined)}>
          <div className="history-list">
            {history.map((h) => (
              <details key={h.revision}>
                <summary>
                  第 {h.revision} 版 ·{" "}
                  {new Date(h.updated).toLocaleString("zh-CN")}
                </summary>
                <h4>{h.title}</h4>
                <pre>{h.script || h.idea}</pre>
              </details>
            ))}
          </div>
        </Modal>
      )}
    </div>
  );
}
export function Assets({
  p,
  busy,
  quote,
  save,
  approve,
  lock,
}: {
  p: Project;
  busy: boolean;
  quote: (s: string) => void;
  save: (p: Project) => void;
  approve: (a: Asset, part: string) => void;
  lock: () => void;
}) {
  const [assets, setAssets] = useState(p.assets);
  const dirty = JSON.stringify(assets) !== JSON.stringify(p.assets);
  if (!p.analyzed)
    return (
      <Empty
        icon={<Layers3 />}
        title="先确定故事，再准备素材"
        description="回到故事与剧本，确认故事分析报价，生成角色、场景与道具清单。"
      />
    );
  const chars = assets.filter((a) => a.kind === "character");
  const needsMaster = chars.some((a) => !a.master);
  const mastersReady = chars.every((a) => a.master?.approved);
  const needsDetails = assets.some((a) =>
    a.kind === "character" ? !a.sheet : !a.master,
  );
  const ready = assets.every(
    (a) => a.master?.approved && (a.kind !== "character" || a.sheet?.approved),
  );
  const pendingMasters = chars.filter(a => a.master && !a.master.approved);
  const detailCounts = [
    { count: chars.filter(a => !a.sheet).length, label: "张角色四格设定图" },
    { count: assets.filter(a => a.kind === "scene" && !a.master).length, label: "张场景图" },
    { count: assets.filter(a => a.kind === "prop" && !a.master).length, label: "张道具图" },
  ].filter(item => item.count > 0).map(item => `${item.count}${item.label}`).join("、");
  const pendingReviews = assets.flatMap(a => [
    ...(a.master && !a.master.approved ? [`${a.name}（${a.kind === "character" ? "主图" : "素材图"}）`] : []),
    ...(a.sheet && !a.sheet.approved ? [`${a.name}（四格设定图）`] : []),
  ]);
  const unavailableReason = p.archived
    ? "作品已归档，请先恢复作品。"
    : busy
      ? "请先查看上方任务状态，等待当前操作完成，或处理需要恢复、放弃的任务后继续。"
      : dirty ? "请先保存修改后的素材设定，再继续确认或生成。" : "";
  let guidance: { title: string; description: string };
  if (unavailableReason) {
    guidance = {
      title: p.archived ? "先恢复作品再继续" : busy ? "先完成当前任务或操作" : "先保存修改后的设定",
      description: unavailableReason,
    };
  } else if (p.assets_locked) {
    guidance = { title: "素材已锁定，可以进入分镜审片", description: "先单独规划文字分镜并人工审核，再为分镜图片查看报价。" };
  } else if (needsMaster) {
    guidance = { title: "第01步：先生成角色正面主图", description: "点击“01 生成角色主图”查看报价，确认后制作。场景和道具在第02步一起生成。" };
  } else if (!mastersReady) {
    guidance = {
      title: `下一步：确认角色主图（${chars.length - pendingMasters.length}/${chars.length}）`,
      description: `待确认：${pendingMasters.map(a => a.name).join("、")}。请在各角色卡片点击“确认角色主图”；全部确认后，才可生成四格设定图、场景和道具。`,
    };
  } else if (needsDetails) {
    guidance = {
      title: "第02步：生成设定图与场景道具",
      description: `本批待制作：${detailCounts}。点击“02 生成设定图与场景道具”查看报价，确认后才开始生成；四格设定图生成后会在本地裁切为四个视角。`,
    };
  } else if (!ready) {
    guidance = {
      title: `下一步：逐项确认素材（还有${pendingReviews.length}项）`,
      description: `待确认：${pendingReviews.join("、")}。请检查图片并点击对应卡片的确认按钮，全部确认后才能锁定素材。`,
    };
  } else {
    guidance = { title: "素材已全部确认，可以锁定并进入分镜", description: "点击右上方“锁定全部素材”，然后进入分镜审片。" };
  }
  const masterReason = unavailableReason || (!needsMaster ? "角色主图已全部生成，请在角色卡片中逐张确认。" : "");
  const detailReason = unavailableReason || (!mastersReady ? "请先在角色卡片中确认全部角色主图。" : !needsDetails ? "本批素材已全部生成，请逐项检查并确认。" : "");
  return (
    <section className="assets-workspace">
      {!p.assets_locked && <div className="asset-lock-action">
          <Button
            kind="primary"
            disabled={busy || dirty || !ready}
            onClick={lock}
          >
            <LockKeyhole size={15} />
            锁定全部素材
          </Button>
      </div>}
      <Alert className="asset-guidance" role="status" type="info" showIcon title={guidance.title} description={guidance.description} />
      <div className="asset-toolbar">
        <div>
          <Badge>{chars.length} 个角色</Badge>
          <Badge>
            {assets.filter((a) => a.kind === "scene").length} 个场景
          </Badge>
          <Badge>{assets.filter((a) => a.kind === "prop").length} 个道具</Badge>
        </div>
        <div>
          {dirty && (
            <Button disabled={busy} onClick={() => save({ ...p, assets })}>
              保存设定并使相关图片失效
            </Button>
          )}
          {!p.assets_locked && (
            <>
              <Tooltip title={masterReason}><span><Button
                disabled={busy || dirty || !needsMaster}
                onClick={() => quote("masters")}
              >
                01 生成角色主图
              </Button></span></Tooltip>
              <Tooltip title={detailReason}><span><Button
                disabled={busy || dirty || !mastersReady || !needsDetails}
                onClick={() => quote("details")}
              >
                02 生成设定图与场景道具
              </Button></span></Tooltip>
            </>
          )}
        </div>
      </div>
      {dirty && (
        <div className="notice">
          保存后，已修改素材的图片、全部分镜与成片失效。未修改素材的图片保留在当前清单。
        </div>
      )}
      <div className="asset-grid">
        {assets.map((a, i) => (
          <article className="asset-card" key={a.id}>
            <div className="asset-card-heading">
              <Badge>
                {a.kind === "character"
                  ? "角色"
                  : a.kind === "scene"
                    ? "场景"
                    : "道具"}
              </Badge>
              <span>{String(i + 1).padStart(2, "0")}</span>
            </div>
            <div className="asset-image">
              {a.master ? (
                <ZoomImage src={media(a.master.file)} alt={a.name} />
              ) : (
                <div>
                  <Layers3 size={36} />
                  <span>{a.kind === "character" ? "角色主图待制作" : "将在第02步制作"}</span>
                  <small>{a.kind === "character" ? "点击上方“01 生成角色主图”查看报价" : mastersReady ? "点击上方02，确认报价后生成" : "请先生成并确认全部角色主图"}</small>
                </div>
              )}
              {p.mode === "mock" && a.master && (
                <span className="simulation-label">模拟示意图</span>
              )}
            </div>
            <div className="asset-body">
              <Field label="名称">
                <Input
                  value={a.name}
                  disabled={busy || p.assets_locked}
                  onChange={(e) =>
                    setAssets(
                      assets.map((item) =>
                        item.id === a.id
                          ? { ...item, name: e.target.value }
                          : item,
                      ),
                    )
                  }
                />
              </Field>
              <Field label="固定设定">
                <Input.TextArea
                  rows={3}
                  value={a.description}
                  disabled={busy || p.assets_locked}
                  onChange={(e) =>
                    setAssets(
                      assets.map((item) =>
                        item.id === a.id
                          ? { ...item, description: e.target.value }
                          : item,
                      ),
                    )
                  }
                />
              </Field>
              {a.master && (
                <Button
                  kind={a.master.approved ? "subtle" : "primary"}
                  disabled={
                    busy || dirty || a.master.approved || p.assets_locked
                  }
                  onClick={() => approve(a, "master")}
                >
                  <Check size={15} />
                  {a.master.approved
                    ? "已人工确认"
                    : a.kind === "character"
                      ? "确认角色主图"
                      : "确认此素材"}
                </Button>
              )}
              {a.kind === "character" && a.master?.approved && !a.sheet && (
                <p className="asset-next-hint">主图已确认，四格设定图将在第02步生成。</p>
              )}
              {a.sheet && (
                <div className="sheet-review">
                  <h4>角色设定 · 四视角</h4>
                  <ZoomImage src={media(a.sheet.file)} alt="2×2角色设定整图" />
                  <div className="view-labels">
                    {["正面", "侧面", "背面", "近景"].map((label, j) => (
                      <div key={label}>
                        {a.views[j] && (
                          <ZoomImage src={media(a.views[j].file)} alt={label} />
                        )}
                        <span>{label}</span>
                      </div>
                    ))}
                  </div>
                  <p>
                    请检查四格布局、身份与服装是否一致。系统只做本地裁切，不能判断画面质量。
                  </p>
                  <Button
                    kind={a.sheet.approved ? "subtle" : "primary"}
                    disabled={
                      busy || dirty || a.sheet.approved || p.assets_locked
                    }
                    onClick={() => approve(a, "sheet")}
                  >
                    <Check size={15} />
                    {a.sheet.approved ? "设定图已确认" : "确认整图与四个视角"}
                  </Button>
                </div>
              )}
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}
export function imageTaskBusy(tasks: Task[], shotID: string, trial: boolean, limit = 2) {
  const unresolved=tasks.filter(blocked);
  return unresolved.length>=limit || unresolved.some(t=>!["running","queued"].includes(t.status)||!["board_retry","board_edit"].includes(t.stage)||!!t.plan_trial!==trial||t.shot_id===shotID||!t.shot_input_hash);
}

export function videoTaskBusy(tasks: Task[], shotID: string, trial: boolean, limit = 2) {
  const unresolved = tasks.filter(blocked).filter(t => !(t.stage === "boards" && !trial && ["running","queued"].includes(t.status)));
  return unresolved.length >= limit || unresolved.some(t => !["running","queued"].includes(t.status) || t.stage !== "shot_video" || !!t.plan_trial !== trial || t.shot_id === shotID || !t.shot_input_hash);
}

export function Boards({
  videoBusy,
  imageBusy,
  task,
  p,
  busy,
  reviewBusy,
  replacementBusy,
  openTasks,
  handleTasks,
  adjustDuration,
  editContent,
  durationPolicy,
  quote,
  imageQuote,
  allImages,
  savePlan,
  trialImage,
  trialRetry,
  trialVideo,
  trialChoose,
  choose,
  retry,
  edit,
  video,
  lock,
}: {
  task?: Task;
  videoBusy?: (shotID:string,trial:boolean)=>boolean;
  imageBusy?: (shotID:string,trial:boolean)=>boolean;
  p: Project;
  busy: boolean;
  reviewBusy?: boolean;
  replacementBusy?: boolean;
  openTasks?: () => void;
  handleTasks?: (shotID?:string) => void;
  adjustDuration?:(q:Shot,trial?:boolean)=>void;
  editContent?:(q:Shot,trial?:boolean)=>void;
  durationPolicy?:ShotDurationPolicy;
  edit?: EditImage;
  quote: () => void;
  imageQuote?: () => void;
  allImages?: (shots:Shot[])=>void|Promise<void>;
  savePlan?: (shots:Shot[],approve:boolean)=>void;
  trialImage?: (shots:Shot[],shot:Shot)=>void | Promise<void>;
  trialRetry?: (q: Shot, feedback: string, referenceIDs?:string[])=>void | Promise<boolean>;
  trialVideo?: (q:Shot,feedback:string,mode?:VideoMode,endShotID?:string)=>void|Promise<boolean>;
  trialChoose?: (q:Shot,s:number,a:boolean,n:string)=>void;
  retry: (q: Shot, feedback: string, referenceIDs?:string[]) => void | Promise<boolean>;
  video: (q: Shot, feedback: string, mode?: VideoMode, endShotID?: string) => void | Promise<boolean>;
  choose: (q: Shot, s: number, a: boolean, n: string) => void;
  lock: () => void;
}) {
  const [preview, setPreview] = useState<BoardPreview>();
  const [planDirty,setPlanDirty] = useState(false);
  const [trialID,setTrialID] = useState("");
  const [cardID,setCardID] = useState("");
  const [boardView,setBoardView] = useState<"table"|"cards">("table");
  useEffect(()=>{setCardID("");setBoardView("table");},[p.id,p.plan_approved,p.plan_version,p.planned_shots?.length]);
  const trialShot = p.planned_shots?.find(q=>q.id===trialID);
  const [previewError, setPreviewError] = useState("");
  useEffect(() => {
    setPreview(undefined);
    setPreviewError("");
    if (!task || task.stage!=="boards" || !blocked(task)) return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    async function load() {
      try {
        const value = await api<BoardPreview>(`/tasks/${task!.id}/preview`);
        if (!stopped && value.task_id === task!.id && value.project_id === p.id && value.revision === p.revision) {
          setPreview(value);
          setPreviewError("");
        }
      } catch {
        if (!stopped) setPreviewError("任务仍在继续，分镜预览暂不可用，可在上方查看任务进度。");
      }
      if (!stopped && (task!.status === "running" || task!.status === "queued")) timer = setTimeout(load, 2500);
    }
    void load();
    return () => { stopped = true; clearTimeout(timer); };
  }, [task?.id, task?.status, p.id, p.revision]);
  const isPreview = !!task && blocked(task) && !p.shots.length && preview?.task_id === task.id && preview?.revision === p.revision && !!preview.shots.length;
  const display = isPreview ? { ...p, shots: preview!.shots } : p;
  const readOnly = busy || isPreview;
  const shotReadOnly = (reviewBusy ?? busy) || isPreview;
  const hasOverview = p.plan_approved && p.plan_version===2 && !p.planned_shots?.length;
  const detailShot = display.shots.find(q=>q.id===cardID);
  if (!p.assets_locked)
    return (
      <Empty
        icon={<Clapperboard />}
        title="素材确认后，让故事进入镜头"
        description="请先在参考素材中确认角色、设定图、场景和道具，并锁定全部素材。"
      />
    );
  const unresolved = !!task && blocked(task);
  const generating = unresolved && ["running","queued"].includes(task.status);
  const attention = unresolved && !generating;
  const imageCount = display.shots.filter(q=>!!q.candidates[q.selected]?.file).length;
  const progressText = isPreview ? `图片${preview!.completed}/${preview!.total}张` : display.shots.length ? `已出图 ${imageCount}/${display.shots.length}` : p.planned_shots?.length ? `待采用 · ${p.planned_shots.length}镜` : "待规划分镜";
  const blockedReason = p.archived ? "请先取消归档" : busy ? "请先处理当前任务，再继续制作" : planDirty ? "请先保存规划修改" : "";
  const canQuoteImages = p.plan_approved && !p.planned_shots?.length && p.shots.some(q=>!q.candidates[q.selected]?.file);
  const toolbar = <div className="board-toolbar" aria-label="分镜操作">
    <div className="board-toolbar-status" role="status" aria-live="polite">
      {unresolved && <Tooltip title={task.message}><Tag color={attention?"warning":"processing"}>{attention?"任务待处理":task.stage==="shot_video"?"视频生成中":["boards","board_retry","board_edit"].includes(task.stage)?"分镜图生成中":"任务进行中"}</Tag></Tooltip>}
      {!unresolved && busy && !p.archived && <span>任务处理中 · </span>}
      <span>{progressText}</span>
      {(unresolved || (busy&&!p.archived)) && openTasks && <AntButton type="link" size="small" onClick={openTasks}>处理当前任务</AntButton>}
      {previewError && <Tooltip title={previewError}><span className="board-toolbar-warning">预览暂不可用</span></Tooltip>}
    </div>
    <div className="board-toolbar-actions">
      {!p.shots.length && <Tooltip title={busy?blockedReason:undefined}><span><AntButton size="small" disabled={busy} onClick={quote}>规划分镜 · 查看报价</AntButton></span></Tooltip>}
      {canQuoteImages && <Tooltip title={blockedReason||"仅补齐缺图镜头，每镜1张；确认报价后才调用模型"}><span><AntButton type="primary" size="small" disabled={busy||planDirty} onClick={imageQuote}>全部生成分镜图 · 查看报价</AntButton></span></Tooltip>}
      {display.shots.length>0 && (p.boards_locked ? <Badge green><LockKeyhole size={12}/>分镜已锁定</Badge> : <Tooltip title={readOnly?blockedReason||"预览期间不可锁定":!display.shots.every(q=>q.approved)?"请先逐镜确认全部画面与台词":undefined}><span><AntButton size="small" disabled={readOnly||!display.shots.every(q=>q.approved)} onClick={lock}><LockKeyhole size={13}/>锁定分镜</AntButton></span></Tooltip>)}
      {!!p.shots.length && <Dropdown trigger={["click"]} menu={{items:[{key:"replan",label:<Tooltip title={busy?blockedReason:"重新规划只生成文字草稿，不改动现有图片或视频"}><span>按总体剧本重新规划 · 查看报价</span></Tooltip>,disabled:busy}],onClick:({key})=>{if(key==="replan")quote();}}}><AntButton size="small">更多 ⋯</AntButton></Dropdown>}
      <Popover trigger="click" placement="bottomRight" title="分镜生成说明" content={<div className="board-generation-help"><p>先审核文字分镜，再确认报价生图。重新规划不会修改现有图片或视频。</p><p>图片逐张保存，每镜1张，并发数可在服务配置中调整（默认2）。已有图片会跳过；出图后可先确认画面与台词，再单独报价生成视频。</p><p>出图期间，图片重生成与整批锁定暂不可用。失败或未知提交可在当前页处理任务；恢复只收集已有结果，缺图需重新报价并确认后才生成。</p></div>}><AntButton type="text" size="small">生成说明</AntButton></Popover>
    </div>
  </div>;
  const planning = <>
    {!!p.planned_shots?.length && savePlan && <BoardPlanReview editContent={editContent?q=>editContent(q,true):undefined} videoBusy={videoBusy?q=>videoBusy(q.id,true):undefined} durationPolicy={durationPolicy} adjustDuration={adjustDuration?(q)=>adjustDuration(q,true):undefined} key={`${p.id}:${p.revision}`} p={p} busy={busy} trialBusy={imageBusy ? q=>imageBusy(q.id,true)||(busy&&planDirty) : undefined} save={savePlan} allImages={allImages} trial={trialImage} previewTrial={q=>setTrialID(q.id)} onDirty={setPlanDirty} handleTasks={handleTasks?q=>handleTasks(q.id):undefined} confirmFrame={trialChoose ? q=>trialChoose(q,q.selected,true,q.narration) : undefined} quoteVideo={trialVideo ? (q,feedback="",mode="first_frame",endShotID="")=>trialVideo(q,feedback,mode,endShotID) : undefined}/>}
    {trialShot && trialChoose && trialVideo && trialRetry && <AntModal className="trial-preview-modal" open centered title="单镜试做 · 确认图片后单独报价生成视频" onCancel={()=>setTrialID("")} footer={null} width="90vw" styles={{body:{maxHeight:"75dvh",overflowY:"auto"}}}>
      {planDirty && <Alert type="warning" title="请先保存上方规划修改，再确认图片或查看新报价"/>}
      <ShotCard contentBusy={busy||planDirty} editContent={editContent?()=>editContent(trialShot,true):undefined} adjustDuration={adjustDuration?()=>adjustDuration(trialShot,true):undefined} key={`trial:${p.id}:${trialShot.id}`} planTrial handleTasks={handleTasks?()=>handleTasks(trialShot.id):undefined} q={trialShot} index={p.planned_shots!.indexOf(trialShot)} p={{...p,shots:p.planned_shots!,boards_locked:false}} videoBusy={(videoBusy?.(trialShot.id,true)??busy)||planDirty} busy={busy||planDirty} retryBusy={(imageBusy?.(trialShot.id,true)??busy)||planDirty} choose={trialChoose} retry={trialRetry} video={trialVideo}/>
    </AntModal>}
    {!!p.previous_shots?.length && <details><summary>历史素材 · {p.previous_shots.length}镜（原图与视频保留）</summary>{p.previous_shots.map((q,i)=><article key={`${q.id}:${i}`}><h3>{q.title}</h3>{q.candidates.map(im=>im.file&&<ZoomImage key={im.file} src={media(im.file)} alt={q.title}/>)}{q.video&&<video controls preload="metadata" src={media(q.video)}/>}</article>)}</details>}
  </>;
  if (!display.shots.length) return <section className="boards-workspace">{toolbar}{planning}</section>;
  return (
    <section className="boards-workspace">
      {planning}
      <div className="boards-review-bar">
        {hasOverview&&<Radio.Group
          aria-label="分镜视图"
          optionType="button"
          buttonStyle="solid"
          value={boardView}
          options={[{label:"表格总览",value:"table"},{label:"卡片视图",value:"cards"}]}
          onChange={event=>{setBoardView(event.target.value);setCardID("");}}
        />}
        <p className="boards-review-caption">{display.shots.length} 个镜头 · 每镜一张图 · 逐镜确认画面与台词</p>
        {toolbar}
      </div>
      {boardView==="table" && hasOverview ? <BoardPlanReview editContent={editContent?q=>editContent(q,false):undefined} videoBusy={videoBusy?q=>videoBusy(q.id,false):undefined} durationPolicy={durationPolicy} adjustDuration={adjustDuration?(q)=>adjustDuration(q,false):undefined} key={`adopted:${p.id}:${p.revision}`} adopted p={display} busy={shotReadOnly} save={()=>{}} previewTrial={q=>{setCardID(q.id);setBoardView("cards");}} handleTasks={handleTasks?q=>handleTasks(q.id):undefined} confirmFrame={q=>choose(q,q.selected,true,q.narration)} quoteVideo={(q,feedback="",mode="first_frame",endShotID="")=>video(q,feedback,mode,endShotID)}/> : <div className="shot-list">
        {display.shots.filter(q=>!detailShot||q.id===cardID).map((q) => (
          <ShotCard
            key={`${p.id}:${q.id}`}
            q={q}
            contentBusy={shotReadOnly}
            editContent={editContent?()=>editContent(q,false):undefined}
            adjustDuration={adjustDuration&&p.plan_version===2?()=>adjustDuration(q,false):undefined}
            handleTasks={handleTasks?()=>handleTasks(q.id):undefined}
            index={display.shots.indexOf(q)}
            p={display}
            videoBusy={videoBusy?.(q.id,false) ?? shotReadOnly}
            busy={shotReadOnly || !q.candidates[q.selected]?.file}
            retryBusy={!q.candidates[q.selected]?.file && task && ["failed","interrupted","cancelled"].includes(task.status) ? (replacementBusy ?? busy) || isPreview : (imageBusy?.(q.id,false) ?? readOnly) || isPreview}
            choose={choose}
            retry={retry}
            edit={edit}
            video={video}
          />
        ))}
      </div>}
    </section>
  );
}
function ShotCard({
  contentBusy,
  editContent,
  adjustDuration,
  handleTasks,
  planTrial,
  q,
  index,
  p,
  busy,
  retryBusy,
  videoBusy,
  choose,
  retry,
  edit,
  video,
}: {
  handleTasks?:()=>void;
  adjustDuration?:()=>void;
  editContent?:()=>void;
  contentBusy?:boolean;
  planTrial?:boolean;
  edit?: EditImage;
  q: Shot;
  index: number;
  p: Project;
  busy: boolean;
  retryBusy?: boolean;
  videoBusy?: boolean;
  choose: (q: Shot, s: number, a: boolean, n: string) => void;
  retry: (q: Shot, feedback: string, referenceIDs?:string[]) => void | Promise<boolean>;
  video: (q: Shot, feedback: string, mode?: VideoMode, endShotID?: string) => void | Promise<boolean>;
}) {
  const [narration, setNarration] = useState(q.narration);
  useEffect(() => setNarration(q.narration), [q.narration]);
  const [retryOpen, setRetryOpen] = useState(false);
  const [editOpen,setEditOpen]=useState(false);
  const [historyOpen,setHistoryOpen]=useState(false);
  const [referenceIDs,setReferenceIDs]=useState(q.references);
  const referenceKey=q.references.join("\u0000");
  useEffect(()=>setReferenceIDs(q.references),[referenceKey]);
  const [feedback, setFeedback] = useState("");
  const [videoRetryOpen, setVideoRetryOpen] = useState(false);
  const [videoFeedback, setVideoFeedback] = useState("");
  const [videoRetryMethod,setVideoRetryMethod] = useState<"edit_video"|"recreate">();
  const [videoMode, setVideoMode] = useState<VideoMode>(q.video_mode && q.video_mode!=="edit_video" ? q.video_mode : "first_frame");
  const [endShotID, setEndShotID] = useState(q.video_end_shot_id || "");
  const previous = index > 0 ? p.shots[index - 1] : undefined;
  const stale = !!q.video && !videoCurrent(p, index);
  const modeReady = videoMode === "continue" ? !!previous?.video && videoCurrent(p,index-1) : videoMode !== "first_last" || p.shots.some(s=>s.id===endShotID&&s.approved&&s.candidates[s.selected]?.file);
  const tailShot = p.shots.find(s=>s.id===endShotID);
  const tailImage = tailShot?.candidates[tailShot.selected];
  const image = q.candidates[q.selected];
  const videoDisabled = (videoBusy ?? busy) || !modeReady || !q.approved || narration !== q.narration || !image?.file;
  const manageTask = (videoBusy ?? busy) && !p.archived && !!handleTasks;
  const frameManageTask = busy && !p.archived && !!handleTasks;
  const frameConfirmed = q.approved && narration === q.narration;
  const videoComplete = !!q.video && !stale && frameConfirmed;
  const speech = p.audio_mode === "dialogue" ? "台词" : "旁白";
  const nextStep = !image?.file ? "先补齐本镜分镜图，再确认画面。" : !frameConfirmed ? `核对图片与${speech}，点击第 1 步确认。` : videoComplete ? "本镜已完成，可以播放预览；需要调整时再重做。" : stale ? "视频来源已变更，请重新生成本镜视频。" : "选择生成方式，查看第 2 步报价；无需等待其他镜头。";
  return (
    <article className="shot-card">
      <div className="shot-top">
        <span className="shot-number">
          {String(index + 1).padStart(2, "0")}
        </span>
        <h3>{q.title}</h3>
        {adjustDuration?<AntButton size="small" icon={<Pencil size={14}/>} onClick={adjustDuration} aria-label={`调整第${index+1}镜时长`}>{q.duration} 秒 · 调整</AntButton>:<Badge>{q.duration} 秒</Badge>}
        {frameConfirmed && (
          <Badge green>
            <Check size={12} />
            已确认
          </Badge>
        )}
      </div>
      <p className="shot-next-step"><strong>{videoComplete?"已完成":"下一步"}</strong>{nextStep}</p>
      <div className="shot-content">
        <div className="board-image">
          {image?.file
            ? <ZoomImage src={media(image.file)} alt={`${q.title}分镜图`} />
            : <div className="candidate-pending"><Clapperboard size={22} /><span>分镜图待生成</span></div>}
          <div className="board-image-actions">
            <Badge>{p.mode === "mock" ? "模拟图片" : "当前画面"}</Badge>
            {image?.file && !planTrial && <AntButton type="link" size="small" disabled={!edit||(retryBusy??busy)||p.boards_locked} onClick={()=>setEditOpen(true)}>基于当前图修改</AntButton>}
            <AntButton type={image?.file?"link":"primary"} size="small" disabled={(retryBusy ?? busy) || p.boards_locked} onClick={() => setRetryOpen(true)}>{image?.file ? "重新生成一版" : "补生成 · 查看报价"}</AntButton>
            {q.candidates.filter(im=>!!im.file).length>1 && <AntButton type="link" size="small" onClick={()=>setHistoryOpen(true)}>新旧对比与切回</AntButton>}
          </div>
          {editOpen && edit && <EditImageDialog p={p} q={q} busy={(retryBusy??busy)||p.boards_locked} close={()=>setEditOpen(false)} quote={edit} />}
          {historyOpen && <ImageHistoryDialog q={q} busy={busy||p.boards_locked} close={()=>setHistoryOpen(false)} choose={index=>choose(q,index,false,q.narration)} />}
          {retryOpen && <Modal title={image?.file ? "重新生成一版" : "补生成缺失的这一张分镜图"} onClose={() => setRetryOpen(false)} footer={<div className="dialog-actions">
            <Button onClick={() => setRetryOpen(false)}>取消</Button>
            <Button kind="primary" disabled={(retryBusy ?? busy) || !referenceIDs.length || (!!image?.file && !feedback.trim())} onClick={() => void (async()=>{const result=referenceIDs.join("\u0000")===referenceKey ? await retry(q,feedback.trim()) : await retry(q,feedback.trim(),referenceIDs);if(result!==false)setRetryOpen(false);})()}>{image?.file ? "查看重生成报价" : "查看补生成报价"}</Button>
          </div>}>
            <p className="modal-intro">{q.title} · 仅生成这一张，其他镜头与已有图片保留。确认新报价后才调用模型。</p>
            <Field label={image?.file ? "修改建议" : "补图建议（可选）"}><Input.TextArea autoFocus rows={4} maxLength={1000} showCount value={feedback} onChange={e => setFeedback(e.target.value)} placeholder="例如：换成近景，让猫看向狗，背景暗一点。" /></Field>
            <Field label="本次参考素材"><Checkbox.Group value={referenceIDs} onChange={values=>setReferenceIDs(values as string[])} options={q.references.map(id=>{const asset=p.assets.find(a=>a.id===id);return {value:id,label:`${asset ? ({character:"角色",scene:"场景",prop:"道具"}[asset.kind] || "素材") : "素材"} · ${asset?.name || id}`};})} /></Field>
            <p className="muted">默认保留全部引用，至少选择1份；对白中说话角色须保留。超过5份时按角色、场景、道具本地合板，保留原图，不裁切、不缩放；每板边长≤4096、像素≤8388608、PNG≤32MB。装不下会停止报价，请主动选择本镜必要素材，不会自动拆成多次生成。</p>
            <p className="muted">结合原分镜和你的建议生成。下一步确认报价后才开始；新图仍需你确认。</p>
          </Modal>}
        </div>
        <div className="shot-description">
          <ol className="shot-flow-steps shot-card-flow" aria-label={`第${index+1}镜卡片制作流程`}>
           <li className={frameConfirmed?"complete":"current"} aria-current={!frameConfirmed?"step":undefined}>
            <span className="shot-flow-marker" aria-hidden="true">{frameConfirmed?<Check size={12}/>:"1"}</span>
            <div className="shot-flow-content">
             <strong>确认画面与{speech}</strong>
             <small>{frameConfirmed?"已确认":!image?.file?"请先补齐分镜图。":`核对左侧图片与下方${speech}，满意后确认。`}</small>
          <Field label={p.audio_mode === "dialogue" ? "角色对白（每行角色名：台词）" : "旁白"}>
            <Input.TextArea
              disabled={busy || p.boards_locked || planTrial}
              rows={3}
              value={narration}
              maxLength={1000}
              onChange={(e) => setNarration(e.target.value)}
            />
          </Field>
          {!frameConfirmed && <Button
            kind="primary"
            disabled={!frameManageTask && (busy || !image?.file || p.boards_locked)}
            onClick={() => frameManageTask ? handleTasks?.() : choose(q, q.selected, true, narration)}
          >
            <Check size={15} />{`确认画面与${speech}`}
          </Button>}
          {busy&&!frameManageTask&&!frameConfirmed&&<small>当前暂不可确认，请先处理当前任务或恢复作品。</small>}
          {p.boards_locked&&!frameConfirmed&&<small>分镜已锁定，暂不可修改或重新确认。</small>}
          {planTrial && <small>台词与动作请在上方规划中修改、重新审核并保存后再试做。</small>}
            </div>
           </li>
           <li className={videoComplete?"complete":frameConfirmed?"current":"upcoming"} aria-current={frameConfirmed&&!videoComplete?"step":undefined}>
            <span className="shot-flow-marker" aria-hidden="true">{videoComplete?<Check size={12}/>:"2"}</span>
            <div className="shot-flow-content shot-video-section">
             <strong>生成本镜视频</strong>
             <small>{videoComplete?"已生成，可在下方播放。":!frameConfirmed?"完成第 1 步后，查看报价生成视频。":"只制作这一镜，先确认报价。"}</small>
             {frameConfirmed && <>
            <Field label="视频生成方式"><Select<VideoMode> value={videoMode} disabled={videoBusy ?? busy} virtual={false} onChange={setVideoMode} options={(Object.keys(videoModeNames).filter(m=>m!=="edit_video") as VideoMode[]).map(value=>({value,label:videoModeNames[value],disabled:value==="continue"&&!previous}))} /></Field>
            {videoMode === "first_last" && <><Field label="结束画面"><Select value={endShotID || undefined} placeholder="选择一张已确认分镜图" disabled={videoBusy ?? busy} virtual={false} onChange={setEndShotID} options={p.shots.filter(s=>s.approved&&s.candidates[s.selected]?.file).map(s=>({value:s.id,label:s.title}))} /></Field>{tailImage?.file && <ZoomImage className="video-tail-preview" alt="结束画面预览" src={media(tailImage.file)} />}</>}
            <small>{videoMode === "first_frame" ? "分镜图作为严格首帧；角色、场景已在分镜图中统一，不混传额外参考图。" : videoMode === "first_last" ? "本镜分镜图为首帧，所选分镜图为尾帧；不混传多参考。" : videoMode === "reference" ? "传本镜分镜图及引用的固定角色、场景、道具主图。分镜图引导开场，不保证严格首帧；需模型支持多参考。" : previous?.video ? `延续「${previous.title}」的已生成视频，并使用固定角色、场景参考；需支持视频延长的模型。` : "请先生成上一镜视频，续接必须按顺序制作。"}</small>
             </>}
            {stale && <Alert type="warning" title={q.video_mode === "first_last" ? "结束画面已变更，此视频的尾帧来源已过期，请重新生成后再合成。" : "上一镜视频已变更，此视频的续接来源已过期，请重新生成后再合成。"} />}
            {q.video && <small>当前预览：{q.video_mode ? videoModeNames[q.video_mode] : "旧版参考图模式"}。切换方式只影响下一次生成。</small>}
            {q.video && <video controls playsInline preload="metadata" aria-label={`${q.title}视频预览`} src={media(q.video)} />}
            {frameConfirmed&&!q.video&&<Button kind="primary" disabled={videoDisabled&&!manageTask} onClick={() => manageTask ? handleTasks?.() : video(q, "", videoMode, videoMode === "first_last" ? endShotID : "")}>
              <Play size={15} />{manageTask?"重新生成视频":"生成本镜视频 · 查看报价"}
            </Button>}
            {frameConfirmed&&!q.video&&<small>{(videoBusy ?? busy)?"当前任务尚未结束，处理后可继续。":!modeReady?videoMode==="first_last"?"请先选择一张已确认的结束画面。":"请先生成上一镜的有效视频，才能续接。":p.mode === "mock" ? "本地模拟视频为程序示意图与静音，仅验证流程。" : p.audio_mode === "dialogue" ? "角色对白随本镜视频生成，请预览台词、音色与口型。" : "本镜预览不含统一旁白，最终制作时统一配音。"}</small>}
            {q.video&&<div className="shot-flow-secondary"><AntButton type="link" size="small" disabled={videoDisabled&&!manageTask} onClick={() => {if(manageTask){handleTasks?.();return;}setVideoRetryMethod(undefined);setVideoFeedback("");setVideoRetryOpen(true);}}>{manageTask?"重新生成视频":"重新生成本镜视频"}</AntButton></div>}
            </div>
           </li>
          </ol>
          <details className="shot-card-context">
            <summary>查看画面、动作与引用素材</summary>
          {editContent&&<AntButton disabled={(contentBusy??busy)||p.archived} onClick={editContent}>编辑首帧、动作与台词</AntButton>}
          <h4>画面与动作</h4>
          <p>{q.first_frame && <><strong>静态首帧：</strong>{q.first_frame}<br/><strong>视频动作：</strong></>}{q.description}</p>
          <h4>引用素材</h4>
          {image?.reference_plan ? <details><summary>当前图片生成时的实际参考</summary><ReferencePlanImages plan={image.reference_plan}/></details> : <ReferenceImages p={p} q={q}/>}
          <p className="muted">{image?.reference_plan ? "当前版本保留生成时的参考快照。" : "新生图将引用角色主图与已确认四视角；历史图片的参考以原任务快照为准。"}最终传入图片见本次报价。</p>
          </details>
          {videoRetryOpen && <Modal title="重新生成本镜视频" onClose={() => setVideoRetryOpen(false)} footer={<div className="dialog-actions">
            <Button onClick={() => setVideoRetryOpen(false)}>取消</Button>
            <Button kind="primary" disabled={videoDisabled || !videoRetryMethod || !videoFeedback.trim()} onClick={()=>void(async()=>{if(await video(q,videoFeedback.trim(),videoRetryMethod==="edit_video"?"edit_video":videoMode,videoRetryMethod!=="edit_video" && videoMode==="first_last"?endShotID:"")!==false)setVideoRetryOpen(false);})()}>查看本镜视频报价</Button>
          </div>}>
            <p className="modal-intro">{q.title} · {q.duration} 秒。仅重做本镜视频，新视频完成前保留原视频。</p>
            <Field label="制作方式"><Radio.Group value={videoRetryMethod} onChange={e=>setVideoRetryMethod(e.target.value)} options={[{value:"edit_video",label:"基于原视频修改"},{value:"recreate",label:"重新生成一版"}]} /></Field>
            <p className="muted">{videoRetryMethod==="edit_video"?"将本镜原视频作为参考，输入视频计费用量；只按建议修改这一镜，失败保留原视频。":videoRetryMethod==="recreate"?`按「${videoModeNames[videoMode]}」重新制作，不上传本镜旧视频。`:"请选择修改原视频或重新生成。"}</p>
            <Field label="视频修改建议"><Input.TextArea autoFocus rows={4} maxLength={1000} showCount value={videoFeedback} onChange={e => setVideoFeedback(e.target.value)} placeholder="例如：动作慢一点，镜头缓慢推进，猫先开口，狗再回应。" /></Field>
            <p className="muted">保留已确认台词、画幅和时长；按所选生成方式和修改建议制作，确认报价后才开始。</p>
          </Modal>}
        </div>
      </div>
    </article>
  );
}
function Production({
  p,
  busy,
  quote,
}: {
  p: Project;
  busy: boolean;
  quote: (s: string) => void;
}) {
  const videosReady =
    p.shots.length > 0 &&
    p.shots.reduce((sum,q)=>sum+q.duration,0) === videoSpec(p).duration &&
    p.shots.every((q,i) => q.video && videoCurrent(p,i)) &&
    (p.audio_mode === "dialogue" || !!p.voice);
  const missingVideos = p.shots.filter(q => !q.video).length;
  return (
    <div className="production-layout">
      <section className="panel production-panel">
        <div className="panel-heading">
          <p>画面在确认之后流动，采用版本与生成记录始终可追溯。</p>
          <Badge>{p.mode === "mock" ? "模拟流程" : "真实服务"}</Badge>
        </div>
        <div className="production-content">
        {p.output ? (
          <div className="film-preview">
            <video controls preload="metadata" src={media(p.output)} />
            {p.mode === "mock" && (
              <div className="notice">
                本地模拟成片：程序示意图与静音音轨，用于验证制作流程。
              </div>
            )}
          </div>
        ) : (
          <Empty
            icon={<Film size={34} />}
            title={
              !p.boards_locked
                ? "先锁定分镜，制作才会开始"
                : videosReady
                  ? "镜头就绪，可以开始编排"
                  : "你的电影，正在等待开机"
            }
            description={
              !p.boards_locked
                ? "逐镜确认画面与台词，然后锁定分镜。"
                : videosReady
                  ? `本机使用 FFmpeg 完成镜头编排、音轨合成与成片检查，输出${videoSpec(p).duration}秒${videoSpec(p).label}视频。`
                  : p.audio_mode === "dialogue" ? `补齐剩余${missingVideos}个有声镜头，已生成的视频会复用；补齐默认严格首帧。延续、首尾帧、多参考请逐镜选择，不叠加旁白。` : `补齐剩余${missingVideos}个镜头视频${p.voice ? "" : "与一条统一旁白"}，已生成的视频会复用；补齐默认严格首帧，其他模式请逐镜选择。`
            }
          />
        )}
        </div>
        <footer className="panel-actions">
          {p.output ? (
            <>
              <a className="button primary" href={media(p.output)} download>
                <ArrowDownToLine size={16} />
                下载 MP4
              </a>
              <a
                className="button"
                href={media(p.output.replace("-final.mp4", "-captions.srt"))}
                download
              >
                下载字幕 SRT
              </a>
            </>
          ) : (
            <Button
              kind="primary"
              disabled={busy || !p.boards_locked}
              onClick={() => quote(videosReady ? "render" : "videos")}
            >
              <Play size={16} />
              {videosReady ? "本地合成 · 查看任务" : "制作视频 · 查看报价"}
            </Button>
          )}
        </footer>
      </section>
      <aside className="guidance">
        <h3>本次制作规格</h3>
        <dl>
          <dt>画幅</dt>
          <dd>{videoSpec(p).label}</dd>
          <dt>时长</dt>
          <dd>
            {videoSpec(p).duration} 秒 / {p.shots.length} 镜
          </dd>
          <dt>导出</dt>
          <dd>
            {videoSpec(p).width} × {videoSpec(p).height} · MP4
          </dd>
          <dt>声音方式</dt><dd>{p.audio_mode === "dialogue" ? "角色对白 · 保留原声音轨" : "统一旁白"}</dd>
          <dt>字幕</dt>
          <dd>独立 SRT</dd>
          <dt>合成</dt>
          <dd>FFmpeg + ffprobe</dd>
        </dl>
        <hr />
        <p>
          字幕按镜头窗口分句，尚未与真实语音逐字对齐。{p.audio_mode === "dialogue" ? "对白随有声视频生成，请逐镜试听台词、音色与口型；缺少音轨会停止合成。" : `远程旁白超出${videoSpec(p).duration}秒时会截断；请在成片验收时听看确认。`}
        </p>
        <p>
          取消本地任务不能撤回供应商已接收的请求。出现异常时，优先恢复原任务。
        </p>
        {videosReady && (
          <div className="clips-grid">
            {p.shots.map((q, i) => (
              <div key={q.id}>
                <video controls preload="none" src={media(q.video!)} />
                <small>镜头 {i + 1}</small>
              </div>
            ))}
          </div>
        )}
      </aside>
    </div>
  );
}
