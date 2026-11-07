// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TopNotifications } from "./TopNotifications";
import type { Task } from "./api";

beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: false, addListener() {}, removeListener() {} })));
  const getStyle = window.getComputedStyle.bind(window);
  vi.spyOn(window, "getComputedStyle").mockImplementation(element => getStyle(element));
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });
const task = { id: "one", project_id: "p", stage: "boards", status: "running", progress: 10, message: "制作中" } as Task;
const view = (tasks: Task[]) => <TopNotifications tasks={tasks} projects={[]} onTasks={vi.fn()} />;

it("counts independent tasks, persists reads, and alerts again on status change but not progress", async () => {
  const ui = render(view([task, { ...task, id: "two" }]));
  fireEvent.click(screen.getByRole("button", { name: "任务通知，2 条未读" }));
  fireEvent.click((await screen.findAllByRole("button", { name: /查看详情/ }))[0]);
  expect(screen.getByRole("button", { name: "任务通知，1 条未读" })).toBeTruthy();
  ui.rerender(view([{ ...task, progress: 80 }, { ...task, id: "two" }]));
  expect(screen.getByRole("button", { name: "任务通知，1 条未读" })).toBeTruthy();
  ui.unmount();
  render(view([{ ...task, progress: 90 }, { ...task, id: "two" }]));
  expect(screen.getByRole("button", { name: "任务通知，1 条未读" })).toBeTruthy();
  cleanup();
  render(view([{ ...task, status: "completed" }, { ...task, id: "two" }]));
  expect(screen.getByRole("button", { name: "任务通知，2 条未读" })).toBeTruthy();
});

it("keeps Token usage out of the task notification menu", () => {
 render(view([task]));
 expect(screen.queryByRole("button", {name:/Token 用量/})).toBeNull();
});
