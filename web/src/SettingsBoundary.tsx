import { Component, type ReactNode } from "react";
import { Alert, Button } from "antd";

// A tab left open across a build may still request a removed lazy chunk.
// Keep navigation usable and let the user choose when to reload.
export default class SettingsBoundary extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    if (this.state.failed) {
      return <Alert
        type="error"
        showIcon
        title="配置页面加载失败"
        description="工作台更新后，旧页面可能无法加载配置模块。请刷新页面加载最新版本；刷新会丢失未保存的编辑，请先保留内容。"
        action={<Button onClick={() => window.location.reload()}>刷新页面</Button>}
      />;
    }
    return this.props.children;
  }
}
