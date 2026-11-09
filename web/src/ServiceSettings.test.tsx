// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import ServiceSettings, { vendorSupports } from "./ServiceSettings";
import { StudioProvider } from "./ui";
import type { Settings } from "./api";
beforeEach(() => {
  vi.stubGlobal(
    "matchMedia",
    vi
      .fn()
      .mockImplementation((query) => ({
        matches: false,
        media: query,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
  );
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockImplementation(async () => new Response(JSON.stringify({ tools: {}, models: [] }), { status: 200 })),
  );
  const getStyle = window.getComputedStyle.bind(window);
  vi.spyOn(window, "getComputedStyle").mockImplementation((el) => getStyle(el));
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
it("defaults both concurrency fields to two and saves independent service limits", () => {
  const settings: Settings = {version:1,registry_version:1,mode:"mock",vendors:[],text:{model:""},image:{model:""},video:{model:""},voice:{model:""}};
  const save = vi.fn();
  const {rerender} = render(<StudioProvider><ServiceSettings settings={settings} credentials={{}} busy={false} save={save}/></StudioProvider>);
  expect(screen.queryByText("运行方式")).toBeNull();
  expect(screen.queryByRole("radio", {name:"本地模拟"})).toBeNull();
  expect(screen.queryByRole("radio", {name:"真实服务 · CLI / API"})).toBeNull();
  expect((screen.getByRole("spinbutton",{name:"图片并发数"}) as HTMLInputElement).value).toBe("2");
  expect((screen.getByRole("spinbutton",{name:"视频并发数"}) as HTMLInputElement).value).toBe("2");
  fireEvent.change(screen.getByRole("spinbutton",{name:"图片并发数"}),{target:{value:"3"}});
  fireEvent.change(screen.getByRole("spinbutton",{name:"视频并发数"}),{target:{value:"1"}});
  fireEvent.click(screen.getByRole("button",{name:"保存配置"}));
  expect(save.mock.calls[0][0]).toMatchObject({section:"services",image_concurrency:3,video_concurrency:1});
  rerender(<StudioProvider><ServiceSettings key="vendors" page="vendors" settings={settings} credentials={{}} busy={false} save={save}/></StudioProvider>);
  expect(screen.queryByRole("spinbutton",{name:"图片并发数"})).toBeNull();
});
it("configures vendors separately and reuses a CLI connection across services", async () => {
  const settings: Settings = { version: 1, registry_version: 1, mode: "mock", vendors: [], text: {model:""}, image:{model:""}, video:{model:""}, voice:{model:""} };
  const save = vi.fn();
  const {rerender} = render(<StudioProvider><ServiceSettings page="vendors" settings={settings} credentials={{}} busy={false} save={save} /></StudioProvider>);
  expect(screen.queryByText("运行方式")).toBeNull();
  fireEvent.click(screen.getByRole("button", {name:"新增厂商"}));
  fireEvent.mouseDown(screen.getByRole("combobox", {name:"厂商类型"}));
  fireEvent.click(await screen.findByRole("option", {name:"Codex CLI"}));
  fireEvent.change(screen.getByLabelText("厂商名称"), {target:{value:"我的 Codex"}});
  expect(screen.queryByLabelText("API根地址")).toBeNull();
  fireEvent.click(screen.getByRole("button", {name:"保存到配置草稿"}));
  fireEvent.click(screen.getByRole("button", {name:"保存配置"}));
  const cfg = save.mock.calls[0][0] as Settings;
  expect(save.mock.calls[0][0].section).toBe("vendors");
  save.mockClear();
  rerender(<StudioProvider><ServiceSettings key="services" page="services" settings={cfg} credentials={{}} supported={{[cfg.vendors[0].id]:["text","image"]}} busy={false} save={save} /></StudioProvider>);
  expect(screen.queryByRole("button", {name:"新增厂商"})).toBeNull();
  for (const name of ["文本导演", "图片制作"]) {
    fireEvent.mouseDown(screen.getByRole("combobox", {name:`${name}厂商`}));
    fireEvent.click(await screen.findByRole("option", {name:"我的 Codex · 本地CLI"}));
    expect((screen.getByLabelText(`${name}模型 ID`) as HTMLInputElement).disabled).toBe(false);
  }
  fireEvent.click(screen.getByRole("button", {name:"保存配置"}));
  expect(save.mock.calls[0][0]).toMatchObject({section:"services",text:{vendor_id:cfg.vendors[0].id},image:{vendor_id:cfg.vendors[0].id}});
});

it("keeps Claude image, voice and video capabilities unavailable", () => {
  const v = { id: "c", name: "Claude", type: "cli" as const, cli: "claude" as const };
  expect(vendorSupports(v, "text")).toBe(true);
  for (const cap of ["image", "voice", "video"] as const) expect(vendorSupports(v, cap)).toBe(false);
});

it("accepts an API key directly, keeps it separate from settings, and hides it when editing again", async () => {
  const save = vi.fn();
  const settings: Settings = { version: 1, registry_version: 1, mode: "mock", vendors: [], text: { model: "" }, image: { model: "" }, video: { model: "" }, voice: { model: "" } };
  render(<StudioProvider><ServiceSettings page="vendors" settings={settings} credentials={{}} busy={false} save={save} /></StudioProvider>);
  fireEvent.click(screen.getByRole("button", { name: "新增厂商" }));
  fireEvent.change(screen.getByLabelText("厂商名称"), { target: { value: "视频厂商" } });
  expect(screen.queryByLabelText("凭据环境变量名")).toBeNull();
  const input = screen.getByLabelText("API Key") as HTMLInputElement;
  expect(input.type).toBe("text");
  expect(input.autocomplete).toBe("off");
  expect(input.getAttribute("data-1p-ignore")).toBe("true");
  expect(input.closest(".api-key-masked")).not.toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "显示 API Key" }));
  expect(input.closest(".api-key-masked")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "隐藏 API Key" }));
  expect(input.closest(".api-key-masked")).not.toBeNull();
  fireEvent.change(input, { target: { value: "test-only-browser-secret" } });
  fireEvent.click(screen.getByRole("button", { name: "保存到配置草稿" }));
  await waitFor(() => expect(screen.queryByLabelText("API Key")).toBeNull());
  expect(save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: /编\s*辑/ }));
  expect((await screen.findByLabelText("API Key") as HTMLInputElement).value).toBe("");
  fireEvent.click(screen.getByRole("button", { name: "保存到配置草稿" }));
  fireEvent.click(screen.getByRole("button", { name: "保存配置" }));
  const payload = save.mock.calls[0][0];
  expect(payload.vendors[0].key_mode).toBe("local");
  expect(payload.api_keys[payload.vendors[0].id]).toBe("test-only-browser-secret");
  expect(JSON.stringify(payload.vendors)).not.toContain("test-only-browser-secret");
});

