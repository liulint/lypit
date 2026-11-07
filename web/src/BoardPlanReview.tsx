import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Alert, Button, Modal as AntModal, Input, InputNumber, Radio, Select, Table, Tag, Tooltip, type TableColumnsType } from "antd";
import { Check, ChevronLeft, ChevronRight, Pencil } from "lucide-react";
import { Field, Modal } from "./ui";
import { media, type Project, type Shot, type VideoMode, type ShotDurationPolicy } from "./api";
import { ReferenceImages } from "./ReferenceImages";
import { ZoomImage } from "./ZoomImage";

type ReviewStatus = NonNullable<Shot["review_status"]>;
const statusNames: Record<ReviewStatus,string> = {pending:"待审核",changes:"需修改",reviewed:"已审核"};
type PlanRow = {shot:Shot;index:number;start:number;end:number;issue:string};

function issueFor(q:Shot,start:number,p:Project) {
 if (!Number.isInteger(q.duration)||q.duration<1||q.duration>12) return "单镜时长须为1～12秒";
 if (!q.title.trim()||!q.first_frame?.trim()||!q.description.trim()) return "请补全名称、首帧和视频动作";
 if (!q.source_text?.trim()||!p.script.includes(q.source_text)) return "原文依据与总体剧本不一致";
 const range=q.source_text.match(/^【\s*(\d+)\s*[—–－-]\s*(\d+)\s*秒/);
 if(range&&(start<Number(range[1])||start+q.duration>Number(range[2]))) return "镜头时间超出原剧本所属段落";
 if(!q.references.length||q.references.some(id=>!p.assets.some(a=>a.id===id))) return "请核对引用素材";
 return "";
}


