import { useState } from "react";
import { Check, ChevronDown, ImagePlus, Layers, X } from "lucide-react";
import { api, media } from "./api";
import type { Board, Project, Shot, Review, SourceMapping } from "./api";
type Action = (fn: () => Promise<unknown>) => void;
type Props = { project: Project; board: Board; disabled: boolean; act: Action };
export function ReviewLabel({ review }: { review?: Review }) {
  if (!review) return <span className="audit-badge">等待审核</span>;
  if (review.accepted)
    return <span className="audit-badge manual">人工接受 · 原始问题保留</span>;
  if (review.state === "passed" || review.state === "failed")
    return (
      <span className={"audit-badge " + review.state}>
        {review.state === "passed" ? "通过" : "需处理"} ·{" "}
        {review.score.toFixed(1)}分
      </span>
    );
  return (
    <span className="audit-badge">
      {review.state === "legacy"
        ? "历史素材 · 未新增审核"
        : review.state === "incomplete"
          ? "审核未完成"
          : "等待审核"}
    </span>
  );
}
function AcceptReview({
  project,
  board,
  disabled,
  act,
  target,
  ...ids
}: Props & {
  target: string;
  shot_id?: string;
  candidate_id?: string;
  asset_id?: string;
  view?: string;
}) {
  const [reason, setReason] = useState("");
  const [open, setOpen] = useState(false);
  return (
    <div className="accept-review">
      <button
        className="text-button"
        disabled={disabled}
        onClick={() => setOpen(!open)}
      >
        处理审核意见 <ChevronDown size={12} />
      </button>
      {open && (
        <div>
          <input
            aria-label="人工接受理由"
            placeholder="填写接受该问题的具体理由"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
          <button
            className="secondary"
            disabled={disabled || !reason.trim()}
            onClick={() =>
              act(async () => {
                await api(`/projects/${project.id}/review`, {
                  version: board.version,
                  revision: project.revision,
                  target,
                  ...ids,
                  accept: true,
                  reason,
                });
                setOpen(false);
                setReason("");
              })
            }
          >
            记录人工接受
          </button>
          <button
            className="text-button"
            disabled={disabled}
            onClick={() =>
              act(() =>
                api(`/projects/${project.id}/review`, {
                  version: board.version,
                  revision: project.revision,
                  target,
                  ...ids,
                  accept: false,
                  reason: "",
                }),
              )
            }
          >
            撤销接受 / 保留问题
          </button>
        </div>
      )}
    </div>
  );
}
export function ShotInspector({
  shot,
  ...props
}: Props & { shot: Shot; onRedoVideo: () => void }) {
  const [open, setOpen] = useState(false);
  const [choice, setChoice] = useState("");
  const [affected, setAffected] = useState<string[]>([]);
  const chosen = shot.candidates?.find((c) => c.id === shot.selected);
  const { project, board, disabled, act } = props;
  return (
    <>
      <div className="candidate-summary">
        <span>
          {shot.stale
            ? "参考已变化 · 待更新"
            : chosen?.id === shot.recommended
              ? "AI 推荐已选"
              : "已选候选"}
        </span>
        <ReviewLabel review={chosen?.review} />
      </div>
      <button className="text-button" onClick={() => setOpen(true)}>
        <Layers size={14} /> 比较候选与镜头设计 · {shot.candidates?.length ?? 0}{" "}
        张
      </button>
      {shot.video_review?.state && (
        <div className="video-audit">
          <span>视频三帧审核</span>
          <button
            className="text-button"
            disabled={disabled || !board.approved}
            onClick={props.onRedoVideo}
          >
            重新制作本镜视频 · 先报价
          </button>
          <ReviewLabel review={shot.video_review} />
          {shot.video_review.reason && <p>{shot.video_review.reason}</p>}
          {shot.video_review.state !== "passed" && (
            <AcceptReview {...props} target="video" shot_id={shot.id} />
          )}
        </div>
      )}
      {open && (
        <div className="modal-backdrop">
          <section
            className="quote-modal candidate-modal"
            role="dialog"
            aria-modal="true"
            aria-label="镜头候选比较"
          >
            <header className="modal-header">
              <div>
                <span className="eyebrow">SHOT COMPARISON</span>
                <h2>{shot.title}</h2>
                <p>{shot.purpose}</p>
              </div>
              <button
                className="icon-button"
                aria-label="关闭候选比较"
                onClick={() => setOpen(false)}
              >
                <X size={22} />
              </button>
            </header>
            <div className="camera-summary">
              {shot.camera &&
                Object.entries(shot.camera).map(([k, v]) => (
                  <span key={k}>{v}</span>
                ))}
            </div>
            {shot.stale && (
              <div className="error-banner">
                引用或提示词已变化。旧图保留供对照，不能直接作为当前画面确认。
              </div>
            )}
            <div className="candidate-grid">
              {shot.candidates?.map((c, index) => (
                <article
                  key={c.id}
                  className={
                    "candidate-option " +
                    (c.id === shot.selected ? "selected" : "")
                  }
                >
                  <div className="candidate-image">
                    {c.image ? (
                      <img
                        src={media(c.image)}
                        alt={`${shot.title}候选${index + 1}`}
                      />
                    ) : (
                      <span>图片尚未完成</span>
                    )}
                  </div>
                  <div className="candidate-option-body">
                    <h3>
                      {c.round === 2 ? "修复候选" : `候选 ${index + 1}`}{" "}
                      {c.id === shot.recommended && (
                        <span className="pill">AI 推荐</span>
                      )}
                    </h3>
                    <ReviewLabel review={c.review} />
                    <p>{c.review?.reason}</p>
                    {c.review?.issues?.length > 0 && (
                      <ul>
                        {c.review.issues.map((x, i) => (
                          <li key={i}>{x}</li>
                        ))}
                      </ul>
                    )}
                    {c.review &&
                      ["passed", "failed"].includes(c.review.state) && (
                        <dl className="score-dimensions">
                          {[
                            ["角色", c.review.identity],
                            ["风格", c.review.style],
                            ["构图剧情", c.review.composition],
                            ["连续性", c.review.continuity],
                            ["技术", c.review.technical],
                          ].map(([label, value]) => (
                            <div key={label}>
                              <dt>{label}</dt>
                              <dd>{value}</dd>
                            </div>
                          ))}
                        </dl>
                      )}
                    <button
                      className={
                        c.id === shot.selected ? "secondary" : "primary"
                      }
                      disabled={
                        disabled ||
                        shot.stale ||
                        !c.image ||
                        c.id === shot.selected
                      }
                      onClick={() =>
                        act(async () => {
                          const result = await api<{ affected: string[] }>(
                            `/projects/${project.id}/impact`,
                            { shot_id: shot.id },
                          );
                          setChoice(c.id);
                          setAffected(result.affected);
                        })
                      }
                    >
                      {c.id === shot.selected ? (
                        <>
                          <Check size={14} /> 当前已选
                        </>
                      ) : (
                        "选择这张 · 先预览影响"
                      )}
                    </button>
                    {c.review?.state !== "passed" && (
                      <AcceptReview
                        {...props}
                        target="candidate"
                        shot_id={shot.id}
                        candidate_id={c.id}
                      />
                    )}
                    <details>
                      <summary>提示词与真实引用</summary>
                      <p className="full-prompt">{c.prompt}</p>
                      <p className="full-prompt">{c.video_prompt}</p>
                      <div className="reference-uses">
                        {c.references?.map((ref, i) => (
                          <div key={ref.asset_id + i}>
                            <img src={media(ref.file)} alt={ref.purpose} />
                            <span>
                              {i + 1}. {ref.asset_id} · {ref.purpose}
                              <small>SHA-256 {ref.hash}</small>
                            </span>
                          </div>
                        ))}
                      </div>
                    </details>
                  </div>
                </article>
              ))}
            </div>
            {choice && (
              <div className="selection-confirm">
                <p>
                  本次换选涉及：
                  {affected
                    .map(
                      (id) => board.shots.find((s) => s.id === id)?.title ?? id,
                    )
                    .join("、")}
                  。旧报价失效，后镜只标记待更新，不会自动生成。
                </p>
                <button
                  className="primary"
                  disabled={disabled}
                  onClick={() =>
                    act(async () => {
                      await api(`/projects/${project.id}/select`, {
                        version: board.version,
                        revision: project.revision,
                        shot_id: shot.id,
                        candidate_id: choice,
                      });
                      setChoice("");
                      setOpen(false);
                    })
                  }
                >
                  确认换选
                </button>
                <button className="text-button" onClick={() => setChoice("")}>
                  取消换选
                </button>
              </div>
            )}
            <details className="continuity-detail">
              <summary>连续性与声音设计</summary>
              <p>旁白：{shot.narration}</p>
              {shot.continuity &&
                Object.entries(shot.continuity).map(([k, v]) => (
                  <p key={k}>
                    {
                      (
                        {
                          time: "时间",
                          weather: "天气",
                          lighting: "光线",
                          positions: "位置",
                          gaze: "视线",
                          direction: "运动方向",
                          props: "道具状态",
                          end_state: "结束状态",
                          handoff: "下镜衔接",
                        } as Record<string, string>
                      )[k]
                    }
                    ：{v}
                  </p>
                ))}
            </details>
            <p className="score-note">
              评分是同批候选的相对比较，不是客观质量概率。模拟模式无法验证真实画质。
            </p>
          </section>
        </div>
      )}
    </>
  );
}
export function BoardAssets(
  props: Props & { onRegenerate: (id: string) => void },
) {
  const { project, board, disabled, act } = props;
  const [mappingOpen, setMappingOpen] = useState(false);
  const [mappings, setMappings] = useState<SourceMapping[]>([]);
  function editMappings() {
    setMappings(
      project.references.map(
        (file) =>
          board.source_mappings?.find((m) => m.file === file) ?? {
            file,
            kind: "character",
            target: board.characters[0]?.id ?? "",
            reason: "人工指定用途",
          },
      ),
    );
    setMappingOpen(true);
  }
  return (
    <section className="board-assets">
      <div className="asset-toolbar">
        <span className="pill">{board.visual?.style ?? "历史风格"}</span>
        <span>每镜两张候选 · 最多一次自动修复</span>
        {project.references.length > 0 && (
          <button
            className="text-button"
            disabled={disabled}
            onClick={editMappings}
          >
            <ImagePlus size={14} /> 查看 / 纠正用户参考用途
          </button>
        )}
      </div>
      <details open className="asset-group">
        <summary>
          角色设定 · 多视角参考 <ChevronDown size={14} />
        </summary>
        <div className="role-assets">
          {board.characters.map((c) => (
            <article className="role-asset" key={c.id}>
              <h3>{c.name}</h3>
              <button
                className="text-button"
                disabled={disabled}
                onClick={() => props.onRegenerate(c.id)}
              >
                重新制作角色参考 · 另行报价
              </button>
              <p>{c.description}</p>
              {c.fixed_features && <p>固定特征：{c.fixed_features}</p>}
              <div className="reference-views">
                {c.views?.length ? (
                  c.views.map((v) => (
                    <div key={v.id}>
                      {v.image && (
                        <a
                          href={media(v.image)}
                          target="_blank"
                          rel="noreferrer"
                        >
                          <img
                            src={media(v.image)}
                            alt={`${c.name}${v.kind}`}
                          />
                        </a>
                      )}
                      <strong>
                        {
                          (
                            {
                              front: "正面全身",
                              back: "背面全身",
                              side: "侧面 / 四分之三",
                              close: "面部近景",
                            } as Record<string, string>
                          )[v.kind]
                        }
                      </strong>
                      <ReviewLabel review={v.review} />
                      {v.review?.state && v.review.state !== "passed" && (
                        <AcceptReview
                          {...props}
                          target="character"
                          asset_id={c.id}
                          view={v.kind}
                        />
                      )}
                    </div>
                  ))
                ) : c.image ? (
                  <img src={media(c.image)} alt={c.name} />
                ) : (
                  <span>角色参考等待生成</span>
                )}
              </div>
            </article>
          ))}
        </div>
      </details>
      {(board.scenes?.length > 0 || board.props?.length > 0) && (
        <details className="asset-group">
          <summary>
            场景与关键道具 <ChevronDown size={14} />
          </summary>
          <div className="scene-assets">
            {[
              ...(board.scenes ?? []).map((a) => ({ ...a, kind: "scene" })),
              ...(board.props ?? []).map((a) => ({ ...a, kind: "prop" })),
            ].map((a) => (
              <article key={a.id}>
                {a.image && <img src={media(a.image)} alt={a.name} />}
                <h3>{a.name}</h3>
                <button
                  className="text-button"
                  disabled={disabled}
                  onClick={() => props.onRegenerate(a.id)}
                >
                  重新制作参考 · 先报价
                </button>
                <p>{a.description}</p>
                <ReviewLabel review={a.review} />
                {a.review?.state && a.review.state !== "passed" && (
                  <AcceptReview {...props} target={a.kind} asset_id={a.id} />
                )}
              </article>
            ))}
          </div>
        </details>
      )}
      {board.story && (
        <details className="asset-group">
          <summary>
            故事与情绪节奏 <ChevronDown size={14} />
          </summary>
          <p>{board.story.relationships}</p>
          <p>{board.story.emotion}</p>
          <p>{board.story.beats?.join(" → ")}</p>
        </details>
      )}
      {mappingOpen && (
        <div className="modal-backdrop">
          <section
            className="quote-modal"
            role="dialog"
            aria-modal="true"
            aria-label="参考图用途"
          >
            <header className="modal-header">
              <h2>参考图用途</h2>
              <button
                className="icon-button"
                aria-label="关闭参考用途"
                onClick={() => setMappingOpen(false)}
              >
                <X size={20} />
              </button>
            </header>
            {mappings.map((m, i) => {
              const targets =
                m.kind === "character"
                  ? board.characters
                  : m.kind === "scene"
                    ? board.scenes
                    : board.props;
              return (
                <div className="mapping-row" key={m.file}>
                  <img src={media(m.file)} alt="用户参考图" />
                  <select
                    aria-label={`参考图${i + 1}用途`}
                    value={m.kind}
                    onChange={(e) => {
                      const next = structuredClone(mappings);
                      next[i].kind = e.target.value;
                      next[i].target =
                        e.target.value === "style"
                          ? ""
                          : ((e.target.value === "character"
                              ? board.characters
                              : e.target.value === "scene"
                                ? board.scenes
                                : board.props)?.[0]?.id ?? "");
                      setMappings(next);
                    }}
                  >
                    <option value="character">角色</option>
                    <option value="scene">场景</option>
                    <option value="prop">道具</option>
                    <option value="style">风格</option>
                  </select>
                  {m.kind !== "style" && (
                    <select
                      aria-label={`参考图${i + 1}关联资产`}
                      value={m.target}
                      onChange={(e) => {
                        const next = structuredClone(mappings);
                        next[i].target = e.target.value;
                        setMappings(next);
                      }}
                    >
                      {targets?.map((a) => (
                        <option key={a.id} value={a.id}>
                          {a.name}
                        </option>
                      ))}
                    </select>
                  )}
                  <input
                    aria-label={`参考图${i + 1}关联理由`}
                    value={m.reason}
                    onChange={(e) => {
                      const next = structuredClone(mappings);
                      next[i].reason = e.target.value;
                      setMappings(next);
                    }}
                  />
                </div>
              );
            })}
            <p>更改用途会建立新版本并标记受影响的素材，不会立即付费生成。</p>
            <button
              className="primary"
              disabled={disabled}
              onClick={() =>
                act(async () => {
                  await api(
                    `/projects/${project.id}/reference-mappings`,
                    {
                      version: board.version,
                      revision: project.revision,
                      mappings,
                    },
                    "PUT",
                  );
                  setMappingOpen(false);
                })
              }
            >
              保存关联
            </button>
          </section>
        </div>
      )}
    </section>
  );
}
