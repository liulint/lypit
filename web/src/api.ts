export type VideoMode = "first_frame" | "continue" | "first_last" | "reference" | "edit_video";
export type VideoInput = {shot_id: string; mode: VideoMode; end_shot_id?: string; images: {name:string;role:string;image:Image}[]; source_video?:string; source_hash?:string; source_shot_id?:string};
export const videoModeNames: Record<VideoMode,string> = {first_frame:"独立镜头 · 严格首帧", continue:"延续上一镜", first_last:"首尾帧控制", reference:"多参考 · 开场引导", edit_video:"基于本镜原视频修改"};
export function videoCurrent(p: Project, index: number): boolean {
 const q=p.shots[index];
 if(q.video_mode==="first_last") {const tail=p.shots.find(s=>s.id===q.video_end_shot_id);return !!tail?.approved && tail.candidates[tail.selected]?.hash===q.video_end_image_hash;}
 return q.video_mode!=="continue" || (index>0 && !!p.shots[index-1].video && q.video_source_file===p.shots[index-1].video && videoCurrent(p,index-1));
}
export type Image = { reference_plan?:ReferencePlan; operation?:string;source_file?:string;source_hash?:string; file: string; hash: string; approved: boolean };
export type Asset = {
  id: string;
  kind: "character" | "scene" | "prop";
  name: string;
  description: string;
  master?: Image;
  sheet?: Image;
  views: Image[];
};
export type ShotContent = {first_frame:string;description:string;narration:string};
export type ShotRewriteResult = {revision:number;settings_version:number;before:ShotContent;after:ShotContent;old_duration:number;duration:number;applied_revision?:number};
export type Shot = {
 content_override?: {version:number};
 review_status?: "pending" | "changes" | "reviewed";
 first_frame?: string;
 source_text?: string;
 video_mode?: VideoMode;
 video_end_shot_id?: string;
 video_end_image_hash?: string;
 video_source_file?: string;
  id: string;
  title: string;
  description: string;
  narration: string;
  duration: number;
  references: string[];
  candidates: Image[];
  selected: number;
  approved: boolean;
  video?: string;
};
export type Project = {
 plan_version?: number;
 plan_approved?: boolean;
 planned_shots?: Shot[];
 previous_shots?: Shot[];
  audio_mode?: "dialogue" | "narration";
  aspect_ratio?: "9:16" | "16:9";
  duration?: number;
  id: string;
  title: string;
  idea: string;
  script: string;
  style: string;
  revision: number;
  created: string;
  updated: string;
  archived: boolean;
  analyzed: boolean;
  assets_locked: boolean;
  boards_locked: boolean;
  assets: Asset[];
  shots: Shot[];
  output?: string;
  voice?: string;
  mode: string;
};
export type Task = {
 rewrite?:ShotRewriteResult;
 target_duration?:number;
  shot_input_hash?: string;
  plan_trial?: boolean;
  video_usage?: VideoUsage[];
  shot_id?: string;
  id: string;
  project_id: string;
  quote_id: string;
  stage: string;
  status: string;
  progress: number;
  message: string;
  created: string;
  updated: string;
  mode: string;
};
export type VideoUsage = {
  shot_id: string; title: string; remote_id: string; model: string;
  completion_tokens?: number; total_tokens?: number;
  cost?: {amount: string; currency: string; model: string; basis: string; source: string; url: string; checked: string};
  reason?: string; recorded: string;
};
export type BoardPreview = {
 task_id: string; project_id: string; revision: number; status: string; shots: Shot[]; completed: number; total: number;
};
export type ReferencePlan = {version:number; sources:{image:{file:string;hash:string};asset_id?:string;name:string;kind:string;width:number;height:number}[];boards:{file?:string;kind:string;width:number;height:number;cells:{source:number;x:number;y:number}[];hash:string;bytes:number}[]};
export type ImageEditSpec = {base:Image;base_index:number;base_version:number;changes:string;preserve:string;auxiliary_ids:string[]};
export type Quote = {
 target_duration?:number;
  plan_trial?: boolean;
  estimate?: { amount: string; currency: string; model: string; basis: string; source: string; url: string; checked: string };
  estimate_reason?: string;
  image_edit?: ImageEditSpec;
  reference_ids?: string[];
  image_reference_plan?: ReferencePlan;
  shot_reference_plans?: Record<string,ReferencePlan>;
  reference_policy_version?: number;
  replaces_task_id?: string;
 video_inputs?: VideoInput[];
  shot_id?: string;
  feedback?: string;
  board_images_per_shot?: number;
  aspect_ratio?: "9:16" | "16:9";
  duration?: number;
  id: string;
  project_id: string;
  revision: number;
  settings_version: number;
  stage: string;
  items: { label: string; count: number }[];
  mode: string;
  unknown: boolean;
  expires: string;
  task_id?: string;
};
export type Capability = "text" | "image" | "video" | "voice" | "music";
export type ModelPricing = { rows: { label: string; condition?: string; amount: string; unit: string; currency?: string }[]; source: string; url: string; checked: string; note: string };
export type CatalogModel = { id: string; name: string; capabilities: string[]; services: Capability[]; verified: boolean; pricing?: ModelPricing };
export type ModelCatalog = { models: CatalogModel[]; source: string; updated: string; stale: boolean; error?: string };
export type Vendor = {
  kind?: "codex" | "claude" | "ark" | "bailian" | "custom" | "minimax";
  region?: string;
  workspace?: string;
  key_mode?: "local" | "env";
  credential_id?: string;
  id: string;
  name: string;
  type: "api" | "cli";
  protocol?: "openai" | "ark" | "minimax";
  base?: string;
  key_env?: string;
  cli?: "codex" | "claude";
  executable?: string;
};
export type Endpoint = {
  vendor_id?: string;
  model: string;
  base?: string;
  key_env?: string;
};
export type Settings = {
  image_concurrency?: number;
  video_concurrency?: number;
  version: number;
  registry_version: number;
  mode: string;
  vendors: Vendor[];
  text: Endpoint;
  image: Endpoint;
  video: Endpoint;
  voice: Endpoint;
  music?: Endpoint;
};
export type SettingsUpdate = Settings & { api_keys?: Record<string, string>; section?: "vendors" | "services" };
export const stageNames: Record<string, string> = {
  analyze: "故事分析",
  masters: "角色主图",
  details: "设定图与场景道具",
  board_plan: "分镜规划",
  shot_rewrite:"按秒数重写单镜",
  boards: "分镜生图",
  board_retry: "重新生成一版",
  board_edit:"基于当前图修改",
  shot_video: "单镜视频制作",
  videos: "视频与声音",
  render: "本地合成",
};
export const statusNames: Record<string, string> = {
  superseded: "已转单镜新任务",
  queued: "等待执行",
  running: "正在制作",
  completed: "已完成",
  failed: "需要处理",
  interrupted: "等待恢复",
  cancelled: "已停止",
  abandoned: "已放弃",
};
export const blocked = (t: Task) =>
  ["queued", "running", "failed", "interrupted", "cancelled"].includes(
    t.status,
  );
