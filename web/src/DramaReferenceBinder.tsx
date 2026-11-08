import {useState} from "react";
import {Alert,App,Button,Form,Input,Select,Space} from "antd";
import {media} from "./api";
import type {Media} from "./drama";
const purposes:Record<string,{label:string;control:string;exclude:string}>={
 身份:{label:"人物长相（身份）",control:"角色的外貌、毛色、体型与标志特征",exclude:"构图、位置与尚未发生的动作"},
 造型状态:{label:"服装或状态",control:"本镜采用的服装和外观状态",exclude:"人物身份与镜头构图"},
 地理:{label:"场景与位置",control:"场景布局、固定物体与空间关系",exclude:"角色外貌和动作"},
 构图:{label:"画面构图",control:"主体位置与画面构图",exclude:"角色身份与动作"},
 尺度:{label:"物体大小关系",control:"主体之间的大小比例",exclude:"构图与动作"},
 效果:{label:"视觉效果",control:"光照和视觉效果",exclude:"角色身份与故事动作"},
 起始帧:{label:"视频开始时的画面",control:"起始构图和画面内容",exclude:"尚未发生的动作"},
 结束帧:{label:"视频结束时的画面",control:"结束构图和画面内容",exclude:"过程中的动作节奏"},
 风格:{label:"整体画面风格",control:"画面的色调和视觉风格",exclude:"人物身份和故事动作"}
};
export default function DramaReferenceBinder({medias,value,change}:{medias:Media[];value:string;change:(v:string)=>void}){
 const {message}=App.useApp();const [id,setID]=useState<string>();const [use,setUse]=useState<string>();const [control,setControl]=useState("");const [exclude,setExclude]=useState("");const selected=medias.find(m=>m.id===id);const ready=selected&&use&&control.trim()&&exclude.trim();
 function purpose(v:string){setUse(v);setControl(purposes[v].control);setExclude(purposes[v].exclude)}
 function apply(append:boolean){if(!ready||!selected)return;const old=append&&value.includes("REF-")?value:"";if(old.includes(selected.path)){message.error("这张图已选入参考，请先替换或清空后调整用途");return}const n=(old.match(/REF-/g)||[]).length+1;const ref=`REF-${selected.id}（顺序：${n}）· ${selected.path}《${selected.entry}》（用途：${use}；控制：${control}；不得控制：${exclude}）`;change(old?`${old}；${ref}`:ref)}
 return <div><Alert title="选已有图片不调用模型。保存后仍需确认修改过的文稿，才会用于制作。"/><div className="reference-selected">{medias.filter(m=>value.includes(m.path)).map(m=><figure key={m.id}><img src={media(m.file)} alt={m.entry}/><figcaption>{m.entry}</figcaption></figure>)}</div><Form.Item label="选择已确认图片"><Select showSearch optionFilterProp="label" value={id} options={medias.filter(m=>m.kind==="image"&&m.approved&&m.current).map(m=>({value:m.id,label:m.entry}))} onChange={v=>{setID(v);const m=medias.find(m=>m.id===v);if(m?.category==="character")purpose("身份");else if(m?.category==="scene")purpose("地理")}}/></Form.Item>{selected&&<img className="guide-media-preview" src={media(selected.file)} alt={selected.entry}/>}<Form.Item label="这张图片用来指导什么？"><Select value={use} options={Object.entries(purposes).map(([value,p])=>({value,label:p.label}))} onChange={purpose}/></Form.Item><Form.Item label="应当参考的部分"><Input value={control} onChange={e=>setControl(e.target.value)}/></Form.Item><Form.Item label="不应照搬的部分"><Input value={exclude} onChange={e=>setExclude(e.target.value)}/></Form.Item><Space wrap><Button disabled={!ready} onClick={()=>apply(false)}>只用这张参考</Button><Button disabled={!ready} onClick={()=>apply(true)}>加入参考列表</Button><Button disabled={!value} onClick={()=>change("")}>清空参考选择</Button></Space></div>
}
