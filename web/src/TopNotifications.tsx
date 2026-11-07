import { useState } from "react";
import { Badge, Button, Empty, Modal, Popover, Progress } from "antd";
import { Bell } from "lucide-react";
import { stageNames, statusNames, type Project, type Task } from "./api";
import { VideoUsageDetails } from "./VideoUsage";

const storageKey = "lypit.notification-reads.v1";
type Reads = Record<string, string>;
type Kind = "task";
function loadReads(): Reads {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(storageKey) || "{}");
    return value && typeof value === "object" && !Array.isArray(value)
      ? Object.fromEntries(Object.entries(value).filter(([, v]) => typeof v === "string")) : {};
  } catch { return {}; }
}
function signature(task: Task, _kind: Kind) {
  // Progress polling is not a new notification. Usage can arrive before completion.
  return task.status;
}

export function TopNotifications({ tasks, projects, onTasks }: {
  tasks: Task[]; projects: Project[]; onTasks: () => void;
}) {
  const [reads, setReads] = useState<Reads>(loadReads);
  const [open, setOpen] = useState<Kind>();
  const [detail, setDetail] = useState<{ id: string; kind: Kind }>();
  const selected = tasks.find(t => t.id === detail?.id);
  const title = (task: Task) => projects.find(p => p.id === task.project_id)?.title || "作品";
  function markRead(items: Task[], kind: Kind) {
    setReads(previous => {
      const next = { ...previous };
      items.forEach(task => { next[`${kind}:${task.id}`] = signature(task, kind); });
      // Persist only public notification fingerprints, never task configuration.
      try { localStorage.setItem(storageKey, JSON.stringify(next)); } catch { /* Still read for this session. */ }
      return next;
    });
  }
  return <>
    <div className="top-notifications">
      {(["task"] as const).map(kind => {
        const label = "任务通知";
        const items = tasks.filter(task => reads[`${kind}:${task.id}`] !== signature(task, kind));
        return <Popover key={kind} trigger="click" placement="bottomRight" open={open === kind}
          onOpenChange={visible => setOpen(visible ? kind : undefined)} title={label}
          content={<div className="notification-panel">
            {items.length ? <>
              <div className="notification-list">{items.map(task => <Button block key={task.id}
                className="notification-item" onClick={() => {
                  markRead([task], kind); setDetail({ id: task.id, kind }); setOpen(undefined);
                }}>
                <span><strong>{title(task)}</strong><span>{stageNames[task.stage] || task.stage} · {statusNames[task.status] || task.status}</span>
                  {task.created && <small>{new Date(task.created).toLocaleString("zh-CN")}</small>}</span>
                <span>查看详情</span>
              </Button>)}</div>
              <Button type="text" onClick={() => markRead(items, kind)}>全部标为已读</Button>
            </> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无未读通知" />}
            <Button type="link" onClick={() => { setOpen(undefined); onTasks(); }}>查看任务历史</Button>
          </div>}>
          <Badge count={items.length} overflowCount={999} size="small">
            <Button type="text" aria-label={`${label}，${items.length} 条未读`}>
              {<Bell size={16} />}<span>{label}</span>
            </Button>
          </Badge>
        </Popover>;
      })}
    </div>
    <Modal open={!!selected} title="任务详情"
      onCancel={() => setDetail(undefined)} footer={<Button onClick={() => {
        setDetail(undefined); onTasks();
      }}>前往任务中心</Button>}>
      {selected && <>
        <h3>{title(selected)} · {stageNames[selected.stage] || selected.stage}</h3>
        <p>{statusNames[selected.status] || selected.status} · {selected.message}</p>
        <Progress percent={selected.progress} />
        <VideoUsageDetails task={selected} />
        {["failed", "interrupted", "cancelled"].includes(selected.status) &&
          <p>恢复只核对已有回执，不会重复提交旧请求。需要重新生成时，请重新报价并确认；原用量可能已计费。</p>}
      </>}
    </Modal>
  </>;
}
