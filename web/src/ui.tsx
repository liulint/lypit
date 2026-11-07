import {
  Children,
  cloneElement,
  isValidElement,
  useId,
  type ReactElement,
  type ReactNode,
} from "react";
import {
  Button as AntButton,
  Form,
  Modal as AntModal,
  Tag,
  ConfigProvider,
  App as AntApp,
} from "antd";
import zhCN from "antd/locale/zh_CN";

export function StudioProvider({ children }: { children: ReactNode }) {
  return (
    <ConfigProvider
      locale={zhCN}
      componentSize="middle"
      theme={{
        token: {
          colorPrimary: "#315d43",
          colorInfo: "#315d43",
          colorInfoBg: "#eef5f0",
          colorInfoBorder: "#d3e3d7",
          colorSuccess: "#4f805d",
          colorText: "#24332b",
          colorTextSecondary: "#647168",
          colorTextPlaceholder: "#8b958e",
          colorBorder: "#d8dfd9",
          colorBgContainer: "#ffffff",
          controlItemBgActive: "#eaf1ec",
          controlItemBgActiveHover: "#dde8e0",
          controlItemBgHover: "#f3f6f3",
          controlOutline: "rgba(49,93,67,.12)",
          borderRadius: 8,
          controlHeight: 40,
          fontSize: 14,
          fontFamily:
            'Inter, -apple-system, BlinkMacSystemFont, "PingFang SC", "Microsoft YaHei", sans-serif',
        },
        components: {
          Modal: { titleFontSize: 20 },
          Form: { labelFontSize: 13, verticalLabelPadding: "0 0 8px" },
          Select: {
            optionSelectedBg: "#eaf1ec",
            optionSelectedColor: "#234c34",
          },
          Button: { primaryShadow: "0 2px 4px rgba(25,55,35,.1)" },
        },
      }}
    >
      <AntApp>{children}</AntApp>
    </ConfigProvider>
  );
}
export function Button({
  children,
  onClick,
  disabled = false,
  kind = "",
  type = "button",
  form,
}: {
  children: ReactNode;
  onClick?: () => void;
  disabled?: boolean;
  kind?: string;
  type?: "button" | "submit";
  form?: string;
}) {
  return (
    <AntButton
      type={kind === "primary" ? "primary" : "default"}
      htmlType={type}
      form={form}
      disabled={disabled}
      onClick={onClick}
      className={`studio-button ${kind}`}
    >
      {children}
    </AntButton>
  );
}
export function Badge({
  children,
  green = false,
}: {
  children: ReactNode;
  green?: boolean;
}) {
  return (
    <Tag className="studio-badge" color={green ? "success" : undefined}>
      {children}
    </Tag>
  );
}
export function Field({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  const id = useId();
  const controls = Children.map(children, (child) => {
    if (!isValidElement(child) || child.type === "small") return child;
    return cloneElement(
      child as ReactElement<{ id?: string; "aria-label"?: string }>,
      { id, "aria-label": label },
    );
  });
  return (
    <Form.Item
      label={label}
      htmlFor={id}
      layout="vertical"
      className="studio-field"
    >
      {controls}
    </Form.Item>
  );
}
export function Modal({
  title,
  children,
  onClose,
  footer,
  size = "default",
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
  footer?: ReactNode;
  size?: "default" | "large";
}) {
  return (
    <AntModal
      open
      centered
      title={title}
      width={size === "large" ? "70vw" : 620}
      footer={footer ?? null}
      onCancel={onClose}
      mask={{ closable: false }}
      destroyOnHidden
      className="studio-modal"
      styles={{
        ...(size === "large" ? {
          container: {height:"70dvh",display:"flex",flexDirection:"column"},
          header: {flexShrink:0},
          footer: {flexShrink:0},
        } : {}),
        body: {
          maxHeight: size === "large" ? undefined : "calc(100dvh - 240px)",
          ...(size === "large" ? {flex:1,minHeight:0} : {}),
          overflowY: "auto",
          paddingRight: 4,
        },
      }}
    >
      {children}
    </AntModal>
  );
}
