// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {StudioProvider} from "./ui";
import DramaStudio from "./DramaStudio";
import type {Production} from "./drama";

vi.mock("./DramaGuide",()=>({default:({produce}:{produce:(p:Production)=>Promise<void>})=><><button onClick={()=>void produce({kind:"video",document:"motion",entry:"MOTION-EP001-001"})}>测试视频报价</button><button onClick={()=>void produce({kind:"video",document:"motion",entry:"*"})}>测试整批报价</button><button onClick={()=>void produce({kind:"image",document:"storyboard",entry:"*"})}>测试图片报价</button></>}));
beforeEach(()=>{
 Object.defineProperty(window,"matchMedia",{value:()=>({matches:false,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){}}),writable:true});
 const computed=window.getComputedStyle;vi.spyOn(window,"getComputedStyle").mockImplementation(el=>computed(el));
 globalThis.ResizeObserver=class{observe(){}unobserve(){}disconnect(){}};
});
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks()});

it.each([true,false])("shows the video estimate and requires cost consent (estimated: %s)",async estimated=>{
 const calls:{url:string;body?:Record<string,unknown>}[]=[];
 vi.stubGlobal("fetch",vi.fn(async(url:string,init?:RequestInit)=>{
  const body=init?.body?JSON.parse(String(init.body)):undefined;calls.push({url,body});
  let data:unknown=[];
  if(url==="/api/settings")data={settings:{mode:"remote"},credentials:{},vendor_capabilities:{}};
  if(url==="/api/drama/works")data=[{id:"work",title:"第一堂课",aspect:"9:16",duration:33}];
  if(url.endsWith("/quotes"))data={id:"quote",model:"doubao-seedance-2-0-mini-260615",vendor:"火山方舟",unknown:true,reason:estimated?"已按本次视频范围估算费用":"费用未知",spec:{kind:"video",entry:"MOTION-EP001-001",duration:4},estimate:estimated?{amount:"0.92",currency:"CNY",basis:"480p · 合计4秒",source:"火山方舟官方参考价",url:"https://www.volcengine.com/docs/82379/1544106",checked:"2026-11-06"}:undefined};
  return {ok:true,json:async()=>data};
 }));
 render(<StudioProvider><DramaStudio/></StudioProvider>);
 fireEvent.click(await screen.findByText("第一堂课"));
 fireEvent.click(await screen.findByRole("button",{name:"测试视频报价"}));
 const confirm=await screen.findByRole("button",{name:"确认执行一次"}) as HTMLButtonElement;
 expect(confirm.disabled).toBe(true);
 if(estimated){
  expect(await screen.findByText("参考估算：约 ¥ 0.92")).toBeTruthy();
  expect(screen.getByText("480p · 合计4秒")).toBeTruthy();
  expect(screen.getByRole("link",{name:"火山方舟官方参考价"}).getAttribute("href")).toContain("1544106");
  expect(screen.getByText(/核对日期 2026-11-06/)).toBeTruthy();
 }else expect(screen.queryByText(/参考估算：约/)).toBeNull();
 expect(calls.some(c=>c.url.endsWith("/confirm"))).toBe(false);
 fireEvent.click(screen.getByRole("checkbox",{name:estimated?/我知晓以上为参考估算/:/我接受本次费用未知/}));
 expect(confirm.disabled).toBe(false);fireEvent.click(confirm);
 await waitFor(()=>expect(calls.find(c=>c.url.endsWith("/confirm"))?.body).toEqual({allow_unknown:true}));
});

