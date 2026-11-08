// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {StudioProvider} from "./ui";
import DramaVideoBatchQuote from "./DramaVideoBatchQuote";
import type {Entry,Media,FrameSelection} from "./drama";
beforeEach(()=>{Object.defineProperty(window,"matchMedia",{value:()=>({matches:false,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){}}),writable:true});const computed=window.getComputedStyle;vi.spyOn(window,"getComputedStyle").mockImplementation(el=>computed(el))});
afterEach(()=>{cleanup();vi.restoreAllMocks()});
const entries=[1,2].map(n=>({id:`MOTION-EP001-00${n}`,title:`镜头${n}`,fields:{"时长":n===1?"4s":"5s"},start:0,end:1,body:""} as Entry));
const frames=entries.map((e,n)=>({id:`frame-${n}`,hash:`hash-${n}`,file:`frame-${n}.png`,entry:e.id.replace("MOTION-","SHOT-"),kind:"image",approved:true,current:true} as Media));
function load(name:string,width=900,height=1600){const im=screen.getByAltText(name);Object.defineProperty(im,"naturalWidth",{configurable:true,value:width});Object.defineProperty(im,"naturalHeight",{configurable:true,value:height});fireEvent.load(im)}
function props(){return {entries,medias:frames,aspect:"9:16",busy:false,cancel:vi.fn(),goBoards:vi.fn(),quote:vi.fn(async(_frames:FrameSelection[])=>{}),adapt:vi.fn(async(m:Media)=>({...m,id:"padded",hash:"padded-hash",approved:false,file:"padded.png"})),approve:vi.fn(async(m:Media)=>({...m,approved:true})),run:async(fn:()=>Promise<void>)=>fn()}}
it("reviews all frame snapshots and quotes once only after dimension checks",async()=>{
 const p=props();const {rerender}=render(<StudioProvider><DramaVideoBatchQuote {...p}/></StudioProvider>);
 const button=screen.getByRole("button",{name:"统一查看 2 镜视频报价"}) as HTMLButtonElement;
 expect(screen.getByText("本次补齐 2 镜视频，共 9 秒")).toBeTruthy();expect(button.disabled).toBe(true);expect(p.quote).not.toHaveBeenCalled();
 load("首帧 1");load("首帧 2");expect(button.disabled).toBe(false);
 fireEvent.click(button);await waitFor(()=>expect(p.quote).toHaveBeenCalledExactlyOnceWith(frames.map((m,n)=>({entry:entries[n].id,media_id:m.id,hash:m.hash}))));
 rerender(<StudioProvider><DramaVideoBatchQuote {...p} medias={[{...frames[0],hash:"changed"},frames[1]]}/></StudioProvider>);expect(button.disabled).toBe(true);expect(screen.getByText(/缺少当前已确认/)).toBeTruthy();
 fireEvent.click(screen.getByRole("button",{name:"去确认分镜画面"}));expect(p.goBoards).toHaveBeenCalledOnce();
});
it("requires review and approval of padded frames before a batch quote",async()=>{
 const p=props();render(<StudioProvider><DramaVideoBatchQuote {...p}/></StudioProvider>);
 load("首帧 1");load("首帧 2",1024,1536);expect((screen.getByRole("button",{name:"统一查看 2 镜视频报价"}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.click(screen.getByRole("button",{name:"本地留边适配"}));await waitFor(()=>expect(p.adapt).toHaveBeenCalledExactlyOnceWith(frames[1]));
 await screen.findByText("新版本待检查确认");load("首帧 2");expect(p.quote).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("button",{name:"看过了，确认此首帧"}));await waitFor(()=>expect(p.approve).toHaveBeenCalledOnce());
 await waitFor(()=>expect((screen.getByRole("button",{name:"统一查看 2 镜视频报价"}) as HTMLButtonElement).disabled).toBe(false));
 fireEvent.click(screen.getByRole("button",{name:"统一查看 2 镜视频报价"}));await waitFor(()=>expect(p.quote).toHaveBeenCalledOnce());expect(p.quote.mock.calls[0][0][1].media_id).toBe("padded");expect(frames[1].hash).toBe("hash-1");
});
