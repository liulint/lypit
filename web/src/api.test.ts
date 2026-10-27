import { describe, it, expect, vi, afterEach } from "vitest";
import { api, media, money } from "./api";
afterEach(() => vi.unstubAllGlobals());
describe("request contract", () => {
  it("does not retry a paid mutation after a lost response", async () => {
    const fetch = vi.fn().mockRejectedValue(new Error("offline"));
    vi.stubGlobal("fetch", fetch);
    await expect(api("/projects/p/confirm", { quote_id: "q" })).rejects.toThrow(
      "offline",
    );
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("surfaces provider configuration failures", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue({
          ok: false,
          json: async () => ({ error: "凭据缺失" }),
        }),
    );
    await expect(api("/settings")).rejects.toThrow("凭据缺失");
  });
  it("keeps unknown cost distinct from zero", () => {
    expect(money(null)).toBe("费用上界未知");
    expect(money(0)).toBe("¥0.00");
  });
  it("encodes media path segments", () =>
    expect(media("project/a b.png")).toBe("/media/project/a%20b.png"));
});
