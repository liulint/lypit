import { useEffect, useState } from "react";
import { Alert, Button, Checkbox, InputNumber, Modal, Space } from "antd";
import { api, type Project, type Shot, type ShotDurationPolicy } from "./api";

type Info={policy:ShotDurationPolicy;rejected_tasks:string[];blocked:boolean};
export function ShotDurationDialog({p,q,trial,busy,close,save,editContent,rewrite}:{p:Project;q:Shot;trial:boolean;busy:boolean;close:()=>void;editContent?:(duration:number)=>void;rewrite?:(duration:number)=>Promise<boolean>;save:(duration:number,settingsVersion:number,finishRejected:boolean)=>Promise<boolean>}) {
 const [info,setInfo]=useState<Info>();
 const [duration,setDuration]=useState(q.duration);
 const [openedRevision]=useState(p.revision);
 const stale=p.revision!==openedRevision;
 const [finish,setFinish]=useState(false);
 const [error,setError]=useState("");
 const [loading,setLoading]=useState(true);
 const [reload,setReload]=useState(0);
 useEffect(()=>{let live=true;setLoading(true);setInfo(undefined);setError("");setFinish(false);
  api<Info>(`/projects/${p.id}/shots/${q.id}/duration${trial?"?plan_trial=true":""}`).then(v=>{if(live){setInfo(v);setDuration(current=>Math.max(v.policy.min,Math.min(v.policy.max,current)));}}).catch(e=>{if(live)setError(e instanceof Error?e.message:"读取时长限制失败");}).finally(()=>{if(live)setLoading(false);});
  return ()=>{live=false;};
 },[p.id,p.revision,q.id,trial,reload]);
 const shots=trial?p.planned_shots??[]:p.shots;
 const start=shots.slice(0,shots.findIndex(s=>s.id===q.id)).reduce((sum,s)=>sum+s.duration,0);
 const total=(p.duration??60)+duration-q.duration;
 const valid=!!info&&Number.isInteger(duration)&&duration>=info.policy.min&&duration<=info.policy.max&&total>=10&&total<=180;
 const rejected=!!info?.rejected_tasks.length;
 return <Modal open centered title="调整镜头时长" onCancel={()=>{if(!busy)close();}} width={760} footer={<Space>
  <Button disabled={busy} onClick={close}>取消</Button>
  {editContent&&<Button disabled={stale||loading||!valid||info?.blocked||rejected||busy||p.archived} onClick={()=>editContent(duration)}>手动编辑并调整</Button>}
  {rewrite&&<Button disabled={stale||loading||!valid||info?.blocked||rejected||busy||p.archived} onClick={()=>void(async()=>{if(await rewrite(duration))close()})()}>按新秒数重写 · 查看报价</Button>}
  <Button type="primary" loading={busy} disabled={stale||loading||!valid||duration===q.duration||info?.blocked||rejected&&!finish||p.archived} onClick={()=>void(async()=>{if(info&&await save(duration,info.policy.settings_version,finish))close();})()}>保存时长调整</Button>
 </Space>}>
  <p className="duration-shot-name">{q.title}</p>
  {stale&&<Alert type="warning" title="作品已发生变化，请关闭并重新打开调整面板，核对最新时长后再保存。"/>}
  {loading?<p>正在读取当前模型的时长范围…</p>:info&&<>
   <p className="muted">当前制作范围：{info.policy.min}～{info.policy.max} 秒{info.policy.model?` · ${info.policy.model}`:""}。</p>
   {!info.policy.known&&<Alert type="warning" title="此模型尚无已核定的时长限制，请先核对供应商要求；通用剪辑范围不能保证模型支持。"/>}
   <div className="duration-stepper"><Button disabled={busy||duration<=info.policy.min} onClick={()=>setDuration(Math.max(info.policy.min,duration-1))}>−1 秒</Button><InputNumber aria-label="镜头时长（秒）" disabled={busy} min={info.policy.min} max={info.policy.max} precision={0} value={duration} onChange={n=>setDuration(n??q.duration)}/><Button disabled={busy||duration>=info.policy.max} onClick={()=>setDuration(Math.max(info.policy.min,Math.min(info.policy.max,duration+1)))}>+1 秒</Button></div>
   {!valid&&<Alert type="error" title={`请输入${info.policy.min}～${info.policy.max}之间的整数秒，调整后的全片须为10～180秒。`}/>}
   <div className="duration-impact"><p>本镜：{q.duration} → <strong>{duration} 秒</strong>（{start}—{start+duration}秒）</p><p>全片：{p.duration??60} → <strong>{total} 秒</strong></p><p>后续镜头起止时间及剧本时间标记同步顺延，不改变其他镜头的时长、正文和台词。</p></div>
   <p>只保存秒数不会重写首帧、动作和台词。可手动编辑，或确认文字报价后重写本镜；建议生成后还需对比确认。</p>
   <Alert type="info" title="保留图片与台词，重新确认本镜后再报价生成视频。" description="旧视频文件保留，但不再作为本镜当前视频。受影响的续接视频须更新；分镜锁定、统一旁白和成片将失效。仅保存时长或手动编辑不调用模型；按新秒数重写须单独确认文字报价。"/>
   {info.blocked&&<Alert type="warning" title="仍有执行中或结果待核对的任务，请先在当前页恢复或结束它，再调整时长。"/>}
   {rejected&&<Checkbox disabled={busy} checked={finish} onChange={e=>setFinish(e.target.checked)}>同时结束本镜已明确被供应商拒绝的失败记录，保留原回执和历史；不重发原请求。</Checkbox>}
  </>}
  {error&&<Alert type="error" title={error} action={<Button size="small" onClick={()=>setReload(v=>v+1)}>重新读取</Button>}/>}
 </Modal>;
}
