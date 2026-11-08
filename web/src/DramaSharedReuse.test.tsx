// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import DramaSharedReuse from "./DramaSharedReuse";
import {StudioProvider} from "./ui";
import type {Series} from "./DramaSeries";
import type {Work} from "./drama";
beforeEach(()=>{Object.defineProperty(window,"matchMedia",{value:()=>({matches:false,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){}}),writable:true});const computed=window.getComputedStyle;vi.spyOn(window,"getComputedStyle").mockImplementation(el=>computed(el));globalThis.ResizeObserver=class{observe(){}unobserve(){}disconnect(){}}});
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks()});
it("continues the same local copy after a partial failure without model routes",async()=>{
 const w={id:"b".repeat(24),title:"第一集",episode_number:1} as Work;
 const s={id:"a".repeat(24),title:"系列",version:2,hash:"series-hash",media:[{id:"c".repeat(24),entry:"角色主图",kind:"image",file:"source.png"}]} as Series;
 const calls:{url:string;body:Record<string,unknown>}[]=[];
 vi.stubGlobal("fetch",vi.fn(async(url:string,init:RequestInit)=>{calls.push({url,body:JSON.parse(String(init.body))});return {ok:true,json:async()=>({results:[{work_id:w.id,state:calls.length===1?"failed":"completed",error:calls.length===1?"暂时不可读":"",media_ids:calls.length===1?[]:["copy"]}]})}}));
 render(<StudioProvider><DramaSharedReuse series={s} versions={[s]} episodes={[w]} refresh={async()=>{}} disabled={false}/></StudioProvider>);
 fireEvent.click(screen.getByRole("button",{name:"把共用素材复用到分集"}));fireEvent.click(screen.getByRole("checkbox",{name:/角色主图/}));fireEvent.click(screen.getByRole("checkbox",{name:/EP001/}));fireEvent.click(screen.getByRole("button",{name:"确认复用，不调用模型"}));await screen.findByText("暂时不可读");fireEvent.click(await screen.findByRole("button",{name:"继续同一次复用"}));await waitFor(()=>expect(calls).toHaveLength(2));expect(calls[0].body).toEqual(calls[1].body);expect(calls[0].url).toBe(`/api/drama/series/${s.id}/reuse-media`);expect(calls[0].body.work_ids).toEqual([w.id]);await screen.findByRole("button",{name:/完\s*成/});
});
