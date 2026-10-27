import { useEffect, useState } from "react";
import {
  Check,
  ChevronDown,
  ImagePlus,
  Layers,
  LockKeyhole,
  Pencil,
  Plus,
  RefreshCw,
  Sparkles,
  X,
} from "lucide-react";
import { api, media } from "./api";
import type {
  AssetSet,
  Character,
  Project,
  ReferenceView,
  Review,
  SceneAsset,
  VisualPlan,
} from "./api";
import { ReviewLabel } from "./StoryboardReview";
import "./asset-workflow.css";
export const viewLabels: Record<string, string> = {
  front: "正面全身 · 主图",
  side: "侧面全身",
  back: "背面全身",
  close: "面部近景",
};
export function masterConfirmed(a: AssetSet) {
  return a.characters.every((c) => !!a.approvals?.[c.id + ":front"]?.hash);
}
export function assetStep(a?: AssetSet) {
  return !a ? 0 : !masterConfirmed(a) ? 1 : !a.locked ? 2 : 3;
}
const emptyAsset = (kind: string): SceneAsset => ({
  id: kind + Date.now().toString(36),
  name: "",
  description: "",
  image: "",
  hash: "",
  review: {} as Review,
  sources: [],
});
export default function AssetWorkflow({
  project: p,
  disabled,
  act,
  onQuote,
}: {
  project: Project;
  disabled: boolean;
  act: (fn: () => Promise<unknown>) => void;
  onQuote: (stage: string, target?: string) => Promise<void>;
}) {
  const normalize = (a: AssetSet) => ({
    ...a,
    characters: a.characters ?? [],
    scenes: a.scenes ?? [],
    props: a.props ?? [],
    approvals: a.approvals ?? {},
    intents: a.intents ?? {},
  });
  const currentRaw = p.asset_sets?.at(-1);
  const current = currentRaw ? normalize(currentRaw) : undefined;
  const [version, setVersion] = useState<number | null>(null);
  const a =
    version === null
      ? current
      : p.asset_sets?.map(normalize).find((x) => x.version === version);
  const historical = !!a && a.version !== current?.version;
  const [editing, setEditing] = useState(false);
  const [editRevision, setEditRevision] = useState(p.revision);
  const [draft, setDraft] = useState<AssetSet | null>(null);
  const [reasons, setReasons] = useState<Record<string, string>>({});
  const [preview, setPreview] = useState<{
    body: unknown;
    affected: string[];
    notice: string;
  } | null>(null);
  useEffect(() => {
    if (!editing && current) setDraft(structuredClone(current));
  }, [currentRaw, editing]);
  const busy = disabled || p.status === "running";
  const controlsDisabled = busy || historical || editing;
  function edit(fn: (next: AssetSet) => void) {
    if (!draft) return;
    const next = structuredClone(draft);
    fn(next);
    setDraft(next);
    setPreview(null);
  }
  async function approve(targets: string[], reason = "") {
    if (!current) return;
    await api(`/projects/${p.id}/assets/approve`, {
      version: current.version,
      revision: p.revision,
      targets,
      reason,
    });
  }
  function sourcePicker(values: string[], change: (sources: string[]) => void) {
    return (
      p.references?.length > 0 && (
        <div className="aw-sources">
          <small>关联已上传参考图</small>
          {p.references.map((file, i) => (
            <label key={file}>
              <input
                type="checkbox"
                checked={values.includes(file)}
                onChange={(e) =>
                  change(
                    e.target.checked
                      ? [...values, file]
                      : values.filter((x) => x !== file),
                  )
                }
              />
              <img src={media(file)} alt={`参考图 ${i + 1}`} />
              <span>参考图 {i + 1}</span>
            </label>
          ))}
        </div>
      )
    );
  }
  const targets = a
    ? [
        ...a.characters.flatMap((c) => c.views.map((v) => c.id + ":" + v.kind)),
        ...a.scenes.map((x) => x.id),
        ...a.props.map((x) => x.id),
      ]
    : [];
  const allApproved =
    !!a && targets.length > 0 && targets.every((id) => a.approvals?.[id]?.hash);
  function imageCard(
    target: string,
    label: string,
    item: ReferenceView | SceneAsset,
    stage: string,
  ) {
    const confirmed = !!a?.approvals?.[target]?.hash;
    const incomplete =
      !item.review?.state || item.review.state === "incomplete";
    return (
      <article
        className={"aw-image-card " + (confirmed ? "confirmed" : "")}
        key={target}
      >
        <div className="aw-image-heading">
          <strong>{label}</strong>
          <span>
            {confirmed ? "已人工确认" : item.image ? "待人工确认" : "待生成"}
          </span>
        </div>
        {item.image ? (
          <a href={media(item.image)} target="_blank" rel="noreferrer">
            <img className="aw-image" src={media(item.image)} alt={label} />
          </a>
        ) : (
          <div className="aw-placeholder">
            <ImagePlus size={25} />
            <span>
              {stage === "reference_assets" && a && !masterConfirmed(a)
                ? "先确认角色主图"
                : "等待生成图片"}
            </span>
          </div>
        )}
        {item.image && (
          <>
            <ReviewLabel review={item.review} />
            <details className="aw-image-details">
              <summary>
                提示词与实际引用 <ChevronDown size={12} />
              </summary>
              <p>{item.prompt || "历史素材，提示词以原制作记录为准"}</p>
              <div>
                {item.references?.length ? (
                  item.references.map((ref, i) => (
                    <a
                      key={ref.file}
                      href={media(ref.file)}
                      target="_blank"
                      rel="noreferrer"
                    >
                      <img src={media(ref.file)} alt={`实际参考 ${i + 1}`} />
                      <span>{ref.purpose}</span>
                    </a>
                  ))
                ) : (
                  <small>无额外参考图片</small>
                )}
              </div>
            </details>
          </>
        )}
        {!confirmed &&
          item.image &&
          item.review?.state !== "passed" &&
          !incomplete && (
            <input
              aria-label={label + "人工接受理由"}
              placeholder="填写人工接受理由，原审核问题保留"
              value={reasons[target] ?? ""}
              onChange={(e) =>
                setReasons((old) => ({ ...old, [target]: e.target.value }))
              }
              disabled={controlsDisabled}
            />
          )}
        <div className="aw-image-actions">
          {item.image && !confirmed && (
            <button
              className="secondary"
              disabled={
                controlsDisabled ||
                incomplete ||
                (item.review?.state !== "passed" && !reasons[target]?.trim())
              }
              onClick={() =>
                act(() => approve([target], reasons[target] ?? ""))
              }
            >
              <Check size={13} /> 确认该素材
            </button>
          )}
          <button
            className="text-button"
            disabled={
              controlsDisabled ||
              (stage === "reference_assets" && !!a && !masterConfirmed(a))
            }
            onClick={() => act(() => onQuote(stage, target))}
          >
            <RefreshCw size={12} />
            {item.image ? "单独报价重做" : "查看单图报价"}
          </button>
        </div>
      </article>
    );
  }
  if (!a)
    return (
      <section className="aw-start">
        <div className="aw-section-heading">
          <div>
            <span className="eyebrow">PRE-PRODUCTION</span>
            <h2>先定角色与场景，再开始分镜</h2>
            <p>
              先分析故事，列出角色、场景和关键道具；确认描述后，每个角色先生成一张主图。
            </p>
          </div>
          <Layers size={26} />
        </div>
        <div className="aw-start-actions">
          <button
            className="primary"
            disabled={busy}
            onClick={() => act(() => onQuote("asset_plan"))}
          >
            <Sparkles size={15} /> 分析故事，准备参考素材
          </button>
          {p.boards?.length > 0 && (
            <button
              className="secondary"
              disabled={busy}
              onClick={() =>
                act(() => api(`/projects/${p.id}/assets/import`, {}))
              }
            >
              导入已有分镜参考素材
            </button>
          )}
        </div>
        <small>
          故事分析仅调用一次规划模型，不生成图片。每一步都可以暂停检查，生成前单独确认费用。
        </small>
      </section>
    );
  return (
    <section className="asset-workflow">
      <div className="aw-section-heading">
        <div>
          <span className="eyebrow">REFERENCE LIBRARY</span>
          <h2>
            角色与参考素材{" "}
            <span className="pill">
              {a.locked
                ? "已锁定"
                : masterConfirmed(a)
                  ? "完善其他参考素材"
                  : "先确认角色主图"}
            </span>
          </h2>
          <p>
            正面主图确认后，再扩展侧面、背面、面部近景和场景、道具。全部人工确认并锁定后生成分镜。
          </p>
        </div>
        <select
          aria-label="素材历史版本"
          value={version ?? current?.version}
          onChange={(e) => {
            setVersion(
              Number(e.target.value) === current?.version
                ? null
                : Number(e.target.value),
            );
            setEditing(false);
          }}
        >
          {p.asset_sets?.map((v) => (
            <option key={v.version} value={v.version}>
              素材版本 {v.version}
              {v.locked ? " · 已锁定" : ""}
            </option>
          ))}
        </select>
      </div>
      {historical && (
        <div className="aw-note">
          正在查看历史素材，只读保留。
          <button className="text-button" onClick={() => setVersion(null)}>
            返回当前版本
          </button>
        </div>
      )}
      <div className="aw-plan-toolbar">
        <div>
          <strong>
            {a.characters.length} 个角色 · {a.scenes.length} 个场景 ·{" "}
            {a.props.length} 个关键道具
          </strong>
          <small>{a.story.synopsis}</small>
        </div>
        {!editing && (
          <button
            className="secondary"
            disabled={busy || historical}
            onClick={() => {
              setDraft(structuredClone(a));
              setEditRevision(p.revision);
              setEditing(true);
            }}
          >
            <Pencil size={13} /> 编辑素材设定
          </button>
        )}
      </div>
      {editing && draft && (
        <div className="aw-editor">
          <label>
            故事概要
            <textarea
              value={draft.story.synopsis}
              onChange={(e) =>
                edit((x) => {
                  x.story.synopsis = e.target.value;
                })
              }
            />
          </label>
          <label>
            视觉风格
            <input
              value={draft.visual.style}
              onChange={(e) =>
                edit((x) => {
                  x.visual.style = e.target.value;
                })
              }
            />
          </label>
          <h3>角色设定</h3>
          {draft.characters.map((c, i) => (
            <div className="aw-definition" key={c.id}>
              <div className="aw-definition-heading">
                <strong>角色 {i + 1}</strong>
                <button
                  aria-label={"移除角色 " + (i + 1)}
                  className="icon-button"
                  disabled={draft.characters.length <= 1}
                  onClick={() =>
                    edit((x) => {
                      x.characters.splice(i, 1);
                    })
                  }
                >
                  <X size={14} />
                </button>
              </div>
              <label>
                名称
                <input
                  value={c.name}
                  onChange={(e) =>
                    edit((x) => {
                      x.characters[i].name = e.target.value;
                    })
                  }
                />
              </label>
              <label>
                外观、服装与身份
                <textarea
                  value={c.description}
                  onChange={(e) =>
                    edit((x) => {
                      x.characters[i].description = e.target.value;
                    })
                  }
                />
              </label>
              <label>
                必须保持的固定特征
                <input
                  value={c.fixed_features}
                  onChange={(e) =>
                    edit((x) => {
                      x.characters[i].fixed_features = e.target.value;
                    })
                  }
                />
              </label>
              {sourcePicker(c.sources ?? [], (sources) =>
                edit((x) => {
                  x.characters[i].sources = sources;
                }),
              )}
            </div>
          ))}
          <button
            className="text-button"
            disabled={draft.characters.length >= 3}
            onClick={() =>
              edit((x) => {
                x.characters.push({
                  ...emptyAsset("c"),
                  fixed_features: "",
                  views: [],
                } as unknown as Character);
              })
            }
          >
            <Plus size={13} /> 添加角色（最多3个）
          </button>
          {(["scenes", "props"] as const).map((group) => (
            <div key={group}>
              <h3>{group === "scenes" ? "场景" : "关键道具"}</h3>
              {draft[group].map((v, i) => (
                <div key={v.id} className="aw-definition">
                  <div className="aw-definition-heading">
                    <strong>
                      {group === "scenes" ? "场景" : "道具"} {i + 1}
                    </strong>
                    <button
                      aria-label={"移除" + group + i}
                      className="icon-button"
                      disabled={group === "scenes" && draft.scenes.length <= 1}
                      onClick={() =>
                        edit((x) => {
                          x[group].splice(i, 1);
                        })
                      }
                    >
                      <X size={14} />
                    </button>
                  </div>
                  <label>
                    名称
                    <input
                      value={v.name}
                      onChange={(e) =>
                        edit((x) => {
                          x[group][i].name = e.target.value;
                        })
                      }
                    />
                  </label>
                  <label>
                    具体描述
                    <textarea
                      value={v.description}
                      onChange={(e) =>
                        edit((x) => {
                          x[group][i].description = e.target.value;
                        })
                      }
                    />
                  </label>
                  {sourcePicker(v.sources ?? [], (sources) =>
                    edit((x) => {
                      x[group][i].sources = sources;
                    }),
                  )}
                </div>
              ))}
              <button
                className="text-button"
                disabled={draft[group].length >= (group === "scenes" ? 3 : 2)}
                onClick={() =>
                  edit((x) => {
                    x[group].push(
                      emptyAsset(group === "scenes" ? "scene" : "prop"),
                    );
                  })
                }
              >
                <Plus size={13} /> 添加{group === "scenes" ? "场景" : "道具"}
              </button>
            </div>
          ))}
          {preview && (
            <div className="aw-note">
              <p>{preview.notice}</p>
              <strong>
                受影响分镜：
                {preview.affected.length
                  ? preview.affected.join("、")
                  : "尚无或无需更新"}
              </strong>
            </div>
          )}
          <div className="aw-editor-actions">
            <button
              className="secondary"
              disabled={busy}
              onClick={() => {
                setEditing(false);
                setPreview(null);
              }}
            >
              取消编辑
            </button>
            <button
              className="primary"
              disabled={busy}
              onClick={() =>
                act(async () => {
                  if (preview) {
                    await api(`/projects/${p.id}/assets`, preview.body, "PUT");
                    setEditing(false);
                    setPreview(null);
                  } else {
                    const body = {
                      version: draft.version,
                      revision: editRevision,
                      assets: draft,
                    };
                    const result = await api<{
                      affected: string[];
                      notice: string;
                    }>(`/projects/${p.id}/assets/impact`, body);
                    setPreview({ ...result, body });
                  }
                })
              }
            >
              {preview ? "确认修改并保存" : "预览修改影响"}
            </button>
          </div>
        </div>
      )}
      <fieldset disabled={busy || editing} className="aw-gallery">
        <div className="aw-section-heading">
          <h3>01 · 角色正面主图</h3>
          <button
            className="secondary"
            disabled={
              controlsDisabled ||
              a.characters.every(
                (c) => c.views.find((v) => v.kind === "front")?.image,
              )
            }
            onClick={() => act(() => onQuote("character_masters"))}
          >
            <Sparkles size={13} /> 按清单生成角色主图
          </button>
        </div>
        {a.characters.map((c) => (
          <div className="aw-character" key={c.id}>
            <div>
              <h3>{c.name}</h3>
              <p>{c.description}</p>
              <small>
                固定特征：{c.fixed_features || "按外观设定保持一致"}
              </small>
            </div>
            <div className="aw-main-grid">
              {c.views
                .filter((v) => v.kind === "front")
                .map((v) =>
                  imageCard(
                    c.id + ":front",
                    c.name + " · " + viewLabels.front,
                    v,
                    "character_masters",
                  ),
                )}
            </div>
          </div>
        ))}
        {!masterConfirmed(a) && (
          <div className="aw-note">
            模型审核不替代人工确认。请先逐张确认角色主图，才能制作其他角度和参考素材。
          </div>
        )}
        <div className="aw-section-heading">
          <h3>02 · 其他视角、场景与关键道具</h3>
          <button
            className="secondary"
            disabled={
              controlsDisabled || !masterConfirmed(a) || allApproved || a.locked
            }
            onClick={() => act(() => onQuote("reference_assets"))}
          >
            <Sparkles size={13} /> 查看剩余素材报价
          </button>
        </div>
        <div className="aw-view-grid">
          {a.characters.flatMap((c) =>
            c.views
              .filter((v) => v.kind !== "front")
              .map((v) =>
                imageCard(
                  c.id + ":" + v.kind,
                  c.name + " · " + viewLabels[v.kind],
                  v,
                  "reference_assets",
                ),
              ),
          )}
        </div>
        {a.scenes.length > 0 && (
          <>
            <h3>场景参考</h3>
            <div className="aw-view-grid">
              {a.scenes.map((v) =>
                imageCard(v.id, v.name, v, "reference_assets"),
              )}
            </div>
          </>
        )}
        {a.props.length > 0 && (
          <>
            <h3>关键道具</h3>
            <div className="aw-view-grid">
              {a.props.map((v) =>
                imageCard(v.id, v.name, v, "reference_assets"),
              )}
            </div>
          </>
        )}
      </fieldset>
      <div className="aw-bottom">
        <span>
          {a.locked
            ? "参考素材已锁定。分镜将直接引用这些图片。"
            : allApproved
              ? "全部图片已人工确认，可以锁定。"
              : "确认全部参考素材后，才能生成整套分镜。"}
        </span>
        {!a.locked ? (
          <button
            className="primary"
            disabled={controlsDisabled || !allApproved}
            onClick={() =>
              act(() =>
                api(`/projects/${p.id}/assets/lock`, {
                  version: current!.version,
                  revision: p.revision,
                }),
              )
            }
          >
            <LockKeyhole size={14} /> 锁定参考素材
          </button>
        ) : (
          <button
            className="primary"
            disabled={controlsDisabled}
            onClick={() => act(() => onQuote("storyboard"))}
          >
            <Layers size={14} /> 使用参考素材生成整套分镜
          </button>
        )}
      </div>
    </section>
  );
}
