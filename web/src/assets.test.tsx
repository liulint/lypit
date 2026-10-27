// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import AssetWorkflow, { assetStep } from "./AssetWorkflow";
import { quoteStageLabels, quoteRequestLabel } from "./api";
import type { AssetSet, Project, Review } from "./api";
const mock = vi.hoisted(() => ({ api: vi.fn() }));
vi.mock("./api", async () => ({
  ...(await vi.importActual("./api")),
  api: mock.api,
}));
afterEach(() => {
  cleanup();
  mock.api.mockReset();
});
function sample(): AssetSet {
  return {
    version: 1,
    work_quote: "q",
    visual: {
      style: "动画",
      photography: "natural",
      palette: "warm",
      candidates: 2,
      repairs: 1,
    },
    story: {
      synopsis: "a cat",
      relationships: "",
      emotion: "warm",
      beats: ["start", "end"],
    },
    characters: [
      {
        id: "c1",
        name: "小猫",
        description: "橘色短毛",
        fixed_features: "绿色围巾",
        image: "front.png",
        sources: [],
        views: ["front", "side", "back", "close"].map((kind) => ({
          id: "c1-" + kind,
          kind,
          image: kind === "front" ? "front.png" : "",
          hash: "hash",
          review:
            kind === "front"
              ? ({ state: "passed", score: 90, reason: "test" } as Review)
              : ({} as Review),
        })),
      },
    ],
    scenes: [
      {
        id: "scene1",
        name: "小屋",
        description: "温暖窗边",
        image: "",
        hash: "",
        sources: [],
        review: {} as Review,
      },
    ],
    props: [],
    approvals: {},
    intents: {},
    locked: false,
  };
}
function project(a?: AssetSet): Project {
  return {
    id: "p",
    revision: 7,
    status: "review",
    asset_workflow: true,
    asset_sets: a ? [a] : [],
    references: [],
    boards: [],
  } as unknown as Project;
}
const act = (fn: () => Promise<unknown>) => {
  void fn();
};
describe("参考素材准备", () => {
  it("没有关键道具时也能展示素材清单", () => {
    const a = sample();
    a.props = null as unknown as AssetSet["props"];
    render(
      <AssetWorkflow
        project={project(a)}
        disabled={false}
        act={act}
        onQuote={vi.fn()}
      />,
    );
    expect(screen.getByText(/1 个角色 · 1 个场景 · 0 个关键道具/)).toBeTruthy();
  });

  it("入口先报价故事分析，不直接生成分镜", async () => {
    const quote = vi.fn().mockResolvedValue(undefined);
    render(
      <AssetWorkflow
        project={project()}
        disabled={false}
        act={act}
        onQuote={quote}
      />,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "分析故事，准备参考素材" }),
    );
    expect(quote).toHaveBeenCalledWith("asset_plan");
    expect(
      screen.queryByRole("button", { name: /使用参考素材生成整套分镜/ }),
    ).toBeNull();
    expect(quoteStageLabels.asset_plan).toBe("确认故事分析");
    expect(
      quoteRequestLabel("character_masters", {
        kind: "image",
        role: "character_image",
      }),
    ).toBe("角色正面主图");
  });
  it("主图模型通过仍需人工确认，才能报价其他视图", async () => {
    const a = sample();
    const quote = vi.fn().mockResolvedValue(undefined);
    mock.api.mockResolvedValue({});
    const ui = render(
      <AssetWorkflow
        project={project(a)}
        disabled={false}
        act={act}
        onQuote={quote}
      />,
    );
    expect(
      (
        screen.getByRole("button", {
          name: "查看剩余素材报价",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(screen.getByText(/背面全身/)).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "确认该素材" }));
    expect(mock.api).toHaveBeenCalledWith("/projects/p/assets/approve", {
      version: 1,
      revision: 7,
      targets: ["c1:front"],
      reason: "",
    });
    expect(quote).not.toHaveBeenCalled();
    a.approvals["c1:front"] = { hash: "verified", at: "now", reason: "" };
    ui.rerender(
      <AssetWorkflow
        project={project(a)}
        disabled={false}
        act={act}
        onQuote={quote}
      />,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "查看剩余素材报价" }),
    );
    expect(quote).toHaveBeenCalledWith("reference_assets");
  });
  it("全部图片人工确认后仍需显式锁定", async () => {
    const a = sample();
    for (const c of a.characters) {
      for (const v of c.views) {
        v.image = v.kind + ".png";
        v.review = { state: "passed", score: 90 } as Review;
        a.approvals[c.id + ":" + v.kind] = {
          hash: "ok",
          at: "now",
          reason: "",
        };
      }
    }
    a.scenes[0].image = "scene.png";
    a.scenes[0].review = { state: "passed", score: 90 } as Review;
    a.approvals.scene1 = { hash: "ok", at: "now", reason: "" };
    mock.api.mockResolvedValue({});
    const quote = vi.fn();
    render(
      <AssetWorkflow
        project={project(a)}
        disabled={false}
        act={act}
        onQuote={quote}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "锁定参考素材" }));
    expect(mock.api).toHaveBeenCalledWith("/projects/p/assets/lock", {
      version: 1,
      revision: 7,
    });
    expect(quote).not.toHaveBeenCalled();
    expect(assetStep(a)).toBe(2);
    a.locked = true;
    expect(assetStep(a)).toBe(3);
  });
  it("编辑已有角色先展示影响，再确认保存", async () => {
    const user = userEvent.setup();
    const a = sample();
    mock.api.mockResolvedValue({
      affected: ["s1", "s2"],
      notice: "历史图片保留",
    });
    const ui = render(
      <AssetWorkflow
        project={project(a)}
        disabled={false}
        act={act}
        onQuote={vi.fn()}
      />,
    );
    await user.click(screen.getByRole("button", { name: "编辑素材设定" }));
    const input = screen.getByLabelText("外观、服装与身份");
    await user.type(input, "，戴帽子");
    ui.rerender(
      <AssetWorkflow
        project={{ ...project(a), revision: 8 }}
        disabled={false}
        act={act}
        onQuote={vi.fn()}
      />,
    );
    await user.click(screen.getByRole("button", { name: "预览修改影响" }));
    await screen.findByText("历史图片保留");
    expect(mock.api.mock.calls[0][1]).toMatchObject({ revision: 7, version: 1 });
    expect(
      mock.api.mock.calls.filter(([, , method]) => method === "PUT"),
    ).toHaveLength(0);
    expect(screen.getByText(/s1、s2/)).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "确认修改并保存" }));
    await waitFor(() =>
      expect(
        mock.api.mock.calls.filter(([, , method]) => method === "PUT"),
      ).toHaveLength(1),
    );
  });
  it("未完成审核不能人工接受，失败审核须填写理由", async () => {
    const a = sample();
    a.characters[0].views[0].review = { state: "incomplete" } as Review;
    const ui = render(
      <AssetWorkflow
        project={project(a)}
        disabled={false}
        act={act}
        onQuote={vi.fn()}
      />,
    );
    expect(
      (screen.getByRole("button", { name: "确认该素材" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    a.characters[0].views[0].review = {
      state: "failed",
      score: 45,
      reason: "漂移",
    } as Review;
    ui.rerender(
      <AssetWorkflow
        project={project(a)}
        disabled={false}
        act={act}
        onQuote={vi.fn()}
      />,
    );
    expect(
      (screen.getByRole("button", { name: "确认该素材" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    await userEvent.type(screen.getByLabelText(/人工接受理由/), "已人工核对");
    expect(
      (screen.getByRole("button", { name: "确认该素材" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
  });
});
