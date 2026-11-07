// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { ModelPriceDetails, modelPriceSummary } from "./ModelPricing";
import type { CatalogModel, Vendor } from "./api";

afterEach(cleanup);
const vendor: Vendor = {id:"ark", name:"火山方舟", type:"api", kind:"ark"};
const model: CatalogModel = {id:"mini",name:"mini",capabilities:["video"],services:["video"],verified:true,pricing:{rows:[{label:"视频生成",condition:"480p / 720p · 不含视频输入",amount:"23.00",unit:"百万视频 Token",currency:"CNY"},{label:"视频生成",condition:"480p / 720p · 含视频输入",amount:"14.00",unit:"百万视频 Token",currency:"CNY"}],source:"官方参考价",url:"https://www.volcengine.com/docs/82379/1544106",checked:"2026-11-06",note:"图片首帧属于不含视频输入档。"}};
it("shows video conditions, currency, source and date without calling tiers input/output prices", () => {
  render(<ModelPriceDetails model={model} vendor={vendor} stale />);
  expect(screen.getByText("¥23.00 / 百万视频 Token")).toBeTruthy();
  expect(screen.getByText("480p / 720p · 含视频输入")).toBeTruthy();
  expect(screen.getByRole("link",{name:"官方参考价"}).getAttribute("href")).toContain("volcengine.com");
  expect(screen.getByText(/2026-11-06 · 目录缓存已过期/)).toBeTruthy();
  expect(modelPriceSummary(model)).toContain("不含视频输入：¥23.00");
});
it("shows unknown CLI pricing and does not infer free use", () => {
  render(<ModelPriceDetails model={model} vendor={{...vendor,type:"cli",cli:"codex"}} />);
  expect(screen.getByText(/费用取决于账户套餐/)).toBeTruthy();
  expect(screen.queryByText(/¥23/)).toBeNull();
});
it("distinguishes missing prices, zero and missing currency", () => {
  const {rerender} = render(<ModelPriceDetails vendor={vendor} />);
  expect(screen.getByText(/暂无可核实的单价/)).toBeTruthy();
  rerender(<ModelPriceDetails vendor={vendor} model={{...model,pricing:{...model.pricing!,rows:[{label:"输入",amount:"0",unit:"百万 tokens"}]}}} />);
  expect(screen.getByText("0 / 百万 tokens（币种待确认）")).toBeTruthy();
});
