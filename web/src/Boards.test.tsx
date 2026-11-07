// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Boards, imageTaskBusy, videoTaskBusy } from "./App";
import { StudioProvider } from "./ui";
import type { BoardPreview, Project, Shot, Task } from "./api";

beforeEach(() => {
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: false, addListener: vi.fn(), removeListener: vi.fn(), addEventListener: vi.fn(), removeEventListener: vi.fn() })));
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  const getStyle = window.getComputedStyle.bind(window);
  vi.spyOn(window, "getComputedStyle").mockImplementation(el => getStyle(el));
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const p: Project = { id:"project",revision:3,title:"故事",idea:"故事",script:"剧本",style:"电影",created:"",updated:"",mode:"remote",archived:false,analyzed:true,assets_locked:true,boards_locked:false,assets:[{id:"asset",kind:"scene",name:"场景",description:"",views:[]}],shots:[] };
const task: Task = {id:"task",project_id:p.id,quote_id:"quote",stage:"boards",status:"running",progress:5,message:"文字规划完成",created:"",updated:"",mode:"remote"};
const shot: Shot = {id:"shot",title:"阳光下的占有",description:"镜头画面",narration:"旁白",duration:8,references:["asset"],candidates:[{file:"",hash:"",approved:false},{file:"b.png",hash:"b",approved:false}],selected:0,approved:false};
const preview = (): BoardPreview => ({task_id:task.id,project_id:p.id,revision:p.revision,status:"running",shots:[shot],completed:1,total:2});
const actions = () => ({quote:vi.fn(),choose:vi.fn(),lock:vi.fn(),retry:vi.fn(),edit:vi.fn(),video:vi.fn()});

