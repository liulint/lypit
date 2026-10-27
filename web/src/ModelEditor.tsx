import type { Model, Provider } from "./api";
import { supportedKinds } from "./settings-utils";
const roles: Record<string, string> = {
  text: "剧本规划",
  image: "图片生成",
  video: "视频生成",
  tts: "旁白",
  vision: "视觉审核",
};
export default function ModelEditor({
  provider,
  patchModel,
}: {
  provider: Provider;
  patchModel: (i: number, patch: Partial<Model>) => void;
}) {
  return (
    <div className="advanced-models">
      {" "}
      {provider.models.map((model, i) => (
        <div className="model-form" key={i}>
          <div className="form-grid">
            <label className="wide">
              模型 ID
              <input
                value={model.id}
                onChange={(e) => patchModel(i, { id: e.target.value })}
                placeholder="供应商提供的实际模型 ID"
              />
            </label>
            <div className="wide capability-list">
              {Object.entries(roles)
                .filter(([kind]) =>
                  supportedKinds(provider.adapter).includes(kind),
                )
                .map(([kind, label]) => (
                  <label key={kind}>
                    <input
                      type="checkbox"
                      checked={model.capabilities.includes(kind)}
                      onChange={(e) =>
                        patchModel(i, {
                          capabilities: e.target.checked
                            ? [...model.capabilities, kind]
                            : model.capabilities.filter((k) => k !== kind),
                        })
                      }
                    />
                    {label}
                  </label>
                ))}
            </div>
            <label>
              参考图能力
              <select
                value={model.reference_images ? "yes" : "no"}
                onChange={(e) =>
                  patchModel(i, {
                    reference_images: e.target.value === "yes",
                    max_references: e.target.value === "yes" ? 4 : 0,
                  })
                }
              >
                <option value="no">不支持</option>
                <option value="yes">支持图片输入</option>
              </select>
            </label>
            {model.capabilities.includes("image") && (
              <>
                <label>
                  参考图编辑能力
                  <select
                    value={
                      model.image_edit === undefined
                        ? "inherit"
                        : model.image_edit
                          ? "yes"
                          : "no"
                    }
                    onChange={(e) =>
                      patchModel(i, {
                        image_edit:
                          e.target.value === "inherit"
                            ? undefined
                            : e.target.value === "yes",
                      })
                    }
                  >
                    <option value="inherit">沿用已有参考能力声明</option>
                    <option value="yes">支持图片编辑 / 参考生成</option>
                    <option value="no">不支持，阻止参考生成</option>
                  </select>
                </label>
                <label>
                  图片返回格式
                  <input
                    value={(model.output_formats ?? []).join(",")}
                    onChange={(e) =>
                      patchModel(i, {
                        output_formats: e.target.value
                          .split(",")
                          .map((x) => x.trim())
                          .filter(Boolean),
                      })
                    }
                    placeholder="png,jpeg,webp（留空自动识别）"
                  />
                </label>
              </>
            )}
            {model.capabilities.includes("video") && (
              <label>
                视频参考方式
                <select
                  value={model.video_reference ?? "image_reference"}
                  onChange={(e) =>
                    patchModel(i, { video_reference: e.target.value })
                  }
                >
                  <option value="image_reference">
                    图片参考（当前适配器）
                  </option>
                </select>
              </label>
            )}
            <label>
              最大参考图数量
              <input
                type="number"
                min="0"
                max="9"
                value={model.max_references}
                onChange={(e) =>
                  patchModel(i, {
                    max_references: Number(e.target.value),
                  })
                }
              />
            </label>
            <label>
              尺寸 / 分辨率
              <input
                value={model.sizes.join(",")}
                onChange={(e) =>
                  patchModel(i, {
                    sizes: e.target.value.split(",").filter(Boolean),
                  })
                }
                placeholder="1024x1536 或 480p"
              />
            </label>
            <label>
              TTS voice
              <input
                value={model.voice}
                onChange={(e) => patchModel(i, { voice: e.target.value })}
                placeholder="供应商支持的声音 ID"
              />
            </label>
            <label>
              最短视频秒数
              <input
                type="number"
                min="4"
                value={model.min_duration}
                onChange={(e) =>
                  patchModel(i, { min_duration: Number(e.target.value) })
                }
              />
            </label>
            <label>
              最长视频秒数
              <input
                type="number"
                max="15"
                value={model.max_duration}
                onChange={(e) =>
                  patchModel(i, { max_duration: Number(e.target.value) })
                }
              />
            </label>
            <div className="wide capability-list">
              {(["structured", "async", "audio"] as const).map((key) => (
                <label key={key}>
                  <input
                    type="checkbox"
                    checked={model[key]}
                    onChange={(e) => patchModel(i, { [key]: e.target.checked })}
                  />
                  {
                    {
                      structured: "结构化输出",
                      async: "异步任务",
                      audio: "支持声音",
                    }[key]
                  }
                </label>
              ))}
            </div>
            {provider.adapter === "seedance" && (
              <label>
                Seedance 计价模型
                <select
                  value={model.price_model}
                  onChange={(e) =>
                    patchModel(i, { price_model: e.target.value })
                  }
                >
                  <option value="mini">mini</option>
                  <option value="fast">fast</option>
                  <option value="standard">standard</option>
                </select>
              </label>
            )}
          </div>
          <details className="pricing-editor">
            <summary>
              计价依据 ·{" "}
              {model.price === null
                ? "费用未知"
                : `${model.price} 元 / ${model.unit}`}
            </summary>
            <div className="form-grid">
              <label>
                单价（留空表示未知）
                <input
                  type="number"
                  min="0"
                  step="any"
                  value={model.price ?? ""}
                  onChange={(e) =>
                    patchModel(i, {
                      price:
                        e.target.value === "" ? null : Number(e.target.value),
                    })
                  }
                />
              </label>
              <label>
                计价单位
                <select
                  value={model.unit}
                  onChange={(e) => patchModel(i, { unit: e.target.value })}
                >
                  <option value="request">每次请求</option>
                  <option value="million_tokens">百万 Token</option>
                  <option value="character">字符</option>
                  <option value="second">秒</option>
                </select>
              </label>
              <label>
                来源
                <input
                  value={model.price_source}
                  onChange={(e) =>
                    patchModel(i, { price_source: e.target.value })
                  }
                />
              </label>
              <label>
                核实日期
                <input
                  type="date"
                  value={model.verified}
                  onChange={(e) => patchModel(i, { verified: e.target.value })}
                />
              </label>
            </div>
            <small>
              Seedance 采用 CLI 的独立计价表；Token
              等动态计费没有可靠上界时显示未知，绝不伪装封顶价。
            </small>
          </details>
        </div>
      ))}
    </div>
  );
}
