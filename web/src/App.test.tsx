// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import App, { Story } from "./App";
import type { Project } from "./api";
import { StudioProvider } from "./ui";
beforeEach(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.stubGlobal(
    "matchMedia",
    vi.fn().mockImplementation((query) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  );
  localStorage.clear();
  const getStyle = window.getComputedStyle.bind(window);
  vi.spyOn(window, "getComputedStyle").mockImplementation((element) =>
    getStyle(element),
  );
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
it("opens the independent Token usage table from the sidebar", async () => {
 const p = {id:"usage-project", title:"用量记录", idea:"故事", assets:[],shots:[],mode:"remote"};
 const usage = {id:"attempt",task_id:"task",project_id:p.id,stage:"analyze",status:"completed",vendor:"本机 Codex",model:"test-model",transport:"cli",input_tokens:12345,output_tokens:42,cached_input_tokens:0,reason:"CLI 费用未知"};
 vi.stubGlobal("fetch",vi.fn(async(path:string)=>new Response(JSON.stringify(path==="/api/projects" ? [p] : path==="/api/tasks" ? [] : path==="/api/usage" ? [usage] : {settings:{version:1,mode:"remote",text:{},image:{},video:{},voice:{}},credentials:{}}))));
 render(<StudioProvider><App /></StudioProvider>);
 fireEvent.click(await screen.findByRole("button",{name:"Token 用量"}));
 expect(await screen.findByText("本机 Codex")).toBeTruthy();
 expect(screen.getByText("12,345")).toBeTruthy();
 expect(screen.getByText("42")).toBeTruthy();
 expect(screen.getByText("未知")).toBeTruthy();
 expect(screen.getByRole("columnheader",{name:"输入 Token"})).toBeTruthy();
 expect(screen.queryByRole("button",{name:/Token 用量，/})).toBeNull();
});
it("saves the reviewed draft and uses its returned revision for a single-shot quote", async () => {
  const first={id:"first",title:"第一镜",first_frame:"静态首帧",source_text:"原剧本",description:"向前走",narration:"",duration:5,references:["asset"],review_status:"reviewed",candidates:[],selected:0,approved:false};
  let p={id:"trial-project",title:"单镜试做测试",idea:"故事",script:"原剧本",style:"电影",duration:10,revision:7,mode:"mock",assets_locked:true,boards_locked:false,assets:[{id:"asset",kind:"scene",name:"场景",description:"",views:[]}],shots:[],planned_shots:[first,{...first,id:"second",title:"第二镜",review_status:"pending"}]};
  const fetcher=vi.fn(async(path:string,opts?:RequestInit)=>{
    let data:unknown=[];
    if(path==="/api/projects") data=[p];
    if(path==="/api/settings")data={settings:{version:1,mode:"mock",text:{},image:{},video:{},voice:{}},credentials:{}};
    if(path===`/api/projects/${p.id}/board-plan`){const body=JSON.parse(String(opts?.body));p={...p,revision:8,planned_shots:body.shots};data=p;}
    if(path===`/api/projects/${p.id}/quotes`)data={id:"trial-quote",project_id:p.id,revision:8,settings_version:1,stage:"board_retry",plan_trial:true,shot_id:"first",items:[{label:"仅第一镜一张图片",count:1}],mode:"mock",unknown:false,aspect_ratio:"9:16",duration:10,expires:new Date(Date.now()+900000).toISOString()};
    return new Response(JSON.stringify(data));
  });
  vi.stubGlobal("fetch",fetcher);
  render(<StudioProvider><App /></StudioProvider>);
  fireEvent.click(await screen.findByRole("button",{name:p.title}));
  fireEvent.click(screen.getAllByRole("button",{name:"试做本镜图片 · 查看报价"})[0]);
  await screen.findByText("规划单镜试做");
  const save=fetcher.mock.calls.find(([url])=>url.endsWith("/board-plan"));
  const quote=fetcher.mock.calls.find(([url])=>url.endsWith("/quotes"));
  expect(JSON.parse(String(save?.[1]?.body))).toMatchObject({revision:7,approve:false});
  expect(JSON.parse(String(quote?.[1]?.body))).toEqual({stage:"board_retry",shot_id:"first",feedback:"",plan_trial:true,revision:8});
  expect(fetcher.mock.calls.some(([url])=>url.endsWith("/confirm"))).toBe(false);
  expect(screen.getByRole("button",{name:"确认并开始"})).toBeTruthy();
});
it.each([
  { aspect: "9:16", duration: 60, audio: "dialogue" },
  { aspect: "16:9", duration: 17, audio: "narration" },
])(
  "creates $aspect / $duration seconds without login",
  async ({ aspect, duration, audio }) => {
    let projects: unknown[] = [];
    const fetcher = vi.fn(async (path: string, opts?: RequestInit) => {
      let data: unknown = [];
      if (path === "/api/settings")
        data = {
          settings: {
            version: 1,
            mode: "mock",
            text: {},
            image: {},
            video: {},
            voice: {},
          },
          credentials: {},
        };
      if (path === "/api/projects") {
        if (opts?.method === "POST") {
          const body = JSON.parse(String(opts.body));
          data = {
            ...body,
            id: "p1",
            revision: 1,
            assets: [],
            shots: [],
            mode: "mock",
            created: new Date().toISOString(),
            updated: new Date().toISOString(),
          };
          projects = [data];
        } else data = projects;
      }
      return new Response(JSON.stringify(data), { status: 200 });
    });
    vi.stubGlobal("fetch", fetcher);
    render(
      <StudioProvider>
        <App />
      </StudioProvider>,
    );
    await screen.findByRole("heading", {name: /^我的作品/, level: 2});
    expect(screen.queryByText("登录")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "新建作品" }));
    const voiceMode = screen.getByRole("combobox", {name: "声音方式"});
    expect(voiceMode.closest(".ant-select")?.textContent).toContain("角色对白（动物自己说话）");
    if (audio === "narration") {
      fireEvent.mouseDown(voiceMode);
      fireEvent.click(await screen.findByRole("option", {name: "旁白讲述"}));
    }
    const ratio = screen.getByRole("combobox", { name: "画幅" });
    expect(ratio.closest(".ant-select")?.textContent).toContain("9:16 竖屏");
    expect(
      (screen.getByLabelText("目标时长（秒）") as HTMLInputElement).value,
    ).toBe("60");
    fireEvent.mouseDown(ratio);
    fireEvent.click(
      await screen.findByRole("option", {
        name: aspect === "16:9" ? "16:9 横屏" : "9:16 竖屏",
      }),
    );
    fireEvent.change(screen.getByLabelText("目标时长（秒）"), {
      target: { value: String(duration) },
    });
    fireEvent.change(screen.getByLabelText("作品名称"), {
      target: { value: "明天的旧书" },
    });
    fireEvent.change(screen.getByLabelText("故事想法"), {
      target: { value: "一本旧书引发的故事" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建作品" }));
    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 1 }).textContent).toContain(
        "明天的旧书",
      ),
    );
    expect(screen.getByRole("button", { name: /分析故事/ })).toBeTruthy();
    expect(
      fetcher.mock.calls.some(
        ([p, o]) => p === "/api/projects" && o?.method === "POST",
      ),
    ).toBe(true);
    const request = fetcher.mock.calls.find(
      ([p, o]) => p === "/api/projects" && o?.method === "POST",
    );
    expect(JSON.parse(String(request?.[1]?.body))).toMatchObject({
      aspect_ratio: aspect,
      duration,
      audio_mode: audio,
    });
  },
);