it("quotes a reviewed draft shot without requiring the other reviews", () => {
  const first:Shot={...shot,id:"first",first_frame:"静态首帧",source_text:"剧本",review_status:"reviewed",candidates:[]};
  const second:Shot={...first,id:"second",review_status:"pending"};
  const trialImage=vi.fn();
  render(<StudioProvider><Boards p={{...p,duration:16,planned_shots:[first,second]}} busy={false} savePlan={vi.fn()} trialImage={trialImage} {...actions()} /></StudioProvider>);
  const buttons=screen.getAllByRole("button",{name:"试做本镜图片 · 查看报价"});
  expect((buttons[0] as HTMLButtonElement).disabled).toBe(false);
  expect((buttons[1] as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(buttons[0]);
  expect(trialImage).toHaveBeenCalledExactlyOnceWith([first,second],first);
  expect((screen.getByRole("button",{name:"确认采用规划"}) as HTMLButtonElement).disabled).toBe(true);
});

it("confirms a trial frame then quotes its video through the draft scope", () => {
  const first:Shot={...shot,first_frame:"静态首帧",source_text:"剧本",review_status:"reviewed",selected:0,candidates:[{file:"trial.png",hash:"t",approved:false}]};
  const calls=actions();const trialChoose=vi.fn(),trialVideo=vi.fn(),trialRetry=vi.fn();
  const draft={...p,duration:16,planned_shots:[first,{...first,id:"other",review_status:"pending" as const,candidates:[]}]};
  const view=render(<StudioProvider><Boards p={draft} busy={false} savePlan={vi.fn()} trialChoose={trialChoose} trialVideo={trialVideo} trialRetry={trialRetry} {...calls}/></StudioProvider>);
  fireEvent.click(screen.getByRole("button",{name:"查看本镜试做"}));
  expect(within(screen.getByRole("dialog")).queryByRole("button",{name:"生成本镜视频 · 查看报价"})).toBeNull();
  fireEvent.click(screen.getByRole("button",{name:"确认画面与旁白"}));
  expect(trialChoose).toHaveBeenCalledExactlyOnceWith(first,0,true,first.narration);
  const confirmed={...first,approved:true};
  view.rerender(<StudioProvider><Boards p={{...draft,revision:4,planned_shots:[confirmed,draft.planned_shots[1]]}} busy={false} savePlan={vi.fn()} trialChoose={trialChoose} trialVideo={trialVideo} trialRetry={trialRetry} {...calls}/></StudioProvider>);
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button",{name:"生成本镜视频 · 查看报价"}));
  expect(trialVideo).toHaveBeenCalledExactlyOnceWith(confirmed,"","first_frame","");
  expect(calls.video).not.toHaveBeenCalled();
  expect(calls.choose).not.toHaveBeenCalled();
});

it("confirms the frame and requests a video quote directly in the table", () => {
  const first:Shot={...shot,id:"table-shot",first_frame:"静态首帧",source_text:"剧本",review_status:"reviewed",selected:0,candidates:[{file:"trial.png",hash:"t",approved:false}]};
  const draft={...p,duration:16,planned_shots:[first,{...first,id:"other",review_status:"pending" as const,candidates:[]}]};
  const trialChoose=vi.fn(),trialVideo=vi.fn();
  const props={busy:false,savePlan:vi.fn(),trialChoose,trialVideo,trialRetry:vi.fn(),...actions()};
  const view=render(<StudioProvider><Boards p={draft} {...props}/></StudioProvider>);
  const quote=()=>screen.getByRole("button",{name:"生成本镜视频 · 查看报价"}) as HTMLButtonElement;
  expect(quote().disabled).toBe(true);
  fireEvent.click(screen.getByRole("button",{name:"确认画面"}));
  expect(trialChoose).toHaveBeenCalledExactlyOnceWith(first,0,true,first.narration);
  expect(trialVideo).not.toHaveBeenCalled();
  expect(screen.queryByRole("dialog")).toBeNull();
  const confirmed={...first,approved:true};
  view.rerender(<StudioProvider><Boards p={{...draft,revision:4,planned_shots:[confirmed,draft.planned_shots[1]]}} {...props}/></StudioProvider>);
  expect(quote().disabled).toBe(false);
  fireEvent.click(quote());
  expect(trialVideo).toHaveBeenCalledExactlyOnceWith(confirmed,"","first_frame","");
  expect(screen.queryByRole("dialog")).toBeNull();
  view.rerender(<StudioProvider><Boards p={{...draft,revision:4,planned_shots:[confirmed,draft.planned_shots[1]]}} {...props} busy/></StudioProvider>);
  expect(quote().disabled).toBe(true);
});

it.each([{mode:"first_frame",label:"重新生成一版",message:/不上传本镜旧视频/},{mode:"edit_video",label:"基于原视频修改",message:/当前这条旧视频作为参考/}])("requires a method and advice before quoting $mode for only the current table shot", async ({mode,label,message}) => {
  const first:Shot={...shot,id:"retry-video",first_frame:"静态首帧",source_text:"剧本",review_status:"reviewed",selected:0,approved:true,video:"old-video.mp4",video_mode:"first_frame",candidates:[{file:"frame.png",hash:"f",approved:false}]};
  const trialVideo=vi.fn().mockResolvedValue(false);
  const other={...first,id:"other",title:"另一镜",video:"other.mp4"};
  render(<StudioProvider><Boards p={{...p,duration:16,planned_shots:[first,other]}} busy={false} savePlan={vi.fn()} trialVideo={trialVideo} trialChoose={vi.fn()} trialRetry={vi.fn()} {...actions()}/></StudioProvider>);
  fireEvent.click(screen.getAllByRole("button",{name:"重新生成本镜视频"})[0]);
  const dialog=within(screen.getByRole("dialog"));
  const quote=dialog.getByRole("button",{name:"查看本镜视频报价"}) as HTMLButtonElement;
  expect(quote.disabled).toBe(true);
  expect(trialVideo).not.toHaveBeenCalled();
  fireEvent.change(dialog.getByRole("textbox",{name:"视频修改建议"}),{target:{value:"  动作慢一点  "}});
  expect(quote.disabled).toBe(true);
  fireEvent.click(dialog.getByRole("radio",{name:label}));
  expect(dialog.getByText(message)).toBeTruthy();
  fireEvent.click(quote);
  await waitFor(()=>expect(trialVideo).toHaveBeenCalledExactlyOnceWith(first,"动作慢一点",mode,""));
  expect(screen.getByRole("dialog")).toBeTruthy(); // A failed quote must keep the advice for correction.
  expect(document.querySelector('video[src="/media/old-video.mp4"]')).toBeTruthy();
  expect(document.querySelector('video[src="/media/other.mp4"]')).toBeTruthy();
});

it("shows the plan and out-of-order images while keeping previews read-only, then enables completed review", async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(preview())));
  vi.stubGlobal("fetch",fetcher);
  const calls = actions();
  const rendered = render(<StudioProvider><Boards p={p} task={task} busy={false} {...calls} /></StudioProvider>);
  expect(await screen.findByRole("heading",{name:shot.title})).toBeTruthy();
  expect(screen.getByRole("status").textContent).toContain("图片1/2张");
  expect(screen.getByText("分镜图待生成")).toBeTruthy();
  expect(screen.queryByRole("img", {name:`${shot.title}分镜图`})).toBeNull(); // The historical alternate is not a second scheme.
  expect((screen.getByRole("button",{name:"确认画面与旁白"}) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("textbox",{name:"旁白"}) as HTMLTextAreaElement).disabled).toBe(true);
  expect(calls.choose).not.toHaveBeenCalled();
  expect(fetcher.mock.calls.every(([url,opts]) => url==="/api/tasks/task/preview" && opts.method==="GET")).toBe(true);
  const completed = {...p,revision:4,shots:[{...shot,candidates:[{file:"a.png",hash:"a",approved:false},shot.candidates[1]]}]};
  rendered.rerender(<StudioProvider><Boards p={completed} busy={false} {...calls} /></StudioProvider>);
  await waitFor(() => expect(screen.queryByText("分镜图待生成")).toBeNull());
  fireEvent.click(screen.getByRole("button",{name:"确认画面与旁白"}));
  expect(calls.choose).toHaveBeenCalledExactlyOnceWith(completed.shots[0],0,true,"旁白");
});

it("ignores a late preview after the user changes projects", async () => {
  let resolve!: (r:Response)=>void;
  vi.stubGlobal("fetch",vi.fn(() => new Promise<Response>(r=>{resolve=r;})));
  const calls = actions();
  const rendered=render(<StudioProvider><Boards p={p} task={task} busy {...calls} /></StudioProvider>);
  rendered.rerender(<StudioProvider><Boards p={{...p,id:"other"}} busy={false} {...calls} /></StudioProvider>);
  await act(async()=>resolve(new Response(JSON.stringify(preview()))));
  expect(screen.queryByRole("heading",{name:shot.title})).toBeNull();
  expect(calls.quote).not.toHaveBeenCalled();
});

