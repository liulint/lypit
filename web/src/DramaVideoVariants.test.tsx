// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {StudioProvider} from "./ui";
import DramaVideoTrial from "./DramaVideoTrial";
import DramaVideoHistory from "./DramaVideoHistory";
import DramaVideoOptions from "./DramaVideoOptions";
import type {Entry,Media} from "./drama";

vi.mock("./api",()=>({media:(file:string)=>`/media/${file}`,api:vi.fn(async()=>({options:[{vendor_id:"ark",vendor:"方舟",model:"doubao-seedance-2-0-mini-260615",resolution:"480p",resolutions:["480p","720p"]}],default:{vendor_id:"ark",model:"doubao-seedance-2-0-mini-260615",resolution:"480p"}}))}));
beforeEach(()=>{vi.stubGlobal("ResizeObserver",class{observe(){}unobserve(){}disconnect(){}});Object.defineProperty(window,"matchMedia",{value:()=>({matches:false,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){}}),writable:true});const computed=window.getComputedStyle;vi.spyOn(window,"getComputedStyle").mockImplementation(el=>computed(el));vi.spyOn(HTMLMediaElement.prototype,"play").mockResolvedValue();vi.spyOn(HTMLMediaElement.prototype,"pause").mockImplementation(()=>{})});
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals()});
const entries=[1,2].map(n=>({id:`MOTION-EP001-00${n}`,title:`镜头${n}`,fields:{时长:"5s"},start:0,end:1,body:"same prompt"} as Entry));
const old={id:"old",entry:entries[0].id,kind:"video",file:"old.mp4",hash:"old-hash",current:true,approved:true,selected:true} as Media;
const candidate={...old,id:"candidate",file:"candidate.mp4",approved:false,selected:false};
const run=async(fn:()=>Promise<void>)=>fn();

it("quotes a single unchanged shot with a different resolution only after explicit action",async()=>{
 const quote=vi.fn(async()=>{});render(<StudioProvider><DramaVideoTrial entries={[entries[0]]} medias={[old]} batch={false} busy={false} cancel={()=>{}} quote={quote} run={run}/></StudioProvider>);
 const button=screen.getByRole("button",{name:"查看 1 镜报价"});await waitFor(()=>expect((button as HTMLButtonElement).disabled).toBe(false));expect(quote).not.toHaveBeenCalled();
 fireEvent.mouseDown(screen.getByRole("combobox",{name:"本次视频清晰度"}));const choices=await screen.findAllByText("720p");fireEvent.click(choices[choices.length-1]);fireEvent.click(button);
 await waitFor(()=>expect(quote).toHaveBeenCalledExactlyOnceWith([entries[0].id],{vendor_id:"ark",model:"doubao-seedance-2-0-mini-260615",resolution:"720p"},[]));
 expect(screen.queryByRole("textbox")).toBeNull();
});

it("requires an explicit batch selection and sends only the checked shots",async()=>{
 const quote=vi.fn(async()=>{});render(<StudioProvider><DramaVideoTrial entries={entries} medias={[old]} batch busy={false} cancel={()=>{}} quote={quote} run={run}/></StudioProvider>);
 await screen.findByText("doubao-seedance-2-0-mini-260615 · 参考价未知");expect((screen.getByRole("button",{name:"查看 0 镜报价"}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.click(screen.getByRole("checkbox",{name:"镜头1 · 5s"}));fireEvent.click(screen.getByRole("button",{name:"查看 1 镜报价"}));
 await waitFor(()=>expect(quote).toHaveBeenCalledExactlyOnceWith([entries[0].id],expect.objectContaining({resolution:"480p"}),[]));
});

it("compares existing versions without selecting until the user confirms",async()=>{
 const select=vi.fn(async()=>{});render(<StudioProvider><DramaVideoHistory versions={[candidate,old]} busy={false} cancel={()=>{}} select={select} run={run}/></StudioProvider>);
 expect(select).not.toHaveBeenCalled();fireEvent.click(screen.getByRole("button",{name:"一起从头播放"}));await waitFor(()=>expect(HTMLMediaElement.prototype.play).toHaveBeenCalledTimes(2));
 fireEvent.click(screen.getByRole("button",{name:"一起暂停"}));expect(HTMLMediaElement.prototype.pause).toHaveBeenCalledTimes(2);
 fireEvent.click(screen.getByRole("button",{name:"看过了，确认选用此版本"}));await waitFor(()=>expect(select).toHaveBeenCalledExactlyOnceWith(candidate,"old"));
});

it("requires an explicit resolution choice when changing to a model that cannot keep 1080p",async()=>{
 const change=vi.fn();render(<StudioProvider><DramaVideoOptions value={{vendor_id:"ark",model:"doubao-seedance-2-0-260128",resolution:"1080p"}} onChange={change} busy={false}/></StudioProvider>);
 await waitFor(()=>expect(screen.queryByText("正在读取兼容模型")).toBeNull());
 fireEvent.mouseDown(screen.getByRole("combobox",{name:"本次视频模型"}));const choices=await screen.findAllByText("doubao-seedance-2-0-mini-260615 · 参考价未知");fireEvent.click(choices[choices.length-1]);
 expect(change).toHaveBeenCalledExactlyOnceWith({vendor_id:"ark",model:"doubao-seedance-2-0-mini-260615",resolution:""});
 expect(screen.getByText("此模型不支持原清晰度，请主动选择新的清晰度后查看报价。")).toBeTruthy();
});
