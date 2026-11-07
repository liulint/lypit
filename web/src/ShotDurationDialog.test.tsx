// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {ShotDurationDialog} from "./ShotDurationDialog";
import {StudioProvider} from "./ui";
import type {Project,Shot} from "./api";
beforeEach(()=>{vi.stubGlobal("ResizeObserver",class{observe(){}unobserve(){}disconnect(){}});vi.stubGlobal("matchMedia",vi.fn(()=>({matches:false,addListener:vi.fn(),removeListener:vi.fn(),addEventListener:vi.fn(),removeEventListener:vi.fn()})));const style=window.getComputedStyle.bind(window);vi.spyOn(window,"getComputedStyle").mockImplementation(el=>style(el));});
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
const q={id:"shot",title:"路灯下",duration:2,narration:"台词",candidates:[],selected:0,references:[],approved:false,description:"动作"} as Shot;
const p={id:"p",revision:4,duration:30,shots:[q],mode:"remote",archived:false} as Project;
const info={policy:{min:4,max:12,model:"doubao-seedance-2-0-mini",known:true,settings_version:3},rejected_tasks:["failed"],blocked:false};
it("previews a longer film without generating and requires explicit ending of rejected receipts",async()=>{
 const fetcher=vi.fn().mockResolvedValue(new Response(JSON.stringify(info)));vi.stubGlobal("fetch",fetcher);
 const save=vi.fn().mockResolvedValue(true),close=vi.fn();
 render(<StudioProvider><ShotDurationDialog p={p} q={q} trial={false} busy={false} close={close} save={save}/></StudioProvider>);
 await screen.findByText(/当前制作范围/);
 fireEvent.click(screen.getByRole("button",{name:"+1 秒"}));
 fireEvent.click(screen.getByRole("button",{name:"−1 秒"}));
 expect((screen.getByRole("spinbutton",{name:"镜头时长（秒）"}) as HTMLInputElement).value).toBe("4");
 expect(screen.getByText("32 秒")).toBeTruthy();
 const button=screen.getByRole("button",{name:"保存时长调整"}) as HTMLButtonElement;
 expect(button.disabled).toBe(true);expect(save).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("checkbox"));fireEvent.click(button);
 await waitFor(()=>expect(save).toHaveBeenCalledExactlyOnceWith(4,3,true));
 expect(close).toHaveBeenCalledOnce();
 expect(fetcher.mock.calls.every(([path,opts])=>String(path).endsWith("/duration")&&opts.method==="GET")).toBe(true);
});
it("blocks unknown or running tasks and does not submit a duration change",async()=>{
 vi.stubGlobal("fetch",vi.fn().mockResolvedValue(new Response(JSON.stringify({...info,rejected_tasks:[],blocked:true}))));
 const save=vi.fn();render(<StudioProvider><ShotDurationDialog p={p} q={q} trial={false} busy={false} close={vi.fn()} save={save}/></StudioProvider>);
 await screen.findByText(/仍有执行中或结果待核对/);fireEvent.click(screen.getByRole("button",{name:"+1 秒"}));
 const button=screen.getByRole("button",{name:"保存时长调整"}) as HTMLButtonElement;expect(button.disabled).toBe(true);fireEvent.click(button);expect(save).not.toHaveBeenCalled();
});

it("requires reopening after another project revision instead of silently rebasing a timing edit",async()=>{
 vi.stubGlobal("fetch",vi.fn(async()=>new Response(JSON.stringify({...info,rejected_tasks:[]}))));
 const save=vi.fn(),props={q,trial:false,busy:false,close:vi.fn(),save};
 const view=render(<StudioProvider><ShotDurationDialog p={p} {...props}/></StudioProvider>);
 await screen.findByText(/当前制作范围/);fireEvent.click(screen.getByRole("button",{name:"+1 秒"}));
 view.rerender(<StudioProvider><ShotDurationDialog p={{...p,revision:5}} {...props}/></StudioProvider>);
 expect(await screen.findByText(/作品已发生变化/)).toBeTruthy();
 expect((screen.getByRole("button",{name:"保存时长调整"}) as HTMLButtonElement).disabled).toBe(true);expect(save).not.toHaveBeenCalled();
});

it("quotes rewriting at the selected duration without saving current timing",async()=>{
 vi.stubGlobal("fetch",vi.fn(async()=>new Response(JSON.stringify({...info,rejected_tasks:[]}))));
 const save=vi.fn(),rewrite=vi.fn().mockResolvedValue(true),close=vi.fn();
 render(<StudioProvider><ShotDurationDialog p={p} q={q} trial={false} busy={false} close={close} save={save} rewrite={rewrite} editContent={vi.fn()}/></StudioProvider>);
 await screen.findByText(/当前制作范围/);fireEvent.click(screen.getByRole("button",{name:"+1 秒"}));
 fireEvent.click(screen.getByRole("button",{name:"按新秒数重写 · 查看报价"}));
 await waitFor(()=>expect(rewrite).toHaveBeenCalledWith(5));expect(save).not.toHaveBeenCalled();expect(close).toHaveBeenCalledOnce();
});
