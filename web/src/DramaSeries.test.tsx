// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import DramaSeries,{EpisodeContinuity,type Series} from "./DramaSeries";
import {StudioProvider} from "./ui";
import type {Work} from "./drama";
beforeEach(()=>{Object.defineProperty(window,"matchMedia",{value:()=>({matches:false,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){}}),writable:true});const computed=window.getComputedStyle;vi.spyOn(window,"getComputedStyle").mockImplementation(el=>computed(el));globalThis.ResizeObserver=class{observe(){}unobserve(){}disconnect(){}}});
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks()});
const series:Series={id:"a".repeat(24),version:2,hash:"series-hash",title:"普通人的一天",idea:"人物在日常生活中成长",settings:"固定人物和场景",genre:"生活流",form:"真人剧情",aspect:"16:9",duration:40,media:[],created:""};
it("creates a generic episode with explicit shared version and no media generation",async()=>{
 const calls:{url:string;body?:Record<string,unknown>}[]=[];const open=vi.fn();
 vi.stubGlobal("fetch",vi.fn(async(url:string,init?:RequestInit)=>{const body=init?.body?JSON.parse(String(init.body)):undefined;calls.push({url,body});let data:unknown=[];if(url===`/api/drama/series/${series.id}`)data={series,versions:[series],episodes:[],continuity:[],media:[]};if(url.endsWith("/episodes")&&body)data={id:"b".repeat(24),series_id:series.id,episode_number:body.number,title:body.title};return{ok:true,json:async()=>data}}));
 render(<StudioProvider><DramaSeries initialID={series.id} onDirty={()=>{}} openWork={open}/></StudioProvider>);
 fireEvent.click(await screen.findByRole("button",{name:"新增分集"}));fireEvent.change(screen.getByLabelText("本集标题"),{target:{value:"雨中的约定"}});fireEvent.change(screen.getByLabelText("本集梗概"),{target:{value:"两位老友在雨中重逢"}});fireEvent.change(screen.getByLabelText("主题（可选）"),{target:{value:"信任"}});fireEvent.click(screen.getByRole("button",{name:"创建本集"}));
 await waitFor(()=>expect(open).toHaveBeenCalledTimes(1));const c=calls.find(c=>c.url.endsWith("/episodes")&&c.body)!;expect(c.body).toMatchObject({version:2,number:1,title:"雨中的约定",plan:{theme:"信任"},previous:[]});expect(calls.some(c=>/quotes|\/create$|confirm$/.test(c.url))).toBe(false);expect(JSON.stringify(c.body)).not.toContain("猫");
});
it("saves continuity as a draft unless the user explicitly confirms the adopted script",async()=>{
 const calls:{url:string;body?:Record<string,unknown>}[]=[];vi.stubGlobal("fetch",vi.fn(async(url:string,init?:RequestInit)=>{const body=init?.body?JSON.parse(String(init.body)):undefined;calls.push({url,body});return{ok:true,json:async()=>({work_id:"b".repeat(24),number:2,version:body?1:0,hash:body?"saved":"",text:body?.text||"",confirmed:body?.confirm||false,current:!!body?.confirm,script_hash:"script-v2",script_version:2})}}));
 const work={id:"b".repeat(24),series_id:series.id,episode_number:2} as Work;render(<StudioProvider><EpisodeContinuity work={work} scriptHash="script-v2" scriptVersion={2} scriptAccepted onDirty={()=>{}}/></StudioProvider>);
 const input=await screen.findByLabelText("本集结束状态");fireEvent.change(input,{target:{value:"两人已和解，但钥匙仍未归还"}});await waitFor(()=>expect(screen.getByRole("button",{name:"保存前情草稿"}).hasAttribute("disabled")).toBe(false));fireEvent.click(screen.getByRole("button",{name:"保存前情草稿"}));await waitFor(()=>expect(calls.some(c=>c.body?.confirm===false)).toBe(true));await screen.findByText("前情草稿已保存");fireEvent.click(screen.getByRole("checkbox"));fireEvent.click(screen.getByRole("button",{name:"保存并确认前情"}));await waitFor(()=>expect(calls.some(c=>c.body?.confirm===true&&c.body.script_hash==="script-v2")).toBe(true));
});
