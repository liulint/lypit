// @vitest-environment jsdom
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {StudioProvider} from "./ui";
import {Boards} from "./App";
import {EditImageDialog} from "./ShotImageDialogs";
import type {Project,Shot} from "./api";

beforeEach(()=>{
  vi.stubGlobal("matchMedia",vi.fn(()=>({matches:false,addListener:vi.fn(),removeListener:vi.fn(),addEventListener:vi.fn(),removeEventListener:vi.fn()})));
  vi.stubGlobal("ResizeObserver",class{observe(){}unobserve(){}disconnect(){}});
  const getStyle=window.getComputedStyle.bind(window);vi.spyOn(window,"getComputedStyle").mockImplementation(el=>getStyle(el));
});
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
const shot:Shot={id:"shot",title:"拥抱",description:"猫拥抱",narration:"台词",duration:7,references:["scene"],candidates:[{file:"base.png",hash:"base",approved:false}],selected:0,approved:false};
const p:Project={id:"p",revision:1,title:"故事",idea:"故事",script:"剧本",style:"温暖",created:"",updated:"",mode:"remote",archived:false,analyzed:true,assets_locked:true,boards_locked:false,assets:[{id:"scene",kind:"scene",name:"墙角",description:"",views:[]}],shots:[shot]};

it("separates edit from regeneration and only requests an edit quote after required fields",async()=>{
  const edit=vi.fn().mockResolvedValue(false);const retry=vi.fn();
  render(<StudioProvider><Boards p={p} busy={false} edit={edit} retry={retry} quote={vi.fn()} choose={vi.fn()} lock={vi.fn()} video={vi.fn()} /></StudioProvider>);
  expect(screen.getByRole("button",{name:"重新生成一版"})).toBeTruthy();
  fireEvent.click(screen.getByRole("button",{name:"基于当前图修改"}));
  expect(screen.getByRole("img",{name:"独立编辑底图"}).getAttribute("src")).toContain("base.png");
  const quote=screen.getByRole("button",{name:"查看单张修改报价"});expect((quote as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(screen.getByRole("textbox",{name:"修改内容"}),{target:{value:" 只移动证明 "}});
  fireEvent.change(screen.getByRole("textbox",{name:"必须保留的内容"}),{target:{value:" 四只猫及其姿势不变 "}});
  expect((screen.getByRole("checkbox",{name:"场景 · 墙角"}) as HTMLInputElement).checked).toBe(false);
  expect((quote as HTMLButtonElement).disabled).toBe(false);expect(edit).not.toHaveBeenCalled();
  fireEvent.click(quote);
  await waitFor(()=>expect(edit).toHaveBeenCalledExactlyOnceWith(shot,"只移动证明","四只猫及其姿势不变",[]));
  expect(retry).not.toHaveBeenCalled();expect(screen.getByRole("button",{name:"查看单张修改报价"})).toBeTruthy();
});

it("does not silently change the edit base when a different version arrives",()=>{
  const quote=vi.fn();const close=vi.fn();
  const view=render(<StudioProvider><EditImageDialog p={p} q={shot} busy={false} close={close} quote={quote} /></StudioProvider>);
  fireEvent.change(screen.getByRole("textbox",{name:"修改内容"}),{target:{value:"移动证明"}});
  const next={...shot,selected:1,candidates:[...shot.candidates,{file:"new.png",hash:"new",approved:false}]};
  view.rerender(<StudioProvider><EditImageDialog p={p} q={next} busy={false} close={close} quote={quote} /></StudioProvider>);
  expect(screen.getByRole("img",{name:"独立编辑底图"}).getAttribute("src")).toContain("base.png");
  expect(screen.getByText(/底图版本已变化/)).toBeTruthy();
  expect((screen.getByRole("button",{name:"查看单张修改报价"}) as HTMLButtonElement).disabled).toBe(true);
  expect(quote).not.toHaveBeenCalled();
});

it("compares the actual parent image and switches back without a generation request",()=>{
  const current:Shot={...shot,selected:2,candidates:[{file:"",hash:"",approved:false},shot.candidates[0],{file:"edited.png",hash:"edited",approved:false,operation:"edit",source_file:"base.png",source_hash:"base"}]};
  const choose=vi.fn();const edit=vi.fn();const retry=vi.fn();
  render(<StudioProvider><Boards p={{...p,shots:[current]}} busy={false} edit={edit} retry={retry} choose={choose} lock={vi.fn()} quote={vi.fn()} video={vi.fn()} /></StudioProvider>);
  fireEvent.click(screen.getByRole("button",{name:"新旧对比与切回"}));
  expect(screen.getByRole("img",{name:"左侧对比图片"}).getAttribute("src")).toContain("base.png");
  expect(screen.getByRole("img",{name:"右侧对比图片"}).getAttribute("src")).toContain("edited.png");
  fireEvent.click(screen.getByRole("button",{name:"切回左侧版本"}));
  expect(choose).toHaveBeenCalledExactlyOnceWith(current,1,false,current.narration);
  expect(edit).not.toHaveBeenCalled();expect(retry).not.toHaveBeenCalled();
});

it("selects all auxiliary materials or none without submitting a quote",()=>{
  const quote=vi.fn();const current={...shot,references:["scene","prop"]};
  render(<StudioProvider><EditImageDialog p={{...p,assets:[...p.assets,{id:"prop",kind:"prop",name:"证明",description:"",views:[]}]}} q={current} busy={false} close={vi.fn()} quote={quote} /></StudioProvider>);
  const boxes=screen.getAllByRole("checkbox") as HTMLInputElement[];
  expect(boxes.every(box=>!box.checked)).toBe(true);
  fireEvent.click(screen.getByRole("button",{name:/^全\s*选$/}));
  expect(boxes.every(box=>box.checked)).toBe(true);
  fireEvent.click(screen.getByRole("button",{name:"全不选"}));
  expect(boxes.every(box=>!box.checked)).toBe(true);
  expect(quote).not.toHaveBeenCalled();
});

it("opens the original edit image in a zoom viewer without submitting a quote",async()=>{
  const quote=vi.fn();
  render(<StudioProvider><EditImageDialog p={p} q={shot} busy={false} close={vi.fn()} quote={quote} /></StudioProvider>);
  fireEvent.click(screen.getByRole("img",{name:"独立编辑底图"}));
  await waitFor(()=>expect(document.querySelector(".ant-image-preview img")?.getAttribute("src")).toContain("base.png"));
  expect(screen.getByRole("textbox",{name:"修改内容"})).toBeTruthy();
  expect(quote).not.toHaveBeenCalled();
});
