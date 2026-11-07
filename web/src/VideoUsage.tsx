import type { Task } from "./api";
import { Collapse } from "antd";

export function VideoUsageDetails({task}: {task: Task}) {
  if (!["shot_video", "videos"].includes(task.stage)) return null;
  if (task.mode === "mock") {
    return task.status === "completed" ? <p>本地模拟 · 未调用视频模型 · 无模型费用</p> : null;
  }
  if (!task.video_usage?.length) {
    return task.status === "completed" ? <p>视频 Token 用量未记录，费用未知；历史任务请查看厂商账单。</p> : null;
  }
  return <section aria-label="视频用量与费用">
    {task.video_usage.map(u => <div key={`${u.shot_id}:${u.remote_id}`}>
      <p><strong>{u.title || u.shot_id}</strong> · 视频生成 Token：{u.completion_tokens == null ? "供应商未返回" : u.completion_tokens.toLocaleString("zh-CN")}
        {u.total_tokens != null && <> · 总 Token：{u.total_tokens.toLocaleString("zh-CN")}</>}
        {u.cost ? <> · 视频参考费用约 {u.cost.currency === "CNY" ? "¥" : u.cost.currency} {u.cost.amount}</> : " · 费用未知"}
      </p>
      {u.cost ? <Collapse ghost size="small" items={[{key:"basis",label:"查看费用折算依据",children:<><p>{u.cost.basis}</p><p>{u.model} · <a href={u.cost.url} target="_blank" rel="noreferrer">{u.cost.source}</a> · 核对日期 {u.cost.checked}</p></>}]} /> : <p className="muted">{u.reason}</p>}
    </div>)}
  </section>;
}
