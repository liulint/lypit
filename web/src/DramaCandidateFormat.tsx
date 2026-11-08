import {Alert,Button,Collapse} from "antd";
import type {Job} from "./drama";
export default function DramaCandidateFormat({job,busy,check}:{job:Job;busy:boolean;check:()=>void}){
 const result=job.job.candidate_check;
 if(!result)return <Alert type="info" showIcon title="这份旧候选还没有格式检查记录" description="可在本地检查并修复明确的格式问题，不调用模型，不改写当前正文。" action={<Button disabled={busy||job.job.applied} onClick={check}>本地检查并修复格式</Button>}/>;
 return <><Alert showIcon type={result.issues.length?"warning":"success"} title={result.issues.length?"格式检查发现问题，请先修改草稿":result.repairs.length?`已在本地修复 ${result.repairs.length} 处格式问题`:"格式检查通过"} action={!job.job.applied&&result.issues.some(i=>i.rule==="LINK")?<Button disabled={busy} onClick={check}>重新检查引用</Button>:undefined} description={<>{result.repairs.map((r,i)=><p key={`repair-${i}`}>第{r.line}行：{r.message}</p>)}{result.issues.map((v,i)=><p key={`issue-${i}`}>第{v.line}行：{v.message}</p>)}<p>{result.links_checked?"已检查结构格式和当前文档引用；内容与实际效果仍需核对，上游变化后须复核。":"仅检查结构格式；内容、引用和实际效果仍需核对。"}没有重新调用模型。</p></>}/>{job.job.candidate_original&&<Collapse items={[{key:"original",label:"查看修复前的原始生成稿",children:<pre className="drama-source">{job.job.candidate_original}</pre>}]}/>}</>;
}
