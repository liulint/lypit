import {useState} from "react";
import {Alert,Button,Form,Input,Modal,Radio,Space} from "antd";
import {media} from "./api";
import type {Entry,Media} from "./drama";

export function videoEntryPrompt(entry:Entry){
 const lines=entry.body.split("\n");const start=lines.findIndex(l=>l.trim()==="### 可复制提示词");
 if(start<0)return "";
 const end=lines.findIndex((l,i)=>i>start&&l.startsWith("### "));
 return lines.slice(start+1,end<0?undefined:end).filter(l=>l.startsWith(">")).map(l=>l.replace(/^> ?/,"")).join("\n").trim();
}

export default function DramaVideoRegenerate({entry,source,busy,cancel,edit,recreate,run}:{entry:Entry;source:Media;busy:boolean;cancel:()=>void;edit:(advice:string)=>Promise<void>;recreate:(prompt:string)=>Promise<void>;run:(fn:()=>Promise<void>)=>Promise<void>}){
 const [mode,setMode]=useState<"edit"|"recreate">();const [advice,setAdvice]=useState("");
 const [error,setError]=useState("");
 const original=videoEntryPrompt(entry);const [prompt,setPrompt]=useState(original);
 const ready=mode==="edit"?source.current&&!!advice.trim():mode==="recreate"&&!!prompt.trim()&&prompt.trim()!==original;
 return <Modal open title="重新制作当前镜头" width={760} onCancel={cancel} styles={{body:{maxHeight:"70vh",overflowY:"auto"}}} footer={<Space><Button onClick={cancel}>取消</Button><Button type="primary" loading={busy} disabled={busy||!ready} onClick={()=>void run(async()=>{setError("");try{await(mode==="edit"?edit(advice.trim()):recreate(prompt.trim()))}catch(e){setError(e instanceof Error?e.message:"无法查看报价，请重新核对本镜输入");throw e}})}>{mode==="edit"?"查看视频修改报价":mode==="recreate"?"查看重新生成报价":"先选择处理方式"}</Button></Space>}>
  <p>{entry.title} · {entry.fields["时长"]}。只处理当前镜头，旧视频和其他镜头保留。</p>
  <Radio.Group aria-label="视频重新制作方式" value={mode} disabled={busy} onChange={e=>{setMode(e.target.value);setError("")}} style={{display:"flex",flexDirection:"column",gap:12,marginBottom:16}}>
   <Radio value="edit" disabled={!source.current}>基于当前视频修改</Radio>
   <Radio value="recreate">修改本镜剧情/生成说明后重新生成</Radio>
  </Radio.Group>
  {error&&<Alert type="error" showIcon title={error}/>}
  {mode==="edit"&&<><video controls preload="metadata" src={media(source.file)} style={{width:"100%",maxHeight:240}}/><Alert type="info" showIcon title="以当前视频为编辑输入，保留未要求修改的内容与时长" description="查看报价时核对模型编辑能力和原视频版本。费用可能包含视频输入；确认报价后才提交修改。"/><Form layout="vertical"><Form.Item label="希望修改的地方" required><Input.TextArea aria-label="视频修改建议" rows={4} maxLength={3000} value={advice} onChange={e=>setAdvice(e.target.value)}/></Form.Item></Form></>}
  {mode==="recreate"&&<><Alert type="info" showIcon title="使用已确认画面或参考方案与下方说明重新生成，旧视频不作为输入" description="仅修改本次生成说明，不改整集正式剧本、镜长或其他镜头；确认报价后生成。需要选择首帧时，会先让你核对。"/><Form layout="vertical"><Form.Item label="本镜剧情/生成说明" required><Input.TextArea aria-label="本镜生成说明" rows={10} maxLength={30000} value={prompt} onChange={e=>setPrompt(e.target.value)}/></Form.Item></Form></>}
 </Modal>;
}
