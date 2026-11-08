// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {StudioProvider} from "./ui";
import DramaVideoPreview from "./DramaVideoPreview";
import type {Entry,Media} from "./drama";
beforeEach(()=>{Object.defineProperty(window,"matchMedia",{value:()=>({matches:false,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){}}),writable:true});const computed=window.getComputedStyle;vi.spyOn(window,"getComputedStyle").mockImplementation(el=>computed(el));vi.spyOn(HTMLMediaElement.prototype,"pause").mockImplementation(()=>{})});
afterEach(()=>{cleanup();vi.restoreAllMocks()});
const entries=[1,2,3].map(n=>({id:`MOTION-${n}`,title:`猫咪 ${n}`,fields:{时长:"4s"}} as unknown as Entry));
const videos=[0,2].map(n=>({id:`video-${n}`,entry:entries[n].id,kind:"video",file:`${n}.mp4`,hash:`hash-${n}`,current:true,approved:false,selected:false} as Media));
function props(){return {entries,medias:videos,entryID:entries[0].id,change:vi.fn(),cancel:vi.fn(),busy:false,run:async(fn:()=>Promise<void>)=>fn(),approve:vi.fn(async()=>{})}}
it("navigates existing videos in shot order and pauses on switch and close",()=>{
 const p=props();const {rerender,unmount}=render(<StudioProvider><DramaVideoPreview {...p}/></StudioProvider>);
 expect((screen.getByRole("button",{name:"上一镜"}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.click(screen.getByRole("button",{name:"下一镜"}));expect(p.change).toHaveBeenCalledExactlyOnceWith(entries[2].id);
 rerender(<StudioProvider><DramaVideoPreview {...p} entryID={entries[2].id}/></StudioProvider>);
 expect(HTMLMediaElement.prototype.pause).toHaveBeenCalledTimes(1);expect(screen.getByText("镜头 3 · 猫咪 3")).toBeTruthy();
 expect((screen.getByRole("button",{name:"下一镜"}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.click(screen.getByRole("button",{name:"上一镜"}));expect(p.change).toHaveBeenLastCalledWith(entries[0].id);
 unmount();expect(HTMLMediaElement.prototype.pause).toHaveBeenCalledTimes(2);expect(p.approve).not.toHaveBeenCalled();
});
it("confirms only the displayed version and reflects refreshed approval without closing",async()=>{
 const p=props();const {rerender}=render(<StudioProvider><DramaVideoPreview {...p}/></StudioProvider>);
 const confirm=screen.getByRole("button",{name:"确认使用此视频"});fireEvent.click(confirm);fireEvent.click(confirm);
 await waitFor(()=>expect(p.approve).toHaveBeenCalledExactlyOnceWith(videos[0]));await waitFor(()=>expect(confirm.className).not.toContain("ant-btn-loading"));expect(p.cancel).not.toHaveBeenCalled();
 rerender(<StudioProvider><DramaVideoPreview {...p} medias={[{...videos[0],approved:true,selected:true},videos[1]]}/></StudioProvider>);
 expect((screen.getByRole("button",{name:"已确认使用"}) as HTMLButtonElement).disabled).toBe(true);
 rerender(<StudioProvider><DramaVideoPreview {...p} medias={[{...videos[0],current:false},videos[1]]}/></StudioProvider>);
 expect((screen.getByRole("button",{name:"确认使用此视频"}) as HTMLButtonElement).disabled).toBe(true);
 expect(screen.getByText("来源变化，需重新制作")).toBeTruthy();
});
