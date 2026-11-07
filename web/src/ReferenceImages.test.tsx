// @vitest-environment jsdom
import {render,screen,cleanup} from "@testing-library/react";
import {afterEach,expect,it} from "vitest";
import {ReferenceImages,ReferencePlanImages} from "./ReferenceImages";
import type {Project,Shot,ReferencePlan} from "./api";
afterEach(cleanup);
const image={file:"master.png",hash:"m",approved:true};
const shot={references:["cat"]} as Shot;
const project={assets:[{id:"cat",kind:"character",name:"猫",master:image,sheet:{...image,file:"sheet.png"},views:[image,image,image,image]}]} as Project;
it("shows confirmed character sheet with its master and hides unconfirmed sheets",()=>{
 const view=render(<ReferenceImages p={project} q={shot}/>);
 expect(screen.getByRole("img",{name:"猫主图"}).getAttribute("src")).toContain("master.png");
 expect(screen.getByRole("img",{name:"猫四视角设定图"}).getAttribute("src")).toContain("sheet.png");
 view.rerender(<ReferenceImages p={{...project,assets:[{...project.assets[0],sheet:{...image,file:"sheet.png",approved:false}}]}} q={shot}/>);
 expect(screen.queryByRole("img",{name:"猫四视角设定图"})).toBeNull();
});
it("renders quoted source files and exact uploaded board rather than current project assets",()=>{
 const plan:ReferencePlan={version:4,sources:[{image:{file:"snapshot.png",hash:"source"},name:"猫四视角",kind:"character",width:1024,height:1024}],boards:[{file:"packed.png",kind:"character",width:1024,height:1024,hash:"packed-hash",bytes:42,cells:[{source:0,x:0,y:0}]}]};
 render(<ReferencePlanImages plan={plan}/>);
 expect(screen.getByRole("img",{name:"猫四视角"}).getAttribute("src")).toContain("snapshot.png");
 expect(screen.getByRole("img",{name:"实际输入参考板1"}).getAttribute("src")).toContain("packed.png");
 expect(screen.getByText(/SHA-256：packed-hash/)).toBeTruthy();
});
