// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { api, blocked, media, type Task } from "./api";
afterEach(() => vi.unstubAllGlobals());
describe("local API contract", () => {
  it("adds the local write guard and never stores credentials", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ id: "project" }), { status: 200 }),
      );
    vi.stubGlobal("fetch", fetcher);
    expect(await api("/projects", { title: "故事" })).toEqual({
      id: "project",
    });
    const [path, options] = fetcher.mock.calls[0];
    expect(path).toBe("/api/projects");
    expect(options.headers["X-Lypit-Client"]).toBe("studio");
    expect(options.method).toBe("POST");
    expect(localStorage.length).toBe(0);
  });
  it("surfaces conflict errors instead of retrying mutations", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ error: "报价已失效" }), { status: 409 }),
      );
    vi.stubGlobal("fetch", fetcher);
    await expect(api("/quotes/a/confirm", {})).rejects.toThrow("报价已失效");
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
  it("blocks editing until interrupted work is handled", () => {
    for (const status of [
      "failed",
      "interrupted",
      "cancelled",
      "running",
      "queued",
    ])
      expect(blocked({ status } as Task)).toBe(true);
    expect(blocked({ status: "completed" } as Task)).toBe(false);
    expect(blocked({ status: "abandoned" } as Task)).toBe(false);
  });
  it("encodes each media path segment", () =>
    expect(media("project/角色 图.png")).toBe(
      "/media/project/%E8%A7%92%E8%89%B2%20%E5%9B%BE.png",
    ));
});
