// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {StudioProvider} from "./ui";
import {api} from "./api";
import DramaVideoFrameQuote from "./DramaVideoFrameQuote";
import DramaVideoBatchQuote from "./DramaVideoBatchQuote";
import type {Entry,Media,VideoStart} from "./drama";
vi.mock("./DramaVideoOptions",()=>({default:()=>null}));
vi.mock("./api",async importOriginal=>({...await importOriginal<typeof import("./api")>(),api:vi.fn()}));
const context={workId:"work",motionBase:"motion",boardBase:"board"};
const entries=[1,2].map(n=>({id:`MOTION-EP001-00${n}`,title:`镜头${n}`,fields:{"时长":"4s"},body:"",start:0,end:0} as Entry));
const frames=entries.map((e,n)=>({id:`board-${n}`,hash:`hash-${n}`,entry:e.id.replace("MOTION-","SHOT-"),kind:"image",file:`board-${n}.png`,approved:true,current:true} as Media));
const source={id:"video-1",hash:"video-hash",entry:entries[0].id} as Media;
const tail={...frames[1],id:"tail",hash:"tail-hash",file:"tail.png",reference_only:true,parent:source.id};
const plans:VideoStart[]=[{entry:entries[0].id,recommended:"board",reason:"第一镜",choice:"auto",mode:"board"},{entry:entries[1].id,previous:entries[0].id,recommended:"tail",reason:"连续接拍",choice:"auto",mode:"tail",frame:tail,source}];
beforeEach(()=>{globalThis.ResizeObserver=class{observe(){}unobserve(){}disconnect(){}};Object.defineProperty(window,"matchMedia",{value:()=>({matches:false,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){}}),writable:true});const computed=window.getComputedStyle;vi.spyOn(window,"getComputedStyle").mockImplementation(el=>computed(el));vi.mocked(api).mockReset()});
afterEach(()=>{cleanup();vi.restoreAllMocks()});
function load(name:string){const image=screen.getByAltText(name);Object.defineProperty(image,"naturalWidth",{value:900,configurable:true});Object.defineProperty(image,"naturalHeight",{value:1600,configurable:true});fireEvent.load(image)}

it("defaults to the recommended tail and binds its source only when requesting the quote",async()=>{
 vi.mocked(api).mockResolvedValue([plans[1]]);const quote=vi.fn(async()=>{});
 render(<StudioProvider><DramaVideoFrameQuote entry={entries[1]} medias={frames} startContext={{...context,entry:entries[1].id}} busy={false} cancel={()=>{}} goBoards={()=>{}} quote={quote} run={async fn=>fn()}/></StudioProvider>);
 await screen.findByAltText(`上一视频真实尾帧 ${entries[1].id}`);expect(quote).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("button",{name:"查看视频报价"}));
 await waitFor(()=>expect(quote).toHaveBeenCalledExactlyOnceWith(tail,undefined,{tail_video_id:source.id,tail_video_hash:source.hash}));
});

it("lets the user override a tail recommendation before a quote",async()=>{
 vi.mocked(api).mockImplementation(async(_path,body)=>{const choice=(body as {choices?:{mode:string}[]}).choices?.[0]?.mode;return [{...plans[1],...(choice==="board"?{choice:"board",mode:"board",frame:undefined,source:undefined}:{})}] as never});
 const quote=vi.fn(async()=>{});render(<StudioProvider><DramaVideoFrameQuote entry={entries[1]} medias={frames} startContext={{...context,entry:entries[1].id}} busy={false} cancel={()=>{}} goBoards={()=>{}} quote={quote} run={async fn=>fn()}/></StudioProvider>);
 await screen.findByAltText(`上一视频真实尾帧 ${entries[1].id}`);
 fireEvent.mouseDown(screen.getByRole("combobox",{name:`开场画面 ${entries[1].id}`}));
 fireEvent.click(await screen.findByText("使用本镜分镜图"));
 await screen.findByAltText("本镜画面 1");expect(quote).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("button",{name:"查看视频报价"}));await waitFor(()=>expect(quote).toHaveBeenCalledExactlyOnceWith(frames[1]));
 expect(vi.mocked(api).mock.calls[1][1]).toMatchObject({choices:[{entry:entries[1].id,mode:"board"}]});
});

it("excludes dependent shots until the previous video has been reviewed, then quotes the actual tail",async()=>{
 vi.mocked(api).mockResolvedValue([plans[0],{...plans[1],frame:undefined,source:undefined,waiting:"等待上一镜生成并确认选用"}]);
 const quote=vi.fn(async()=>{});const props={entries,medias:frames,startContext:context,aspect:"9:16",busy:false,cancel:()=>{},goBoards:()=>{},quote,adapt:async(m:Media)=>m,approve:async(m:Media)=>m,run:async(fn:()=>Promise<void>)=>fn()};
 const view=render(<StudioProvider><DramaVideoBatchQuote {...props}/></StudioProvider>);
 await screen.findByText("等待上一镜生成并确认选用");load("首帧 1");
 fireEvent.click(screen.getByRole("button",{name:"统一查看 1 镜视频报价"}));
 await waitFor(()=>expect(quote).toHaveBeenCalledExactlyOnceWith([{entry:entries[0].id,media_id:frames[0].id,hash:frames[0].hash}]));
 view.unmount();vi.mocked(api).mockResolvedValue([plans[1]]);
 render(<StudioProvider><DramaVideoBatchQuote {...props} entries={[entries[1]]}/></StudioProvider>);
 await screen.findByText(`来源：${entries[0].id} · 当前已确认视频的真实尾帧`);load("首帧 1");
 fireEvent.click(screen.getByRole("button",{name:"统一查看 1 镜视频报价"}));
 await waitFor(()=>expect(quote).toHaveBeenLastCalledWith([{entry:entries[1].id,media_id:tail.id,hash:tail.hash,tail_video_id:source.id,tail_video_hash:source.hash}]));
});

it("blocks quotes if planning fails instead of silently using the storyboard",async()=>{
 vi.mocked(api).mockRejectedValue(new Error("来源已变化"));const quote=vi.fn(async()=>{});
 render(<StudioProvider><DramaVideoFrameQuote entry={entries[1]} medias={frames} startContext={{...context,entry:entries[1].id}} busy={false} cancel={()=>{}} goBoards={()=>{}} quote={quote} run={async fn=>fn()}/></StudioProvider>);
 await screen.findByText("来源已变化");expect((screen.getByRole("button",{name:"查看视频报价"}) as HTMLButtonElement).disabled).toBe(true);expect(quote).not.toHaveBeenCalled();
});
