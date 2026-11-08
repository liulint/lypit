// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {StudioProvider} from "./ui";
import DramaVideoFrameQuote from "./DramaVideoFrameQuote";
import type {Entry,Media} from "./drama";
beforeEach(()=>{Object.defineProperty(window,"matchMedia",{value:()=>({matches:false,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){}}),writable:true});const computed=window.getComputedStyle;vi.spyOn(window,"getComputedStyle").mockImplementation(el=>computed(el))});
afterEach(()=>{cleanup();vi.restoreAllMocks()});
const entry={id:"MOTION-EP001-001",title:"镜头一",fields:{"时长":"4s"},start:0,end:0,body:""} as Entry;
const frame={id:"frame",entry:"SHOT-EP001-001",kind:"image",current:true,approved:true,file:"frame.png",hash:"hash"} as Media;
it("defaults to the only eligible first frame and only previews a quote on click",async()=>{
 const quote=vi.fn(async()=>{});const p={entry,medias:[frame,{...frame,id:"wrong",entry:"SHOT-EP001-002"},{...frame,id:"stale",current:false},{...frame,id:"draft",approved:false}],busy:false,cancel:vi.fn(),goBoards:vi.fn(),quote,run:async(fn:()=>Promise<void>)=>fn()};
 const {rerender}=render(<StudioProvider><DramaVideoFrameQuote {...p}/></StudioProvider>);
 const button=screen.getByRole("button",{name:"查看视频报价"}) as HTMLButtonElement;
 expect(button.disabled).toBe(false);expect(screen.getAllByRole("radio")).toHaveLength(1);expect((screen.getByRole("radio") as HTMLInputElement).checked).toBe(true);expect(quote).not.toHaveBeenCalled();
 rerender(<StudioProvider><DramaVideoFrameQuote {...p} medias={[]}/></StudioProvider>);expect(button.disabled).toBe(true);
 fireEvent.click(screen.getByRole("button",{name:"去确认分镜画面"}));expect(p.goBoards).toHaveBeenCalledOnce();
 rerender(<StudioProvider><DramaVideoFrameQuote {...p}/></StudioProvider>);
 fireEvent.click(screen.getByRole("button",{name:"查看视频报价"}));await waitFor(()=>expect(quote).toHaveBeenCalledExactlyOnceWith(frame));
});
it("requires review of a local padded derivative before quoting and preserves the source",async()=>{
 const padded={...frame,id:"padded",file:"padded.png",approved:false};const adapt=vi.fn(async()=>padded);const approve=vi.fn(async()=>({...padded,approved:true}));const quote=vi.fn(async()=>{});
 render(<StudioProvider><DramaVideoFrameQuote entry={entry} medias={[frame]} aspect="9:16" adapt={adapt} approve={approve} busy={false} cancel={()=>{}} goBoards={()=>{}} quote={quote} run={async fn=>fn()}/></StudioProvider>);
 const image=screen.getByAltText("本镜画面 1");Object.defineProperty(image,"naturalWidth",{value:1024});Object.defineProperty(image,"naturalHeight",{value:1536});fireEvent.load(image);fireEvent.click(screen.getByRole("radio"));
 expect((screen.getByRole("button",{name:"查看视频报价"}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.click(screen.getByRole("button",{name:"本地留边适配画幅"}));await waitFor(()=>expect(screen.getAllByRole("radio")).toHaveLength(2));expect(adapt).toHaveBeenCalledExactlyOnceWith(frame);expect(quote).not.toHaveBeenCalled();
 expect((screen.getByRole("button",{name:"查看视频报价"}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.click(screen.getByRole("button",{name:"看过了，确认此首帧"}));await waitFor(()=>expect(approve).toHaveBeenCalledOnce());
 fireEvent.click(screen.getByRole("button",{name:"查看视频报价"}));await waitFor(()=>expect(quote).toHaveBeenCalledExactlyOnceWith({...padded,approved:true}));expect(frame.approved).toBe(true);
});

it("requires selection when multiple eligible frames exist",async()=>{
 const second={...frame,id:"second",file:"second.png"};const quote=vi.fn(async()=>{});
 const p={entry,medias:[frame],busy:false,cancel:vi.fn(),goBoards:vi.fn(),quote,run:async(fn:()=>Promise<void>)=>fn()};
 const {rerender}=render(<StudioProvider><DramaVideoFrameQuote {...p}/></StudioProvider>);
 rerender(<StudioProvider><DramaVideoFrameQuote {...p} medias={[frame,second]}/></StudioProvider>);
 const button=screen.getByRole("button",{name:"查看视频报价"}) as HTMLButtonElement;
 expect(button.disabled).toBe(true);expect(quote).not.toHaveBeenCalled();
 expect(screen.getAllByRole("radio").every(r=>!(r as HTMLInputElement).checked)).toBe(true);
 fireEvent.click(screen.getByRole("radio",{name:/本镜画面 2/}));expect(button.disabled).toBe(false);
 fireEvent.click(button);await waitFor(()=>expect(quote).toHaveBeenCalledExactlyOnceWith(second));
});
