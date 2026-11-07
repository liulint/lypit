// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Assets } from "./App";
import { StudioProvider } from "./ui";
import type { Project } from "./api";

beforeEach(() => {
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: false, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn() })));
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  const getStyle = window.getComputedStyle.bind(window);
  vi.spyOn(window, "getComputedStyle").mockImplementation(el => getStyle(el));
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

function project(): Project {
  return { id: "p", title: "猫和狗", idea: "故事", script: "", style: "写实", revision: 3, created: "", updated: "", mode: "remote", archived: false, analyzed: true, assets_locked: false, boards_locked: false, shots: [], assets: [
    { id: "cat", kind: "character", name: "橘猫", description: "角色", views: [], master: { file: "cat.png", hash: "cat", approved: false } },
    { id: "dog", kind: "character", name: "黑白边牧", description: "角色", views: [], master: { file: "dog.png", hash: "dog", approved: false } },
    { id: "room", kind: "scene", name: "午后客厅", description: "场景", views: [] },
    { id: "ball", kind: "prop", name: "红色布球", description: "道具", views: [] },
  ] };
}
function actions() { return { quote: vi.fn(), save: vi.fn(), approve: vi.fn(), lock: vi.fn() }; }

it("explains pending approval and only enables the second quote after all masters are confirmed", () => {
  let p = project();
  const calls = actions();
  const view = (p: Project) => <StudioProvider><Assets key={p.revision} p={p} busy={false} {...calls} /></StudioProvider>;
  const rendered = render(view(p));
  expect(screen.getByRole("status").textContent).toContain("确认角色主图（0/2）");
  expect(screen.getByRole("status").textContent).toContain("橘猫、黑白边牧");
  expect(screen.getAllByText("将在第02步制作")).toHaveLength(2);
  expect((screen.getByRole("button", { name: "02 生成设定图与场景道具" }) as HTMLButtonElement).disabled).toBe(true);
  expect(calls.quote).not.toHaveBeenCalled();
  fireEvent.click(screen.getAllByRole("button", { name: "确认角色主图" })[0]);
  expect(calls.approve).toHaveBeenCalledWith(p.assets[0], "master");
  p = { ...p, revision: 4, assets: p.assets.map((a, i) => i === 0 ? { ...a, master: { ...a.master!, approved: true } } : a) };
  rendered.rerender(view(p));
  expect(screen.getByRole("status").textContent).toContain("确认角色主图（1/2）");
  p = { ...p, revision: 5, assets: p.assets.map(a => a.master ? { ...a, master: { ...a.master, approved: true } } : a) };
  rendered.rerender(view(p));
  expect(screen.getByRole("status").textContent).toContain("2张角色四格设定图、1张场景图、1张道具图");
  const next = screen.getByRole("button", { name: "02 生成设定图与场景道具" }) as HTMLButtonElement;
  expect(next.disabled).toBe(false);
  fireEvent.click(next);
  expect(calls.quote).toHaveBeenCalledExactlyOnceWith("details");
});

it("guides final review and locking after all images are generated", () => {
  let p = project();
  p.assets = p.assets.map(a => ({ ...a, master: { file: `${a.id}.png`, hash: a.id, approved: true }, ...(a.kind === "character" ? { sheet: { file: `${a.id}-sheet.png`, hash: a.id, approved: false } } : {}) }));
  const calls = actions();
  const view = (p: Project) => <StudioProvider><Assets key={p.revision} p={p} busy={false} {...calls} /></StudioProvider>;
  const rendered = render(view(p));
  expect(screen.getByRole("status").textContent).toContain("逐项确认素材（还有2项）");
  expect((screen.getByRole("button", { name: "锁定全部素材" }) as HTMLButtonElement).disabled).toBe(true);
  p = { ...p, revision: 4, assets: p.assets.map(a => a.sheet ? { ...a, sheet: { ...a.sheet, approved: true } } : a) };
  rendered.rerender(view(p));
  expect(screen.getByRole("status").textContent).toContain("素材已全部确认");
  fireEvent.click(screen.getByRole("button", { name: "锁定全部素材" }));
  expect(calls.lock).toHaveBeenCalledOnce();
});

it("explains why edited settings must be saved before continuing", () => {
  const p = project();
  render(<StudioProvider><Assets p={p} busy={false} {...actions()} /></StudioProvider>);
  fireEvent.change(screen.getAllByRole("textbox")[0], { target: { value: "新的橘猫设定" } });
  expect(screen.getByRole("status").textContent).toContain("先保存修改后的设定");
  expect((screen.getByRole("button", { name: "02 生成设定图与场景道具" }) as HTMLButtonElement).disabled).toBe(true);
});

it.each([
  { archived: true, title: "先恢复作品再继续" },
  { archived: false, title: "先完成当前任务或操作" },
])("prioritizes blocked operations over locked-asset instructions: $title", ({archived, title}) => {
  const p = { ...project(), archived, assets_locked: true };
  render(<StudioProvider><Assets p={p} busy={true} {...actions()} /></StudioProvider>);
  expect(screen.getByRole("status").textContent).toContain(title);
});
