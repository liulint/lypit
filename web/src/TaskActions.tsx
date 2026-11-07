import { App, Button, Space } from "antd";
import { RefreshCw, Square } from "lucide-react";
import type { Task } from "./api";

export type TaskAction = "cancel" | "resume" | "abandon";
export function TaskActions({task,pending,onAction}:{task:Task;pending:boolean;onAction:(task:Task,action:TaskAction)=>void|Promise<void>}) {
 const {modal}=App.useApp();
 if (["running","queued"].includes(task.status)) return <Button disabled={pending} icon={<Square size={13}/>} onClick={()=>void onAction(task,"cancel")}>停止</Button>;
 if (!["failed","interrupted","cancelled"].includes(task.status)) return null;
 return <Space wrap>
  <Button type="primary" disabled={pending} icon={<RefreshCw size={14}/>} onClick={()=>void onAction(task,"resume")}>恢复已有结果</Button>
  <Button disabled={pending} onClick={()=>modal.confirm({
   title:"放弃当前任务？",
   content:"放弃后可继续编辑。供应商已接收的任务可能继续执行并计费；新任务将重新报价。",
   okText:"确认放弃",cancelText:"继续保留",okButtonProps:{danger:true},
   onOk:()=>onAction(task,"abandon"),
  })}>放弃</Button>
 </Space>;
}