it.each([true,false])("reviews per-shot costs and total before batch submission (complete: %s)",async complete=>{
 const calls:{url:string;body?:Record<string,unknown>}[]=[];
 const estimate={amount:"0.92",currency:"CNY",basis:"480p · 按本次待生成镜头估算",source:"参考价",url:"https://www.volcengine.com/docs/82379/1544106",checked:"2026-11-06"};
 vi.stubGlobal("fetch",vi.fn(async(url:string,init?:RequestInit)=>{
  const body=init?.body?JSON.parse(String(init.body)):undefined;calls.push({url,body});
  let data:unknown=[];
  if(url==="/api/settings")data={settings:{mode:"remote"},credentials:{},vendor_capabilities:{}};
  if(url==="/api/drama/works")data=[{id:"work",title:"第一堂课",aspect:"9:16",duration:33}];
  if(url.endsWith("/quotes"))data={id:"batch",model:"seedance-mini",unknown:true,reason:"生成前请检查费用",spec:{kind:"batch",document:"motion",entry:"*",items:[{kind:"video",entry:"MOTION-EP001-001",duration:4},{kind:"video",entry:"MOTION-EP001-002",duration:5}]},estimate:complete?{...estimate,amount:"2.08"}:undefined,item_estimates:[{entry:"MOTION-EP001-001",estimate},{entry:"MOTION-EP001-002",estimate:complete?{...estimate,amount:"1.16"}:undefined,reason:complete?undefined:"输入用量尚未核实"}]};
  return {ok:true,json:async()=>data};
 }));
 render(<StudioProvider><DramaStudio/></StudioProvider>);
 fireEvent.click(await screen.findByText("第一堂课"));fireEvent.click(await screen.findByRole("button",{name:"测试整批报价"}));
 const confirm=await screen.findByRole("button",{name:"确认生成 2 镜视频"}) as HTMLButtonElement;
 expect(confirm.disabled).toBe(true);expect(screen.getByText("约 ¥ 0.92")).toBeTruthy();
 if(complete){expect(screen.getByText("约 ¥ 1.16")).toBeTruthy();expect(screen.getByText("总预估费用：约 ¥ 2.08")).toBeTruthy()}
 else{expect(screen.getByText("费用未知：输入用量尚未核实")).toBeTruthy();expect(screen.getByText("总预估费用未知，未将未知费用按零计入")).toBeTruthy();expect(screen.queryByText(/总预估费用：约/)).toBeNull()}
 expect(calls.filter(c=>c.url.endsWith("/confirm"))).toHaveLength(0);
 fireEvent.click(screen.getByRole("checkbox"));expect(confirm.disabled).toBe(false);fireEvent.click(confirm);
 await waitFor(()=>expect(calls.filter(c=>c.url.endsWith("/confirm"))).toEqual([{url:"/api/drama/quotes/batch/confirm",body:{allow_unknown:true}}]));
});

it("discloses the maximum image calls before confirming an aspect retry budget",async()=>{
 const calls:string[]=[];
 vi.stubGlobal("fetch",vi.fn(async(url:string)=>{
  calls.push(url);let data:unknown=[];
  if(url==="/api/settings")data={settings:{mode:"remote"},credentials:{},vendor_capabilities:{}};
  if(url==="/api/drama/works")data=[{id:"work",title:"第一堂课",aspect:"9:16",duration:33}];
  if(url.endsWith("/quotes"))data={id:"image-batch",unknown:true,reason:"每次均可能计费，提交结果未知不重提",spec:{kind:"batch",document:"storyboard",entry:"*",items:[{kind:"image",entry:"SHOT-EP001-001",image_aspect:"9:16",image_max_calls:2},{kind:"image",entry:"SHOT-EP001-002",image_aspect:"9:16",image_max_calls:2}]}};
  return {ok:true,json:async()=>data};
 }));
 render(<StudioProvider><DramaStudio/></StudioProvider>);
 fireEvent.click(await screen.findByText("第一堂课"));fireEvent.click(await screen.findByRole("button",{name:"测试图片报价"}));
 const confirm=await screen.findByRole("button",{name:"确认生成 2 项素材"}) as HTMLButtonElement;
 expect(screen.getByText(/新图 2 张 · 常规 2 次，最多 4 次调用/)).toBeTruthy();
 expect(screen.getByText("9:16")).toBeTruthy();expect(confirm.disabled).toBe(true);
 expect(calls.some(c=>c.endsWith("/confirm"))).toBe(false);
 fireEvent.click(screen.getByRole("checkbox"));expect(confirm.disabled).toBe(false);
});
