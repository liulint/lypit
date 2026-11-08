import {expect,it} from "vitest";
import {videoQuoteMatches,type Production,type Quote} from "./drama";

it("rejects old backend quotes that ignore the chosen model or resolution",()=>{
 const p:Production={kind:"video",document:"motion",entry:"MOTION-1",operation:"compare",video_options:{vendor_id:"ark",model:"seedance",resolution:"720p"}};
 const q={spec:{...p,video_options:undefined}} as Quote;
 expect(videoQuoteMatches(p,q)).toBe(false);
 q.spec={...p,video_options:{...p.video_options!,resolution:"480p"}};expect(videoQuoteMatches(p,q)).toBe(false);
 q.spec={...p};expect(videoQuoteMatches(p,q)).toBe(true);
});

it("rejects a batch quote that omits or adds a selected shot",()=>{
 const p:Production={kind:"video",document:"motion",entry:"*",operation:"compare",entries:["MOTION-1","MOTION-2"],video_options:{vendor_id:"ark",model:"seedance",resolution:"720p"}};
 const q={spec:{...p,kind:"batch",items:[{kind:"video",document:"motion",entry:"MOTION-1"}]}} as Quote;
 expect(videoQuoteMatches(p,q)).toBe(false);
 q.spec.items!.push({kind:"video",document:"motion",entry:"MOTION-2"});expect(videoQuoteMatches(p,q)).toBe(true);
 q.spec.items!.push({kind:"video",document:"motion",entry:"MOTION-3"});expect(videoQuoteMatches(p,q)).toBe(false);
});


it("rejects a quote that substitutes the storyboard for the selected real tail",()=>{
 const p:Production={kind:"video",document:"motion",entry:"MOTION-2",start_plan_version:1,frame_media_id:"tail",frame_media_hash:"tail-hash",tail_video_id:"video-1",tail_video_hash:"video-hash"};
 const q={spec:{...p}} as Quote;expect(videoQuoteMatches(p,q)).toBe(true);
 q.spec={...p,tail_video_id:undefined,tail_video_hash:undefined};expect(videoQuoteMatches(p,q)).toBe(false);
 q.spec={...p,frame_media_hash:"changed"};expect(videoQuoteMatches(p,q)).toBe(false);
 const batch:Production={kind:"video",document:"motion",entry:"*",frame_selections:[{entry:p.entry,media_id:p.frame_media_id!,hash:p.frame_media_hash!,tail_video_id:p.tail_video_id,tail_video_hash:p.tail_video_hash}]};
 q.spec={kind:"batch",document:"motion",entry:"*",items:[{...p}]};expect(videoQuoteMatches(batch,q)).toBe(true);
 q.spec.items![0].tail_video_hash="changed";expect(videoQuoteMatches(batch,q)).toBe(false);
});
