import {useState} from "react";
import {Alert,Button,Card,Modal,Select,Space,Tag} from "antd";
import {media} from "./api";
import type {Entry,FrameSelection,Media,VideoOptions,StartContext} from "./drama";

import DramaVideoStart,{useVideoStarts,VideoStartStatus} from "./DramaVideoStart";
import DramaVideoOptions from "./DramaVideoOptions";

type Props={startContext?:StartContext;entries:Entry[];medias:Media[];aspect:string;busy:boolean;cancel:()=>void;goBoards:()=>void;quote:(frames:FrameSelection[],options?:VideoOptions)=>Promise<void>;adapt:(m:Media)=>Promise<Media>;approve:(m:Media)=>Promise<Media>;run:(fn:()=>Promise<void>)=>Promise<void>};
export default function DramaVideoBatchQuote({entries,medias,aspect,busy,cancel,goBoards,quote,adapt,approve,run,startContext}:Props){
 const starts=useVideoStarts(startContext);
 const [options,setOptions]=useState<VideoOptions>();
 const [choices,setChoices]=useState<Record<string,{id:string;hash:string}>>(()=>Object.fromEntries(entries.map(e=>{const m=medias.find(m=>m.kind==="image"&&m.entry===e.id.replace(/^MOTION-/,"SHOT-")&&m.current&&m.approved&&!m.reference_only);return [e.id,{id:m?.id||"",hash:m?.hash||""}]})));
 const [local,setLocal]=useState<Media[]>([]);
 const [ratios,setRatios]=useState<Record<string,number>>({});
 const target=aspect==="16:9"?16/9:9/16;
 const rows=entries.map(entry=>{
  const frames=[...local,...medias.filter(m=>!local.some(v=>v.id===m.id))].filter(m=>m.kind==="image"&&m.entry===entry.id.replace(/^MOTION-/,"SHOT-")&&m.current&&!m.reference_only&&(m.approved||local.some(v=>v.id===m.id)));
  const plan=starts.plans.find(p=>p.entry===entry.id);const tail=plan?.mode==="tail";
  const boardSelected=frames.find(m=>m.id===choices[entry.id]?.id&&m.hash===choices[entry.id]?.hash);
  const selected=tail?plan.frame:boardSelected;
  const waiting=!!startContext&&(!plan||tail&&!!plan.waiting);
  const ratio=selected?ratios[selected.id+":"+selected.hash]:undefined;
  return {entry,frames,selected,plan,tail,waiting,loading:!!selected&&ratio===undefined,mismatch:ratio!==undefined&&Math.abs(ratio-target)>0.02};
 });
 const readyRows=rows.filter(r=>r.selected?.approved&&!r.waiting&&!r.loading&&!r.mismatch);
 const ready=!starts.loading&&!starts.error&&readyRows.length>0&&(!!startContext||readyRows.length===rows.length);
 const submitFrames=()=>readyRows.map(r=>({entry:r.entry.id,media_id:r.selected!.id,hash:r.selected!.hash,...(r.tail&&r.plan?.source?{tail_video_id:r.plan.source.id,tail_video_hash:r.plan.source.hash}:{})}));
 function choose(entry:string,m:Media){setChoices(v=>({...v,[entry]:{id:m.id,hash:m.hash}}))}
 function remember(m:Media){setLocal(v=>[m,...v.filter(x=>x.id!==m.id)])}
 return <Modal open width={900} title="全部视频 · 检查首帧与制作范围" onCancel={cancel} styles={{body:{maxHeight:"65vh",overflowY:"auto"}}} footer={<Space><Button onClick={cancel}>取消</Button><Button type="primary" loading={busy} disabled={busy||!ready||!!options&&!options.resolution} onClick={()=>ready&&void run(()=>options?quote(submitFrames(),options):quote(submitFrames()))}>统一查看 {startContext?readyRows.length:entries.length} 镜视频报价</Button></Space>}>
  <Alert showIcon type="info" title={`本次补齐 ${entries.length} 镜视频，共 ${entries.reduce((n,e)=>n+(parseFloat(e.fields["时长"])||0),0)} 秒`} description={startContext?"系统推荐每镜开场输入，你可在报价前切换。只为已就绪镜头报价；依赖上一镜的镜头等待其生成并确认后，再继续报价。已有视频跳过。":"每镜使用下方已确认的分镜图作为首帧，已有视频跳过。这里只查看统一报价，确认报价后才开始生成；生成后仍需逐镜检查。"}/>
  <VideoStartStatus loading={starts.loading} error={starts.error} reload={()=>void starts.reload()}/>
  {startContext&&<p>当前可报价 {readyRows.length} 镜，待就绪 {rows.length-readyRows.length} 镜。选择会保存；完成并确认前镜后，继续点击“一键全部生成”处理下一批。</p>}
  <DramaVideoOptions value={options} onChange={setOptions} busy={busy}/>
  {rows.map(({entry,frames,selected,loading,mismatch,plan,tail,waiting},i)=><Card key={entry.id} size="small" style={{marginTop:12}} title={`${i+1}. ${entry.title} · ${entry.fields["时长"]}`}>
   {plan&&<DramaVideoStart showPreview={false} plan={plan} busy={busy||starts.loading} change={(entry,mode)=>void starts.change(entry,mode)}/>}
   {waiting?<p>此镜不在本轮报价范围内，等待依赖就绪。</p>:!selected?<Alert type="warning" title="缺少当前已确认的本镜分镜图，或所选版本已变化" action={<Button onClick={goBoards}>去确认分镜画面</Button>}/>:<>
    <img src={media(selected.file)} alt={`首帧 ${i+1}`} style={{maxWidth:"100%",height:160,objectFit:"contain",display:"block"}} onLoad={e=>{const im=e.currentTarget;if(im.naturalHeight)setRatios(v=>({...v,[selected.id+":"+selected.hash]:im.naturalWidth/im.naturalHeight}))}} onError={()=>setRatios(v=>({...v,[selected.id+":"+selected.hash]:0}))}/>
    <Tag color={selected.approved?"success":"warning"}>{selected.approved?"已确认画面":"新版本待检查确认"}</Tag>
    {loading&&<span>正在读取首帧尺寸</span>}
    {mismatch&&tail&&<Alert type="warning" title="上一镜尾帧画幅不符，请复核来源视频或选择本镜分镜图"/>}
    {mismatch&&!tail&&<Alert type="warning" title={`首帧画幅不符，需要 ${aspect}`} description="本地留边保留全部原画面，不裁切、不调用模型。新版本检查确认后才能用于报价。" action={<Button disabled={busy} onClick={()=>void run(async()=>{const m=await adapt(selected);remember(m);choose(entry.id,m)})}>本地留边适配</Button>}/>}
    {!selected.approved&&<Button disabled={busy||loading||mismatch} onClick={()=>void run(async()=>remember(await approve(selected)))}>看过了，确认此首帧</Button>}
   </>}
   {!tail&&frames.length>1&&<Select aria-label={`镜头 ${i+1} 首帧版本`} value={selected?.id} style={{width:"100%",marginTop:8}} options={frames.map((m,n)=>({value:m.id,label:`画面版本 ${n+1}${m.approved?" · 已确认":" · 待确认"}`}))} onChange={id=>{const m=frames.find(m=>m.id===id);if(m)choose(entry.id,m)}}/>}
  </Card>)}
 </Modal>;
}