it("keeps a saved local key when its replacement field is left empty", async () => {
  const save = vi.fn();
  const settings: Settings = { version: 2, registry_version: 1, mode: "mock", vendors: [{ id: "saved", name: "已保存厂商", type: "api", protocol: "openai", base: "https://example.com/v1", key_mode: "local", credential_id: "a".repeat(24) }], text: { model: "" }, image: { model: "" }, video: { model: "" }, voice: { model: "" } };
  render(<StudioProvider><ServiceSettings page="vendors" settings={settings} credentials={{saved: true}} busy={false} save={save} /></StudioProvider>);
  fireEvent.click(screen.getByRole("button", { name: /编\s*辑/ }));
  expect((screen.getByLabelText("API Key") as HTMLInputElement).value).toBe("");
  fireEvent.change(screen.getByLabelText("厂商名称"), { target: { value: "改名厂商" } });
  fireEvent.click(screen.getByRole("button", { name: "保存到配置草稿" }));
  fireEvent.click(screen.getByRole("button", { name: "保存配置" }));
  expect(save.mock.calls[0][0].api_keys).toEqual({});
  expect(save.mock.calls[0][0].vendors[0].credential_id).toBe("a".repeat(24));
});

it("installs missing media tools only after clicking and refreshes until both are ready", async () => {
  let running = false;
  let polls = 0;
  const fetch = vi.fn().mockImplementation(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/system/tools/install")) {
      expect(init?.method).toBe("POST");
      running = true;
      return new Response(JSON.stringify({tools:{ffmpeg:false,ffprobe:false},installer:{available:true,status:"running",message:"正在安装"}}));
    }
    const done = running && ++polls >= 1;
    return new Response(JSON.stringify({tools:{ffmpeg:done,ffprobe:done},installer:{available:true,status:done?"succeeded":"idle",message:done?"安装完成":""}}));
  });
  vi.stubGlobal("fetch", fetch);
  const settings: Settings = { version:1,registry_version:1,mode:"mock",vendors:[],text:{model:""},image:{model:""},video:{model:""},voice:{model:""} };
  render(<StudioProvider><ServiceSettings settings={settings} credentials={{}} busy={false} save={vi.fn()} /></StudioProvider>);
  const button = await screen.findByRole("button", {name:"安装缺少的工具"});
  await waitFor(() => expect((button as HTMLButtonElement).disabled).toBe(false));
  expect(fetch.mock.calls.filter(([url]) => url.endsWith("/system/tools/install"))).toHaveLength(0);
  fireEvent.click(button);
  await screen.findByRole("button", {name:"正在安装…"});
  await waitFor(() => expect(screen.getAllByText("已安装")).toHaveLength(2), {timeout:5000});
  expect(fetch.mock.calls.filter(([url]) => url.endsWith("/system/tools/install"))).toHaveLength(1);
});

