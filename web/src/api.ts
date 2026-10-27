export type Model = {
  cli_options?: { reasoning?: string; service_tier?: string };
  image_edit?: boolean;
  output_formats?: string[];
  video_reference?: string;
  id: string;
  capabilities: string[];
  reference_images: boolean;
  max_references: number;
  structured: boolean;
  async: boolean;
  audio: boolean;
  sizes: string[];
  min_duration: number;
  max_duration: number;
  price: number | null;
  unit: string;
  price_source: string;
  verified: string;
  price_model: string;
  voice: string;
};
export type CLIConfig = {
  image_mode?: "tool" | "codex";
  skill_digest?: string;
  kind: "codex" | "claude" | "gemini";
  path: string;
  image_instructions: string;
  skill_path: string;
  timeout_seconds: number;
};
export type CLIStatus = {
  catalog?: CLICatalog;
  kind: string;
  installed: boolean;
  path: string;
  version: string;
  auth: string;
  message: string;
  models: string[];
};
export type CLIModelOption = {
  id: string;
  label: string;
  description?: string;
  reasoning: { id: string; label: string }[];
  default_reasoning?: string;
  service_tiers: { id: string; label: string }[];
};
export type CLICatalog = {
  models: string[];
  options: CLIModelOption[];
  source: "live" | "cache" | "unavailable";
  fetched_at?: string;
  stale: boolean;
  error?: string;
};
export type ImageProbe = {
  id: string;
  state: string;
  quote: Quote;
  image: string;
  error: string;
};
export type Provider = {
  credential_source?: "env" | "file";
  credential_version?: string;
  cli?: CLIConfig;
  image_verified?: Record<string, string>;
  id: string;
  name: string;
  adapter: string;
  base_url: string;
  key_env: string;
  models: Model[];
};
export type Settings = {
  providers: Provider[];
  bindings: Record<string, { provider: string; model: string }>;
};
export type Character = {
  views: ReferenceView[];
  fixed_features: string;
  sources: string[];
  id: string;
  name: string;
  description: string;
  image: string;
};
export type Shot = {
  scene_id: string;
  prop_id: string;
  purpose: string;
  camera: CameraPlan;
  continuity: ContinuityPlan;
  candidates: Candidate[];
  selected: string;
  recommended: string;
  stale: boolean;
  video_review: Review;
  references: string[];
  id: string;
  title: string;
  scene: string;
  action: string;
  characters: string[];
  duration: number;
  narration: string;
  image_prompt: string;
  video_prompt: string;
  transition: string;
  image: string;
  video: string;
  voice: string;
  warning: string;
};
export type Board = {
  asset_hash?: string;
  schema: number;
  visual: VisualPlan;
  scenes: SceneAsset[];
  props: SceneAsset[];
  source_mappings: SourceMapping[];
  story: {
    synopsis: string;
    relationships: string;
    emotion: string;
    beats: string[];
  };
  version: number;
  title: string;
  synopsis: string;
  characters: Character[];
  shots: Shot[];
  approved: boolean;
};
export type Project = {
  asset_workflow?: boolean;
  asset_sets?: AssetSet[];
  visual: VisualPlan;
  reference_mappings: SourceMapping[];
  id: string;
  idea: string;
  revision: number;
  status: string;
  boards: Board[];
  references: string[];
  music: string;
  tasks: {
    id: string;
    kind: string;
    state: string;
    message: string;
    started: string;
    finished: string;
  }[];
  error: string;
  output: string;
  attempts: Record<
    string,
    {
      state: string;
      kind: string;
      fee: number | null;
      usage: unknown;
      detail: string;
    }
  >;
};
export type Quote = {
  created?: string;
  id: string;
  stage: string;
  estimate: number | null;
  notice: string;
  requests: {
    count: number;
    conditional: boolean;
    role: string;
    kind: string;
    provider: string;
    model: string;
    prompt: string;
    references: string[];
    duration: number;
    parameters: unknown;
    estimate: number | null;
  }[];
};
export async function api<T>(
  path: string,
  body?: unknown,
  method?: string,
): Promise<T> {
  const response = await fetch("/api" + path, {
    method: method ?? (body === undefined ? "GET" : "POST"),
    headers:
      body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error ?? "请求失败");
  return data as T;
}
export const media = (path: string) =>
  "/media/" + path.split("/").map(encodeURIComponent).join("/");
export const money = (amount: number | null) =>
  amount === null ? "费用上界未知" : `¥${amount.toFixed(2)}`;
export const statusLabel: Record<string, string> = {
  draft: "等待创作",
  running: "制作中",
  review: "等待审核",
  complete: "已完成",
  attention: "需要处理",
  cancelled: "已暂停",
  interrupted: "可恢复",
  succeeded: "已完成",
  failed: "失败",
};

export type VisualPlan = {
  style: string;
  photography: string;
  palette: string;
  candidates: number;
  repairs: number;
};
export type Review = {
  state: string;
  identity: number;
  style: number;
  composition: number;
  continuity: number;
  technical: number;
  score: number;
  identity_error: boolean;
  technical_error: boolean;
  issues: string[];
  reason: string;
  correction: string;
  accepted: string;
};
export type ReferenceView = {
  prompt?: string;
  references?: ReferenceUse[];
  id: string;
  kind: string;
  image: string;
  hash: string;
  review: Review;
};
export type SceneAsset = {
  prompt?: string;
  references?: ReferenceUse[];
  id: string;
  name: string;
  description: string;
  image: string;
  hash: string;
  review: Review;
  sources: string[];
};
export type SourceMapping = {
  file: string;
  kind: string;
  target: string;
  reason: string;
};
export type ReferenceUse = {
  asset_id: string;
  file: string;
  hash: string;
  purpose: string;
};
export type Candidate = {
  id: string;
  intent: string;
  round: number;
  prompt: string;
  video_prompt: string;
  image: string;
  hash: string;
  references: ReferenceUse[];
  review: Review;
};
export type CameraPlan = {
  scale: string;
  angle: string;
  composition: string;
  movement: string;
  view: string;
};
export type ContinuityPlan = {
  time: string;
  weather: string;
  lighting: string;
  positions: string;
  gaze: string;
  direction: string;
  props: string;
  end_state: string;
  handoff: string;
};

export type AssetSet = {
  version: number;
  work_quote: string;
  visual: VisualPlan;
  story: {
    synopsis: string;
    relationships: string;
    emotion: string;
    beats: string[];
  };
  characters: Character[];
  scenes: SceneAsset[];
  props: SceneAsset[];
  locked: boolean;
  approvals: Record<string, { hash: string; reason: string; at: string }>;
  intents: Record<string, string>;
};
export const quoteStageLabels: Record<string, string> = {
  asset_plan: "确认故事分析",
  character_masters: "确认角色主图制作",
  reference_assets: "确认参考素材制作",
  storyboard: "确认分镜制作",
  fill: "确认局部补图",
  video: "确认整批视频制作",
};

export function quoteRequestLabel(
  stage: string,
  r: { kind: string; role?: string },
) {
  if (r.kind === "image") {
    if (stage === "character_masters") return "角色正面主图";
    if (stage === "reference_assets")
      return r.role === "character_image" ? "角色其他视角" : "场景或道具参考图";
    return "分镜图片";
  }
  if (r.kind === "text" && stage === "asset_plan") return "故事与资产分析";
  return (
    (
      {
        text: "分镜规划",
        vision: "视觉审核",
        video: "视频镜头",
        tts: "旁白配音",
      } as Record<string, string>
    )[r.kind] ?? r.kind
  );
}
