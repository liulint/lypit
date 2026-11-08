// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {StudioProvider} from "./ui";
import DramaCandidateFormat from "./DramaCandidateFormat";
import type {Job} from "./drama";
beforeEach(()=>{Object.defineProperty(window,"matchMedia",{value:()=>({matches:false,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){}}),writable:true});globalThis.ResizeObserver=class{observe(){}unobserve(){}disconnect(){}}});
afterEach(()=>{cleanup();vi.restoreAllMocks()});
it("shows local repairs, original text and unresolved problems before adoption",async()=>{
 const job={job:{candidate_original:"## 原始说明标题",candidate_check:{version:1,hash:"hash",repairs:[{line:167,message:"说明标题改为三级标题"}],issues:[{line:27,rule:"FIELD",message:"缺少字段：时长"}]}}} as Job;
 const check=vi.fn();render(<StudioProvider><DramaCandidateFormat job={job} busy={false} check={check}/></StudioProvider>);
 expect(screen.getByText("格式检查发现问题，请先修改草稿")).toBeTruthy();expect(screen.getByText(/第167行/)).toBeTruthy();expect(screen.getByText(/缺少字段：时长/)).toBeTruthy();
 fireEvent.click(screen.getByText("查看修复前的原始生成稿"));expect(await screen.findByText("## 原始说明标题")).toBeTruthy();expect(check).not.toHaveBeenCalled();
});
it("offers an explicit local check for old candidates without starting a generation",()=>{
 const check=vi.fn();render(<StudioProvider><DramaCandidateFormat job={{job:{candidate:"旧稿"}} as Job} busy={false} check={check}/></StudioProvider>);
 expect(check).not.toHaveBeenCalled();fireEvent.click(screen.getByRole("button",{name:"本地检查并修复格式"}));expect(check).toHaveBeenCalledOnce();
});
it("allows local reference rechecking after an upstream correction",()=>{
 const check=vi.fn();const job={task:{id:"reference-check",status:"completed",message:"",stage:"drama_storyboard",mode:"mock"},job:{id:"reference-check",kind:"storyboard",base:"",candidate:"分镜",applied:false,references:[],candidate_check:{version:1,hash:"hash",links_checked:true,repairs:[],issues:[{line:27,rule:"LINK",message:"来源场景不存在"}]}}} as Job;
 render(<StudioProvider><DramaCandidateFormat job={job} busy={false} check={check}/></StudioProvider>);
 expect(check).not.toHaveBeenCalled();fireEvent.click(screen.getByRole("button",{name:"重新检查引用"}));expect(check).toHaveBeenCalledOnce();
});
