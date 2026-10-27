import { useState } from "react";
import type { CLIModelOption, Model } from "./api";

export default function CLIModelSelect({
  value,
  models,
  onChange,
  options: metadata = [],
  cliOptions,
  onOptionsChange,
}: {
  value: string;
  models: string[];
  onChange: (value: string) => void;
  options?: CLIModelOption[];
  cliOptions?: Model["cli_options"];
  onOptionsChange?: (value: NonNullable<Model["cli_options"]>) => void;
}) {
  const [manual, setManual] = useState(false);
  const [search, setSearch] = useState("");
  const options = [...new Set(["default", ...models, value].filter(Boolean))];
  const current = metadata.find((m) => m.id === value);
  const matches = (id: string) =>
    id === value ||
    id === "default" ||
    `${id} ${metadata.find((m) => m.id === id)?.label ?? ""}`
      .toLowerCase()
      .includes(search.toLowerCase());
  return (
    <div>
      {options.length > 5 && (
        <label>
          搜索模型
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="搜索模型名称或 ID"
          />
        </label>
      )}
      <label>
        模型
        <select
          value={manual ? "__manual__" : value}
          onChange={(e) => {
            const custom = e.target.value === "__manual__";
            setManual(custom);
            if (!custom) onChange(e.target.value);
          }}
        >
          {!value && <option value="">请选择模型</option>}
          {options.filter(matches).map((id) => (
            <option key={id} value={id}>
              {id === "default"
                ? "CLI 默认模型"
                : metadata.find((m) => m.id === id)
                  ? `${metadata.find((m) => m.id === id)!.label} · ${id}`
                  : `${id}${!models.includes(id) ? "（自定义或未在当前目录中）" : ""}`}
            </option>
          ))}
          <option value="__manual__">手动填写模型 ID…</option>
        </select>
      </label>
      {current?.description && <small>{current.description}</small>}
      {manual && (
        <label>
          自定义模型 ID
          <input
            value={value}
            placeholder="填写 CLI 支持的模型 ID"
            onChange={(e) => onChange(e.target.value)}
          />
        </label>
      )}
      {!models.length && (
        <small>尚未读取到模型目录，可使用 CLI 默认模型或手动填写。</small>
      )}
      {onOptionsChange && (
        <>
          <label>
            推理强度
            <select
              value={cliOptions?.reasoning || "default"}
              onChange={(e) =>
                onOptionsChange({
                  ...cliOptions,
                  reasoning:
                    e.target.value === "default" ? undefined : e.target.value,
                })
              }
            >
              <option value="default">
                沿用 CLI 配置
                {current?.default_reasoning
                  ? `（模型默认 ${current.default_reasoning}）`
                  : ""}
              </option>
              {(current?.reasoning ?? []).map((o) => (
                <option key={o.id} value={o.id}>
                  {o.label}
                </option>
              ))}
              {cliOptions?.reasoning &&
                cliOptions.reasoning !== "default" &&
                !current?.reasoning.some(
                  (o) => o.id === cliOptions.reasoning,
                ) && (
                  <option value={cliOptions.reasoning} disabled>
                    {cliOptions.reasoning}（当前目录未核实，请重选）
                  </option>
                )}
            </select>
          </label>
          <label>
            服务档位
            <select
              value={cliOptions?.service_tier || "default"}
              onChange={(e) =>
                onOptionsChange({
                  ...cliOptions,
                  service_tier:
                    e.target.value === "default" ? undefined : e.target.value,
                })
              }
            >
              <option value="default">沿用 CLI 配置</option>
              {(current?.service_tiers ?? []).map((o) => (
                <option key={o.id} value={o.id}>
                  {o.label}
                </option>
              ))}
              {cliOptions?.service_tier &&
                cliOptions.service_tier !== "default" &&
                !current?.service_tiers.some(
                  (o) => o.id === cliOptions.service_tier,
                ) && (
                  <option value={cliOptions.service_tier} disabled>
                    {cliOptions.service_tier}（当前目录未核实，请重选）
                  </option>
                )}
            </select>
          </label>
          {cliOptions?.service_tier === "priority" && (
            <small>Fast 可能增加额度消耗或费用，以 CLI 与账户规则为准。</small>
          )}
        </>
      )}
    </div>
  );
}
