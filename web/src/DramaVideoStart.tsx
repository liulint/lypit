import {useEffect,useRef,useState} from "react";
import {Alert,Button,Select,Space,Spin} from "antd";
import {api,media} from "./api";
import type {StartContext,VideoStart} from "./drama";

export function useVideoStarts(context?:StartContext){
 const [plans,setPlans]=useState<VideoStart[]>([]),[loading,setLoading]=useState(!!context),[error,setError]=useState("");
 const sequence=useRef(0);
 async function load(choices?:{entry:string;mode:string}[]){
  if(!context)return;
  const token=++sequence.current;setLoading(true);setError("");
  try{const next=await api<VideoStart[]>(`/drama/works/${context.workId}/video-starts`,{motion_base:context.motionBase,board_base:context.boardBase,entry:context.entry,choices});if(!Array.isArray(next))throw new Error("开场输入响应无效，请重新读取");if(sequence.current===token)setPlans(next)}
  catch(e){if(sequence.current===token)setError(e instanceof Error?e.message:"读取开场画面失败")}
  finally{if(sequence.current===token)setLoading(false)}
 }
 useEffect(()=>{setPlans([]);void load();return()=>{sequence.current++}},[context?.workId,context?.motionBase,context?.boardBase,context?.entry]);
 return {plans,loading,error,change:(entry:string,mode:string)=>load([{entry,mode}]),reload:()=>load()};
}

export default function DramaVideoStart({plan,busy,change,showPreview=true}:{showPreview?:boolean;plan:VideoStart;busy:boolean;change:(entry:string,mode:string)=>void}){
 return <Space orientation="vertical" style={{width:"100%",marginBottom:16}}>
  <p>系统推荐：{plan.recommended==="tail"?"承接上一镜结尾":"使用本镜分镜图"}。{plan.reason}</p>
  <Select aria-label={`开场画面 ${plan.entry}`} style={{width:"100%",minWidth:240}} disabled={busy} value={plan.choice} options={[{value:"auto",label:"自动选择（按系统推荐）"},{value:"board",label:"使用本镜分镜图"},{value:"tail",label:"承接上一镜结尾",disabled:!plan.previous}]} onChange={mode=>change(plan.entry,mode)}/>
  {plan.mode==="tail"&&<>
   <Alert type={plan.waiting?"warning":"info"} showIcon title={plan.waiting||`来源：${plan.previous} · 当前已确认视频的真实尾帧`} description="本地提取，不调用生图模型；尾帧作为严格首帧，仅衔接开场画面，不额外传入上一段视频。"/>
   {showPreview&&plan.frame&&<img src={media(plan.frame.file)} alt={`上一视频真实尾帧 ${plan.entry}`} style={{display:"block",maxWidth:"100%",maxHeight:280}}/>}
  </>}
 </Space>;
}

export function VideoStartStatus({loading,error,reload}:{loading:boolean;error:string;reload:()=>void}){
 return <>{loading&&<Spin description="正在核对开场输入与镜头依赖"/>}{error&&<Alert type="error" showIcon title={error} action={<Button onClick={reload}>重新读取</Button>}/>}</>;
}
