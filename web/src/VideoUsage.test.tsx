// @vitest-environment jsdom
import {cleanup, fireEvent, render, screen} from "@testing-library/react";
import {afterEach, expect, it} from "vitest";
import {VideoUsageDetails} from "./VideoUsage";
import type {Task} from "./api";

afterEach(cleanup);
const task = {stage:"shot_video",status:"completed",mode:"remote"} as Task;
it("shows reported video tokens, cost and its price snapshot", () => {
  render(<VideoUsageDetails task={{...task, video_usage:[{shot_id:"a",title:"第一镜",remote_id:"remote",model:"model",completion_tokens:100000,total_tokens:100000,recorded:"",cost:{amount:"4.6000",currency:"CNY",model:"model",basis:"仅视频费用参考折算；以厂商账单为准。",source:"官方参考价",url:"https://www.volcengine.com/docs/82379/1544106",checked:"2026-11-06"}}]}}/>);
  expect(screen.getByText(/视频生成 Token：100,000/).textContent).toContain("¥ 4.6000");
  fireEvent.click(screen.getByText("查看费用折算依据"));
  expect(screen.getByText(/仅视频费用参考折算/)).toBeTruthy();
  expect(screen.getByText(/核对日期 2026-11-06/)).toBeTruthy();
});
it("distinguishes unavailable usage and prices from zero", () => {
  const {rerender}=render(<VideoUsageDetails task={{...task,video_usage:[{shot_id:"a",title:"",remote_id:"r",model:"m",recorded:"",completion_tokens:0,reason:"单价未知"}]}}/>);
  expect(screen.getByText(/视频生成 Token：0/).textContent).toContain("费用未知");
  rerender(<VideoUsageDetails task={{...task,video_usage:[{shot_id:"a",title:"",remote_id:"r",model:"m",recorded:"",reason:"未返回用量"}]}}/>);
  expect(screen.getByText(/视频生成 Token：供应商未返回/)).toBeTruthy();
  rerender(<VideoUsageDetails task={task}/>);
  expect(screen.getByText(/视频 Token 用量未记录/)).toBeTruthy();
  rerender(<VideoUsageDetails task={{...task,mode:"mock"}}/>);
  expect(screen.getByText(/未调用视频模型/)).toBeTruthy();
});
