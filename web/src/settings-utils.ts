import type { Model, Provider, Settings } from "./api";
export const roles: Record<string, string> = {
  text: "剧本与分镜规划",
  image: "通用图片生成",
  character_image: "角色参考图",
  storyboard_image: "分镜画面",
  video: "视频镜头",
  tts: "中文旁白",
  vision: "视觉审核",
};
export const cliNames: Record<string, string> = {
  codex: "Codex",
  claude: "Claude Code",
  gemini: "Gemini",
};
export const protocols: Record<string, { name: string; url: string }> = {
  openai: { name: "OpenAI 兼容", url: "https://api.openai.com/v1" },
  anthropic: { name: "Anthropic", url: "https://api.anthropic.com/v1" },
  gemini: {
    name: "Gemini",
    url: "https://generativelanguage.googleapis.com/v1beta",
  },
  seedance: {
    name: "Seedance",
    url: "https://ark.cn-beijing.volces.com/api/v3",
  },
};
export function supportedKinds(adapter: string): string[] {
  return adapter === "cli"
    ? ["text", "image"]
    : adapter === "seedance"
      ? ["video"]
      : adapter === "anthropic" || adapter === "gemini"
        ? ["text", "vision"]
        : adapter === "openai"
          ? ["text", "vision", "image", "tts"]
          : ["text", "vision", "image", "video", "tts"];
}
export function newModel(id = ""): Model {
  return {
    id,
    capabilities: ["text"],
    reference_images: false,
    max_references: 0,
    structured: true,
    async: false,
    audio: false,
    sizes: ["1024x1536"],
    min_duration: 4,
    max_duration: 15,
    price: null,
    unit: "request",
    price_source: "",
    verified: "",
    price_model: "mini",
    voice: "",
  };
}
export function codexImageModel(m: Model): Model {
  return {
    ...m,
    capabilities: [...new Set([...m.capabilities, "image"])],
    reference_images: true,
    max_references: m.max_references || 6,
    image_edit: true,
    output_formats: m.output_formats?.length
      ? m.output_formats
      : ["png", "jpeg", "webp"],
    sizes: m.sizes.length ? m.sizes : ["1024x1536"],
  };
}
export function imageVerified(p: Provider, m: Model, saved: Settings): boolean {
  const old = saved.providers.find((x) => x.id === p.id);
  return (
    !!old?.image_verified?.[m.id] &&
    JSON.stringify(old.cli) === JSON.stringify(p.cli) &&
    JSON.stringify(old.models.find((x) => x.id === m.id)) === JSON.stringify(m)
  );
}
export function bindingOptions(s: Settings, saved: Settings, role: string) {
  const kind = role.endsWith("_image") ? "image" : role;
  return s.providers.flatMap((p) =>
    p.models
      .filter(
        (m) =>
          m.id &&
          supportedKinds(p.adapter).includes(kind) &&
          m.capabilities.includes(kind) &&
          (kind !== "image" ||
            p.adapter !== "cli" ||
            imageVerified(p, m, saved)),
      )
      .map((m) => ({ value: p.id + "/" + m.id, label: p.name + " / " + m.id })),
  );
}
