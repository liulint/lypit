import { describe, it, expect } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ReviewLabel, ShotInspector } from "./StoryboardReview";
import type { Board, Project, Review, Shot } from "./api";

describe("storyboard review presentation", () => {
  it("never gives an incomplete audit a passing score", () => {
    const html = renderToStaticMarkup(
      <ReviewLabel review={{ state: "incomplete", score: 99 } as Review} />,
    );
    expect(html).toContain("审核未完成");
    expect(html).not.toContain("99");
    expect(html).not.toContain("通过");
  });
  it("keeps human acceptance distinct from model approval", () => {
    const html = renderToStaticMarkup(
      <ReviewLabel
        review={
          { state: "failed", score: 42, accepted: "已人工核对" } as Review
        }
      />,
    );
    expect(html).toContain("人工接受");
    expect(html).toContain("原始问题保留");
  });
  it("labels migrated media without inventing a new audit", () => {
    const html = renderToStaticMarkup(
      <ReviewLabel review={{ state: "legacy" } as Review} />,
    );
    expect(html).toContain("历史素材");
    expect(html).not.toContain("分");
  });
  it("exposes candidate comparison and stale dependencies", () => {
    const shot = {
      id: "s1",
      stale: true,
      candidates: [{ id: "a" }, { id: "b" }],
    } as Shot;
    const html = renderToStaticMarkup(
      <ShotInspector
        shot={shot}
        board={{ approved: false } as Board}
        project={{ id: "p" } as Project}
        disabled={false}
        act={() => {}}
        onRedoVideo={() => {}}
      />,
    );
    expect(html).toContain("待更新");
    expect(html).toContain("比较候选");
    expect(html).toContain("2");
  });
});