it("polls and displays each newly completed candidate without allowing premature review", async () => {
  let calls = 0;
  vi.stubGlobal("fetch",vi.fn(async () => {
    const value = preview();
    if (++calls > 1) {
      value.shots = [{...shot,candidates:[{file:"new-a.png",hash:"a",approved:false},shot.candidates[1]]}];
      value.completed = 2;
    }
    return new Response(JSON.stringify(value));
  }));
  render(<StudioProvider><Boards p={p} task={task} busy {...actions()} /></StudioProvider>);
  await screen.findByText("分镜图待生成");
  await waitFor(() => expect(screen.getByRole("img",{name:`${shot.title}分镜图`}).getAttribute("src")).toContain("new-a.png"),{timeout:5000});
  expect(screen.getByRole("status").textContent).toContain("图片2/2张");
  expect((screen.getByRole("button",{name:"确认画面与旁白"}) as HTMLButtonElement).disabled).toBe(true);
});

it("shows only the selected image and asks for advice before quoting a single-image retry", () => {
  const current = {...shot, selected:1, candidates:[{file:"a.png",hash:"a",approved:false}, {file:"b.png",hash:"b",approved:false}]};
  const calls = actions();
  render(<StudioProvider><Boards p={{...p,shots:[current]}} busy={false} {...calls} /></StudioProvider>);
  expect(screen.getAllByRole("img")).toHaveLength(1);
  expect(screen.getByRole("img").getAttribute("src")).toContain("b.png");
  fireEvent.click(screen.getByRole("button",{name:"重新生成一版"}));
  expect(calls.retry).not.toHaveBeenCalled();
  const quote = screen.getByRole("button",{name:"查看重生成报价"});
  expect((quote as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(screen.getByRole("textbox",{name:"修改建议"}),{target:{value:" 换成近景，背景暗一点 "}});
  fireEvent.click(quote);
  expect(calls.retry).toHaveBeenCalledExactlyOnceWith(current,"换成近景，背景暗一点");
  expect(calls.choose).not.toHaveBeenCalled();
});

it("generates an approved shot independently without requiring the other shots or a global lock", () => {
  const current = {...shot, approved:true, candidates:[{file:"a.png",hash:"a",approved:false}]};
  const other = {...current,id:"second",title:"第二镜",approved:false};
  const calls = actions();
  render(<StudioProvider><Boards p={{...p,shots:[current,other]}} busy={false} {...calls} /></StudioProvider>);
  const buttons=screen.getAllByRole("button",{name:"生成本镜视频 · 查看报价"});
  expect((buttons[0] as HTMLButtonElement).disabled).toBe(false);
  expect(buttons).toHaveLength(1);
  fireEvent.click(buttons[0]);
  expect(calls.video).toHaveBeenCalledExactlyOnceWith(current,"","first_frame","");
  fireEvent.change(screen.getAllByRole("textbox",{name:"旁白"})[0],{target:{value:"未保存台词"}});
  expect(screen.queryByRole("button",{name:"生成本镜视频 · 查看报价"})).toBeNull();
  expect(screen.getAllByRole("button",{name:"确认画面与旁白"})).toHaveLength(2);
});

it("previews and retries a locked shot video only after advice, retaining the prior clip", () => {
  const current = {...shot, approved:true, video:"original.mp4", candidates:[{file:"a.png",hash:"a",approved:false}]};
  const calls = actions();
  render(<StudioProvider><Boards p={{...p,boards_locked:true,shots:[current]}} busy={false} {...calls} /></StudioProvider>);
  expect(screen.getByLabelText(`${current.title}视频预览`).getAttribute("src")).toContain("original.mp4");
  const flow=within(screen.getByLabelText("第1镜卡片制作流程"));
  expect(flow.getByText("已确认")).toBeTruthy();
  expect(flow.getByText("已生成，可在下方播放。")).toBeTruthy();
  expect(flow.queryByRole("button",{name:"确认画面与旁白"})).toBeNull();
  expect(flow.queryByRole("button",{name:"生成本镜视频 · 查看报价"})).toBeNull();
  fireEvent.click(screen.getByRole("button",{name:"重新生成本镜视频"}));
  fireEvent.click(screen.getByRole("radio",{name:"重新生成一版"}));
  const button=screen.getByRole("button",{name:"查看本镜视频报价"});
  expect((button as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(screen.getByRole("textbox",{name:"视频修改建议"}),{target:{value:"镜头慢一点"}});
  fireEvent.click(button);
  expect(calls.video).toHaveBeenCalledExactlyOnceWith(current,"镜头慢一点","first_frame","");
  expect(screen.getByLabelText(`${current.title}视频预览`).getAttribute("src")).toContain("original.mp4");
});

it("selects first-last control with an approved end frame and passes the exact choice to the quote", async () => {
  const current={...shot,approved:true,candidates:[{file:"start.png",hash:"start",approved:false}]};
  const end={...current,id:"end",title:"结束镜头",candidates:[{file:"end.png",hash:"end",approved:false}]};
  const calls=actions();
  render(<StudioProvider><Boards p={{...p,shots:[current,end]}} busy={false} {...calls} /></StudioProvider>);
  const mode=screen.getAllByRole("combobox",{name:"视频生成方式"})[0];
  expect(mode.closest(".ant-select")?.textContent).toContain("严格首帧");
  fireEvent.mouseDown(mode);fireEvent.click(await screen.findByRole("option",{name:"首尾帧控制"}));
  expect((screen.getAllByRole("button",{name:"生成本镜视频 · 查看报价"})[0] as HTMLButtonElement).disabled).toBe(true);
  fireEvent.mouseDown(screen.getByRole("combobox",{name:"结束画面"}));fireEvent.click(await screen.findByRole("option",{name:end.title}));
  expect(screen.getByRole("img",{name:"结束画面预览"}).getAttribute("src")).toContain("end.png");
  fireEvent.click(screen.getAllByRole("button",{name:"生成本镜视频 · 查看报价"})[0]);
  expect(calls.video).toHaveBeenCalledExactlyOnceWith(current,"","first_last","end");
});

it("blocks continuation until the previous clip exists and marks stale continuation videos", async () => {
  const first={...shot,approved:true,candidates:[{file:"start.png",hash:"start",approved:false}]};
  const second={...first,id:"end",title:"第二镜",video_mode:"continue" as const};
  const calls=actions();
  const view=render(<StudioProvider><Boards p={{...p,shots:[first,second]}} busy={false} {...calls} /></StudioProvider>);
  expect((screen.getAllByRole("button",{name:"生成本镜视频 · 查看报价"})[1] as HTMLButtonElement).disabled).toBe(true);
  view.rerender(<StudioProvider><Boards p={{...p,revision:4,shots:[{...first,video:"new.mp4"},{...second,video:"second.mp4",video_source_file:"old.mp4"}]}} busy={false} {...calls} /></StudioProvider>);
  expect(screen.getByText(/此视频的续接来源已过期/)).toBeTruthy();
});


it("allows reviewing and quoting a published image while other board images are still running", () => {
  const current = {...shot,candidates:[{file:"ready.png",hash:"ready",approved:false}]};
  const missing = {...shot,id:"missing",title:"尚未出图",candidates:[]};
  const calls=actions();
  const view=render(<StudioProvider><Boards p={{...p,shots:[current,missing]}} task={task} busy reviewBusy={false} {...calls} /></StudioProvider>);
  const approvals=screen.getAllByRole("button",{name:"确认画面与旁白"});
  expect((approvals[0] as HTMLButtonElement).disabled).toBe(false);
  expect((approvals[1] as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("button",{name:"锁定分镜"}) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getAllByRole("button",{name:"重新生成一版"}).every(b=>(b as HTMLButtonElement).disabled)).toBe(true);
  fireEvent.change(screen.getAllByRole("textbox",{name:"旁白"})[0],{target:{value:"新台词"}});
  // A different image arriving must preserve the current draft.
  view.rerender(<StudioProvider><Boards p={{...p,revision:4,shots:[current,{...missing,candidates:current.candidates}]}} task={task} busy reviewBusy={false} {...calls} /></StudioProvider>);
  expect((screen.getAllByRole("textbox",{name:"旁白"})[0] as HTMLTextAreaElement).value).toBe("新台词");
  fireEvent.click(screen.getAllByRole("button",{name:"确认画面与旁白"})[0]);
  expect(calls.choose).toHaveBeenCalledWith(current,0,true,"新台词");
  const approved={...current,approved:true,narration:"新台词"};
  view.rerender(<StudioProvider><Boards p={{...p,revision:5,shots:[approved,missing]}} task={task} busy reviewBusy={false} {...calls} /></StudioProvider>);
  fireEvent.click(screen.getAllByRole("button",{name:"生成本镜视频 · 查看报价"})[0]);
  expect(calls.video).toHaveBeenCalledExactlyOnceWith(approved,"","first_frame","");
  view.rerender(<StudioProvider><Boards p={{...p,shots:[approved,missing]}} task={{...task,status:"failed"}} busy reviewBusy {...calls} /></StudioProvider>);
  expect((screen.getAllByRole("button",{name:"生成本镜视频 · 查看报价"})[0] as HTMLButtonElement).disabled).toBe(true);
});


it("quotes exactly the missing shot during a stopped board task without requiring an old image or advice", () => {
  const current={...shot,candidates:[]};
  const calls=actions();
  render(<StudioProvider><Boards p={{...p,shots:[current]}} task={{...task,status:"failed"}} busy reviewBusy replacementBusy={false} {...calls} /></StudioProvider>);
  const button=screen.getByRole("button",{name:"补生成 · 查看报价"});
  expect((button as HTMLButtonElement).disabled).toBe(false);
  fireEvent.click(button);
  expect(calls.retry).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button",{name:"查看补生成报价"}));
  expect(calls.retry).toHaveBeenCalledExactlyOnceWith(current,"");
  expect(calls.choose).not.toHaveBeenCalled();
  expect(calls.video).not.toHaveBeenCalled();
});


it("keeps the reference selector open after overflow and only sends an explicit user selection", async () => {
  const assets=Array.from({length:6},(_,i)=>({id:`a${i}`,kind:"prop" as const,name:`道具${i+1}`,description:"",views:[]}));
  const current={...shot,references:assets.map(a=>a.id),candidates:[]};
  const calls=actions();calls.retry.mockResolvedValue(false);
  render(<StudioProvider><Boards p={{...p,assets,shots:[current]}} task={{...task,status:"failed"}} busy replacementBusy={false} {...calls} /></StudioProvider>);
  fireEvent.click(screen.getByRole("button",{name:"补生成 · 查看报价"}));
  fireEvent.click(screen.getByRole("button",{name:"查看补生成报价"}));
  await waitFor(()=>expect(calls.retry).toHaveBeenCalledExactlyOnceWith(current,""));
  expect(screen.getByRole("button",{name:"查看补生成报价"})).toBeTruthy();
  fireEvent.click(screen.getByRole("checkbox",{name:"道具 · 道具6"}));
  fireEvent.click(screen.getByRole("button",{name:"查看补生成报价"}));
  await waitFor(()=>expect(calls.retry).toHaveBeenLastCalledWith(current,"",assets.slice(0,5).map(a=>a.id)));
  expect(calls.video).not.toHaveBeenCalled();
});

it("reviews the draft separately, preserves current media, and never quotes images on approval", () => {
  const calls=actions(); const savePlan=vi.fn();const imageQuote=vi.fn();
  const draft={...shot,id:"draft",title:"窗外立志",first_frame:"橘猫独自扒着窗沿",source_text:"原剧本第一段",duration:5,candidates:[],narration:""};
  render(<StudioProvider><Boards p={{...p,shots:[{...shot,selected:1,video:"old.mp4"}],planned_shots:[draft],script:"原剧本第一段",duration:5}} busy={false} savePlan={savePlan} imageQuote={imageQuote} {...calls}/></StudioProvider>);
  expect(screen.getByRole("heading",{name:"审核分镜规划"})).toBeTruthy();
  expect(screen.getByRole("img",{name:`${shot.title}分镜图`}).getAttribute("src")).toContain("b.png");
  expect(calls.quote).not.toHaveBeenCalled();expect(imageQuote).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button",{name:"审核第1镜"}));
  fireEvent.change(screen.getByRole("textbox",{name:"静态首帧"}),{target:{value:"扒窗，尚未转身"}});
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button",{name:"保存规划草稿"}));
  expect(savePlan).toHaveBeenLastCalledWith([expect.objectContaining({first_frame:"扒窗，尚未转身"})],false);
  fireEvent.click(screen.getByRole("button",{name:"本镜审核通过"}));
  fireEvent.click(screen.getByRole("button",{name:"返回总览"}));
  fireEvent.click(screen.getByRole("button",{name:"确认采用规划"}));
  expect(savePlan).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole("button",{name:"确认采用，不生图"}));
  expect(savePlan).toHaveBeenLastCalledWith([expect.objectContaining({first_frame:"扒窗，尚未转身"})],true);
  expect(imageQuote).not.toHaveBeenCalled();expect(calls.quote).not.toHaveBeenCalled();
});

it("only offers a separate image quote for an approved plan and displays historical media", () => {
 const calls=actions();const imageQuote=vi.fn();
 render(<StudioProvider><Boards p={{...p,plan_version:2,plan_approved:true,shots:[{...shot,candidates:[]}],previous_shots:[{...shot,selected:1,video:"old.mp4"}]}} busy={false} imageQuote={imageQuote} {...calls}/></StudioProvider>);
 expect(imageQuote).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("button",{name:"全部生成分镜图 · 查看报价"}));
 expect(imageQuote).toHaveBeenCalledOnce();
 expect(screen.getByText(/历史素材 · 1镜/)).toBeTruthy();
 expect(document.querySelector('video[src="/media/old.mp4"]')).toBeTruthy();
});

it("keeps unsaved edits across shots and clears review when timing affects subsequent shots", () => {
 const calls=actions();const savePlan=vi.fn();
 let detail:ReturnType<typeof within>;
 const first={...shot,id:"one",title:"第一镜",source_text:"原文A",first_frame:"开场A",narration:"",duration:5,candidates:[]};
 const second={...first,id:"two",title:"第二镜",source_text:"原文B",first_frame:"开场B"};
 render(<StudioProvider><Boards p={{...p,script:"原文A\n原文B",planned_shots:[first,second],duration:10}} busy={false} savePlan={savePlan} {...calls}/></StudioProvider>);
 expect(screen.queryByRole("textbox",{name:"静态首帧"})).toBeNull();
 fireEvent.click(screen.getByRole("button",{name:"审核第1镜"}));
 detail=within(screen.getByRole("dialog"));
 fireEvent.change(detail.getByRole("textbox",{name:"静态首帧"}),{target:{value:"修改后的开场A"}});
 fireEvent.click(detail.getByRole("button",{name:"下一镜"}));
 expect((detail.getByRole("textbox",{name:"静态首帧"}) as HTMLTextAreaElement).value).toBe("开场B");
 fireEvent.click(detail.getByRole("button",{name:"上一镜"}));
 expect((detail.getByRole("textbox",{name:"静态首帧"}) as HTMLTextAreaElement).value).toBe("修改后的开场A");
 fireEvent.click(detail.getByRole("button",{name:"本镜通过，下一镜"}));
 fireEvent.click(detail.getByRole("button",{name:"本镜审核通过"}));
 fireEvent.click(detail.getByRole("button",{name:"返回总览"}));
 expect((screen.getByRole("button",{name:"确认采用规划"}) as HTMLButtonElement).disabled).toBe(false);
 fireEvent.click(screen.getByRole("button",{name:"保存规划草稿"}));
 expect(savePlan).toHaveBeenLastCalledWith([expect.objectContaining({review_status:"reviewed",first_frame:"修改后的开场A"}),expect.objectContaining({review_status:"reviewed"})],false);
 fireEvent.click(screen.getByRole("button",{name:"审核第1镜"}));
 detail=within(screen.getByRole("dialog"));
 fireEvent.change(detail.getByRole("spinbutton",{name:"时长（秒）"}),{target:{value:"6"}});
 fireEvent.click(detail.getByRole("button",{name:"保存规划草稿"}));
 expect(savePlan).toHaveBeenLastCalledWith([expect.objectContaining({review_status:"pending",duration:6}),expect.objectContaining({review_status:"pending"})],false);
 fireEvent.click(detail.getByRole("button",{name:"返回总览"}));
 expect((screen.getByRole("button",{name:"确认采用规划"}) as HTMLButtonElement).disabled).toBe(true);
 expect(screen.getByText("总时长11秒，与目标10秒不一致")).toBeTruthy();
 expect(calls.quote).not.toHaveBeenCalled();
});


it("allows another draft image while one shot runs and blocks duplicate or third submissions", () => {
 const first:Shot={...shot,id:"first",first_frame:"静态首帧",source_text:"剧本",review_status:"reviewed",candidates:[]};
 const second:Shot={...first,id:"second"};
 const running:Task={...task,stage:"board_retry",plan_trial:true,shot_id:first.id,shot_input_hash:"original"};
 const tasks=[running];
 const trialImage=vi.fn();
 render(<StudioProvider><Boards p={{...p,duration:16,planned_shots:[first,second]}} busy imageBusy={(id,trial)=>imageTaskBusy(tasks,id,trial)} savePlan={vi.fn()} trialImage={trialImage} {...actions()}/></StudioProvider>);
 const buttons=screen.getAllByRole("button",{name:"试做本镜图片 · 查看报价"});
 expect((buttons[0] as HTMLButtonElement).disabled).toBe(true);
 expect((buttons[1] as HTMLButtonElement).disabled).toBe(false);
 fireEvent.click(buttons[1]);
 expect(trialImage).toHaveBeenCalledExactlyOnceWith([first,second],second);
 expect(imageTaskBusy([...tasks,{...running,id:"second-task",shot_id:second.id}],"third",true)).toBe(true);
 expect(imageTaskBusy([{...running,status:"failed"}],second.id,true)).toBe(true);
 expect(imageTaskBusy([{...running,stage:"boards"}],second.id,true)).toBe(true);
 expect(imageTaskBusy([{...running,shot_input_hash:undefined}],second.id,true)).toBe(true);
 expect((screen.getByRole("button",{name:"确认采用规划"}) as HTMLButtonElement).disabled).toBe(true);
});

it("gates the batch image entry on complete review, missing images and task state", () => {
 const first:Shot={...shot,id:"first",first_frame:"首帧",source_text:"剧本",review_status:"reviewed",candidates:[]};
 const second:Shot={...first,id:"second",review_status:"pending"};
 const allImages=vi.fn();const props={busy:false,savePlan:vi.fn(),allImages,...actions()};
 const draft={...p,duration:16,planned_shots:[first,second]};
 const view=render(<StudioProvider><Boards p={draft} {...props}/></StudioProvider>);
 const button=()=>screen.getByRole("button",{name:"全部生成分镜图 · 查看报价"}) as HTMLButtonElement;
 expect(button().disabled).toBe(true);
 view.rerender(<StudioProvider><Boards p={{...draft,revision:4,planned_shots:[first,{...second,review_status:"reviewed"}]}} {...props} busy/></StudioProvider>);
 expect(button().disabled).toBe(true);
 view.rerender(<StudioProvider><Boards p={{...draft,revision:5,planned_shots:[first,{...second,review_status:"reviewed"}]}} {...props}/></StudioProvider>);
 expect(button().disabled).toBe(false);
 fireEvent.click(button());
 expect(allImages).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("button",{name:"确认采用并查看报价"}));
 expect(allImages).toHaveBeenCalledExactlyOnceWith([first,{...second,review_status:"reviewed"}]);
 view.rerender(<StudioProvider><Boards p={{...draft,revision:4,planned_shots:[{...first,candidates:[{file:"done.png",hash:"d",approved:false}]}]}} {...props}/></StudioProvider>);
 expect(screen.queryByRole("button",{name:"全部生成分镜图 · 查看报价"})).toBeNull();
});

it("keeps the adopted plan in a table and returns from shot details during a failed batch", async () => {
 const first:Shot={...shot,id:"first",title:"已生成镜头",first_frame:"首帧",source_text:"剧本",review_status:"reviewed",selected:0,approved:true,video:"kept.mp4",candidates:[{file:"kept.png",hash:"k",approved:true}]};
 const missing:Shot={...first,id:"missing",title:"缺图镜头",candidates:[],approved:false,video:""};
 const current={...p,plan_version:2,plan_approved:true,duration:16,shots:[first,missing]};
 const failed={...task,status:"failed",message:"任务缺少四视角参考报价快照，禁止提交"};
 vi.stubGlobal("fetch",vi.fn().mockResolvedValue(new Response(JSON.stringify({...preview(),shots:[first,missing]}))));
 render(<StudioProvider><Boards p={current} task={failed} busy {...actions()}/></StudioProvider>);
 expect(screen.getByRole("heading",{name:"全片分镜总览"})).toBeTruthy();
 const completed=within(screen.getByLabelText("第1镜制作流程"));
 expect(completed.getByText("已确认")).toBeTruthy();
 expect(completed.getByText("已生成，可在左侧播放。")).toBeTruthy();
 expect(completed.queryByRole("button",{name:"确认画面与旁白"})).toBeNull();
 expect(completed.queryByRole("button",{name:"生成本镜视频 · 查看报价"})).toBeNull();
 expect(completed.getByRole("button",{name:"重新生成本镜视频"})).toBeTruthy();
 expect(within(screen.getByLabelText("第2镜制作流程")).getByText("尚未生图，先打开详情生成图片。")).toBeTruthy();
 expect((screen.getByRole("radio",{name:"表格总览"}) as HTMLInputElement).checked).toBe(true);
 expect((screen.getByRole("radio",{name:"卡片视图"}) as HTMLInputElement).checked).toBe(false);
 fireEvent.click(screen.getByRole("radio",{name:"卡片视图"}));
 expect((screen.getByRole("radio",{name:"表格总览"}) as HTMLInputElement).checked).toBe(false);
 expect((screen.getByRole("radio",{name:"卡片视图"}) as HTMLInputElement).checked).toBe(true);
 fireEvent.click(screen.getByRole("radio",{name:"表格总览"}));
 expect(screen.getAllByRole("table").length).toBeGreaterThan(0);
 fireEvent.click(screen.getByRole("button",{name:"缺图镜头"}));
 expect(screen.getByRole("heading",{name:"缺图镜头"})).toBeTruthy();
 expect((screen.getByRole("radio",{name:"卡片视图"}) as HTMLInputElement).checked).toBe(true);
 fireEvent.click(screen.getByRole("radio",{name:"表格总览"}));
 expect((screen.getByRole("radio",{name:"表格总览"}) as HTMLInputElement).checked).toBe(true);
 expect((screen.getByRole("radio",{name:"卡片视图"}) as HTMLInputElement).checked).toBe(false);
 expect(screen.getAllByRole("table").length).toBeGreaterThan(0);
 expect(screen.getByRole("img",{name:"已生成镜头分镜图"}).getAttribute("src")).toContain("kept.png");
 expect(document.querySelector('video[src="/media/kept.mp4"]')).toBeTruthy();
 expect((screen.getByRole("button",{name:"全部生成分镜图 · 查看报价"}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.mouseDown(screen.getByRole("combobox",{name:"筛选审核状态"}));
 fireEvent.click(await screen.findByText("只看缺图"));
 expect(screen.queryByRole("button",{name:"已生成镜头"})).toBeNull();
 expect(screen.getByRole("button",{name:"缺图镜头"})).toBeTruthy();
});

it("uses current-shot actions in the adopted table while sibling images run and refreshes published results", () => {
 const first:Shot={...shot,id:"first",title:"正式镜头",first_frame:"首帧",source_text:"剧本",review_status:"reviewed",selected:0,candidates:[{file:"published.png",hash:"p",approved:false}]};
 const missing={...first,id:"missing",title:"另一镜",candidates:[]};
 const current={...p,plan_version:2,plan_approved:true,duration:16,shots:[first,missing]};
 const calls=actions();const props={...calls,busy:true,reviewBusy:false};
 const view=render(<StudioProvider><Boards p={current} {...props}/></StudioProvider>);
 const flow=within(screen.getByLabelText("第1镜制作流程"));
 const confirm=flow.getByRole("button",{name:"确认画面与旁白"}) as HTMLButtonElement;
 expect(flow.queryByRole("button",{name:"生成本镜视频 · 查看报价"})).toBeNull();
 expect(flow.getByText("完成第 1 步后，查看报价生成视频。")).toBeTruthy();
 expect(confirm.disabled).toBe(false);
 fireEvent.click(confirm);
 expect(calls.choose).toHaveBeenCalledExactlyOnceWith(first,0,true,first.narration);
 const approved={...first,approved:true};
 const updated={...current,revision:4,shots:[approved,{...missing,candidates:[{file:"next.png",hash:"n",approved:false}]}]};
 view.rerender(<StudioProvider><Boards p={updated} {...props}/></StudioProvider>);
 expect(screen.getByRole("img",{name:"另一镜分镜图"}).getAttribute("src")).toContain("next.png");
 const approvedFlow=within(screen.getByLabelText("第1镜制作流程"));
 expect(approvedFlow.getByText("已确认")).toBeTruthy();
 expect(approvedFlow.queryByRole("button",{name:"确认画面与旁白"})).toBeNull();
 fireEvent.click(screen.getAllByRole("button",{name:"生成本镜视频 · 查看报价"})[0]);
 expect(calls.video).toHaveBeenCalledExactlyOnceWith(approved,"","first_frame","");
});

it("keeps blocked tasks visible in the compact toolbar and opens help without submitting work", async () => {
 const openTasks=vi.fn(),calls=actions(),imageQuote=vi.fn();
 const failed={...task,status:"failed",message:"等待处理"};
 vi.stubGlobal("fetch",vi.fn().mockResolvedValue(new Response(JSON.stringify(preview()))));
 render(<StudioProvider><Boards p={{...p,plan_version:2,plan_approved:true,shots:[{...shot,candidates:[]}]}} task={failed} busy openTasks={openTasks} imageQuote={imageQuote} {...calls}/></StudioProvider>);
 expect(screen.getByRole("status").textContent).toContain("任务待处理");
 expect(screen.getByRole("status").textContent).toContain("已出图 0/1");
 expect(document.querySelector(".board-planning-actions,.board-preview-notice")).toBeNull();
 expect((screen.getByRole("button",{name:"全部生成分镜图 · 查看报价"}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.click(screen.getByRole("button",{name:"处理当前任务"}));
 expect(openTasks).toHaveBeenCalledOnce();
 expect(screen.queryByText(/恢复只收集已有结果/)).toBeNull();
 fireEvent.click(screen.getByRole("button",{name:"生成说明"}));
 expect(await screen.findByText(/恢复只收集已有结果/)).toBeTruthy();
 fireEvent.click(screen.getByRole("button",{name:/更多/}));
 expect((await screen.findByRole("menuitem",{name:"按总体剧本重新规划 · 查看报价"})).getAttribute("aria-disabled")).toBe("true");
 expect(calls.quote).not.toHaveBeenCalled();expect(imageQuote).not.toHaveBeenCalled();
});

it.each(["table","cards"])("uses existing %s step buttons to handle a blocked task without confirming or regenerating", viewMode=>{
 const current={...shot,id:"managed-shot",approved:false,candidates:[{file:"frame.png",hash:"f",approved:false}]};
 const project={...p,plan_approved:viewMode==="table",plan_version:2,duration:8,shots:[current]};
 const calls=actions(),handleTasks=vi.fn();
 const view=render(<StudioProvider><Boards p={project} busy handleTasks={handleTasks} {...calls}/></StudioProvider>);
 expect(screen.queryByRole("button",{name:"在此处理任务"})).toBeNull();
 fireEvent.click(screen.getByRole("button",{name:"确认画面与旁白"}));
 expect(handleTasks).toHaveBeenCalledExactlyOnceWith(current.id);
 expect(calls.choose).not.toHaveBeenCalled();
 const completed={...current,approved:true,video:"old.mp4"};
 view.rerender(<StudioProvider><Boards p={{...project,revision:4,shots:[completed]}} busy handleTasks={handleTasks} {...calls}/></StudioProvider>);
 fireEvent.click(screen.getByRole("button",{name:"重新生成视频"}));
 expect(handleTasks).toHaveBeenCalledTimes(2);
 expect(calls.video).not.toHaveBeenCalled();
 expect(screen.queryByRole("dialog")).toBeNull();
});

it.each(["table","cards"])("allows an independent video in %s while another task runs", viewMode=>{
 const current={...shot,id:"available-shot",approved:true,candidates:[{file:"frame.png",hash:"f",approved:true}]};
 const project={...p,plan_approved:viewMode==="table",plan_version:2,duration:8,shots:[current]};
 const calls=actions(),handleTasks=vi.fn();
 render(<StudioProvider><Boards p={project} busy videoBusy={()=>false} handleTasks={handleTasks} {...calls}/></StudioProvider>);
 fireEvent.click(screen.getByRole("button",{name:"生成本镜视频 · 查看报价"}));
 expect(calls.video).toHaveBeenCalled();
 expect(handleTasks).not.toHaveBeenCalled();
});

it("uses configured limits for independent image and video actions",()=>{
 const image:Task={...task,stage:"board_retry",shot_id:"first",shot_input_hash:"hash"};
 expect(imageTaskBusy([image],"second",false,1)).toBe(true);
 expect(imageTaskBusy([image,{...image,id:"other",shot_id:"second"}],"third",false,3)).toBe(false);
 const video:Task={...image,stage:"shot_video"};
 expect(videoTaskBusy([video],"second",false,1)).toBe(true);
 expect(videoTaskBusy([video],"second",false,2)).toBe(false);
 expect(videoTaskBusy([video],"first",false,3)).toBe(true);
 expect(videoTaskBusy([{...video,status:"failed"}],"second",false,3)).toBe(true);
 });

it("opens the same content editor from adopted table and card even before an image exists",()=>{
 const first:Shot={...shot,first_frame:"静态首帧",candidates:[],selected:0};
 const editContent=vi.fn();
 render(<StudioProvider><Boards p={{...p,plan_version:2,plan_approved:true,shots:[first]}} busy={false} editContent={editContent} {...actions()}/></StudioProvider>);
 const tableButton=screen.getByRole("button",{name:"编辑首帧、动作与台词"});
 expect((tableButton as HTMLButtonElement).disabled).toBe(false);fireEvent.click(tableButton);expect(editContent).toHaveBeenCalledWith(first,false);
 editContent.mockClear();fireEvent.click(screen.getByRole("radio",{name:"卡片视图"}));fireEvent.click(screen.getByText("查看画面、动作与引用素材"));
 const cardButton=screen.getByRole("button",{name:"编辑首帧、动作与台词"});
 expect((cardButton as HTMLButtonElement).disabled).toBe(false);fireEvent.click(cardButton);expect(editContent).toHaveBeenCalledWith(first,false);
});
