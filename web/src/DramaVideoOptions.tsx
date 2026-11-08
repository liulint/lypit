import {useEffect,useState} from "react";
import {Alert,Form,Select} from "antd";
import {api,type ModelPricing} from "./api";
import type {VideoOptions} from "./drama";

type Option=VideoOptions&{vendor:string;resolutions:string[];pricing?:ModelPricing};
export default function DramaVideoOptions({value,onChange,busy}:{value?:VideoOptions;onChange:(v:VideoOptions)=>void;busy:boolean}){
 const [options,setOptions]=useState<Option[]>([]);const [error,setError]=useState("");const [loading,setLoading]=useState(true);
 useEffect(()=>{let alive=true;void api<{options:Option[];default:VideoOptions}>("/drama/video-options").then(v=>{if(!alive)return;setOptions(v.options);if(!value){const initial=v.options.find(o=>o.vendor_id===v.default.vendor_id&&o.model===v.default.model);if(!initial&&v.options.length)setError("当前默认模型不在兼容列表中，请明确选择本次厂商与模型。");if(initial)onChange({vendor_id:initial.vendor_id,model:initial.model,resolution:"480p"})}}).catch(e=>{if(alive)setError(String(e))}).finally(()=>{if(alive)setLoading(false)});return ()=>{alive=false}},[]);
 const chosen=options.find(o=>o.vendor_id===value?.vendor_id&&o.model===value?.model);
 const vendors=Array.from(new Map(options.map(o=>[o.vendor_id,{value:o.vendor_id,label:o.vendor}])).values());
 function choose(o:Option){const resolution=value?.resolution&&!o.resolutions.includes(value.resolution)?"":value?.resolution||"480p";setError(resolution?"":"此模型不支持原清晰度，请主动选择新的清晰度后查看报价。");onChange({vendor_id:o.vendor_id,model:o.model,resolution})}
 return <><p>仅作用于本次报价；沿用本镜画幅、时长、提示词与声音要求。</p>{error&&<Alert type="error" title={error}/>}{!options.length&&!error&&<Alert type="info" title={loading?"正在读取兼容模型":"尚无兼容视频模型，请先到服务配置绑定视频API厂商。"}/>}<Form layout="vertical">
  <Form.Item label="本次厂商"><Select aria-label="本次视频厂商" disabled={busy} value={value?.vendor_id} options={vendors} onChange={id=>{const o=options.find(o=>o.vendor_id===id);if(o)choose(o)}}/></Form.Item>
  <Form.Item label="本次模型"><Select aria-label="本次视频模型" disabled={busy} value={value?.model} options={options.filter(o=>o.vendor_id===value?.vendor_id).map(o=>({value:o.model,label:`${o.model} · ${o.pricing?"有参考价":"参考价未知"}`}))} onChange={id=>{const o=options.find(o=>o.vendor_id===value?.vendor_id&&o.model===id);if(o)choose(o)}}/></Form.Item>
  <Form.Item label="本次清晰度"><Select aria-label="本次视频清晰度" disabled={busy} value={value?.resolution} options={chosen?.resolutions.map(r=>({value:r,label:r}))} onChange={resolution=>{setError("");if(value)onChange({...value,resolution})}}/></Form.Item>
 </Form>{chosen?.pricing?<div><p>{chosen.pricing.rows.map(r=>`${r.condition||r.label}：${r.currency||"币种未知"} ${r.amount}/${r.unit}`).join("；")}</p><p><a href={chosen.pricing.url} target="_blank" rel="noreferrer">{chosen.pricing.source}</a> · 核对日期 {chosen.pricing.checked}</p><p>{chosen.pricing.note}</p></div>:<p>参考价未知，以确认页的费用说明及供应商实际账单为准。</p>}</>;
}
