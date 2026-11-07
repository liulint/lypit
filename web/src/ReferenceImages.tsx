import {media,type Project,type Shot,type ReferencePlan} from "./api";
import {Button} from "antd";
import {ZoomImage} from "./ZoomImage";

export function ReferenceImages({p,q,compact=false,onMore}:{p:Project;q:Shot;compact?:boolean;onMore?:()=>void}) {
 const ids=compact?q.references.slice(0,3):q.references;
 return <div className={`plan-reference-previews${compact?" plan-reference-compact":""}`} aria-label="引用素材预览">
 {ids.map(id=>{const a=p.assets.find(a=>a.id===id);if(!a)return null;
 const sheet=a.kind==="character"&&a.sheet?.approved&&a.views.length===4&&a.views.every(v=>v.approved)?a.sheet:undefined;
 return <figure key={id}>
 {a.master?.file?<ZoomImage src={media(a.master.file)} alt={`${a.name}主图`} loading="lazy"/>:<span>暂无主图</span>}
 {sheet&&<ZoomImage src={media(sheet.file)} alt={`${a.name}四视角设定图`} loading="lazy"/>}
 <figcaption>{a.name}<small>{sheet?"主图＋四视角":"主图"}</small></figcaption>
 </figure>;})}
 {compact&&q.references.length>3&&<Button type="text" size="small" onClick={onMore}>+{q.references.length-3}份</Button>}
 </div>;
}

export function ReferencePlanImages({plan}:{plan:ReferencePlan}) {
 return <div className="reference-plan-images">
 <p>{plan.sources.length}份原图 → {plan.boards.length}张实际输入图片；仅生成1张分镜。不裁切、不缩放，合板不产生模型费用。</p>
 <div className="plan-reference-previews">{plan.sources.map((s,i)=><figure key={i}><ZoomImage src={media(s.image.file)} alt={s.name} loading="lazy"/><figcaption>{s.name}<small>{s.width}×{s.height}</small></figcaption></figure>)}</div>
 {plan.boards.map((b,i)=><div key={i} className="reference-board-preview"><strong>实际输入 {i+1} · {b.width}×{b.height}</strong>{b.file&&<ZoomImage src={media(b.file)} alt={`实际输入参考板${i+1}`} loading="lazy"/>}<p>{b.cells.map(c=>`${plan.sources[c.source].name}：(${c.x},${c.y})`).join("；")}</p><small>SHA-256：{b.hash}</small></div>)}
 <small>原图、坐标与参考板哈希均与报价绑定；变化后须重新报价。</small>
 </div>;
}
