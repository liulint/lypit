// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen,waitFor,within} from "@testing-library/react";
import {StudioProvider} from "./ui";
import DramaGuide from "./DramaGuide";
import type {Doc,Job,Work,Media} from "./drama";
const w={id:"a".repeat(24),title:"第一堂课",duration:33,aspect:"9:16",idea:"教学故事梗概",form:"写实剧情",context:{series:{settings:"很长的共用设定".repeat(500)},media:[],previous:[]}} as unknown as Work;
const script:Doc={kind:"script",text:"",hash:"",state:"draft",entries:[],issues:[],inputs:{},version:0,path:"",accepted_hash:""};
beforeEach(()=>{vi.stubGlobal("fetch",vi.fn(async(url:string,init?:RequestInit)=>{if(!url.endsWith("/video-starts"))throw new Error(`Unexpected request ${url}`);const req=JSON.parse(String(init?.body||"{}"));return {ok:true,json:async()=>[1,2].map(n=>({entry:`MOTION-EP001-00${n}`,recommended:"board",reason:"测试分镜关系未明确",choice:"auto",mode:"board"})).filter(p=>!req.entry||p.entry===req.entry)}}));Object.defineProperty(window,"matchMedia",{value:()=>({matches:false,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){}}),writable:true});const computed=window.getComputedStyle;vi.spyOn(window,"getComputedStyle").mockImplementation(el=>computed(el));globalThis.ResizeObserver=class{observe(){}unobserve(){}disconnect(){}}});
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks()});
function props(){return {work:w,docs:[script],jobs:[] as Job[],medias:[],loaded:true,busy:false,run:async(fn:()=>Promise<void>)=>{await fn()},refresh:vi.fn(async()=>{}),produce:vi.fn(async()=>{}),advanced:vi.fn(),materials:vi.fn(),onDirty:vi.fn()}}
it("keeps long shared settings out of the workbench and shows workflow navigation above the content",async()=>{
 const p=props();const {container}=render(<StudioProvider><DramaGuide {...p}/></StudioProvider>);expect(await screen.findByRole("button",{name:"生成本集剧本"})).toBeTruthy();expect(screen.queryByText(/很长的共用设定/)).toBeNull();expect(container.querySelector("nav.guide-rail")).toBeTruthy();expect(screen.queryByLabelText("本集结束状态")).toBeNull();fireEvent.click(screen.getByRole("button",{name:"制作资料"}));expect(p.materials).toHaveBeenCalledOnce();expect(p.produce).not.toHaveBeenCalled();
});
it("submits exactly one bounded task, adopts its result, and never auto-generates the next step",async()=>{
 const calls:{url:string;body:Record<string,unknown>}[]=[];vi.stubGlobal("fetch",vi.fn(async(url:string,init?:RequestInit)=>{calls.push({url,body:JSON.parse(String(init?.body||"{}"))});return {ok:true,json:async()=>({})}}));const p=props();const {rerender}=render(<StudioProvider><DramaGuide {...p}/></StudioProvider>);fireEvent.click(await screen.findByRole("button",{name:"生成本集剧本"}));await waitFor(()=>expect(calls).toHaveLength(1));expect(calls[0].body.kind).toBe("script");expect(calls[0].body.operation_id).toMatch(/^[a-f0-9]{24}$/);
 const job:Job={task:{id:"b".repeat(24),stage:"drama_script",status:"completed",message:"",mode:"mock"},job:{id:"b".repeat(24),kind:"script",base:"",candidate:"# 第一堂课\n\n## EP001-SC001\n猫停下脚步。",applied:false,references:[]}};
 rerender(<StudioProvider><DramaGuide {...p} jobs={[job]}/></StudioProvider>);expect(await screen.findByText("猫停下脚步。")).toBeTruthy();fireEvent.click(screen.getByRole("button",{name:"确认使用这版剧本"}));await waitFor(()=>expect(calls).toHaveLength(2));expect(calls[1].url).toContain("/adopt");expect(calls.filter(c=>c.url.endsWith("/create"))).toHaveLength(1);
 rerender(<StudioProvider><DramaGuide {...p} docs={[{...script,text:job.job.candidate,hash:"accepted",state:"accepted"}]} jobs={[{...job,job:{...job.job,applied:true}}]}/></StudioProvider>);fireEvent.click(await screen.findByRole("button",{name:"下一步：整理角色与场景"}));await screen.findByRole("button",{name:"整理角色与场景设定"});expect(calls).toHaveLength(2);
});
it("offers only original-result recovery for an interrupted task",async()=>{
 const calls:string[]=[];vi.stubGlobal("fetch",vi.fn(async(url:string)=>{calls.push(url);return{ok:true,json:async()=>({})}}));const p=props();const job={task:{id:"b".repeat(24),status:"interrupted",message:"提交结果未知"},job:{kind:"script"}} as Job;render(<StudioProvider><DramaGuide {...p} jobs={[job]}/></StudioProvider>);fireEvent.click(await screen.findByRole("button",{name:"恢复已有结果"}));await waitFor(()=>expect(calls).toEqual([`/api/drama/jobs/${job.task.id}/recover`]));expect(screen.queryByRole("button",{name:"生成本集剧本"})).toBeNull();
});

