import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";

export default function DramaReading({text}:{text:string}) {
 return <div className="drama-reading"><Markdown
  remarkPlugins={[remarkGfm]}
  skipHtml
  components={{
   table: ({children}) => <div className="drama-reading-table"><table>{children}</table></div>,
   // Reading a document must not fetch embedded remote images.
   img: ({alt}) => <span className="drama-reading-image">{alt || "图片"}</span>,
  }}
 >{text}</Markdown></div>;
}