it.each([false, true])("requires explicit acknowledgement before confirming a paid quote (estimate: %s)", async (estimated) => {
  const p = {
    id: "paid-project",
    title: "费用测试",
    idea: "故事",
    style: "电影",
    script: "",
    revision: 1,
    aspect_ratio: "9:16",
    duration: 60,
    assets: [],
    shots: [],
    mode: "remote",
    created: new Date().toISOString(),
    updated: new Date().toISOString(),
  };
  const fetcher = vi.fn(async (path: string, opts?: RequestInit) => {
    let data: unknown = [];
    if (path === "/api/projects") data = [p];
    if (path === "/api/settings")
      data = {
        settings: {
          version: 1,
          mode: "remote",
          text: {},
          image: {},
          video: {},
          voice: {},
        },
        credentials: {},
      };
    if (path === "/api/projects/paid-project/quotes")
      data = {
        id: "paid-quote",
        project_id: p.id,
        revision: 1,
        settings_version: 1,
        stage: "analyze",
        items: [{ label: "故事分析", count: 1 }],
        mode: "remote",
        unknown: true,
        estimate: estimated ? { amount: "3.23", currency: "CNY", model: "doubao-seedance-2-0-260128", basis: "480p · 7秒 · 按视频Token估算", source: "火山方舟官方参考价", url: "https://www.volcengine.com/docs/82379/1544106", checked: "2026-11-07" } : undefined,
        aspect_ratio: "9:16",
        duration: 60,
        expires: new Date(Date.now() + 900000).toISOString(),
      };
    if (path === "/api/quotes/paid-quote/confirm") data = { id: "task" };
    return new Response(JSON.stringify(data), { status: 200 });
  });
  vi.stubGlobal("fetch", fetcher);
  render(
    <StudioProvider>
      <App />
    </StudioProvider>,
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "费用测试" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "分析故事 · 查看报价" }));
  const checkbox = await screen.findByRole("checkbox", { name: estimated ? /我知晓以上为参考估算/ : /我接受/ });
  if (estimated) {
    expect(screen.getByText("约 ¥ 3.23")).toBeTruthy();
    expect(screen.queryByText("金额未知")).toBeNull();
    expect(screen.getByRole("link", {name: "火山方舟官方参考价"}).getAttribute("href")).toContain("volcengine.com");
  }
  const confirm = screen.getByRole("button", { name: "确认并开始" });
  expect((confirm as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(checkbox);
  await waitFor(() =>
    expect((confirm as HTMLButtonElement).disabled).toBe(false),
  );
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(
      fetcher.mock.calls.some(
        ([path]) => path === "/api/quotes/paid-quote/confirm",
      ),
    ).toBe(true),
  );
  const call = fetcher.mock.calls.find(
    ([path]) => path === "/api/quotes/paid-quote/confirm",
  );
  expect(JSON.parse(String(call?.[1]?.body))).toEqual({ allow_unknown: true });
});