it("loads compatible models, keeps the selection after refresh failure, and clears it when changing vendor", async () => {
  const settings: Settings = {version:1, registry_version:1, mode:"mock", vendors:[{id:"ark", name:"豆包",kind:"ark",type:"api",protocol:"ark",base:"https://example.com/v3"},{id:"q",name:"千问",kind:"bailian",type:"api",protocol:"openai",base:"https://example.com/v1"}],text:{vendor_id:"ark",model:"doubao-text"},image:{model:""},video:{model:""},voice:{model:""}};
  let fail = false;
  vi.stubGlobal("fetch", vi.fn(async (path: string) => {
    if (path === "/api/system") return new Response(JSON.stringify({tools:{}}));
    if (fail) return new Response(JSON.stringify({error:"模型目录权限不足"}),{status:403});
    return new Response(JSON.stringify({models:[{id:"doubao-text",name:"豆包文本",capabilities:["text"],services:["text"],verified:true},{id:"doubao-image",name:"图片模型",capabilities:["image"],services:[],verified:true}],source:"厂商接口",updated:new Date().toISOString(),stale:false}));
  }));
  const save = vi.fn();
  render(<StudioProvider><ServiceSettings settings={settings} supported={{ark:["text","video"],q:["text"]}} credentials={{}} busy={false} save={save} /></StudioProvider>);
  await waitFor(() => expect(screen.getByRole("combobox",{name:"文本导演模型"}).closest(".ant-select")?.textContent).toContain("豆包文本"));
  fireEvent.mouseDown(screen.getByRole("combobox",{name:"文本导演模型"}));
  expect(await screen.findByRole("option",{name:/豆包文本/})).toBeTruthy();
  expect(screen.queryByRole("option",{name:/图片模型/})).toBeNull();
  fireEvent.keyDown(screen.getByRole("combobox",{name:"文本导演模型"}),{key:"Escape"});
  fail = true;
  fireEvent.click(screen.getByRole("button",{name:"刷新模型"}));
  await screen.findByText("模型目录权限不足");
  expect(screen.getByRole("combobox",{name:"文本导演模型"}).closest(".ant-select")?.textContent).toContain("豆包文本");
  fireEvent.mouseDown(screen.getByRole("combobox",{name:"文本导演厂商"}));
  fireEvent.click(await screen.findByRole("option",{name:"千问 · 远程API"}));
  fireEvent.click(screen.getByRole("button",{name:"保存配置"}));
  expect(save.mock.calls[0][0].text).toEqual({vendor_id:"q",model:""});
});

