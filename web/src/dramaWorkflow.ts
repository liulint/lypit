import type {Doc,Job,Media} from "./drama";
export const guideSteps=["写剧本","准备角色与场景","做分镜画面","生成视频","剪辑与导出"];
const rawPhases=[
 {key:"script",step:0,title:"写本集剧本",generate:"生成本集剧本",confirm:"确认使用这版剧本",intro:"把梗概写成能拍的场景、动作与对白。先看草稿，再决定用哪一版。"},
 {key:"assets",step:1,title:"整理角色与场景",generate:"整理角色与场景设定",confirm:"确认角色与场景设定",intro:"确认人物外貌、场景和画面风格，后面生成的画面才有一致的依据。"},
 {key:"images",step:1,title:"准备角色出图说明",generate:"准备角色与场景出图说明",confirm:"确认这份出图说明",intro:"把设定整理成每张图的制作要求。这里只生成文字，不会开始生图。"},
 {key:"asset_media",step:1,title:"准备角色与场景图片",intro:"可导入已有图片，或查看新图片报价。看过并确认后，再用于分镜。",document:"images",kind:"image"},
 {key:"storyboard",step:2,title:"设计分镜",generate:"设计本集分镜",confirm:"确认使用这版分镜",intro:"把剧本拆成镜头，核对每镜动作、时长和参考图。"},
 {key:"board_media",step:2,title:"确认分镜画面",intro:"每镜准备一张画面。检查确认后，点击“用作本镜视频首帧”；参考选择保存后需再次确认分镜。",document:"storyboard",kind:"image"},
 {key:"motion",step:3,title:"准备视频制作说明",generate:"准备各镜视频制作说明",confirm:"确认这份视频制作说明",intro:"指定每镜怎样动、持续多久、使用哪张起始图。这里只生成文字。"},
 {key:"video_media",step:3,title:"观看并确认视频",intro:"逐镜查看报价和时长，生成后完整观看，再确认可用于成片的版本。",document:"motion",kind:"video"},
 {key:"edit",step:4,title:"编排剪辑片段",generate:"生成剪辑建议",confirm:"确认使用这份剪辑单",intro:"选择已确认片段，填写入出点和取舍理由。确认剪辑单后再合成。"},
 {key:"render",step:4,title:"合成与下载",intro:"按已确认剪辑单在本机合成。技术检查通过后，完整观看成片，再下载发布。"}
] as const;
export type Phase={key:string;step:number;title:string;intro:string;generate:string;confirm:string;document:string;kind:"image"|"video"};
export const phases:Phase[]=rawPhases.map(p=>({document:"",kind:"image",generate:"",confirm:"",...p}));
const deps:Record<string,string[]>={script:["develop"],assets:["script"],images:["assets"],storyboard:["script","assets"],motion:["script","assets","storyboard"],edit:["script","storyboard","motion"]};
export const docNames:Record<string,string>={develop:"创作开发（可选）",script:"剧本",assets:"角色与场景设定",images:"出图说明",storyboard:"分镜",motion:"视频制作说明",edit:"剪辑单",review:"文字审查"};
export const docFor=(phase:Phase)=>!!phase.document?phase.document:phase.key;
export function phaseJobs(phase:Phase,jobs:Job[]){return jobs.filter(j=>phase.key==="render"?j.job.kind==="render":!!phase.document?(j.job.production?.kind==="batch"?j.job.production.items?.some(item=>item.document===phase.document&&item.kind===phase.kind):j.job.production?.document===phase.document&&j.job.production?.kind===phase.kind):j.job.kind===phase.key)}
export function phaseComplete(phase:Phase,docs:Doc[],medias:Media[]):boolean{
 if(phase.key==="render")return medias.some(m=>m.kind==="film"&&m.current&&m.verification?.full_decode&&m.verification?.audio);
 if(!!phase.document){const d=docs.find(d=>d.kind===phase.document);const prefix=phase.document==="images"?"IMG-":phase.document==="storyboard"?"SHOT-":"MOTION-";const entries=d?.entries.filter(e=>e.id.startsWith(prefix))||[];if(!entries.length)return phase.key==="asset_media"&&medias.some(m=>m.kind==="image"&&m.approved&&m.current)&&docs.some(d=>d.kind==="assets"&&d.state==="accepted");return d?.state==="accepted"&&entries.every(e=>!!medias.find(m=>m.entry===e.id&&m.kind===phase.kind&&m.current&&(phase.kind!=="video"||m.selected!==false))?.approved)}
 return docs.some(d=>d.kind===phase.key&&d.state==="accepted");
}
export function recommendedPhase(docs:Doc[],medias:Media[]):number{
 for(let i=0;i<phases.length;i++){if(phases[i].key==="images"&&!docs.find(d=>d.kind==="images")?.text&&medias.some(m=>m.kind==="image"&&m.current&&m.approved))continue;if(!phaseComplete(phases[i],docs,medias))return i}return phases.length-1;
}
export function phaseBlocker(phase:Phase,docs:Doc[]):string|undefined{
 const keys=!!phase.document?[phase.document]:phase.key==="render"?["edit"]:deps[phase.key]||[];
 return keys.find(k=>{const d=docs.find(d=>d.kind===k);return !(k==="develop"&&!d?.text)&&d?.state!=="accepted"});
}
export function phaseState(phase:Phase,docs:Doc[],jobs:Job[],medias:Media[],loaded:boolean){
 if(!loaded)return "loading";
 const relevant=phaseJobs(phase,jobs),latest=relevant[0];
 if(relevant.some(j=>["queued","running"].includes(j.task.status)))return "running";
 if(latest&&["failed","interrupted","cancelled"].includes(latest.task.status))return "attention";
 if(relevant.some(j=>!j.job.applied&&j.job.candidate&&j.task.status==="completed"&&j.job.base===(docs.find(d=>d.kind===phase.key)?.hash||"")))return "review";
 if(phaseComplete(phase,docs,medias))return "completed";
 const d=docs.find(d=>d.kind===docFor(phase));if(d?.state==="stale")return "stale";
 if(d?.text||latest?.job.candidate)return "review";return "empty";
}

export function stepComplete(step:number,docs:Doc[],medias:Media[]):boolean{
 const hasImportedImages=!docs.find(d=>d.kind==="images")?.text&&medias.some(m=>m.kind==="image"&&m.current&&m.approved);
 return phases.filter(p=>p.step===step).every(p=>p.key==="images"&&hasImportedImages||phaseComplete(p,docs,medias));
}
