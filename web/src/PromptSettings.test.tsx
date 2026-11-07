// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import PromptSettings, { type PromptLibrary } from "./PromptSettings";
import { StudioProvider } from "./ui";

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: false, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn() })));
  const style = window.getComputedStyle.bind(window);
  vi.spyOn(window, "getComputedStyle").mockImplementation(element => style(element));
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

function fixture(): PromptLibrary {
  const names = ["剧本与资产分析", "分镜规划", "单镜改写", "角色主图", "角色四视图", "场景 / 道具参考图", "分镜图", "分镜重新生成", "基于当前图修改", "视频生成"];
  return { version: 7, max_bytes: 50000, items: names.map((name, i) => ({ id: i === 0 ? "analyze" : `prompt-${i}`, name, group: i < 3 ? "文字" : i < 9 ? "图片" : "视频", description: `${name}的用途`, template: `原模板${i} {{style}}`, default_template: `原模板${i} {{style}}`, variables: [{ name: "style", label: "作品风格", fixed: "" }] })) };
}

it("keeps drafts across all ten types and saves them with the loaded version without generating", async () => {
  const library = fixture();
  const fetcher = vi.fn(async (_url: string, options?: RequestInit) => {
    if (options?.method === "PUT") {
      const body = JSON.parse(String(options.body));
      return new Response(JSON.stringify({ ...library, version: 8, items: library.items.map(item => ({ ...item, template: body.templates[item.id] })) }));
    }
    return new Response(JSON.stringify(library));
  });
  vi.stubGlobal("fetch", fetcher);
  const dirty = vi.fn(), saved = vi.fn();
  render(<StudioProvider><PromptSettings onDirty={dirty} onSaved={saved} /></StudioProvider>);
  const editor = await screen.findByRole("textbox", { name: "提示词内容" });
  fireEvent.change(editor, { target: { value: "更具体的剧本要求 {{style}}" } });
  fireEvent.click(screen.getByRole("button", { name: "角色主图" }));
  expect((editor as HTMLTextAreaElement).value).toBe("原模板3 {{style}}");
  fireEvent.change(editor, { target: { value: "统一身份 {{style}}" } });
  fireEvent.click(screen.getByRole("button", { name: "剧本与资产分析" }));
  expect((editor as HTMLTextAreaElement).value).toBe("更具体的剧本要求 {{style}}");
  expect(screen.getByText("2 项未保存")).toBeTruthy();
  expect(fetcher).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole("button", { name: "保存全部修改" }));
  await waitFor(() => expect(saved).toHaveBeenCalledOnce());
  const body = JSON.parse(String(fetcher.mock.calls[1][1]?.body));
  expect(body.version).toBe(7);
  expect(Object.keys(body.templates)).toHaveLength(10);
  expect(body.templates.analyze).toBe("更具体的剧本要求 {{style}}");
  expect(body.templates["prompt-3"]).toBe("统一身份 {{style}}");
  expect(dirty).toHaveBeenLastCalledWith(false);
  expect(fetcher.mock.calls.every(([path]) => path === "/api/prompts")).toBe(true);
});

it("restores only the selected default as an unsaved draft", async () => {
  const library = fixture(); library.items[0].template = "已保存的定制 {{style}}";
  const fetcher = vi.fn(async () => new Response(JSON.stringify(library)));
  vi.stubGlobal("fetch", fetcher);
  render(<StudioProvider><PromptSettings /></StudioProvider>);
  await screen.findByRole("textbox", { name: "提示词内容" });
  fireEvent.click(screen.getByRole("button", { name: "恢复当前默认值" }));
  fireEvent.click(await screen.findByRole("button", { name: "恢复默认值" }));
  await waitFor(() => expect((screen.getByRole("textbox", { name: "提示词内容" }) as HTMLTextAreaElement).value).toBe("原模板0 {{style}}"));
  expect(screen.getByText("1 项未保存")).toBeTruthy();
  expect(fetcher).toHaveBeenCalledOnce();
});

it("preserves edits after a version conflict and requires confirmation before reloading", async () => {
  const fetcher = vi.fn(async (_path: string, opts?: RequestInit) => new Response(JSON.stringify(opts?.method === "PUT" ? { error: "数据版本冲突，请刷新" } : fixture()), { status: opts?.method === "PUT" ? 409 : 200 }));
  vi.stubGlobal("fetch", fetcher);
  render(<StudioProvider><PromptSettings /></StudioProvider>);
  const editor = await screen.findByRole("textbox", { name: "提示词内容" });
  fireEvent.change(editor, { target: { value: "保留我的修改 {{style}}" } });
  fireEvent.click(screen.getByRole("button", { name: "保存全部修改" }));
  await screen.findByText("数据版本冲突，请刷新");
  expect((editor as HTMLTextAreaElement).value).toBe("保留我的修改 {{style}}");
  fireEvent.click(screen.getByRole("button", { name: "重新加载" }));
  await screen.findByRole("button", { name: "继续编辑" });
  expect(fetcher).toHaveBeenCalledTimes(2);
  fireEvent.click(screen.getByRole("button", { name: "继续编辑" }));
  expect((editor as HTMLTextAreaElement).value).toBe("保留我的修改 {{style}}");
});

it("offers retry on load failure and filters types by name", async () => {
  const fetcher = vi.fn().mockRejectedValueOnce(new Error("offline")).mockResolvedValue(new Response(JSON.stringify(fixture())));
  vi.stubGlobal("fetch", fetcher);
  render(<StudioProvider><PromptSettings /></StudioProvider>);
  fireEvent.click(await screen.findByRole("button", { name: /重\s*试/ }));
  await screen.findByRole("textbox", { name: "提示词内容" });
  fireEvent.change(screen.getByRole("searchbox", { name: "搜索提示词" }), { target: { value: "角色" } });
  expect(screen.getByRole("button", { name: "角色主图" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: "分镜规划" })).toBeNull();
});
