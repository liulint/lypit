import {useEffect,useRef,useState} from "react";
import {Alert,Button,Modal,Space,Tag} from "antd";
import {media} from "./api";
import type {Entry,Media} from "./drama";

type Props={entries:Entry[];medias:Media[];entryID:string;change:(id:string)=>void;cancel:()=>void;busy:boolean;run:(fn:()=>Promise<void>)=>Promise<void>;approve:(m:Media)=>Promise<void>};
export default function DramaVideoPreview({entries,medias,entryID,change,cancel,busy,run,approve}:Props){
 const shots=entries.flatMap((entry,index)=>{
  const matches=medias.filter(m=>m.entry===entry.id&&m.kind==="video");
  const item=matches.find(m=>m.selected)||matches.find(m=>m.current)||matches[0];
  return item?[{entry,item,number:index+1}]:[];
 });
 const index=shots.findIndex(s=>s.entry.id===entryID),shot=shots[index];
 const pending=useRef(false);const [confirming,setConfirming]=useState(false);const [error,setError]=useState("");
 useEffect(()=>setError(""),[shot?.item.id,shot?.item.hash]);
 async function confirm(){
  if(!shot||busy||pending.current||!shot.item.current||shot.item.approved)return;
  const item=shot.item;pending.current=true;setConfirming(true);setError("");
  try{await run(async()=>{try{await approve(item)}catch(e){setError(e instanceof Error?e.message:"确认失败，请重试");throw e}})}finally{pending.current=false;setConfirming(false)}
 }
 return <Modal open title={shot?`镜头 ${shot.number} · ${shot.entry.title}`:"视频预览"} width="min(960px, 94vw)" centered onCancel={cancel} footer={<div className="guide-video-footer"><Space wrap><Button disabled={index<=0||confirming} onClick={()=>change(shots[index-1].entry.id)}>上一镜</Button><Button disabled={index<0||index>=shots.length-1||confirming} onClick={()=>change(shots[index+1].entry.id)}>下一镜</Button><span>{index>=0?`${index+1} / ${shots.length} 个已有视频`:""}</span></Space><Button type="primary" loading={confirming} disabled={busy||!shot?.item.current||shot?.item.approved} onClick={()=>void confirm()}>{shot?.item.approved&&shot.item.current?"已确认使用":"确认使用此视频"}</Button></div>}>
  {error&&<Alert type="error" showIcon title={error}/>}
  {shot?<><PreviewPlayer key={`${shot.item.id}:${shot.item.hash}:${shot.item.file}`} item={shot.item}/><Space wrap><Tag color={shot.item.current?shot.item.approved?"success":"default":"warning"}>{!shot.item.current?"来源变化，需重新制作":shot.item.approved?"已确认使用":"待检查确认"}</Tag>{shot.item.mock&&<Tag>模拟示意 / 静音</Tag>}<span>{shot.entry.fields["时长"]} · {shot.item.video_info?.model||"模型未知"} · {shot.item.video_info?.resolution||"清晰度未知"}</span></Space></>:<Alert type="warning" title="此镜头的视频已不可用，请关闭后重新选择。"/>}
 </Modal>;
}
function PreviewPlayer({item}:{item:Media}){
 const ref=useRef<HTMLVideoElement>(null);const [failed,setFailed]=useState(false);
 useEffect(()=>{const video=ref.current;return ()=>{video?.pause()}},[]);
 return <>{failed&&<Alert type="error" title="视频加载失败，请关闭后重试或检查原文件。"/>}<video ref={ref} className="guide-video-player" aria-label="大预览视频" controls autoPlay playsInline preload="metadata" src={media(item.file)} onError={()=>setFailed(true)}/></>;
}
