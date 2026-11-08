import {useState} from "react";
import {Alert,Button,Collapse,Descriptions,Empty,Modal,Radio,Space} from "antd";
import {media} from "./api";
import type {Doc,Entry,Media,VideoOptions,StartContext,TailStart} from "./drama";
import DramaVideoOptions from "./DramaVideoOptions";
import DramaVideoStart,{useVideoStarts,VideoStartStatus} from "./DramaVideoStart";
import DramaReading from "./DramaReading";

export default function DramaVideoFrameQuote({entry,storyboard,script,medias,busy,cancel,goBoards,quote,run,aspect,adapt,approve,startContext}:{entry:Entry;storyboard?:Entry;script?:Doc;medias:Media[];busy:boolean;cancel:()=>void;goBoards:()=>void;quote:(frame:Media,options?:VideoOptions,tail?:TailStart)=>Promise<void>;startContext?:StartContext;run:(fn:()=>Promise<void>)=>Promise<void>;aspect?:string;adapt?:(m:Media)=>Promise<Media>;approve?:(m:Media)=>Promise<Media>}){
 const starts=useVideoStarts(startContext);const plan=starts.plans.find(p=>p.entry===entry.id);const tail=plan?.mode==="tail";
 const startBlocked=!!startContext&&(starts.loading||!!starts.error||!plan||tail&&(!plan.frame||!!plan.waiting));
 const [options,setOptions]=useState<VideoOptions>();
 const [selection,setSelection]=useState("");const [derived,setDerived]=useState<Media>();const [sizes,setSizes]=useState<Record<string,number>>({});
 const available=medias.filter(m=>m.entry===entry.id.replace(/^MOTION-/,"SHOT-")&&m.kind==="image"&&m.current&&m.approved&&!m.reference_only);
 const frames=derived?[...available.filter(m=>m.id!==derived.id),derived]:available;
 const boardChosen=frames.find(m=>m.id===selection)||(frames.length===1?frames[0]:undefined);
 const chosen=tail?plan?.frame:boardChosen;
 const target=aspect==="16:9"?16/9:9/16;const mismatch=!!chosen&&sizes[chosen.id]!==undefined&&Math.abs(sizes[chosen.id]-target)>0.02;
 const sourceIDs=new Set(storyboard?.fields["来源"]?.match(/EP\d+-SC\d+/g)||[]);
 const scenes=script?.entries.filter(scene=>sourceIDs.has(scene.id))||[];
 const details=["起点","唯一动作","终点","声音","台词"].filter(key=>storyboard?.fields[key]);
 return <Modal open centered width="80vw" styles={{container:{height:"80dvh",display:"flex",flexDirection:"column"},header:{flexShrink:0},body:{flex:1,minHeight:0,overflowY:"auto",overscrollBehavior:"contain"},footer:{flexShrink:0}}} title={startContext?"选择视频开场画面":"选择本镜视频首帧"} onCancel={cancel} footer={<Space><Button onClick={cancel}>取消</Button><Button type="primary" loading={busy} disabled={startBlocked||!chosen||!chosen.approved||mismatch||busy||!!options&&!options.resolution} onClick={()=>chosen&&chosen.approved&&!mismatch&&!startBlocked&&void run(()=>tail&&plan?.source?quote(chosen,options,{tail_video_id:plan.source.id,tail_video_hash:plan.source.hash}):options?quote(chosen,options):quote(chosen))}>查看视频报价</Button></Space>}>
  <VideoStartStatus loading={starts.loading} error={starts.error} reload={()=>void starts.reload()}/>
  {plan&&<DramaVideoStart plan={plan} busy={busy||starts.loading} change={(entry,mode)=>void starts.change(entry,mode)}/>}
  <DramaVideoOptions value={options} onChange={setOptions} busy={busy}/>
  <p>{entry.title} · {entry.fields["时长"]}</p>
  <Alert type="info" showIcon title={tail?"使用上一镜真实尾帧作为首帧":"使用已经确认的分镜图，无需重新导入"} description="这里仅选择首帧并查看报价；确认报价后才生成视频，不改写已有分镜或视频说明。"/>
  <div className="video-frame-review"><div>
  {tail?<p>{plan?.frame?"请核对上方真实尾帧与本镜起点，确认后再查看报价。":"等待上一镜生成并确认，或切换为本镜分镜图。"}</p>:!frames.length?<Empty description="本镜没有当前已确认的图片，请先检查并确认画面。"><Button onClick={goBoards}>去确认分镜画面</Button></Empty>:<Radio.Group value={chosen?.id} onChange={e=>setSelection(e.target.value)}>{frames.map((m,i)=><label key={m.id} style={{display:"block",marginTop:16}}><Radio value={m.id}>本镜画面 {i+1}</Radio><img src={media(m.file)} alt={`本镜画面 ${i+1}`} onLoad={e=>{const image=e.currentTarget;if(image.naturalHeight)setSizes(v=>({...v,[m.id]:image.naturalWidth/image.naturalHeight}))}} style={{display:"block",maxWidth:"100%",maxHeight:"55dvh",marginTop:8}}/></label>)}</Radio.Group>}
  </div><section className="video-frame-script" aria-label="本镜剧本">
   <h3>本镜剧本</h3>
   {details.length?<Descriptions column={1} size="small" items={details.map(key=>({key,label:key,children:<span style={{whiteSpace:"pre-wrap"}}>{storyboard!.fields[key]}</span>}))}/>:<p>暂无对应分镜的剧情内容，请核对下方视频说明。</p>}
   {scenes.length>0?<Collapse size="small" defaultActiveKey={["source"]} items={[{key:"source",label:"对应原剧本场景",children:scenes.map(scene=><DramaReading key={scene.id} text={scene.body}/>)}]}/>:<p>暂无可对应的原剧本场景。</p>}
   {entry.body.trim()&&<Collapse size="small" items={[{key:"motion",label:"查看本镜视频说明",children:<DramaReading text={entry.body}/>} ]}/>}
  </section></div>
  {!tail&&mismatch&&chosen&&adapt&&<Alert type="warning" showIcon title={`这张图与作品 ${aspect||"9:16"} 画幅不同`} description="可在本地加留白，保留全部画面、不裁切、不调用模型。新版本需要你检查确认。" action={<Button disabled={busy} onClick={()=>void run(async()=>{const next=await adapt(chosen);setDerived(next);setSelection(next.id)})}>本地留边适配画幅</Button>}/>}
  {chosen&&!chosen.approved&&approve&&<Button disabled={busy} onClick={()=>void run(async()=>{const next=await approve(chosen);setDerived(next)})}>看过了，确认此首帧</Button>}
 </Modal>;
}
