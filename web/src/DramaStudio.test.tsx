// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {cleanup,render,screen,fireEvent,waitFor} from "@testing-library/react";
import DramaStudio from "./DramaStudio";
import {StudioProvider} from "./ui";
beforeEach(()=>{Object.defineProperty(window,"matchMedia",{value:()=>({matches:false,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){}}),writable:true});const computed=window.getComputedStyle;vi.spyOn(window,"getComputedStyle").mockImplementation(el=>computed(el));globalThis.ResizeObserver=class{observe(){}unobserve(){}disconnect(){}};});
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks()});
it("creates a new document workflow without calling legacy project or quote routes",async()=>{
 const calls:{url:string;body?:Record<string,unknown>}[]=[];vi.stubGlobal("fetch",vi.fn(async(url:string,init?:RequestInit)=>{const body=init?.body?JSON.parse(String(init.body)):undefined;calls.push({url,body});let data:unknown=[];if(url==="/api/settings")data={settings:{version:1,registry_version:1,mode:"mock",vendors:[],text:{model:""},image:{model:""},video:{model:""},voice:{model:""}},credentials:{},vendor_capabilities:{}};if(url==="/api/drama/works"&&body)data={id:"a".repeat(24),...body};if(url.includes("/documents"))data=[{kind:"script",path:"剧集/EP001/剧本.md",text:"",hash:"",version:0,state:"draft",entries:[],issues:[],inputs:{}}];return {ok:true,json:async()=>data}}));
 render(<StudioProvider><DramaStudio/></StudioProvider>);fireEvent.click(await screen.findByRole("button",{name:"新建作品"}));fireEvent.change(screen.getByLabelText("作品名称"),{target:{value:"第一堂课"}});fireEvent.change(screen.getByLabelText("创作意图与必须保留的事实"),{target:{value:"两只猫互相让出暖处"}});fireEvent.click(screen.getByRole("button",{name:"创建作品"}));await waitFor(()=>expect(calls.filter(c=>c.url==="/api/drama/works"&&c.body)).toHaveLength(1)); expect(calls.find(c=>c.url==="/api/drama/works"&&c.body)?.body?.prompt_language).toBe("zh-CN"); await screen.findByText("第一堂课"); await waitFor(()=>expect(screen.getByRole("button",{name:"生成本集剧本"})).toBeTruthy());fireEvent.click(screen.getByRole("button",{name:"生成本集剧本"}));await waitFor(()=>expect(calls.some(c=>c.url.endsWith("/create"))).toBe(true));expect(calls.some(c=>c.url.startsWith("/api/projects")||c.url.includes("/quotes"))).toBe(false);expect(calls.find(c=>c.url.endsWith("/create"))?.body?.kind).toBe("script");
});
it("imports a ZIP as a new work only after confirmation and exposes export in the guided header",async()=>{
 const calls:{url:string;init?:RequestInit}[]=[];
 const work={id:"b".repeat(24),title:"朋友的作品",idea:"分享剧本",duration:5,aspect:"9:16",form:"动画短片",profile:{},profile_hash:""};
 vi.stubGlobal("fetch",vi.fn(async(url:string,init?:RequestInit)=>{calls.push({url,init});let data:unknown=[];if(url==="/api/settings")data={settings:{mode:"mock"},credentials:{},vendor_capabilities:{}};if(url.includes("/works/import?"))data=work;return {ok:true,json:async()=>data}}));
 render(<StudioProvider><DramaStudio/></StudioProvider>);
 fireEvent.click(await screen.findByRole("button",{name:"导入作品包"}));
 const file=new File(["zip payload"],"分享作品.zip",{type:"application/zip"});
 fireEvent.change(document.querySelector('input[type="file"][accept=".zip"]')!,{target:{files:[file]}});
 await screen.findByText("分享作品.zip");
 expect(calls.some(c=>c.url.includes("/works/import?"))).toBe(false);
 fireEvent.click(screen.getByRole("button",{name:"导入为新作品"}));
 await screen.findByText("朋友的作品");
 const uploaded=calls.filter(c=>c.url.includes("/works/import?"));expect(uploaded).toHaveLength(1);expect(uploaded[0].init?.body).toBe(file);expect(uploaded[0].init?.headers).toMatchObject({"X-Lypit-Client":"studio"});
 expect(screen.getByRole("link",{name:"导出作品包"}).getAttribute("href")).toBe(`/api/drama/works/${work.id}/package`);
 expect(calls.some(c=>c.url.endsWith("/create")||c.url.includes("/quotes"))).toBe(false);
});
