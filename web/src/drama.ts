import type {EpisodeContext} from "./DramaSeries";
import type {ReferencePlan, Quote as LegacyQuote} from "./api";
export const stages = [{key:"develop",name:"创作开发"},{key:"script",name:"剧本"},{key:"assets",name:"视觉设定"},{key:"images",name:"图片提示词"},{key:"storyboard",name:"分镜"},{key:"motion",name:"视频提示词"},{key:"production",name:"生产"},{key:"edit",name:"剪辑"},{key:"review",name:"审查"}];
export type Work={prompt_language?:string;series_id?:string;episode_number?:number;plan?:{theme:string;requirements:string};plan_hash?:string;context?:EpisodeContext;id:string;title:string;idea:string;genre:string;form:string;aspect:string;duration:number;created:string;profile_hash:string;profile:{model:string;vendor:string;min:number;max:number;known:boolean;mode:string;language:string}};
export type Entry={id:string;title:string;start:number;end:number;body:string;fields:Record<string,string>};
export type Doc={kind:string;path:string;text:string;hash:string;version:number;state:string;accepted_hash:string;inputs:Record<string,string>;entries:Entry[];issues:{line:number;rule:string;message:string}[]};
export type VideoOptions={vendor_id:string;model:string;resolution:string};
export type Media={selected?:boolean;video_info?:VideoOptions&{vendor:string;created:string;width:number;height:number};id:string;work_id:string;entry:string;kind:string;file:string;path:string;hash:string;approved:boolean;task_id:string;source_hash:string;duration:number;mock:boolean;parent?:string;category?:string;current:boolean;reference_only?:boolean;verification?:{full_decode:boolean;audio:boolean;content_quality?:string};shared_origin?:{series_id:string;version:number;media_id:string;work_id:string;hash:string}};
export type TailStart={tail_video_id:string;tail_video_hash:string};
export type VideoStart={entry:string;previous?:string;recommended:"board"|"tail";reason:string;choice:"auto"|"board"|"tail";mode:"board"|"tail";waiting?:string;source?:Media;frame?:Media};
export type StartContext={workId:string;motionBase:string;boardBase:string;entry?:string};
export type FrameSelection={entry:string;media_id:string;hash:string;tail_video_id?:string;tail_video_hash?:string};
export type Production={start_plan_version?:number;tail_video_id?:string;tail_video_hash?:string;tail_source?:Media;video_options?:VideoOptions;entries?:string[];compare_source?:Media;prompt_override?:string;image_aspect?:string;image_max_calls?:number;image_retry_prompt?:string;frame_selections?:FrameSelection[];frame_media_id?:string;frame_media_hash?:string;reference_plan?:ReferencePlan;items?:Production[];kind:string;document:string;entry:string;prompt?:string;duration?:number;mode?:string;bindings?:{slot:string;use:string;control:string;exclude:string;media:Media}[];source?:Media;operation?:string;changes?:string;preserve?:string;sheet?:boolean};
export type Quote={item_estimates?:{entry:string;estimate?:LegacyQuote["estimate"];reason?:string}[];estimate?:LegacyQuote["estimate"];estimate_reason?:string;id:string;model:string;vendor:string;spec:Production;unknown:boolean;reason:string;expires:string};
export type Job={task:{id:string;status:string;message:string;stage:string;mode:string};job:{id:string;kind:string;base:string;media_references?:Media[];candidate:string;candidate_original?:string;candidate_check?:{version:number;hash:string;links_checked?:boolean;repairs:{line:number;message:string}[];issues:{line:number;rule:string;message:string}[]};applied:boolean;references:string[];production?:Production}};
export const operationID=()=>crypto.randomUUID().replaceAll("-","").slice(0,24);
// Replace a bounded H2 section only. Unknown Markdown outside it remains byte-for-byte intact.
export function patchEntry(text:string,entry:Entry,fields:Record<string,string>):string{
 const lines=text.split("\n");const section=lines.slice(entry.start,entry.end);for(const [key,value]of Object.entries(fields)){
  const index=section.findIndex(line=>line.startsWith(`- ${key}：`));if(index>=0)section[index]=`- ${key}：${value}`;else section.splice(1,0,`- ${key}：${value}`);
 }return [...lines.slice(0,entry.start),...section,...lines.slice(entry.end)].join("\n");
}

export function videoQuoteMatches(request:Production,quote:Quote){
 const frameMatches=(r:{frame_media_id?:string;frame_media_hash?:string;tail_video_id?:string;tail_video_hash?:string},q:Production)=>q.frame_media_id===r.frame_media_id&&q.frame_media_hash===r.frame_media_hash&&(q.tail_video_id||"")===(r.tail_video_id||"")&&(q.tail_video_hash||"")===(r.tail_video_hash||"");
 if(request.frame_media_id&&!frameMatches(request,quote.spec))return false;
 if(request.frame_selections){const actual=quote.spec.items||[];if(actual.length!==request.frame_selections.length||request.frame_selections.some(f=>{const q=actual.find(p=>p.entry===f.entry);return !q||!frameMatches({frame_media_id:f.media_id,frame_media_hash:f.hash,tail_video_id:f.tail_video_id,tail_video_hash:f.tail_video_hash},q)}))return false}
 if(!request.video_options)return true;
 const o=quote.spec.video_options,r=request.video_options;
 if(!o||o.vendor_id!==r.vendor_id||o.model!==r.model||o.resolution!==r.resolution)return false;
 if(request.operation==="compare"&&quote.spec.operation!=="compare")return false;
 if(request.compare_source&&(quote.spec.compare_source?.id!==request.compare_source.id||quote.spec.compare_source?.hash!==request.compare_source.hash))return false;
 if(request.entries){const actual=quote.spec.items?.map(p=>p.entry).sort()||[];if(JSON.stringify(actual)!==JSON.stringify([...request.entries].sort()))return false}
 return true;
}
