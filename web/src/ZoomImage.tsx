import { useState, type ComponentProps } from "react";
import { Image } from "antd";

// Keep the original image element and layout; the viewer opens in a portal.
export function ZoomImage(props: ComponentProps<"img">) {
  const [open, setOpen] = useState(false);
  return <>
    <img {...props} tabIndex={0} title="点击放大，可缩放查看细节" style={{...props.style,cursor:"zoom-in"}}
      onClick={()=>setOpen(true)} onKeyDown={e=>{if(e.key==="Enter"||e.key===" "){e.preventDefault();setOpen(true);}}} />
    <Image.PreviewGroup items={props.src ? [{src:props.src,alt:props.alt}] : []} preview={{open,onOpenChange:setOpen}} />
  </>;
}
