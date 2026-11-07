import {useEffect,useState} from "react";
import {Alert,Button,Input,Modal,Space} from "antd";
import "./shot-content.css";
import {api,type Project,type Shot,type ShotContent,type ShotRewriteResult,type ShotDurationPolicy} from "./api";

type Props={p:Project;q:Shot;trial:boolean;duration?:number;taskID?:string;busy:boolean;close:()=>void;save:(content:ShotContent,duration:number,settingsVersion:number)=>Promise<boolean>};
export function ShotContentDialog({p,q,trial,duration=q.duration,taskID,busy,close,save}:Props){
 const [openedRevision]=useState(p.revision);
 const [content,setContent]=useState<ShotContent>({first_frame:q.first_frame??"",description:q.description,narration:q.narration});
 const [suggestion,setSuggestion]=useState<ShotRewriteResult>();
 const [info,setInfo]=useState<{policy:ShotDurationPolicy;blocked:boolean;rejected_tasks:string[]}>();
 const [error,setError]=useState("");
 const [loading,setLoading]=useState(true);
 const [retry,setRetry]=useState(0);
 useEffect(()=>{let live=true;setLoading(true);setError("");setInfo(undefined);Promise.all([
  api<{policy:ShotDurationPolicy;blocked:boolean;rejected_tasks:string[]}>(`/projects/${p.id}/shots/${q.id}/duration${trial?"?plan_trial=true":""}`),
  taskID?api<ShotRewriteResult>(`/tasks/${taskID}/rewrite`):Promise.resolve(undefined)
 ]).then(([policy,result])=>{if(!live)return;setInfo(policy);if(result){setSuggestion(result);setContent(result.after);}}).catch(e=>{if(live)setError(e instanceof Error?e.message:"读取失败");}).finally(()=>{if(live)setLoading(false);});return()=>{live=false};},[p.id,q.id,trial,taskID,retry]);
 const target=suggestion?.duration??duration;
 const stale=openedRevision!==p.revision || !!suggestion&&(suggestion.revision!==p.revision || !!info&&suggestion.settings_version!==info.policy.settings_version);
 const valid=!!content.first_frame.trim()&&!!content.description.trim()&&target>0;
 return <Modal className="shot-content-modal" open centered width={960} styles={{body:{maxHeight:"72dvh",overflowY:"auto"}}} title={taskID?"对比单镜改写建议":"编辑首帧、动作与台词"} onCancel={()=>{if(!busy)close()}} footer={<Space><Button disabled={busy} onClick={close}>取消</Button><Button type="primary" loading={busy} disabled={loading||!info||stale||!valid||info.blocked||!!info.rejected_tasks.length||p.archived||!!suggestion?.applied_revision} onClick={()=>void(async()=>{setError("");try{if(info&&await save(content,target,info.policy.settings_version))close()}catch(e){setError(e instanceof Error?e.message:"保存失败")}})()}>确认应用</Button></Space>}>
  <p>{q.title} · {suggestion?.old_duration??q.duration} → {target} 秒</p>
  {loading&&<p>正在读取内容…</p>}
  {stale&&<Alert type="warning" title="作品或模型配置已变化，这份内容无法直接应用；请关闭后重新编辑或报价改写。"/>}
  {(info?.blocked||!!info?.rejected_tasks.length)&&<Alert type="warning" title="请先恢复或处理当前任务，再应用内容修改。"/>}
  <Alert type="info" title="保留当前图片，应用后须重新确认图文；需要改图时另行报价。" description="本镜旧视频文件保留到历史，当前视频关联、分镜锁定、统一旁白和成片将失效。其他镜头正文不变。动作是否自然演完，仍需视频预览确认。"/>
  <div className={`shot-content-grid${taskID?" comparison":""}`}>
   {suggestion&&<section aria-label="原内容"><strong>原内容 · {suggestion.old_duration} 秒</strong><p>首帧：{suggestion.before.first_frame}</p><p>动作：{suggestion.before.description}</p><p>{p.audio_mode==="dialogue"?"台词":"旁白"}：{suggestion.before.narration||"无"}</p></section>}
   <section aria-label="编辑内容"><strong>{taskID?"改写建议（可继续编辑）":"本镜内容"} · {target} 秒</strong>
    <label>静态首帧<Input.TextArea aria-label="静态首帧" rows={4} maxLength={16000} disabled={busy||loading} value={content.first_frame} onChange={e=>setContent(v=>({...v,first_frame:e.target.value}))}/></label>
    <label>视频动作与运镜<Input.TextArea aria-label="视频动作与运镜" rows={5} maxLength={16000} disabled={busy||loading} value={content.description} onChange={e=>setContent(v=>({...v,description:e.target.value}))}/></label>
    <label>{p.audio_mode==="dialogue"?"角色台词（每行角色名：台词）":"旁白"}<Input.TextArea aria-label="台词或旁白" rows={3} maxLength={1000} disabled={busy||loading} value={content.narration} onChange={e=>setContent(v=>({...v,narration:e.target.value}))}/></label>
    <p className="muted">台词容量按每秒最多6个汉字或字母校验；角色须来自本镜引用素材。保存本次手动修改不调用模型。</p>
   </section>
  </div>
  {error&&<Alert type="error" title={error} action={<Button onClick={()=>setRetry(v=>v+1)}>重新读取</Button>}/>}
 </Modal>
}
