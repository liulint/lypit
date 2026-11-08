// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import DramaMaterialsDrawer from "./DramaMaterialsDrawer";
import {StudioProvider} from "./ui";
import type {Work} from "./drama";
beforeEach(()=>{Object.defineProperty(window,"matchMedia",{value:()=>({matches:false,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){}}),writable:true});const computed=window.getComputedStyle;vi.spyOn(window,"getComputedStyle").mockImplementation(el=>computed(el));globalThis.ResizeObserver=class{observe(){}unobserve(){}disconnect(){}}});
afterEach(()=>{cleanup();vi.restoreAllMocks()});
it("shows the stored language and changes only future language through an explicit selection",async()=>{
 const update=vi.fn();const profile=vi.fn();const w={id:"work",title:"第一集",duration:33,aspect:"9:16",idea:"故事",prompt_language:"zh-CN"} as Work;
 render(<StudioProvider><DramaMaterialsDrawer open close={()=>{}} work={w} updateLanguage={update} updateProfile={profile} busy={false} continuity={null}/></StudioProvider>);
 expect(update).not.toHaveBeenCalled();expect(screen.getByText(/不会自动生成或翻译/)).toBeTruthy();
 fireEvent.mouseDown(screen.getByRole("combobox",{name:"提示词语言"}));fireEvent.click(await screen.findByText("英文"));
 expect(update).toHaveBeenCalledExactlyOnceWith("en");expect(profile).not.toHaveBeenCalled();
});
