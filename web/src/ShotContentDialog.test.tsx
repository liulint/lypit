// @vitest-environment jsdom
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {ShotContentDialog} from "./ShotContentDialog";
import {StudioProvider} from "./ui";
import type {Project,Shot} from "./api";
beforeEach(()=>{vi.stubGlobal("ResizeObserver",class{observe(){}unobserve(){}disconnect(){}});vi.stubGlobal("matchMedia",vi.fn(()=>({matches:false,addListener:vi.fn(),removeListener:vi.fn(),addEventListener:vi.fn(),removeEventListener:vi.fn()})));const style=window.getComputedStyle.bind(window);vi.spyOn(window,"getComputedStyle").mockImplementation(el=>style(el));});
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
const q={id:"s",title:"路灯",first_frame:"旧首帧",description:"旧动作",narration:"旧台词",duration:5,candidates:[],references:[],selected:0,approved:true} as Shot;
const p={id:"p",revision:4,duration:30,shots:[q],archived:false} as Project;
const info={policy:{min:1,max:12,settings_version:3},blocked:false,rejected_tasks:[]};
const result={revision:4,settings_version:3,before:{first_frame:"旧首帧",description:"旧动作",narration:"旧台词"},after:{first_frame:"新首帧",description:"新动作",narration:""},old_duration:5,duration:4};
it("loads persisted suggestion for comparison and only saves on explicit application",async()=>{
 const fetcher=vi.fn(async(path:string,_opts?:RequestInit)=>new Response(JSON.stringify(path.endsWith("/rewrite")?result:info)));vi.stubGlobal("fetch",fetcher);
 const save=vi.fn().mockResolvedValue(true),close=vi.fn();
 render(<StudioProvider><ShotContentDialog p={p} q={q} trial={false} taskID="task" busy={false} save={save} close={close}/></StudioProvider>);
 await waitFor(()=>expect((screen.getByRole("textbox",{name:"静态首帧"}) as HTMLTextAreaElement).value).toBe("新首帧"));
 expect(screen.getByText("首帧：旧首帧")).toBeTruthy();expect(save).not.toHaveBeenCalled();
 fireEvent.change(screen.getByRole("textbox",{name:"视频动作与运镜"}),{target:{value:"手动精简的新动作"}});
 fireEvent.click(screen.getByRole("button",{name:"确认应用"}));
 await waitFor(()=>expect(save).toHaveBeenCalledWith({...result.after,description:"手动精简的新动作"},4,3));expect(close).toHaveBeenCalledOnce();
 expect(fetcher.mock.calls.every(([,opts])=>!(opts as RequestInit)?.body)).toBe(true);
});
it("manual editing and cancellation never generate a model request",async()=>{
 const fetcher=vi.fn(async()=>new Response(JSON.stringify(info)));vi.stubGlobal("fetch",fetcher);const save=vi.fn();
 render(<StudioProvider><ShotContentDialog p={p} q={q} trial={false} duration={6} busy={false} save={save} close={vi.fn()}/></StudioProvider>);
 await waitFor(()=>expect((screen.getByRole("textbox",{name:"静态首帧"}) as HTMLTextAreaElement).disabled).toBe(false));
 fireEvent.change(screen.getByRole("textbox",{name:"静态首帧"}),{target:{value:"修改但不保存"}});fireEvent.click(screen.getByRole("button",{name:/取\s*消/}));expect(save).not.toHaveBeenCalled();expect(fetcher).toHaveBeenCalledOnce();
});
it("blocks applying unknown tasks and revision changes",async()=>{
 vi.stubGlobal("fetch",vi.fn(async()=>new Response(JSON.stringify({...info,blocked:true}))));const save=vi.fn();
 const props={q,trial:false,busy:false,save,close:vi.fn()};const view=render(<StudioProvider><ShotContentDialog p={p} {...props}/></StudioProvider>);
 await screen.findByText(/请先恢复或处理当前任务/);expect((screen.getByRole("button",{name:"确认应用"}) as HTMLButtonElement).disabled).toBe(true);
 view.rerender(<StudioProvider><ShotContentDialog p={{...p,revision:5}} {...props}/></StudioProvider>);expect(screen.getByText(/作品或模型配置已变化/)).toBeTruthy();expect(save).not.toHaveBeenCalled();
});
