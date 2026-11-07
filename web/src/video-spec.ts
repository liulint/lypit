import type { Project } from "./api";

export type AspectRatio = "9:16" | "16:9";
export function videoSpec(p: Pick<Project, "aspect_ratio" | "duration">) {
  const aspect = p.aspect_ratio ?? "9:16";
  const duration = p.duration ?? 60;
  return {
    aspect,
    duration,
    label: aspect === "16:9" ? "16:9 横屏" : "9:16 竖屏",
    width: aspect === "16:9" ? 640 : 360,
    height: aspect === "16:9" ? 360 : 640,
  };
}