it("shows unavailable models and a stale warning in the vendor catalog", async () => {
  const settings: Settings = {version:1,registry_version:1,mode:"mock",vendors:[{id:"q",name:"千问",kind:"bailian",type:"api",protocol:"openai",base:"https://example.com/v1"}],text:{model:""},image:{model:""},video:{model:""},voice:{model:""}};
  vi.stubGlobal("fetch",vi.fn(async () => new Response(JSON.stringify({models:[{id:"qwen-image",name:"千问图片",capabilities:["image"],services:[],verified:true}],source:"厂商接口",updated:new Date().toISOString(),stale:true,error:"刷新失败，沿用缓存"}))));
  render(<StudioProvider><ServiceSettings page="vendors" settings={settings} credentials={{}} busy={false} save={vi.fn()} /></StudioProvider>);
  fireEvent.click(screen.getByRole("button",{name:"获取模型"}));
  await screen.findByText(/缓存已过期/);
  fireEvent.click(screen.getByRole("button",{name:"查看模型"}));
  expect(await screen.findByText("千问图片")).toBeTruthy();
  expect(screen.getByText(/待支持/)).toBeTruthy();
});


it("selects CLI execution models for text and images while retaining manual IDs", async () => {
  const settings: Settings = { version:1, registry_version:1, mode:"mock", vendors:[{id:"codex",name:"Codex",type:"cli",cli:"codex"}],text:{vendor_id:"codex",model:""},image:{vendor_id:"codex",model:""},video:{model:""},voice:{model:""} };
  const fetch = vi.fn(async (path: string) => new Response(JSON.stringify(path === "/api/system" ? {tools:{}} : {models:[{id:"exec-model",name:"执行模型",capabilities:["text"],services:["text","image"],verified:false}],source:"Codex 本机模型缓存（只读）",updated:new Date().toISOString(),stale:false})));
  vi.stubGlobal("fetch", fetch);
  const save = vi.fn();
  render(<StudioProvider><ServiceSettings settings={settings} credentials={{}} busy={false} save={save} /></StudioProvider>);
  await screen.findAllByText(/来源：Codex/);
  expect(fetch.mock.calls.filter(([path]) => path.includes("/vendors/codex/models"))).toHaveLength(1);
  for (const name of ["文本导演", "图片制作"]) {
    const select = screen.getByRole("combobox", {name:`${name}模型`});
    expect((select as HTMLInputElement).disabled).toBe(false);
    fireEvent.mouseDown(select);
    fireEvent.click(await screen.findByRole("option", {name:/执行模型/}));
    expect((screen.getByLabelText(`${name}模型 ID`) as HTMLInputElement).value).toBe("exec-model");
  }
  fireEvent.change(screen.getByLabelText("文本导演模型 ID"), {target:{value:"custom-model"}});
  fireEvent.click(screen.getAllByRole("button", {name:"刷新模型"})[0]);
  await waitFor(() => expect(fetch.mock.calls.some(([path]) => path.endsWith("?refresh=1"))).toBe(true));
  expect((screen.getByLabelText("文本导演模型 ID") as HTMLInputElement).value).toBe("custom-model");
  fireEvent.change(screen.getByLabelText("图片制作模型 ID"), {target:{value:""}});
  fireEvent.click(screen.getByRole("button", {name:"保存配置"}));
  expect(save.mock.calls[0][0]).toMatchObject({text:{model:"custom-model"},image:{model:""}});
});


