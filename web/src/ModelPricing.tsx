import type { CatalogModel, ModelPricing, Vendor } from "./api";

export function priceAmount(row: ModelPricing["rows"][number]) {
  const currency = row.currency === "CNY" ? "¥" : row.currency === "USD" ? "$" : "";
  return `${currency}${row.amount} / ${row.unit}${currency ? "" : "（币种待确认）"}`;
}

export function modelPriceSummary(model: CatalogModel) {
  const rows = model.pricing?.rows;
  if (!rows?.length) return "输入 / 输出价格待确认";
  // Keep the condition with the amount; a lower video-input tier is not an input-token price.
  return rows.slice(0, 2).map(r => `${r.label}${r.condition ? ` · ${r.condition}` : ""}：${priceAmount(r)}`).join("；");
}

export function ModelPriceDetails({model, vendor, stale = false}: {model?: CatalogModel; vendor: Vendor; stale?: boolean}) {
  const pricing = model?.pricing;
  return <div className="model-pricing" aria-label="模型价格">
    <strong>模型参考价格</strong>
    {vendor.type === "cli" ? <p>输入 / 输出：价格待确认。使用本机 CLI 账户，费用取决于账户套餐与实际调用；未读取账户账单，不按免费计算。</p> : !pricing?.rows.length ?
      <p>输入 / 输出：价格待确认。此模型暂无可核实的单价，请查看厂商价目或合同，制作前仍需确认报价。</p> : <>
      <dl>{pricing.rows.map((row, index) => <div key={index}><dt>{row.label}{row.condition && <small>{row.condition}</small>}</dt><dd>{priceAmount(row)}</dd></div>)}</dl>
      <p>{pricing.note}</p>
      <div className="model-price-source"><a href={pricing.url} target="_blank" rel="noreferrer">{pricing.source}</a><span>核对日期：{pricing.checked}{stale ? " · 目录缓存已过期" : ""}</span></div>
    </>}
  </div>;
}
