import { ZoomImage } from "./ZoomImage";
import { useState } from "react";
import { Alert, Checkbox, Input, Select } from "antd";
import { Button, Field, Modal } from "./ui";
import { media, type Project, type Shot } from "./api";

export type EditImage = (shot:Shot,changes:string,preserve:string,referenceIDs:string[])=>void|Promise<boolean>;

export function EditImageDialog({p,q,busy,close,quote}:{p:Project;q:Shot;busy:boolean;close:()=>void;quote:EditImage}){
  const [changes,setChanges]=useState("");
  const [preserve,setPreserve]=useState("除明确要求修改的部分外，保持主体身份、数量、位置关系、构图、画幅、光线和整体风格不变。");
  const [references,setReferences]=useState<string[]>([]);
  const [base]=useState(q.candidates[q.selected]);
  const [baseIndex]=useState(q.selected);
  const [originalReferences]=useState(q.references.join("\u0000"));
  const referencesChanged=originalReferences!==q.references.join("\u0000");
  const baseChanged=q.selected!==baseIndex||q.candidates[q.selected]?.file!==base.file||q.candidates[q.selected]?.hash!==base.hash;
  return <Modal title="基于当前图修改" onClose={()=>{if(!busy)close();}} footer={<div className="dialog-actions">
    <Button disabled={busy} onClick={close}>取消</Button>
    <Button kind="primary" disabled={busy||baseChanged||referencesChanged||!base?.file||!changes.trim()||!preserve.trim()} onClick={()=>void(async()=>{if(await quote(q,changes.trim(),preserve.trim(),references)!==false)close();})()}>查看单张修改报价</Button>
  </div>}>
    <figure style={{margin:"0 0 16px",textAlign:"center"}}><ZoomImage src={media(base.file)} alt="独立编辑底图" style={{width:"100%",maxHeight:260,objectFit:"contain"}} /><figcaption>当前图独立作为唯一编辑底图，不放入参考拼板。</figcaption></figure>
    {referencesChanged && !baseChanged && <Alert type="warning" showIcon title="本镜引用素材已变化，请关闭后重新打开编辑，不会自动改变你的辅助素材选择。" />}
    {baseChanged && <Alert type="warning" showIcon title="当前底图版本已变化，请关闭后重新打开编辑，不会自动换用新底图。" />}
    <Field label="修改内容"><Input.TextArea autoFocus rows={3} maxLength={1000} showCount value={changes} disabled={busy} onChange={e=>setChanges(e.target.value)} placeholder="例如：只把墙上的合格证明移到猫身旁的地面。" /></Field>
    <Field label="必须保留的内容"><Input.TextArea rows={3} maxLength={2000} showCount value={preserve} disabled={busy} onChange={e=>setPreserve(e.target.value)} /></Field>
    <Field label="辅助参考素材（可选）"><div><div className="dialog-actions" style={{marginBottom:8}}><Button disabled={busy||referencesChanged||references.length===q.references.length} onClick={()=>setReferences([...q.references])}>全选</Button><Button disabled={busy||references.length===0} onClick={()=>setReferences([])}>全不选</Button><span className="muted">已选{references.length}/{q.references.length}份，默认全不选</span></div><Checkbox.Group value={references} disabled={busy} onChange={v=>setReferences(v as string[])} options={q.references.map(id=>{const a=p.assets.find(a=>a.id===id);return {value:id,label:`${({character:"角色",scene:"场景",prop:"道具"} as Record<string,string>)[a?.kind||""]||"素材"} · ${a?.name||id}`};})} /></div></Field>
    <p className="muted">辅助素材最多合成4张参考板，底图另占1张；可不选辅助素材。超限会停止报价，请主动选择，不会自动丢图或增加生成次数。</p>
    <p>本次只修改1张图。先查看并确认报价才调用模型；原图保留，成功后可对比、切回，并需重新确认画面。</p>
  </Modal>;
}

export function ImageHistoryDialog({q,busy,close,choose}:{q:Shot;busy:boolean;close:()=>void;choose:(index:number)=>void}){
  const versions=q.candidates.map((image,index)=>({image,index})).filter(v=>!!v.image.file);
  const newest=versions[versions.length-1].index;
  const initialRight=q.candidates[q.selected]?.operation==="edit"?q.selected:newest;
  const rightImage=q.candidates[initialRight];
  const parent=versions.find(v=>v.image.file===rightImage.source_file&&v.image.hash===rightImage.source_hash);
  const [left,setLeft]=useState(parent?.index??versions[Math.max(0,versions.findIndex(v=>v.index===initialRight)-1)].index);
  const [right,setRight]=useState(initialRight);
  const label=(index:number)=>{const version=versions.findIndex(v=>v.index===index)+1;const image=q.candidates[index];return `版本${version}${version===1?" · 初始图":image.operation==="edit"?" · 基于当前图修改":image.operation==="regenerate"?" · 重新生成":""}${index===q.selected?"（当前）":""}`;};
  const options=versions.map(v=>({value:v.index,label:label(v.index)}));
  const switchTo=(index:number)=>{choose(index);close();};
  return <Modal title="新旧对比与历史版本" onClose={close} footer={<div className="dialog-actions" style={{flexWrap:"wrap"}}>
    <Button onClick={close}>关闭</Button>
    <Button disabled={busy||left===q.selected} onClick={()=>switchTo(left)}>切回左侧版本</Button>
    <Button disabled={busy||right===q.selected} onClick={()=>switchTo(right)}>采用右侧版本</Button>
  </div>}>
    <div style={{display:"grid",gridTemplateColumns:"repeat(2,minmax(0,1fr))",gap:16}}>
      <div><Field label="左侧版本"><Select virtual={false} value={left} options={options} onChange={setLeft} /></Field><ZoomImage src={media(q.candidates[left].file)} alt="左侧对比图片" style={{width:"100%",maxHeight:360,objectFit:"contain"}} /></div>
      <div><Field label="右侧版本"><Select virtual={false} value={right} options={options} onChange={setRight} /></Field><ZoomImage src={media(q.candidates[right].file)} alt="右侧对比图片" style={{width:"100%",maxHeight:360,objectFit:"contain"}} /></div>
    </div>
    <p className="muted">原图及各版本文件均保留。切换只选择已保存图片，不调用模型、不计费；切换后需重新确认画面，当前视频会解除关联，旧视频文件仍保留。</p>
  </Modal>;
}
