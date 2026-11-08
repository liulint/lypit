// @vitest-environment jsdom
import { lazy, Suspense, useState } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import SettingsBoundary from "./SettingsBoundary";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("keeps navigation available when a versioned settings chunk cannot load", async () => {
  vi.spyOn(console, "error").mockImplementation(() => {});
  const MissingSettings = lazy(() => Promise.reject(new TypeError("Failed to fetch dynamically imported module")));
  function Host() {
    const [settings, setSettings] = useState(true);
    return <>
      <button onClick={() => setSettings(false)}>返回作品</button>
      {settings
        ? <SettingsBoundary><Suspense fallback="加载中"><MissingSettings /></Suspense></SettingsBoundary>
        : <h1>作品</h1>}
    </>;
  }
  render(<Host />);
  expect(await screen.findByText("配置页面加载失败")).toBeTruthy();
  expect(screen.getByRole("button", { name: "刷新页面" })).toBeTruthy();
  expect(screen.getByText(/刷新会丢失未保存的编辑/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "返回作品" }));
  expect(screen.getByRole("heading", { name: "作品" })).toBeTruthy();
});