it("quotes all missing asset images once and tracks the batch without generating automatically",async()=>{
 const p=props();const docs=[{...script,text:"剧本",hash:"s",state:"accepted"},{...script,kind:"assets",text:"设定",hash:"a",state:"accepted"},{...script,kind:"images",text:"出图说明",hash:"i",state:"accepted",entries:[1,2,3].map(n=>({id:`IMG-${n}`,title:`IMG-${n} · 图片${n}`,fields:{},body:"",start:0,end:0}))}];
 const existing={id:"existing",entry:"IMG-1",kind:"image",current:true,approved:false,file:"existing.png",path:"existing.png"} as Media;
 const {rerender}=render(<StudioProvider><DramaGuide {...p} docs={docs} medias={[existing]}/></StudioProvider>);
 expect(p.produce).not.toHaveBeenCalled();expect(await screen.findByText(/汇总 2 张待生成图片的报价/)).toBeTruthy();
 fireEvent.click(screen.getByRole("button",{name:"一键全部生成"}));
 await waitFor(()=>expect(p.produce).toHaveBeenCalledExactlyOnceWith({kind:"image",document:"images",entry:"*"}));
 expect(p.refresh).not.toHaveBeenCalled();
 const batch={task:{id:"batch",status:"running",message:"批量生成中"},job:{kind:"produce",production:{kind:"batch",document:"images",entry:"*",items:[{kind:"image",document:"images",entry:"IMG-2"},{kind:"image",document:"images",entry:"IMG-3"}]}}} as Job;
 rerender(<StudioProvider><DramaGuide {...p} docs={docs} medias={[existing]} jobs={[batch]}/></StudioProvider>);
 expect(await screen.findByRole("button",{name:"查看制作进度"})).toBeTruthy();expect(screen.queryByRole("button",{name:"一键全部生成"})).toBeNull();expect(p.produce).toHaveBeenCalledOnce();
 rerender(<StudioProvider><DramaGuide {...p} docs={docs} medias={[existing]} jobs={[{...batch,task:{...batch.task,status:"interrupted"}}]}/></StudioProvider>);
 expect(await screen.findByRole("button",{name:"恢复已有结果"})).toBeTruthy();expect(screen.queryByRole("button",{name:"一键全部生成"})).toBeNull();
});
it("shows unresolved candidate issues before confirmation and permits manual editing without regeneration",async()=>{
 const p=props();const fetch=vi.fn();vi.stubGlobal("fetch",fetch);
 const job={task:{id:"bad",status:"completed",message:"",stage:"drama_script",mode:"mock"},job:{id:"bad",kind:"script",base:"",references:[],candidate:"## BAD-ID\n故事",applied:false,candidate_check:{version:1,hash:"hash",repairs:[],issues:[{line:1,rule:"ID",message:"场景标识待修改"}]}}} as Job;
 render(<StudioProvider><DramaGuide {...p} jobs={[job]}/></StudioProvider>);
 expect(await screen.findByText("格式检查发现问题，请先修改草稿")).toBeTruthy();
 expect((screen.getByRole("button",{name:"确认使用这版剧本"}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.click(screen.getByRole("button",{name:"先修改这份草稿"}));expect(await screen.findByLabelText("修改正文")).toBeTruthy();expect(fetch).not.toHaveBeenCalled();
});

it("quotes only missing storyboard images and preserves review and recovery controls",async()=>{
 const p=props();const docs=[...['script','assets','images','storyboard'].map(kind=>({...script,kind,text:kind,hash:kind,state:"accepted",entries:kind==="storyboard"?[1,2,3].map(n=>({id:`SHOT-EP001-00${n}`,title:`SHOT-EP001-00${n} · 镜头${n}`,fields:{},body:"",start:0,end:0})):[]}))];
 const existing={id:"existing",entry:"SHOT-EP001-001",kind:"image",current:true,approved:false,file:"existing.png",path:"existing.png"} as Media;
 // A confirmed character image lets the guide advance past asset preparation.
 const asset={...existing,id:"asset",entry:"IMG-1",approved:true} as Media;
 const {rerender}=render(<StudioProvider><DramaGuide {...p} docs={docs} medias={[asset,existing]}/></StudioProvider>);
 expect(await screen.findByText(/汇总 2 张待生成图片的报价/)).toBeTruthy();expect(p.produce).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("button",{name:"一键全部生成"}));await waitFor(()=>expect(p.produce).toHaveBeenCalledExactlyOnceWith({kind:"image",document:"storyboard",entry:"*"}));
 const batch={task:{id:"board-batch",status:"running",message:"批量生成中"},job:{kind:"produce",production:{kind:"batch",document:"storyboard",entry:"*",items:[{kind:"image",document:"storyboard",entry:"SHOT-EP001-002"}]}}} as Job;
 rerender(<StudioProvider><DramaGuide {...p} docs={docs} medias={[asset,existing]} jobs={[batch]}/></StudioProvider>);
 expect(await screen.findByRole("button",{name:"查看制作进度"})).toBeTruthy();expect(screen.queryByRole("button",{name:"一键全部生成"})).toBeNull();
 rerender(<StudioProvider><DramaGuide {...p} docs={docs} medias={[asset,existing]} jobs={[{...batch,task:{...batch.task,status:"interrupted"}}]}/></StudioProvider>);
 expect(await screen.findByRole("button",{name:"恢复已有结果"})).toBeTruthy();expect(screen.queryByRole("button",{name:"一键全部生成"})).toBeNull();
 const all=[asset,...[1,2,3].map(n=>({...existing,id:`board-${n}`,entry:`SHOT-EP001-00${n}`}))];
 rerender(<StudioProvider><DramaGuide {...p} docs={docs} medias={all}/></StudioProvider>);
 expect(await screen.findByRole("button",{name:"查看各项素材与操作"})).toBeTruthy();expect(screen.getAllByRole("button",{name:"确认使用此图"})).toHaveLength(3);expect(p.produce).toHaveBeenCalledOnce();
 rerender(<StudioProvider><DramaGuide {...p} docs={docs.map(d=>d.kind==="storyboard"?{...d,state:"draft"}:d)} medias={[asset,existing]}/></StudioProvider>);
 expect((await screen.findByRole("button",{name:"一键全部生成"}) as HTMLButtonElement).disabled).toBe(true);
});

it("opens confirmed shot-image selection before quoting a planned video frame",async()=>{
 const p=props();const docs:Doc[]=['script','assets','images','storyboard','motion'].map(kind=>({...script,kind,text:kind,hash:kind,state:"accepted",entries:kind==="storyboard"||kind==="motion"?[{id:kind==="motion"?"MOTION-EP001-001":"SHOT-EP001-001",title:"第一镜",fields:{"输入参考图":"PLAN-START（用途：起始帧）","时长":"4s"},body:"",start:0,end:0}]:[]}));
 const board=docs.find(d=>d.kind==="storyboard")!;
 board.entries[0].fields={...board.entries[0].fields,"来源":"EP001-SC001","起点":"小白靠近阿灰。","唯一动作":"阿灰抬爪，小白躲开。","声音":"阿灰：别怕。"};
 docs[0].entries=[{id:"EP001-SC001",title:"巷口",fields:{},body:"## EP001-SC001 · 巷口\n\n小白缩回爪子，阿灰耐心等待。",start:0,end:0},{id:"EP001-SC002",title:"次日",fields:{},body:"不属于本镜的剧情",start:0,end:0}];
 const frame={id:"frame",entry:"SHOT-EP001-001",kind:"image",current:true,approved:true,file:"frame.png",hash:"hash"} as Media;
 render(<StudioProvider><DramaGuide {...p} docs={docs} medias={[frame]}/></StudioProvider>);
 fireEvent.click(await screen.findByRole("button",{name:"查看生成报价"}));expect(await screen.findByText("选择视频开场画面")).toBeTruthy();expect(p.produce).not.toHaveBeenCalled();
 expect(screen.getByLabelText("本镜剧本")).toBeTruthy();expect(screen.getByText("阿灰抬爪，小白躲开。")).toBeTruthy();expect(screen.getByText("阿灰：别怕。")).toBeTruthy();expect(screen.getByText("小白缩回爪子，阿灰耐心等待。")).toBeTruthy();expect(screen.queryByText("不属于本镜的剧情")).toBeNull();
 fireEvent.click(screen.getByRole("button",{name:"查看视频报价"}));
 await waitFor(()=>expect(p.produce).toHaveBeenCalledExactlyOnceWith({kind:"video",document:"motion",entry:"MOTION-EP001-001",frame_media_id:frame.id,frame_media_hash:frame.hash}));
});

it("quotes missing videos together with confirmed shot frames and preserves unknown-result recovery",async()=>{
 const p=props();const docs=['script','assets','images','storyboard','motion'].map(kind=>({...script,kind,text:kind,hash:kind,state:"accepted",entries:kind==="storyboard"||kind==="motion"?[1,2].map(n=>({id:`${kind==="motion"?"MOTION":"SHOT"}-EP001-00${n}`,title:`镜头${n}`,fields:{"输入参考图":"PLAN-START（顺序：1）（用途：起始帧）","时长":"4s"},body:"",start:0,end:1})):[]}));
 const frames=[1,2].map(n=>({id:`frame-${n}`,hash:`hash-${n}`,entry:`SHOT-EP001-00${n}`,kind:"image",current:true,approved:true,file:`frame-${n}.png`} as Media));
 const existing={id:"video",entry:"MOTION-EP001-001",kind:"video",approved:false,current:true,file:"existing.mp4"} as Media;
 const {rerender}=render(<StudioProvider><DramaGuide {...p} docs={docs} medias={[...frames,existing]}/></StudioProvider>);
 fireEvent.click(await screen.findByRole("button",{name:"一键全部生成"}));expect(await screen.findByText("本次补齐 1 镜视频，共 4 秒")).toBeTruthy();expect(p.produce).not.toHaveBeenCalled();
 const image=screen.getByAltText("首帧 1");Object.defineProperty(image,"naturalWidth",{value:900});Object.defineProperty(image,"naturalHeight",{value:1600});fireEvent.load(image);
 fireEvent.click(screen.getByRole("button",{name:"统一查看 1 镜视频报价"}));await waitFor(()=>expect(p.produce).toHaveBeenCalledExactlyOnceWith({kind:"video",document:"motion",entry:"*",frame_selections:[{entry:"MOTION-EP001-002",media_id:"frame-2",hash:"hash-2"}]}));
 const batch={task:{id:"batch",status:"running",message:"执行中"},job:{kind:"produce",production:{kind:"batch",document:"motion",entry:"*",items:[{kind:"video",document:"motion",entry:"MOTION-EP001-002"}]}}} as Job;
 rerender(<StudioProvider><DramaGuide {...p} docs={docs} medias={[...frames,existing]} jobs={[batch]}/></StudioProvider>);expect(await screen.findByRole("button",{name:"查看制作进度"})).toBeTruthy();expect(screen.queryByRole("button",{name:"一键全部生成"})).toBeNull();
 rerender(<StudioProvider><DramaGuide {...p} docs={docs} medias={[...frames,existing]} jobs={[{...batch,task:{...batch.task,status:"interrupted"}}]}/></StudioProvider>);expect(await screen.findByRole("button",{name:"恢复已有结果"})).toBeTruthy();expect(p.produce).toHaveBeenCalledOnce();
});

it("separates video editing from a recreation with reviewed shot text",async()=>{
 const p=props();const docs:Doc[]=['script','assets','images','storyboard','motion'].map(kind=>({...script,kind,text:kind,hash:kind,state:"accepted",entries:kind==="motion"||kind==="storyboard"?[{id:kind==="motion"?"MOTION-EP001-001":"SHOT-EP001-001",title:"第一镜",fields:{"输入参考图":"PLAN-START（用途：起始帧）","时长":"4s"},body:"## 第一镜\n### 可复制提示词\n> 阿灰看向小白。",start:0,end:0}]:[]}));
 const frame={id:"frame",entry:"SHOT-EP001-001",kind:"image",current:true,approved:true,file:"frame.png",hash:"frame-hash"} as Media;
 const video={id:"video",entry:"MOTION-EP001-001",kind:"video",current:true,approved:false,file:"video.mp4",hash:"video-hash"} as Media;
 render(<StudioProvider><DramaGuide {...p} docs={docs} medias={[video,frame]}/></StudioProvider>);
 fireEvent.click(await screen.findByRole("button",{name:"修改或重新生成…"}));
 fireEvent.click(screen.getByRole("radio",{name:"基于当前视频修改"}));
 fireEvent.change(screen.getByLabelText("视频修改建议"),{target:{value:"仅修改背景光线"}});
 fireEvent.click(screen.getByRole("button",{name:"查看视频修改报价"}));
 await waitFor(()=>expect(p.produce).toHaveBeenCalledExactlyOnceWith({kind:"video",document:"motion",entry:video.entry,operation:"regenerate",mode:"edit_video",changes:"仅修改背景光线",source:video}));
 fireEvent.click(screen.getByRole("button",{name:"修改或重新生成…"}));
 fireEvent.click(screen.getByRole("radio",{name:"修改本镜剧情/生成说明后重新生成"}));
 fireEvent.change(screen.getByLabelText("本镜生成说明"),{target:{value:"小白先抬爪，阿灰等待。"}});
 fireEvent.click(screen.getByRole("button",{name:"查看重新生成报价"}));
 expect(await screen.findByText("选择视频开场画面")).toBeTruthy();expect(p.produce).toHaveBeenCalledTimes(1);
 fireEvent.click(screen.getByRole("button",{name:"查看视频报价"}));
 await waitFor(()=>expect(p.produce).toHaveBeenLastCalledWith({kind:"video",document:"motion",entry:video.entry,operation:"regenerate",changes:"按本次修改的本镜生成说明重新生成",prompt_override:"小白先抬爪，阿灰等待。",frame_media_id:frame.id,frame_media_hash:frame.hash}));
});

function assetFixture(){
 const docs=["script","assets","images"].map(kind=>({...script,kind,text:kind,hash:kind,state:"accepted",entries:kind==="images"?[{id:"IMG-1",title:"IMG-1 · 阿灰",fields:{},body:"",start:0,end:0}]:[]}));
 const image={id:"image",entry:"IMG-1",kind:"image",current:true,approved:false,file:"image.png",path:"image.png",hash:"image-hash"} as Media;
 return {docs,image};
}
async function openAssetMedia(){fireEvent.click(screen.getByRole("button",{name:"准备角色与场景"}));fireEvent.click(await screen.findByRole("button",{name:"准备角色与场景图片"}));}
it("replaces the confirmation button with an accessible visual seal and keeps one next action",async()=>{
 const p=props(),{docs,image}=assetFixture();const fetch=vi.fn(async(_url:string,_init?:RequestInit)=>({ok:true,json:async()=>({})}));vi.stubGlobal("fetch",fetch);
 const {rerender}=render(<StudioProvider><DramaGuide {...p} docs={docs} medias={[image]}/></StudioProvider>);
 fireEvent.click(await screen.findByRole("button",{name:"确认使用此图"}));await waitFor(()=>expect(fetch).toHaveBeenCalledOnce());expect(String(fetch.mock.calls[0]?.[0])).toContain(`/media/${image.id}/approve`);
 rerender(<StudioProvider><DramaGuide {...p} docs={docs} medias={[{...image,approved:true}]}/></StudioProvider>);
 expect(await screen.findByRole("img",{name:"已确认可用"})).toBeTruthy();expect(screen.queryByRole("button",{name:"确认使用此图"})).toBeNull();expect(screen.getAllByRole("button",{name:"下一步：设计分镜"})).toHaveLength(1);expect(screen.queryByText("补充素材")).toBeNull();expect(screen.queryByRole("button",{name:"使用已有确认素材继续"})).toBeNull();expect(p.produce).not.toHaveBeenCalled();
});
it("keeps stale images unconfirmed and blocks new quotes during unknown-result recovery",async()=>{
 const p=props(),{docs,image}=assetFixture();const job={task:{id:"unknown",status:"interrupted",message:"提交结果未知"},job:{kind:"produce",production:{kind:"image",document:"images",entry:image.entry}}} as Job;
 render(<StudioProvider><DramaGuide {...p} docs={docs} medias={[{...image,current:false}]} jobs={[job]}/></StudioProvider>);
 expect(await screen.findByText("来源变化，需复核")).toBeTruthy();expect(screen.queryByRole("img",{name:"已确认可用"})).toBeNull();expect((screen.getByRole("button",{name:"确认使用此图"}) as HTMLButtonElement).disabled).toBe(true);expect((screen.getByRole("button",{name:"重新生成…"}) as HTMLButtonElement).disabled).toBe(true);expect(screen.getByRole("button",{name:"恢复已有结果"})).toBeTruthy();expect(p.produce).not.toHaveBeenCalled();
});
it("groups secondary actions and requires advice before quoting only one image",async()=>{
 const p=props(),{docs,image}=assetFixture();render(<StudioProvider><DramaGuide {...p} work={{...w,series_id:"series"}} docs={docs} medias={[image]}/></StudioProvider>);
 expect(screen.queryByRole("menuitem",{name:"修改生成参考…"})).toBeNull();fireEvent.click(await screen.findByRole("button",{name:"IMG-1 更多操作"}));
 expect(await screen.findByRole("menuitem",{name:"替换此图…"})).toBeTruthy();expect(screen.getByRole("menuitem",{name:"设为系列共用"}).getAttribute("aria-disabled")).toBe("true");
 fireEvent.click(screen.getByRole("menuitem",{name:"修改生成参考…"}));await waitFor(()=>expect(screen.getByRole("dialog").textContent).toContain("修改后续生成使用的参考图片"));expect(p.produce).not.toHaveBeenCalled();fireEvent.click(within(screen.getByRole("dialog")).getByRole("button",{name:"关闭"}));
 fireEvent.click(screen.getByRole("button",{name:"重新生成…"}));const dialog=await screen.findByRole("dialog");expect((within(dialog).getByRole("button",{name:"查看这一项报价"}) as HTMLButtonElement).disabled).toBe(true);
 fireEvent.change(screen.getByLabelText("重新生成修改建议"),{target:{value:"保持毛色，调整光线"}});expect(p.produce).not.toHaveBeenCalled();fireEvent.click(within(dialog).getByRole("button",{name:"查看这一项报价"}));await waitFor(()=>expect(p.produce).toHaveBeenCalledExactlyOnceWith({kind:"image",document:"images",entry:image.entry,operation:"regenerate",changes:"保持毛色，调整光线"}));
});
it("imports a replacement for the exact entry without generating",async()=>{
 const p=props(),{docs,image}=assetFixture();const calls:{url:string;body:Record<string,unknown>}[]=[];vi.stubGlobal("fetch",vi.fn(async(url:string,init?:RequestInit)=>{calls.push({url,body:JSON.parse(String(init?.body||"{}"))});return {ok:true,json:async()=>({})}}));
 render(<StudioProvider><DramaGuide {...p} docs={docs} medias={[image]}/></StudioProvider>);fireEvent.click(await screen.findByRole("button",{name:"IMG-1 更多操作"}));fireEvent.click(await screen.findByRole("menuitem",{name:"替换此图…"}));const dialog=await screen.findByRole("dialog");
 fireEvent.change(dialog.querySelector('input[type="file"]')!,{target:{files:[new File(["image"],"替换.png",{type:"image/png"})]}});await waitFor(()=>expect(calls).toHaveLength(1));expect(calls[0].url).toContain("/media/import");expect(calls[0].body.entry).toBe(image.entry);expect(calls[0].body.name).toBe("替换.png");expect(p.produce).not.toHaveBeenCalled();
});
it("keeps imported-only materials able to continue through the single header action",async()=>{
 const p=props(),{docs,image}=assetFixture();render(<StudioProvider><DramaGuide {...p} docs={docs.filter(d=>d.kind!=="images")} medias={[{...image,entry:"阿灰",approved:true}]}/></StudioProvider>);await openAssetMedia();
 expect(await screen.findByText("补充素材")).toBeTruthy();const next=screen.getByRole("button",{name:"下一步：设计分镜"}) as HTMLButtonElement;expect(next.disabled).toBe(false);fireEvent.click(next);expect(await screen.findByRole("heading",{name:"设计分镜"})).toBeTruthy();expect(p.produce).not.toHaveBeenCalled();
});
it("keeps the explicit existing-material shortcut when planned images are incomplete",async()=>{
 const p=props(),{docs,image}=assetFixture();render(<StudioProvider><DramaGuide {...p} docs={docs} medias={[{...image,entry:"导入角色",approved:true}]}/></StudioProvider>);
 expect(await screen.findByRole("button",{name:"使用已有确认素材继续"})).toBeTruthy();fireEvent.click(screen.getByRole("button",{name:"使用已有确认素材继续"}));expect(await screen.findByRole("heading",{name:"设计分镜"})).toBeTruthy();expect(p.produce).not.toHaveBeenCalled();
});

it("keeps binding a confirmed storyboard frame directly available",async()=>{
 const p=props(),{docs,image}=assetFixture();const shot={id:"SHOT-EP001-001",title:"第一镜",fields:{},body:"",start:0,end:0};const allDocs=[...docs,{...script,kind:"storyboard",text:"分镜",hash:"board",state:"accepted",entries:[shot]}];const frame={...image,id:"frame",entry:shot.id,approved:true};const calls:{url:string;body:Record<string,unknown>}[]=[];
 vi.stubGlobal("fetch",vi.fn(async(url:string,init?:RequestInit)=>{calls.push({url,body:JSON.parse(String(init?.body||"{}"))});return{ok:true,json:async()=>({})}}));render(<StudioProvider><DramaGuide {...p} docs={allDocs} medias={[{...image,approved:true},frame]}/></StudioProvider>);
 fireEvent.click(screen.getByRole("button",{name:"做分镜画面"}));fireEvent.click(await screen.findByRole("button",{name:"确认分镜画面"}));fireEvent.click(await screen.findByRole("button",{name:"用作本镜视频首帧"}));await waitFor(()=>expect(calls).toHaveLength(1));expect(calls[0].url).toContain("/bind-frame");expect(calls[0].body).toEqual({base:"board",entry:shot.id,media_id:frame.id});expect(p.produce).not.toHaveBeenCalled();
});
it("shows video-only version actions without image replacement or reference controls",async()=>{
 const p=props(),{docs,image}=assetFixture();const allDocs=[...docs,...["storyboard","motion"].map(kind=>({...script,kind,text:kind,hash:kind,state:"accepted",entries:[{id:`${kind==="motion"?"MOTION":"SHOT"}-EP001-001`,title:"第一镜",fields:{},body:"",start:0,end:0}]}))];
 const video={...image,id:"video",entry:"MOTION-EP001-001",kind:"video",approved:false} as Media;render(<StudioProvider><DramaGuide {...p} docs={allDocs} medias={[{...image,approved:true},{...image,id:"frame",entry:"SHOT-EP001-001",approved:true},video]}/></StudioProvider>);
 expect(await screen.findByRole("button",{name:"确认使用此视频"})).toBeTruthy();fireEvent.click(screen.getByRole("button",{name:"MOTION-EP001-001 更多操作"}));expect(await screen.findByRole("menuitem",{name:"历史与对比（1）"})).toBeTruthy();expect(screen.getByRole("menuitem",{name:"换模型 / 清晰度试一版"})).toBeTruthy();expect(screen.queryByRole("menuitem",{name:"替换此图…"})).toBeNull();expect(screen.queryByRole("menuitem",{name:"修改生成参考…"})).toBeNull();expect(p.produce).not.toHaveBeenCalled();
});

it("opens the shot video in a large preview and confirms through the existing version selection API",async()=>{
 vi.spyOn(HTMLMediaElement.prototype,"pause").mockImplementation(()=>{});
 const p=props(),{docs,image}=assetFixture();const allDocs=[...docs,...["storyboard","motion"].map(kind=>({...script,kind,text:kind,hash:kind,state:"accepted",entries:[{id:`${kind==="motion"?"MOTION":"SHOT"}-EP001-001`,title:"第一镜",fields:{},body:"",start:0,end:0}]}))];
 const video={...image,id:"video",hash:"video-hash",entry:"MOTION-EP001-001",kind:"video",file:"video.mp4",approved:false} as Media;
 const calls:{url:string;body:unknown}[]=[];vi.stubGlobal("fetch",vi.fn(async(url:string,init?:RequestInit)=>{calls.push({url,body:JSON.parse(String(init?.body||"{}"))});return {ok:true,json:async()=>({})}}));
 const {container}=render(<StudioProvider><DramaGuide {...p} docs={allDocs} medias={[{...image,approved:true},{...image,id:"frame",entry:"SHOT-EP001-001",approved:true},video]}/></StudioProvider>);
 fireEvent.click(await screen.findByRole("button",{name:"放大查看视频 MOTION-EP001-001"}));
 const dialog=await screen.findByRole("dialog");expect(within(dialog).getByLabelText("大预览视频")).toBeTruthy();
 expect(container.querySelector(".guide-video-open video")?.hasAttribute("controls")).toBe(false);expect(calls).toHaveLength(0);
 fireEvent.click(within(dialog).getByRole("button",{name:"确认使用此视频"}));
 await waitFor(()=>expect(calls).toEqual([{url:`/api/drama/works/${w.id}/media/video/select`,body:{hash:"video-hash",base:""}}]));
 expect(p.refresh).toHaveBeenCalledOnce();expect(p.produce).not.toHaveBeenCalled();
 fireEvent.click(within(dialog).getByRole("button",{name:"关闭"}));await waitFor(()=>expect(screen.queryByRole("dialog")).toBeNull());expect(HTMLMediaElement.prototype.pause).toHaveBeenCalled();
});