it("disables optional narration explicitly and restores its configuration when enabled again", async () => {
  const settings: Settings = {version:1,registry_version:1,mode:"remote",vendors:[{id:"voice",name:"配音服务",type:"api",protocol:"openai",base:"https://example.com/v1"}],text:{model:""},image:{model:""},video:{model:""},voice:{vendor_id:"voice",model:"tts-1"}};
  const save = vi.fn();
  const fetch = vi.fn(async (_path: string) => new Response(JSON.stringify({models:[],tools:{},source:"测试",updated:new Date().toISOString(),stale:false})));
  vi.stubGlobal("fetch", fetch);
  const {unmount} = render(<StudioProvider><ServiceSettings settings={settings} credentials={{}} supported={{voice:["voice"]}} busy={false} save={save} /></StudioProvider>);
  expect(screen.getByRole("combobox", {name:"配音模型"})).toBeTruthy();
  fireEvent.mouseDown(screen.getByRole("combobox", {name:"配音厂商"}));
  fireEvent.click(await screen.findByRole("option", {name:"无（不使用统一旁白）"}));
  expect(screen.queryByRole("combobox", {name:"配音模型"})).toBeNull();
  expect(screen.getByText("未启用统一旁白，无需配置旁白模型和 API Key。")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", {name:"保存配置"}));
  const saved = save.mock.calls[0][0];
  expect(saved.voice).toEqual({vendor_id:"",model:""});
  expect(saved.vendors).toEqual(settings.vendors);
  unmount();
  fetch.mockClear();
  render(<StudioProvider><ServiceSettings settings={saved} credentials={{}} supported={{voice:["voice"]}} busy={false} save={save} /></StudioProvider>);
  expect(screen.queryByRole("combobox", {name:"配音模型"})).toBeNull();
  await waitFor(() => expect(fetch).toHaveBeenCalled());
  expect(fetch.mock.calls.every(call => !String(call[0]).includes("/vendors/"))).toBe(true);
  fireEvent.mouseDown(screen.getByRole("combobox", {name:"配音厂商"}));
  fireEvent.click(await screen.findByRole("option", {name:"配音服务 · 远程API"}));
  expect(screen.getByRole("combobox", {name:"配音模型"})).toBeTruthy();
});

it("shows model prices in both the dropdown and selected detail", async () => {
  const pricing = {rows:[{label:"视频生成",condition:"480p / 720p · 不含视频输入",amount:"23.00",unit:"百万视频 Token",currency:"CNY"}],source:"火山官方参考价",url:"https://www.volcengine.com/docs/82379/1544106",checked:"2026-11-06",note:"按厂商账单结算"};
  vi.mocked(fetch).mockImplementation(async () => new Response(JSON.stringify({models:[{id:"mini",name:"Seedance Mini",capabilities:["video"],services:["video"],verified:true,pricing}],updated:"2026-11-06T00:00:00Z"})));
  const settings: Settings = {version:1,registry_version:1,mode:"mock",vendors:[{id:"ark",name:"火山",type:"api",kind:"ark",protocol:"ark"}],text:{model:""},image:{model:""},video:{vendor_id:"ark",model:"mini"},voice:{model:""}};
  render(<StudioProvider><ServiceSettings settings={settings} credentials={{}} supported={{ark:["video"]}} busy={false} save={vi.fn()} /></StudioProvider>);
  expect(await screen.findByText("¥23.00 / 百万视频 Token")).toBeTruthy();
  fireEvent.mouseDown(screen.getByRole("combobox",{name:"视频生成模型"}));
  expect(await screen.findByText("视频生成 · 480p / 720p · 不含视频输入：¥23.00 / 百万视频 Token")).toBeTruthy();
});


it("shows the persistent data directory and reports backup failures and completion", async () => {
  let fail = true;
  const fetcher = vi.fn(async (path: string, init?: RequestInit) => {
    if (path === "/api/system/data") return new Response(JSON.stringify({directory:"/users/test/Lypit Studio",backup_directory:"/users/test/Lypit Studio Backups"}));
    if (path === "/api/system/data/backup") {
      expect(init?.method).toBe("POST");
      if (fail) return new Response(JSON.stringify({error:"请等待当前任务结束后再备份"}),{status:400});
      return new Response(JSON.stringify({directory:"/users/test/Lypit Studio Backups/snapshot"}));
    }
    return new Response(JSON.stringify({tools:{}}));
  });
  vi.stubGlobal("fetch",fetcher);
  const settings: Settings = {version:1,registry_version:1,mode:"mock",vendors:[],text:{model:""},image:{model:""},video:{model:""},voice:{model:""}};
  render(<StudioProvider><ServiceSettings settings={settings} credentials={{}} busy={false} save={vi.fn()} /></StudioProvider>);
  await screen.findByText("/users/test/Lypit Studio");
  fireEvent.click(screen.getByRole("button",{name:"备份全部数据"}));
  await screen.findByText("请等待当前任务结束后再备份");
  fail = false;
  fireEvent.click(screen.getByRole("button",{name:"备份全部数据"}));
  await screen.findByText("完整备份已保存");
  expect(screen.getByText("/users/test/Lypit Studio Backups/snapshot")).toBeTruthy();
});
