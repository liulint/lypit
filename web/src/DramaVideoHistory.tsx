import {useRef,useState} from "react";
import {Alert,Button,Modal,Select,Space,Tag} from "antd";
import {media} from "./api";
import type {Media} from "./drama";

export default function DramaVideoHistory({versions,busy,cancel,select,run}:{versions:Media[];busy:boolean;cancel:()=>void;select:(m:Media,base:string)=>Promise<void>;run:(fn:()=>Promise<void>)=>Promise<void>}){
 const current=versions.find(m=>m.selected);const [left,setLeft]=useState(current?.id||versions[0]?.id);const [right,setRight]=useState(versions.find(m=>m.id!==left)?.id);const refs=useRef<(HTMLVideoElement|null)[]>([]);const [error,setError]=useState("");
 const opts=versions.map((m,i)=>({value:m.id,label:`版本 ${versions.length-i} · ${m.video_info?.model||"模型未知"} · ${m.video_info?.resolution||"清晰度未知"}${m.selected?" · 当前选用":""}`}));
 async function play(){setError("");const videos=refs.current.filter((v):v is HTMLVideoElement=>!!v);for(const v of videos)v.currentTime=0;try{await Promise.all(videos.map(v=>v.play()))}catch{for(const v of videos)v.pause();setError("无法一起播放，请分别使用视频播放器检查。")}}
 return <Modal open width="90vw" title="历史与对比" onCancel={cancel} footer={<Button onClick={cancel}>关闭</Button>} styles={{body:{maxHeight:"75vh",overflowY:"auto"}}}>
 <p>完整观看后选择用于成片的版本；选用、切回和播放均不调用模型。</p><Space><Button onClick={()=>void play()}>一起从头播放</Button><Button onClick={()=>refs.current.forEach(v=>v?.pause())}>一起暂停</Button></Space>{error&&<Alert type="warning" title={error}/>}
 <div style={{display:"flex",flexWrap:"wrap",gap:20,marginTop:16}}>{[left,right].map((id,i)=>{const m=versions.find(m=>m.id===id);return <section key={i} style={{flex:"1 1 300px",minWidth:0}}><Select aria-label={i?"右侧视频版本":"左侧视频版本"} value={id} options={opts} style={{width:"100%"}} onChange={v=>{refs.current.forEach(r=>r?.pause());(i?setRight:setLeft)(v)}}/>{m&&<><video ref={v=>{refs.current[i]=v}} key={m.id} controls preload="metadata" src={media(m.file)} style={{width:"100%",height:"45vh",marginTop:12}}/><p>{m.video_info?.vendor||"厂商未知"} · {m.video_info?.model||"模型未知"} · 请求 {m.video_info?.resolution||"未知"}{m.video_info?.width?` · 实际 ${m.video_info.width}×${m.video_info.height}`:""}</p><p>{m.video_info?.created||"生成时间未知"}{m.parent?` · 来源版本 ${m.parent}`:""}</p>{!m.current&&<Tag color="warning">来源变化，需重新制作</Tag>}<Button type={m.selected?"default":"primary"} disabled={busy||!m.current||m.selected&&m.approved} onClick={()=>void run(()=>select(m,current?.id||""))}>{m.selected&&m.approved?"当前选用":"看过了，确认选用此版本"}</Button></>}</section>})}</div>
 </Modal>;
}
