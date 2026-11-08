// @vitest-environment jsdom
import {afterEach,expect,it} from "vitest";
import {cleanup,render,screen} from "@testing-library/react";
import DramaReading from "./DramaReading";

afterEach(cleanup);

it("renders document structure and inline Markdown instead of source symbols",()=>{
 const {container}=render(<DramaReading text={'# 视觉设定\n\n## 人物\n- **阿灰**：`CHAR-001`\n- 小白\n\n1. 确认画面\n2. 生成视频\n\n> 保持角色一致\n\n| 角色 | 场景 |\n| --- | --- |\n| 阿灰 | 屋檐 |\n\n```text\n保留原始提示词\n```'}/>);
 expect(screen.getByRole("heading",{level:1,name:"视觉设定"})).toBeTruthy();
 expect(screen.getByRole("heading",{level:2,name:"人物"})).toBeTruthy();
 expect(container.querySelectorAll("ul>li")).toHaveLength(2);
 expect(container.querySelectorAll("ol>li")).toHaveLength(2);
 expect(container.querySelector("strong")?.textContent).toBe("阿灰");
 expect(container.querySelector("blockquote")?.textContent).toContain("保持角色一致");
 expect(screen.getByRole("table")).toBeTruthy();
 expect(container.querySelector("pre code")?.textContent).toContain("保留原始提示词");
});

it("does not execute embedded HTML, unsafe links, or fetch document images",()=>{
 const {container}=render(<DramaReading text={'<script>alert(1)</script>\n\n[危险链接](javascript:alert%281%29)\n\n![参考图](https://example.com/image.png)\n\n[说明](https://example.com)'}/>);
 expect(container.querySelector("script")).toBeNull();
 expect(container.querySelector("img")).toBeNull();
 expect(screen.getByText("危险链接").getAttribute("href")).toBe("");
 expect(screen.getByRole("link",{name:"说明"}).getAttribute("href")).toBe("https://example.com");
});