it("saves the chosen audio mode in the script stage and explains what is reset", async () => {
  const p = {id: "p", title: "猫和狗", idea: "一起玩球", style: "电影", script: "猫与狗的故事", revision: 12, assets: [], shots: [], analyzed: true, mode: "remote"} as unknown as Project;
  const save = vi.fn();
  render(<StudioProvider><Story p={p} busy={false} onSave={save} onAnalyze={vi.fn()} /></StudioProvider>);
  const select = screen.getByRole("combobox", {name: "声音方式"});
  expect(select.closest(".ant-select")?.textContent).toContain("旁白讲述");
  fireEvent.mouseDown(select);
  fireEvent.click(await screen.findByRole("option", {name: "角色对白（动物自己说话）"}));
  expect(screen.getByText(/仅切换声音方式会保留图片/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", {name: /保存/}));
  expect(save).toHaveBeenCalledWith(expect.objectContaining({audio_mode: "dialogue", script: p.script, revision: 12}));
});

it("separates vendor and service pages and guards unsaved navigation", async () => {
  vi.stubGlobal("fetch",vi.fn(async (path:string) => new Response(JSON.stringify(path === "/api/settings" ? {settings:{version:1,registry_version:1,mode:"mock",vendors:[],text:{model:""},image:{model:""},video:{model:""},voice:{model:""}},credentials:{},vendor_capabilities:{}} : path === "/api/system" ? {tools:{}} : []))));
  render(<StudioProvider><App /></StudioProvider>);
  await screen.findByRole("heading", {name: /^我的作品/, level: 2});
  fireEvent.click(screen.getByRole("button",{name:"厂商管理"}));
  await screen.findByRole("button",{name:"新增厂商"});
  expect(screen.queryByRole("tab")).toBeNull();
  fireEvent.click(screen.getByRole("button",{name:"新增厂商"}));
  fireEvent.click(screen.getByRole("button",{name:/取\s*消/}));
  fireEvent.click(screen.getByRole("button",{name:"服务配置"}));
  await screen.findByRole("combobox",{name:"文本导演厂商"});
  fireEvent.change(screen.getByRole("spinbutton",{name:"图片并发数"}),{target:{value:"3"}});
  fireEvent.click(screen.getByRole("button",{name:"厂商管理"}));
  expect(await screen.findByRole("button",{name:"继续编辑"})).toBeTruthy();
  fireEvent.click(screen.getByRole("button",{name:"继续编辑"}));
  expect(screen.getByRole("combobox",{name:"文本导演厂商"})).toBeTruthy();
  fireEvent.click(screen.getByRole("button",{name:"厂商管理"}));
  fireEvent.click(await screen.findByRole("button",{name:"放弃并离开"}));
  await screen.findByRole("button",{name:"新增厂商"});
});


it("refuses an old backend quote before allowing any paid confirmation", async () => {
  const p:Project={id:"reference-project",revision:1,title:"参考板测试",idea:"故事",script:"剧本",style:"电影",created:"",updated:"",mode:"remote",archived:false,analyzed:true,assets_locked:true,boards_locked:false,assets:[{id:"a",kind:"scene",name:"场景",description:"",views:[]}],shots:[{id:"shot",title:"缺图镜头",description:"画面",narration:"旁白",duration:8,references:["a"],candidates:[],selected:0,approved:false}]};
  const fetcher=vi.fn(async(path:string)=>{
    let data:unknown=[];
    if(path==="/api/projects")data=[p];
    if(path==="/api/tasks")data=[{id:"old",project_id:p.id,quote_id:"old-quote",stage:"board_retry",shot_id:"shot",status:"failed",progress:5,message:"failure",created:"",updated:"",mode:"remote"}];
    if(path==="/api/settings")data={settings:{version:1,mode:"remote",text:{},image:{},video:{},voice:{}},credentials:{}};
    if(path===`/api/projects/${p.id}/quotes`)data={id:"old-quote-response",project_id:p.id,revision:1,settings_version:1,stage:"board_retry",items:[{label:"补图",count:1}],mode:"remote",unknown:true,expires:new Date(Date.now()+900000).toISOString()};
    return new Response(JSON.stringify(data));
  });
  vi.stubGlobal("fetch",fetcher);
  render(<StudioProvider><App /></StudioProvider>);
  fireEvent.click(await screen.findByRole("button",{name:p.title}));
  fireEvent.click(await screen.findByRole("button",{name:"补生成 · 查看报价"}));
  fireEvent.click(screen.getByRole("button",{name:"查看补生成报价"}));
  expect((await screen.findAllByText(/后端尚未提供有效的参考板报价方案/)).length).toBeGreaterThan(0);
  expect(screen.queryByRole("button",{name:"确认并开始"})).toBeNull();
  expect(screen.getByRole("button",{name:"查看补生成报价"})).toBeTruthy();
  expect(fetcher.mock.calls.some(([url])=>url.endsWith("/confirm"))).toBe(false);
});

it("quotes a second image without saving or blocking on another running draft shot", async () => {
  const first={id:"first",title:"第一镜",first_frame:"静态首帧",source_text:"原剧本",description:"向前走",narration:"",duration:5,references:["asset"],review_status:"reviewed",candidates:[],selected:0,approved:false};
  let p={id:"trial-project",title:"单镜试做测试",idea:"故事",script:"原剧本",style:"电影",duration:10,revision:7,mode:"mock",assets_locked:true,boards_locked:false,assets:[{id:"asset",kind:"scene",name:"场景",description:"",views:[]}],shots:[],planned_shots:[first,{...first,id:"second",title:"第二镜",review_status:"pending"}]};
  const fetcher=vi.fn(async(path:string,opts?:RequestInit)=>{
    let data:unknown=[];
    if(path==="/api/projects") data=[p];
    if(path==="/api/settings")data={settings:{version:1,mode:"mock",text:{},image:{},video:{},voice:{}},credentials:{}};
    if(path==="/api/tasks")data=[{id:"running-image",project_id:p.id,stage:"board_retry",status:"running",plan_trial:true,shot_id:"second",shot_input_hash:"original",progress:5,message:"正在生图"}];
    if(path===`/api/projects/${p.id}/board-plan`){const body=JSON.parse(String(opts?.body));p={...p,revision:8,planned_shots:body.shots};data=p;}
    if(path===`/api/projects/${p.id}/quotes`)data={id:"trial-quote",project_id:p.id,revision:7,settings_version:1,stage:"board_retry",plan_trial:true,shot_id:"first",items:[{label:"仅第一镜一张图片",count:1}],mode:"mock",unknown:false,aspect_ratio:"9:16",duration:10,expires:new Date(Date.now()+900000).toISOString()};
    return new Response(JSON.stringify(data));
  });
  vi.stubGlobal("fetch",fetcher);
  render(<StudioProvider><App /></StudioProvider>);
  fireEvent.click(await screen.findByRole("button",{name:p.title}));
  fireEvent.click(screen.getAllByRole("button",{name:"试做本镜图片 · 查看报价"})[0]);
  await screen.findByText("规划单镜试做");
  const save=fetcher.mock.calls.find(([url])=>url.endsWith("/board-plan"));
  const quote=fetcher.mock.calls.find(([url])=>url.endsWith("/quotes"));
  expect(save).toBeUndefined();
  expect(JSON.parse(String(quote?.[1]?.body))).toEqual({stage:"board_retry",shot_id:"first",feedback:"",plan_trial:true,revision:7});
  expect(fetcher.mock.calls.some(([url])=>url.endsWith("/confirm"))).toBe(false);
  expect(screen.getByRole("button",{name:"确认并开始"})).toBeTruthy();
});

it.each([false,true])("adopts the reviewed plan before the batch quote; approval failure=%s", async (failApproval) => {
  const first={id:"first",title:"第一镜",first_frame:"静态首帧",source_text:"原剧本",description:"向前走",narration:"",duration:5,references:["asset"],review_status:"reviewed",candidates:[{file:"existing.png",hash:"old",approved:true}],selected:0,approved:true,video:"existing.mp4"};
  const second={...first,id:"second",title:"第二镜",candidates:[],approved:false,video:""};
  let p={id:"batch-project",title:"批量生图测试",idea:"故事",script:"原剧本",style:"电影",duration:10,revision:7,mode:"mock",assets_locked:true,boards_locked:false,assets:[{id:"asset",kind:"scene",name:"场景",description:"",views:[]}],shots:[] as typeof first[],planned_shots:[first,second]};
  const fetcher=vi.fn(async(path:string,opts?:RequestInit)=>{
    let data:unknown=[];
    if(path==="/api/projects")data=[p];
    if(path==="/api/settings")data={settings:{version:1,mode:"mock",text:{},image:{},video:{},voice:{}},credentials:{}};
    if(path.endsWith("/board-plan")){
      if(failApproval)return new Response(JSON.stringify({error:"规划版本已变化"}),{status:409});
      p={...p,revision:8,shots:JSON.parse(String(opts?.body)).shots,planned_shots:[]};data=p;
    }
    if(path.endsWith("/quotes"))data={id:"batch-quote",project_id:p.id,revision:8,settings_version:1,stage:"boards",items:[{label:"分镜图片（1镜×1张）",count:1}],mode:"mock",unknown:false,aspect_ratio:"9:16",duration:10,expires:new Date(Date.now()+900000).toISOString()};
    return new Response(JSON.stringify(data));
  });
  vi.stubGlobal("fetch",fetcher);
  render(<StudioProvider><App /></StudioProvider>);
  fireEvent.click(await screen.findByRole("button",{name:p.title}));
  fireEvent.click(screen.getByRole("button",{name:"全部生成分镜图 · 查看报价"}));
  expect(fetcher.mock.calls.some(([url])=>url.endsWith("/quotes")||url.endsWith("/board-plan"))).toBe(false);
  expect(screen.getByText(/仅补齐1个缺图镜头/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button",{name:"确认采用并查看报价"}));
  if(failApproval){
    await screen.findByText("规划版本已变化");
    expect(fetcher.mock.calls.some(([url])=>url.endsWith("/quotes"))).toBe(false);
  }else{
    await screen.findByRole("button",{name:"确认并开始"});
    const save=fetcher.mock.calls.find(([url])=>url.endsWith("/board-plan"));
    const quote=fetcher.mock.calls.find(([url])=>url.endsWith("/quotes"));
    expect(JSON.parse(String(save?.[1]?.body))).toEqual({revision:7,shots:[first,second],approve:true});
    expect(JSON.parse(String(quote?.[1]?.body))).toEqual({stage:"boards",revision:8});
    expect(screen.getByText("分镜图片（1镜×1张）")).toBeTruthy();
    expect(p.shots[0].video).toBe("existing.mp4");
  }
  expect(fetcher.mock.calls.some(([url])=>url.endsWith("/confirm"))).toBe(false);
}, 30000);

it.each([{status:"interrupted",label:"恢复已有结果",action:"resume"},{status:"running",label:"停止",action:"cancel"},{status:"failed",label:/放\s*弃/,action:"abandon"}])("handles a $status shot task inside the storyboard without navigating or generating again", async ({status,label,action})=>{
 const shot={id:"inline-shot",title:"当前镜头",first_frame:"首帧",source_text:"剧本",description:"动作",narration:"台词",duration:8,references:[],review_status:"reviewed",candidates:[{file:"frame.png",hash:"f",approved:true}],selected:0,approved:true};
 const p={id:"inline-project",title:"就地处理任务",idea:"故事",script:"剧本",style:"电影",duration:8,revision:1,mode:"remote",assets:[],shots:[shot],assets_locked:true,boards_locked:false,plan_approved:true,plan_version:2};
 let tasks:unknown[]=[{id:"inline-task",project_id:p.id,shot_id:shot.id,stage:"shot_video",status,progress:25,message:"保留原请求，提交结果未知",created:"2026-11-07T00:00:00Z",mode:"remote"}];
 const fetcher=vi.fn(async(path:string,opts?:RequestInit)=>{
  let data:unknown=[];
  if(path==="/api/projects")data=[p];
  if(path==="/api/tasks")data=tasks;
  if(path==="/api/settings")data={settings:{version:1,mode:"remote",text:{},image:{},video:{},voice:{}},credentials:{}};
  if(path===`/api/tasks/inline-task/${action}`&&opts?.method==="POST")tasks=[];
  return new Response(JSON.stringify(data));
 });
 vi.stubGlobal("fetch",fetcher);
 render(<StudioProvider><App/></StudioProvider>);
 fireEvent.click(await screen.findByRole("button",{name:p.title}));
 expect(screen.queryByRole("button",{name:"在此处理任务"})).toBeNull();
 fireEvent.click(screen.getByRole("button",{name:"重新生成视频"}));
 const panel=within(await screen.findByRole("dialog"));
 expect(panel.getByText("处理当前作品任务")).toBeTruthy();
 expect(panel.getByText(/当前镜头：当前镜头/)).toBeTruthy();
 expect(panel.getByText("保留原请求，提交结果未知")).toBeTruthy();
 fireEvent.click(panel.getByRole("button",{name:label}));
 if(action==="abandon") {
  expect(fetcher.mock.calls.some(([,opts])=>opts?.method==="POST")).toBe(false);
  fireEvent.click(await screen.findByRole("button",{name:"继续保留"}));
  expect(fetcher.mock.calls.some(([,opts])=>opts?.method==="POST")).toBe(false);
  fireEvent.click(panel.getByRole("button",{name:label}));
  fireEvent.click(await screen.findByRole("button",{name:"确认放弃"}));
 }
 await waitFor(()=>expect(fetcher.mock.calls.some(([path,opts])=>path===`/api/tasks/inline-task/${action}`&&opts?.method==="POST")).toBe(true));
 await panel.findByText("当前没有待处理任务，可以关闭面板继续制作。");
 fireEvent.click(panel.getByRole("button",{name:"关闭"}));
 await waitFor(()=>expect((screen.getByRole("button",{name:"生成本镜视频 · 查看报价"}) as HTMLButtonElement).disabled).toBe(false));
 expect(screen.queryByRole("heading",{name:"任务中心"})).toBeNull();
 expect(fetcher.mock.calls.filter(([path,opts])=>opts?.method==="POST").map(([path])=>path)).toEqual([`/api/tasks/inline-task/${action}`]);
 expect(fetcher.mock.calls.some(([path])=>path.includes("/quotes"))).toBe(false);
}, 30000);

it("opens prompt management from the sidebar and guards unsaved navigation", async () => {
 const names=["剧本与资产分析","分镜规划","单镜改写","角色主图","角色四视图","场景 / 道具参考图","分镜图","分镜重新生成","基于当前图修改","视频生成"];
 const prompts={version:1,max_bytes:50000,items:names.map((name,i)=>({id:i===0?"analyze":`p-${i}`,name,group:i<3?"文字":i<9?"图片":"视频",description:"模板",template:"默认 {{style}}",default_template:"默认 {{style}}",variables:[]}))};
 const fetcher=vi.fn(async(path:string)=>new Response(JSON.stringify(path==="/api/prompts"?prompts:path==="/api/settings"?{settings:{version:1,mode:"mock",text:{},image:{},video:{},voice:{}},credentials:{}}:[])));
 vi.stubGlobal("fetch",fetcher);
 render(<StudioProvider><App /></StudioProvider>);
 fireEvent.click(await screen.findByRole("button",{name:"提示词管理"}));
 const editor=await screen.findByRole("textbox",{name:"提示词内容"});
 fireEvent.change(editor,{target:{value:"修改 {{style}}"}});
 fireEvent.click(screen.getByRole("button",{name:/我的作品/}));
 await screen.findByRole("button",{name:"继续编辑"});
 fireEvent.click(screen.getByRole("button",{name:"继续编辑"}));
 expect((editor as HTMLTextAreaElement).value).toBe("修改 {{style}}");
 expect(fetcher.mock.calls.every(([url])=>!url.includes("/quotes"))).toBe(true);
});