export const media = (file: string) =>
  `/media/${file.split("/").map(encodeURIComponent).join("/")}`;
export async function api<T>(
  path: string,
  body?: unknown,
  method = body === undefined ? "GET" : "POST",
  timeoutMs = 20000,
): Promise<T> {
  let response: Response;
  try {
    response = await fetch("/api" + path, {
      method,
      headers: {
        "Content-Type": "application/json",
        "X-Lypit-Client": "studio",
      },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(timeoutMs),
    });
  } catch {
    throw new Error(
      "无法连接本机服务或请求超时，请检查启动终端；提交中的操作请先查看任务记录再重试。",
    );
  }
  const data = await response
    .json()
    .catch(() => ({ error: "服务响应格式异常，请检查后端是否正在运行" }));
  if (!response.ok)
    throw new Error(data.error || `请求失败（${response.status}）`);
  return data as T;
}

export type ShotDurationPolicy = {min:number;max:number;model:string;known:boolean;settings_version:number};

export type UsageRecord = {
 id:string; task_id:string; project_id:string; stage:string; status:string; title?:string;
 vendor:string; model:string; transport:string; recorded:string;
 input_tokens?:number; output_tokens?:number; cached_input_tokens?:number; total_tokens?:number; video_tokens?:number;
 cost?:VideoUsage["cost"]; reason:string;
};