export function BoardPlanReview({p,busy,adopted=false,trialBusy,save,allImages,trial,onDirty,previewTrial,confirmFrame,quoteVideo,handleTasks,adjustDuration,durationPolicy,videoBusy,editContent}:{p:Project;busy:boolean;editContent?:(q:Shot)=>void;videoBusy?:(shot:Shot)=>boolean;adopted?:boolean;trialBusy?:(shot:Shot)=>boolean;save:(shots:Shot[],approve:boolean)=>void;allImages?:(shots:Shot[])=>void|Promise<void>;trial?:(shots:Shot[],shot:Shot)=>void|Promise<void>;onDirty?:(dirty:boolean)=>void;previewTrial?:(shot:Shot)=>void;confirmFrame?:(shot:Shot)=>void;handleTasks?:(shot:Shot)=>void;adjustDuration?:(shot:Shot)=>void;durationPolicy?:ShotDurationPolicy;quoteVideo?:(shot:Shot,feedback?:string,mode?:VideoMode,endShotID?:string)=>void|Promise<boolean>}) {
 const [shots,setShots]=useState<Shot[]>(adopted?p.shots:p.planned_shots??[]);
 const [active,setActive]=useState<number|null>(null);
 const [filter,setFilter]=useState("all");
 const [accept,setAccept]=useState(false);
 const [acceptImages,setAcceptImages]=useState(false);
 const [scriptOpen,setScriptOpen]=useState(false);
 const [videoRetryID,setVideoRetryID]=useState("");
 const [videoFeedback,setVideoFeedback]=useState("");
 const [videoRetryMode,setVideoRetryMode]=useState<"edit_video"|"first_frame">();
 const detailContentRef=useRef<HTMLDivElement>(null);
 useLayoutEffect(()=>{const container=detailContentRef.current?.parentElement;if(container)container.scrollTop=0;},[active]);
 const target=p.duration??60;
 let offset=0;
 const rows:PlanRow[]=shots.map((shot,index)=>{const start=offset;offset+=shot.duration;return {shot,index,start,end:offset,issue:adopted?"":issueFor(shot,start,p)};});
 const reviewed=shots.filter(q=>q.review_status==="reviewed").length;
 const changed=!adopted&&JSON.stringify(shots)!==JSON.stringify(p.planned_shots??[]);
 useEffect(()=>{onDirty?.(changed);return ()=>onDirty?.(false);},[changed,onDirty]);
 const issues=rows.filter(r=>r.issue);
 const ready=shots.length>0&&reviewed===shots.length&&offset===target&&!issues.length;
 const missing=shots.filter(q=>!q.candidates[q.selected]?.file).length;
 const selected=active===null?undefined:rows[active];
 const visible=rows.filter(r=>filter==="missing"?!r.shot.candidates[r.shot.selected]?.file:filter==="unconfirmed"?!r.shot.approved:filter==="all"||(filter==="unreviewed"?r.shot.review_status!=="reviewed":r.shot.review_status==="changes"));
 const retryRow=rows.find(r=>r.shot.id===videoRetryID);
 const retryMode=videoRetryMode;
 const retryDisabled=(retryRow ? (videoBusy?.(retryRow.shot)??busy) : busy)||changed||!retryRow||!!retryRow.issue||retryRow.shot.review_status!=="reviewed"||!retryRow.shot.approved||!retryRow.shot.video||!retryRow.shot.candidates[retryRow.shot.selected]?.file;
 function update(index:number,patch:Partial<Shot>) {
  setShots(old=>{const shifted=patch.duration!==undefined&&patch.duration!==old[index].duration;
   return old.map((q,i)=>i===index?{...q,...patch,review_status:"pending"}:shifted&&i>index?{...q,review_status:"pending"}:q);
  });setAccept(false);
 }
 function mark(index:number,status:ReviewStatus) {setShots(old=>old.map((q,i)=>i===index?{...q,review_status:status}:q));setAccept(false);}
 function locateIssue(){setFilter("all");setActive(issues[0]?.index??rows.find(r=>r.shot.review_status!=="reviewed")?.index??Math.max(0,rows.length-1));}
 function reviewNext(index:number){mark(index,"reviewed");if(index+1<shots.length)setActive(index+1);}
 function adoptedActions(r:PlanRow) {
  const q=r.shot;
  const hasImage=!!q.candidates[q.selected]?.file;
  const speech=p.audio_mode==="dialogue"?"台词":"旁白";
  const disabled=busy||changed||!!r.issue;
  const videoBlocked=(videoBusy?.(q)??busy)||changed||!!r.issue;
  const manageTask=videoBlocked&&!p.archived&&!!handleTasks;
  const frameManageTask=disabled&&!p.archived&&!!handleTasks;
  return <div className="shot-flow" aria-label={`第${r.index+1}镜制作流程`}>
   <ol className="shot-flow-steps">
    <li className={q.approved?"complete":"current"} aria-current={!q.approved?"step":undefined}>
     <span className="shot-flow-marker" aria-hidden="true">{q.approved?<Check size={12}/>:"1"}</span>
     <div className="shot-flow-content">
      <strong>确认画面与{speech}</strong>
      <small>{q.approved?"已确认":hasImage?`核对左侧图片与${speech}，满意后确认。`:"尚未生图，先打开详情生成图片。"}</small>
      {hasImage&&!q.approved&&confirmFrame&&<Button size="small" type="primary" disabled={disabled&&!frameManageTask} onClick={()=>frameManageTask?handleTasks?.(q):confirmFrame(q)}>{`确认画面与${speech}`}</Button>}
      {!hasImage&&previewTrial&&<Button size="small" type="primary" onClick={()=>previewTrial(q)}>查看本镜详情</Button>}
     </div>
    </li>
    <li className={q.video?"complete":q.approved?"current":"upcoming"} aria-current={q.approved&&!q.video?"step":undefined}>
     <span className="shot-flow-marker" aria-hidden="true">{q.video?<Check size={12}/>:"2"}</span>
     <div className="shot-flow-content">
      <strong>生成本镜视频</strong>
      <small>{q.video?"已生成，可在左侧播放。":q.approved?"只制作这一镜，先确认报价。":"完成第 1 步后，查看报价生成视频。"}</small>
      {hasImage&&q.approved&&!q.video&&quoteVideo&&<Button size="small" type="primary" disabled={videoBlocked&&!manageTask} onClick={()=>manageTask?handleTasks?.(q):quoteVideo(q)}>{manageTask?"重新生成视频":"生成本镜视频 · 查看报价"}</Button>}
     </div>
    </li>
   </ol>
   {disabled&&!manageTask&&<small className="shot-flow-blocked">{p.archived?"作品已归档，恢复后可继续。":changed?"请先保存规划修改。":r.issue||"当前任务尚未结束，可在此处理后继续。"}</small>}
   {(hasImage&&(previewTrial||q.video&&quoteVideo))&&<div className="shot-flow-secondary">
    {hasImage&&previewTrial&&<Button type="link" size="small" onClick={()=>previewTrial(q)}>查看本镜详情</Button>}
    {hasImage&&q.video&&quoteVideo&&<Button type="link" size="small" disabled={!manageTask&&(videoBlocked||!q.approved)} onClick={()=>{if(manageTask){handleTasks?.(q);return;}setVideoRetryID(q.id);setVideoFeedback("");setVideoRetryMode(undefined);}}>{manageTask?"重新生成视频":"重新生成本镜视频"}</Button>}
   </div>}
  </div>;
 }
 const columns:TableColumnsType<PlanRow>=[
  {title:"镜头 / 时间",key:"shot",width:164,render:(_,r)=><div className="plan-shot-name"><span className="plan-shot-index">{String(r.index+1).padStart(2,"0")} · {r.start}—{r.end}秒</span><Button type="link" onClick={()=>adopted?previewTrial?.(r.shot):setActive(r.index)}>{r.shot.title||"未命名镜头"}</Button><small>{adjustDuration?<Button size="small" icon={<Pencil size={14}/>} disabled={changed} aria-label={`调整第${r.index+1}镜时长`} onClick={()=>adjustDuration(r.shot)}>{r.shot.duration}秒 · 调整</Button>:`${r.shot.duration}秒`} · {r.shot.candidates[r.shot.selected]?.file ? "已试做图片" : "尚未生图"}</small></div>},
  {title:"引用素材",key:"references",width:166,render:(_,r)=><ReferenceImages p={p} q={r.shot} compact onMore={()=>adopted?previewTrial?.(r.shot):setActive(r.index)}/>},
  {title:"生成分镜",key:"image",width:168,render:(_,r)=>{const image=r.shot.candidates[r.shot.selected];return image?.file ? <div className="plan-generated-image"><ZoomImage src={media(image.file)} alt={`${r.shot.title}分镜图`} loading="lazy"/><small>{r.shot.approved?"画面已确认":"待确认画面"}</small></div> : <span className="plan-empty-speech">尚未生图</span>;}},
  {title:"生成视频",key:"video",width:240,render:(_,r)=>r.shot.video ? <video className="plan-generated-video" key={r.shot.video} controls playsInline preload="none" poster={r.shot.candidates[r.shot.selected]?.file ? media(r.shot.candidates[r.shot.selected].file) : undefined} aria-label={`${r.shot.title}视频预览`} src={media(r.shot.video)}/> : <span className="plan-empty-speech">尚未生成视频</span>},
  {title:"首帧与视频动作",key:"action",render:(_,r)=><div className="plan-shot-copy">{editContent&&<Button size="small" icon={<Pencil size={14}/>} style={{maxWidth:"100%",height:"auto",minHeight:24,whiteSpace:"normal",textAlign:"left"}} disabled={busy||changed||p.archived} onClick={()=>editContent(r.shot)}>编辑首帧、动作与台词</Button>}<div><span>首帧</span><p className="plan-copy-clamp">{r.shot.first_frame||"待填写"}</p></div><div><span>动作</span><p className="plan-copy-clamp">{r.shot.description||"待填写"}</p></div><details><summary>展开完整描述</summary><p><strong>首帧：</strong>{r.shot.first_frame}</p><p><strong>动作：</strong>{r.shot.description}</p></details></div>},
  {title:p.audio_mode==="dialogue"?"角色对白（全文）":"旁白（全文）",key:"speech",width:208,render:(_,r)=><p className={`plan-speech${r.shot.narration?"":" plan-empty-speech"}`}>{r.shot.narration|| (p.audio_mode==="dialogue"?"无对白":"无旁白")}</p>},
  {title:adopted?"本镜制作流程":"审核",key:"review",width:adopted?232:190,fixed:adopted?"right":undefined,render:(_,r)=>adopted?adoptedActions(r):<div className="plan-review-cell"><Tag color={r.issue?"error":r.shot.review_status==="reviewed"?"success":r.shot.review_status==="changes"?"warning":undefined}>{r.issue?"需检查":statusNames[r.shot.review_status??"pending"]}</Tag><Button size="small" onClick={()=>setActive(r.index)}>审核第{r.index+1}镜</Button>{trial && !r.shot.candidates[r.shot.selected]?.file && <Button size="small" disabled={(trialBusy?.(r.shot)??busy)||(busy&&changed)||!!r.issue||r.shot.review_status!=="reviewed"} onClick={()=>void trial(shots,r.shot)}>试做本镜图片 · 查看报价</Button>}{previewTrial && r.shot.candidates[r.shot.selected]?.file && <Button size="small" onClick={()=>previewTrial(r.shot)}>查看本镜试做</Button>}{r.shot.candidates[r.shot.selected]?.file && <>
    {confirmFrame && <Button size="small" disabled={busy||changed||!!r.issue||r.shot.review_status!=="reviewed"||r.shot.approved} onClick={()=>confirmFrame(r.shot)}>{r.shot.approved?"画面已确认":"确认画面"}</Button>}
    {quoteVideo && !r.shot.video && <Button size="small" type="primary" disabled={(videoBusy?.(r.shot)??busy)||changed||!!r.issue||r.shot.review_status!=="reviewed"||!r.shot.approved} onClick={()=>quoteVideo(r.shot)}>生成本镜视频 · 查看报价</Button>}
    {quoteVideo && r.shot.video && <Button size="small" disabled={(videoBusy?.(r.shot)??busy)||changed||!!r.issue||r.shot.review_status!=="reviewed"||!r.shot.approved} onClick={()=>{setVideoRetryID(r.shot.id);setVideoFeedback("");setVideoRetryMode(undefined);}}>重新生成本镜视频</Button>}
    {changed && <small>请先保存规划修改</small>}
  </>}{r.issue&&<small className="plan-error">{r.issue}</small>}</div>},
 ];
 const detail=selected&&<>
  <div className="plan-detail-nav"><Button icon={<ChevronLeft size={15}/>} disabled={selected.index===0} onClick={()=>setActive(selected.index-1)}>上一镜</Button><span>{selected.index+1} / {shots.length}</span><Button disabled={selected.index===shots.length-1} onClick={()=>setActive(selected.index+1)}>下一镜<ChevronRight size={15}/></Button></div>
  <div className="plan-source"><strong>对应总体剧本 · {selected.start}—{selected.end}秒</strong><p>{selected.shot.source_text||"缺少原文依据"}</p></div>
  {selected.issue&&<Alert type="error" showIcon title={selected.issue}/>}
  <div className="plan-detail-form">
   <Field label="镜头名称"><Input disabled={busy} value={selected.shot.title} onChange={e=>update(selected.index,{title:e.target.value})}/></Field>
   <Field label="时长（秒）">{adjustDuration?<Button icon={<Pencil size={14}/>} disabled={changed} onClick={()=>{setActive(null);adjustDuration(selected.shot);}}>{selected.shot.duration} 秒 · 调整时长</Button>:<InputNumber disabled={busy} min={durationPolicy?.min??1} max={durationPolicy?.max??12} value={selected.shot.duration} onChange={n=>update(selected.index,{duration:n??0})}/>}</Field>
   <Field label="静态首帧"><Input.TextArea disabled={busy} rows={4} value={selected.shot.first_frame} onChange={e=>update(selected.index,{first_frame:e.target.value})}/></Field>
   <Field label="视频动作与运镜"><Input.TextArea disabled={busy} rows={5} value={selected.shot.description} onChange={e=>update(selected.index,{description:e.target.value})}/></Field>
   <Field label={p.audio_mode==="dialogue"?"角色对白（保留原文）":"旁白（保留原文）"}><Input.TextArea disabled={busy} rows={3} value={selected.shot.narration} onChange={e=>update(selected.index,{narration:e.target.value})}/></Field>
   <Field label="引用素材"><Select disabled={busy} mode="multiple" style={{width:"100%"}} value={selected.shot.references} options={p.assets.map(a=>({value:a.id,label:a.name}))} onChange={references=>update(selected.index,{references})}/></Field>
   <ReferenceImages p={p} q={selected.shot}/>
  </div>
 </>;
 const detailFooter=selected&&<div className="plan-detail-actions">{trial && !selected.shot.candidates[selected.shot.selected]?.file && <Button disabled={(trialBusy?.(selected.shot)??busy)||(busy&&changed)||!!selected.issue||selected.shot.review_status!=="reviewed"} onClick={()=>{void trial(shots,selected.shot);setActive(null);}}>试做本镜图片 · 查看报价</Button>}<Button onClick={()=>setActive(null)}>返回总览</Button><Button disabled={busy} onClick={()=>save(shots,false)}>保存规划草稿</Button><Button disabled={busy} onClick={()=>mark(selected.index,"changes")}>标记需修改</Button><Button type="primary" icon={<Check size={15}/>} disabled={busy||!!selected.issue} onClick={()=>reviewNext(selected.index)}>{selected.index===shots.length-1?"本镜审核通过":"本镜通过，下一镜"}</Button></div>;
 return <section className="panel plan-review-panel">
  <div className="plan-review-heading"><div><h2>{adopted?"全片分镜总览":"审核分镜规划"}</h2><p>{adopted?`每镜按两步完成：先核对画面与${p.audio_mode==="dialogue"?"台词":"旁白"}并确认，再查看报价生成视频。已确认的镜头可独立制作，无需等待其他镜头。`:"先通读全片，再逐镜对照原剧本。审核通过一镜即可保存并单独报价试做图片；确认画面后再单独报价生成视频。不同镜头的图片并发数可在服务配置中调整（默认2），也可在全部审核后一次确认缺图报价。"}</p></div><div className="plan-review-controls">{allImages && missing>0 && <Tooltip title={busy?"请先完成、恢复或处理当前任务；归档项目需先取消归档":!ready?"请先完成全部镜头审核并核对总时长":"采用当前规划后，仅为缺图镜头报价；已有图片和视频保留"}><Button type="primary" disabled={busy||!ready} onClick={()=>{setAcceptImages(true);setAccept(true);}}>全部生成分镜图 · 查看报价</Button></Tooltip>}<Select aria-label="筛选审核状态" value={filter} onChange={setFilter} options={adopted?[{value:"all",label:"全部镜头"},{value:"missing",label:"只看缺图"},{value:"unconfirmed",label:"只看未确认"}]:[{value:"all",label:"全部镜头"},{value:"unreviewed",label:"只看未审核"},{value:"changes",label:"只看需修改"}]}/><Button onClick={()=>setScriptOpen(true)}>查看总体剧本</Button></div></div>
  <div className="plan-review-summary"><strong>{shots.length}镜 · {target}秒</strong><span>{adopted?`已生图 ${shots.length-missing}/${shots.length} · 已确认 ${shots.filter(q=>q.approved).length}/${shots.length}`:`已审核 ${reviewed}/${shots.length}`}</span>{!adopted&&<Tag color={changed?"warning":undefined}>{changed?"有未保存改动":"草稿已保存"}</Tag>}</div>
  <nav className="plan-timeline" aria-label="全片时间轴">{rows.map(r=><Tooltip key={r.shot.id} title={`第${r.index+1}镜 · ${r.start}—${r.end}秒 · ${r.shot.title}`}><Button className={`plan-timeline-shot ${r.shot.review_status??"pending"}${active===r.index?" active":""}`} style={{flexGrow:r.shot.duration||1}} aria-label={`时间轴第${r.index+1}镜 ${r.start}—${r.end}秒`} onClick={()=>setActive(r.index)}><strong>{String(r.index+1).padStart(2,"0")}</strong><span>{r.shot.title}</span><small>{r.shot.duration}秒</small></Button></Tooltip>)}</nav>
  <div className="plan-timeline-scale"><span>0秒</span><span>{offset}秒 / 目标{target}秒</span></div>
  {(offset!==target||issues.length>0)&&<Alert className="plan-validation" type="error" showIcon title={offset!==target?`总时长${offset}秒，与目标${target}秒不一致`:`${issues.length}个镜头需要检查`} action={<Button size="small" onClick={locateIssue}>定位问题</Button>}/>}

  <div className="plan-review-workspace">
   <div className="plan-overview-table"><Table<PlanRow> columns={columns} dataSource={visible} rowKey={r=>r.shot.id} pagination={false} size="small" scroll={{x:1340,y:"100%"}} rowClassName={r=>active===r.index?"plan-row-active":""} locale={{emptyText:"当前筛选下没有镜头"}}/></div>
  </div>
  <AntModal centered title={selected?`第${selected.index+1}镜 · 单镜详情`:"单镜详情"} open={!!selected} onCancel={()=>setActive(null)} mask={{closable:false}} width="90vw" className="plan-detail-modal" footer={detailFooter}
   styles={{container:{height:"90dvh",display:"flex",flexDirection:"column"},header:{flexShrink:0},body:{flex:1,minHeight:0,overflowY:"auto",paddingRight:8},footer:{flexShrink:0}}}>
   <div ref={detailContentRef}>{detail}</div>
  </AntModal>
  {!adopted&&<footer className="plan-review-footer"><div><strong className={offset!==target?"plan-error":""}>合计 {offset} / {target} 秒</strong><span>已审核 {reviewed}/{shots.length} · {changed?"有未保存改动":"已保存"}</span><small>采用规划后，现有图与视频保留；生图仍需单独确认报价。</small></div><div className="plan-footer-actions"><Button disabled={busy} onClick={()=>save(shots,false)}>保存规划草稿</Button>{!ready&&<Button onClick={locateIssue}>继续审核</Button>}<Button type="primary" disabled={busy||!ready} onClick={()=>{setAcceptImages(false);setAccept(true);}}>确认采用规划</Button></div></footer>}
  {scriptOpen&&<Modal title="总体剧本" onClose={()=>setScriptOpen(false)}><p className="plan-source-text">{p.script}</p></Modal>}
  {retryRow && quoteVideo && <Modal title="重新生成本镜视频" onClose={()=>{if(!busy)setVideoRetryID("");}} footer={<div className="dialog-actions">
    <Button disabled={busy} onClick={()=>setVideoRetryID("")}>取消</Button>
    <Button type="primary" disabled={retryDisabled||!retryMode||!videoFeedback.trim()} onClick={()=>void(async()=>{const ok=await quoteVideo(retryRow.shot,videoFeedback.trim(),retryMode,"");if(ok!==false)setVideoRetryID("");})()}>查看本镜视频报价</Button>
  </div>}>
    <p className="modal-intro">{retryRow.shot.title} · {retryRow.shot.duration} 秒。只重做这一镜，其他镜头不重做；新视频成功前保留原视频，失败或结果未知时不会替换原视频。</p>
    <Field label="视频修改建议"><Input.TextArea autoFocus disabled={busy} rows={4} maxLength={1000} showCount value={videoFeedback} onChange={e=>setVideoFeedback(e.target.value)} placeholder="例如：动作慢一点，镜头缓慢推进，保留当前台词。"/></Field>
    <Field label="制作方式"><Radio.Group disabled={busy} value={videoRetryMode} onChange={e=>setVideoRetryMode(e.target.value)} options={[{value:"edit_video",label:"基于原视频修改"},{value:"first_frame",label:"重新生成一版"}]}/></Field>
    <p className="notice">{retryMode==="edit_video"?"将当前这条旧视频作为参考，保留未要求改变的内容。原视频来源与哈希会绑定报价；输入视频会计费用量，模型需支持视频编辑。":retryMode==="first_frame"?"使用已确认分镜图作严格首帧，加上原动作描述与本次建议，不上传本镜旧视频。":"请选择基于原视频修改，或使用分镜图重新生成。"}</p>
    <p className="muted">本次只修改当前镜头。下一步先确认当前镜头的准确报价，再提交模型。失败及未知结果保留原视频，不自动重试。</p>
    {changed&&<Alert type="warning" title="请先保存规划修改，再重新报价"/>}
  </Modal>}
  {accept&&<Modal title={acceptImages?"全部生成分镜图":"确认采用分镜规划"} onClose={()=>setAccept(false)} footer={<Button type="primary" disabled={busy||!ready} onClick={()=>acceptImages?void allImages?.(shots):save(shots,true)}>{acceptImages?"确认采用并查看报价":"确认采用，不生图"}</Button>}><p>已完成全部{shots.length}镜审核，总时长{offset}秒。采用后新规划成为当前分镜，内容未变的试做图片与视频会沿用，原有正式分镜保留在历史素材区。</p><p>{acceptImages?`仅补齐${missing}个缺图镜头，每镜1张，并发数按服务配置（默认2）；已有图片和视频保留。下一步展示合计报价，确认报价后才调用模型。`:"本次不调用模型。生图需另外查看并确认报价。"}</p></Modal>}
 </section>;
}
