import {useState} from "react";
import {Alert,Button,Card,Checkbox,Modal,Space,Select} from "antd";
import {media} from "./api";
import type {Entry,Media,VideoOptions,FrameSelection} from "./drama";
import DramaVideoOptions from "./DramaVideoOptions";

export default function DramaVideoTrial({entries,medias,batch,busy,cancel,quote,run}:{entries:Entry[];medias:Media[];batch:boolean;busy:boolean;cancel:()=>void;quote:(entries:string[],options:VideoOptions,frames:FrameSelection[])=>Promise<void>;run:(fn:()=>Promise<void>)=>Promise<void>}){
 const [frames,setFrames]=useState<Record<string,string>>(()=>Object.fromEntries(entries.map(e=>[e.id,medias.find(m=>m.kind==="image"&&m.entry===e.id.replace(/^MOTION-/,"SHOT-")&&m.current&&m.approved&&!m.reference_only)?.id||""])));
 const [selected,setSelected]=useState<string[]>(batch?[]:entries.map(e=>e.id));const [options,setOptions]=useState<VideoOptions>();
 return <Modal open title={batch?"批量试一版":"换模型 / 清晰度试一版"} width={850} onCancel={cancel} styles={{body:{maxHeight:"70vh",overflowY:"auto"}}} footer={<Space><Button onClick={cancel}>取消</Button><Button type="primary" loading={busy} disabled={busy||!options?.resolution||!selected.length} onClick={()=>options&&void run(()=>quote(selected,options,selected.flatMap(entry=>{if(medias.some(m=>m.kind==="video"&&m.entry===entry&&m.selected))return [];const m=medias.find(m=>m.id===frames[entry]);return m?[{entry,media_id:m.id,hash:m.hash}]:[]})))}>查看 {selected.length} 镜报价</Button></Space>}>
  <Alert type="info" showIcon title="新版本作为候选，比较后再选用" description="有视频的镜头复用该选用版本的原始生成说明与参考输入；无视频的镜头按当前已确认分镜生成。旧视频继续保留，报价确认前不调用模型。"/>
  <DramaVideoOptions value={options} onChange={setOptions} busy={busy}/>
  {batch&&<Checkbox checked={selected.length===entries.length&&!!entries.length} indeterminate={!!selected.length&&selected.length<entries.length} disabled={busy} onChange={e=>setSelected(e.target.checked?entries.map(e=>e.id):[])}>选择全部镜头</Checkbox>}
  {entries.map(e=>{const m=medias.find(m=>m.entry===e.id&&m.kind==="video"&&m.selected);return <Card key={e.id} size="small" style={{marginTop:12}} title={<Checkbox checked={selected.includes(e.id)} disabled={!batch||busy} onChange={v=>setSelected(ids=>v.target.checked?[...ids,e.id]:ids.filter(id=>id!==e.id))}>{e.title} · {e.fields["时长"]}</Checkbox>}>{m?<><video controls preload="metadata" src={media(m.file)} style={{maxWidth:"100%",height:160}}/><p>沿用此版本输入 · {m.video_info?.model||"模型未知"} · {m.video_info?.resolution||"清晰度未知"}</p></>:<><p>尚无视频，使用当前视频说明及下方已确认首帧。</p><Select aria-label={`${e.title}试版首帧`} value={frames[e.id]||undefined} placeholder="选择已确认首帧" style={{width:"100%"}} options={medias.filter(m=>m.kind==="image"&&m.entry===e.id.replace(/^MOTION-/,"SHOT-")&&m.current&&m.approved&&!m.reference_only).map(m=>({value:m.id,label:m.path||m.id}))} onChange={id=>setFrames(v=>({...v,[e.id]:id}))}/>{frames[e.id]&&<img src={media(medias.find(m=>m.id===frames[e.id])!.file)} alt={`${e.title}试版首帧`} style={{height:140,maxWidth:"100%",objectFit:"contain"}}/>}</>}</Card>})}
  <p>本次 {selected.length} 镜，共 {entries.filter(e=>selected.includes(e.id)).reduce((n,e)=>n+(parseFloat(e.fields["时长"])||0),0)} 秒；下一步逐镜核对报价。</p>
 </Modal>;
}
