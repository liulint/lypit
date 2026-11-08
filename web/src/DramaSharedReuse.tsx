import {useState} from "react";
import {Alert,Button,Checkbox,Empty,Modal,Select,Space,Table} from "antd";
import {api,media} from "./api";
import {operationID,type Work} from "./drama";
import {episodeCode,type Series} from "./DramaSeries";
type ReuseRequest={operation_id:string;version:number;series_hash:string;media_ids:string[];work_ids:string[]};
type ReuseResult={work_id:string;state:string;error?:string;media_ids:string[]};
export default function DramaSharedReuse({series,versions,episodes,refresh,disabled}:{series:Series;versions:Series[];episodes:Work[];refresh:()=>Promise<void>;disabled:boolean}){
 const [open,setOpen]=useState(false);const [version,setVersion]=useState(series.version);const [selected,setSelected]=useState<string[]>([]);const [targets,setTargets]=useState<string[]>([]);const [request,setRequest]=useState<ReuseRequest>();const [results,setResults]=useState<ReuseResult[]>([]);const [busy,setBusy]=useState(false);const [error,setError]=useState("");const chosen=versions.find(v=>v.version===version)||series;
 function start(){setVersion(series.version);setSelected([]);setTargets([]);setRequest(undefined);setResults([]);setError("");setOpen(true)}
 async function submit(){const req=request||{operation_id:operationID(),version:chosen.version,series_hash:chosen.hash,media_ids:selected,work_ids:targets};setRequest(req);setBusy(true);setError("");try{const res=await api<{results:ReuseResult[]}>(`/drama/series/${series.id}/reuse-media`,req);setResults(res.results);await refresh()}catch(e){setError(e instanceof Error?e.message:String(e))}finally{setBusy(false)}}
 const done=results.length>0&&results.every(r=>r.state==="completed");
 return <><Button disabled={disabled} onClick={start}>把共用素材复用到分集</Button><Modal title="把共用素材复用到已有分集" open={open} width={760} onCancel={()=>{if(!busy)setOpen(false)}} closable={!busy} mask={{closable:!busy}} confirmLoading={busy} okText={done?"完成":request?"继续同一次复用":"确认复用，不调用模型"} okButtonProps={{disabled:!done&&(!selected.length||!targets.length)}} onOk={()=>done?setOpen(false):void submit()}>
 <p>把已确认素材复制为各集自己的文件。保留原图、原引用和已有制作任务，不改变各集原来的共用设定版本。</p>{error&&<Alert type="error" title={error} description="可继续同一次复用，不会重新生成图片或重复添加已完成副本。"/>}
 <p>选择来源版本</p><Select aria-label="来源共用版本" disabled={!!request} value={version} options={versions.map(v=>({value:v.version,label:`版本 ${v.version} · ${v.media.length}份素材`}))} onChange={v=>{setVersion(v);setSelected([])}} style={{width:"100%"}}/>
 <p>选择要复用的素材</p>{!chosen.media.length?<Empty description="本版还没有共用素材。先在分集里确认角色或场景图，点击“设为系列共用”；也可在素材库导入、确认并保存共用版本。"/>:<div className="shared-reuse-preview">{chosen.media.map(m=><label key={m.id}><Checkbox disabled={!!request} checked={selected.includes(m.id)} onChange={e=>setSelected(e.target.checked?[...selected,m.id]:selected.filter(id=>id!==m.id))}/>{m.kind==="image"?<img src={media(m.file)} alt={m.entry}/>:<audio controls src={media(m.file)}/>}<p>{m.entry}</p></label>)}</div>}
 <p>选择接收素材的分集</p><Space orientation="vertical">{episodes.map(w=><Checkbox key={w.id} disabled={!!request} checked={targets.includes(w.id)} onChange={e=>setTargets(e.target.checked?[...targets,w.id]:targets.filter(id=>id!==w.id))}>{episodeCode(w)} · {w.title}</Checkbox>)}</Space>
 {!!results.length&&<Table<ReuseResult> rowKey="work_id" pagination={false} dataSource={results} columns={[{title:"分集",render:(_,r)=>{const w=episodes.find(w=>w.id===r.work_id);return w?`${episodeCode(w)} · ${w.title}`:r.work_id}},{title:"结果",render:(_,r)=>r.state==="completed"?`已就绪 · ${r.media_ids.length}份素材`:r.error||"等待处理"}]}/>}
 </Modal></>
}
