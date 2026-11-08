// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import DramaVideoRegenerate from "./DramaVideoRegenerate";
import type {Entry,Media} from "./drama";
beforeEach(()=>{Object.defineProperty(window,"matchMedia",{value:()=>({matches:false,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){}}),writable:true});const computed=window.getComputedStyle;vi.spyOn(window,"getComputedStyle").mockImplementation(el=>computed(el));globalThis.ResizeObserver=class{observe(){}unobserve(){}disconnect(){}}});
afterEach(()=>{cleanup();vi.restoreAllMocks()});
const entry={id:"MOTION-EP001-001",title:"第一镜",start:0,end:0,fields:{"时长":"4s"},body:"## 第一镜\n### 可复制提示词\n> 阿灰保持原位，小白抬爪。\n### 其他说明\n> 不应混入提示词"} as Entry;
const source={id:"clip",file:"clip.mp4",kind:"video",current:true,hash:"hash"} as Media;
function props(){return {entry,source,busy:false,cancel:vi.fn(),edit:vi.fn(async()=>{}),recreate:vi.fn(async()=>{}),run:async(fn:()=>Promise<void>)=>fn()}}
it("waits for an explicit choice and advice, then quotes a video edit only",async()=>{
 const p=props();render(<DramaVideoRegenerate {...p}/>);
 expect((screen.getByRole("button",{name:"先选择处理方式"}) as HTMLButtonElement).disabled).toBe(true);
 expect(screen.getAllByRole("radio").every(r=>!(r as HTMLInputElement).checked)).toBe(true);
 fireEvent.click(screen.getByRole("radio",{name:"基于当前视频修改"}));
 const button=screen.getByRole("button",{name:"查看视频修改报价"}) as HTMLButtonElement;expect(button.disabled).toBe(true);
 fireEvent.change(screen.getByLabelText("视频修改建议"),{target:{value:"仅把背景变暗"}});fireEvent.click(button);
 await waitFor(()=>expect(p.edit).toHaveBeenCalledExactlyOnceWith("仅把背景变暗"));expect(p.recreate).not.toHaveBeenCalled();
});
it("lets the user edit the original shot prompt before quoting a recreation",async()=>{
 const p=props();render(<DramaVideoRegenerate {...p}/>);
 fireEvent.click(screen.getByRole("radio",{name:"修改本镜剧情/生成说明后重新生成"}));
 const input=screen.getByLabelText("本镜生成说明") as HTMLTextAreaElement;expect(input.value).toBe("阿灰保持原位，小白抬爪。");
 const button=screen.getByRole("button",{name:"查看重新生成报价"}) as HTMLButtonElement;expect(button.disabled).toBe(true);
 fireEvent.change(input,{target:{value:"小白先抬爪，阿灰后退。"}});expect(p.recreate).not.toHaveBeenCalled();fireEvent.click(button);
 await waitFor(()=>expect(p.recreate).toHaveBeenCalledExactlyOnceWith("小白先抬爪，阿灰后退。"));expect(p.edit).not.toHaveBeenCalled();
});
it("does not allow an obsolete video as an editing input",()=>{
 render(<DramaVideoRegenerate {...props()} source={{...source,current:false}}/>);
 expect((screen.getByRole("radio",{name:"基于当前视频修改"}) as HTMLInputElement).disabled).toBe(true);
});

it("shows unsupported edit errors inside the modal and preserves the advice",async()=>{
 const p=props();p.edit.mockRejectedValue(new Error("当前模型不支持视频编辑"));
 render(<DramaVideoRegenerate {...p} run={async fn=>{try{await fn()}catch{}}}/>);
 fireEvent.click(screen.getByRole("radio",{name:"基于当前视频修改"}));
 fireEvent.change(screen.getByLabelText("视频修改建议"),{target:{value:"保持动作，调整背景"}});
 fireEvent.click(screen.getByRole("button",{name:"查看视频修改报价"}));
 expect(await screen.findByText("当前模型不支持视频编辑")).toBeTruthy();
 expect((screen.getByLabelText("视频修改建议") as HTMLTextAreaElement).value).toBe("保持动作，调整背景");
 expect(p.recreate).not.toHaveBeenCalled();
});
