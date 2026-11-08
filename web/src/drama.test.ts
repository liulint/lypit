import {describe,it,expect} from "vitest";
import {patchEntry,type Entry} from "./drama";
describe("document section editing",()=>{
 it("retains author prose and unknown sections outside the edited entry",()=>{
 const text="# 分镜\n作者补充。\n## SHOT-001 第一镜\n- 时长：5s\n- 起点：门外\n\n### 冻结关键帧提示词\n> Keep this exact image prompt.\n\n## SHOT-002 第二镜\n- 时长：8s\n未知扩展。\n";
 const lines=text.split("\n");const end=lines.findIndex(v=>v.startsWith("## SHOT-002"));const e:Entry={id:"SHOT-001",title:"第一镜",start:2,end,body:"",fields:{}};
 const out=patchEntry(text,e,{时长:"6s",终点:"收手"});expect(out.startsWith("# 分镜\n作者补充。\n")).toBe(true);expect(out.slice(out.indexOf("## SHOT-002"))).toBe(text.slice(text.indexOf("## SHOT-002")));expect(out).toContain("> Keep this exact image prompt.");expect(out).toContain("- 时长：6s");expect(out).toContain("- 终点：收手");
 });
});
