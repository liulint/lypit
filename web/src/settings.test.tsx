// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import ServiceSettings from "./ServiceSettings";
import type { CLICatalog, Provider, Settings } from "./api";
import { bindingOptions, newModel, supportedKinds } from "./settings-utils";
const mock = vi.hoisted(() => ({ api: vi.fn() }));
vi.mock("./api", async () => ({
  ...(await vi.importActual("./api")),
  api: mock.api,
}));
const base = (): Settings => ({
  providers: [
    {
      id: "demo",
      name: "模拟服务",
      adapter: "mock",
      base_url: "",
      key_env: "",
      models: [
        {
          ...newModel("demo-v1"),
          capabilities: ["text", "vision", "image", "video", "tts"],
        },
      ],
    },
  ],
  bindings: Object.fromEntries(
    ["text", "image", "vision", "video", "tts"].map((role) => [
      role,
      { provider: "demo", model: "demo-v1" },
    ]),
  ),
});
let settings: Settings;
let rejectCredential = false;
function catalogFixture(): CLICatalog {
  const models = [
    "test-model-a",
    "test-model-b",
    "test-c",
    "test-d",
    "test-e",
    "test-f",
  ];
  return {
    models,
    source: "live",
    stale: false,
    fetched_at: "2026-10-26T12:00:00Z",
    options: models.map((id, i) => ({
      id,
      label: i === 0 ? "Test A" : id,
      reasoning: [
        { id: "low", label: "low" },
        ...(i === 0 ? [{ id: "high", label: "high" }] : []),
      ],
      default_reasoning: "low",
      service_tiers: i === 0 ? [{ id: "priority", label: "Fast" }] : [],
    })),
  };
}
beforeEach(() => {
  settings = base();
  rejectCredential = false;
  mock.api.mockReset();
  Object.defineProperty(HTMLDialogElement.prototype, "showModal", {
    configurable: true,
    value: function () {
      this.setAttribute("open", "");
    },
  });
  Object.defineProperty(HTMLDialogElement.prototype, "close", {
    configurable: true,
    value: function () {
      this.removeAttribute("open");
    },
  });
  mock.api.mockImplementation(
    async (path: string, body: unknown, method?: string) => {
      if (path === "/cli/status")
        return [
          {
            kind: "codex",
            installed: true,
            auth: "ready",
            version: "test",
            path: "/test/codex",
            models: [],
          },
          { kind: "claude", installed: false, auth: "unknown", models: [] },
          { kind: "gemini", installed: false, auth: "unknown", models: [] },
        ];
      if (path === "/cli/models") return catalogFixture();
      if (path === "/settings" && method === "PUT") {
        settings = structuredClone(body as Settings);
        return settings;
      }
      if (path === "/settings")
        return {
          settings: structuredClone(settings),
          credentials_present: {},
          credential_errors: {},
        };
      if (path.endsWith("/credential")) {
        if (rejectCredential) throw new Error("保存失败，旧 Key 保留");
        settings.providers.find((p) => p.id === "remote")!.credential_version =
          "version-2";
        return { message: "saved" };
      }
      if (path.endsWith("/image-verifications")) return [];
      if (path.endsWith("/models"))
        return { models: ["test-model-a", "test-model-b"] };
      if (path.endsWith("/image-verification/quote"))
        return {
          id: "probe-1",
          state: "pending",
          image: "",
          error: "",
          quote: {
            id: "probe-1",
            stage: "cli_image_verification",
            estimate: null,
            notice: "单张验证，费用未知",
            requests: [
              {
                provider: "Codex",
                model: "default",
                prompt: "cup",
                count: 1,
                parameters: { sizes: ["1024x1536"] },
              },
            ],
          },
        };
      if (path === "/image-verifications/probe-1/confirm")
        return {
          id: "probe-1",
          state: "failed",
          image: "",
          error: "CLI 未返回真实图片，不自动重试",
          quote: {
            id: "probe-1",
            notice: "单张验证，费用未知",
            requests: [
              {
                provider: "Codex",
                model: "default",
                prompt: "cup",
                parameters: {},
              },
            ],
          },
        };

      if (path.endsWith("/test"))
        return {
          message: "检测失败：未安装",
          cli: {
            kind: "gemini",
            installed: false,
            auth: "unknown",
            models: [],
          },
        };
      throw new Error("Unexpected request: " + path);
    },
  );
});
afterEach(cleanup);
function mount() {
  const onClose = vi.fn();
  const cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={cache}>
      <ServiceSettings onError={() => {}} onClose={onClose} />
    </QueryClientProvider>,
  );
  return onClose;
}
function remote() {
  const p: Provider = {
    id: "remote",
    name: "测试远程服务",
    adapter: "openai",
    base_url: "https://example.test/v1",
    credential_source: "file",
    key_env: "",
    models: [newModel("test-model")],
  };
  settings.providers.push(p);
  return p;
}
function codex() {
  const p: Provider = {
    id: "cli-codex",
    name: "Codex",
    adapter: "cli",
    base_url: "",
    key_env: "",
    cli: {
      kind: "codex",
      path: "",
      image_instructions: "",
      skill_path: "",
      timeout_seconds: 600,
    },
    models: [newModel("test-model-a")],
  };
  settings.providers.push(p);
  return p;
}
describe("模型服务交互", () => {
  it("新 Codex 默认提供自动生图方式，无需工具说明，报价后仍需明确确认", async () => {
    const user = userEvent.setup();
    mount();
    await user.click(await screen.findByRole("button", { name: /配置 Codex/ }));
    expect((screen.getByLabelText("生图方式") as HTMLSelectElement).value).toBe(
      "codex",
    );
    expect(screen.queryByLabelText(/生图工具或 Skill 说明/)).toBeNull();
    expect(screen.queryByPlaceholderText(/本机 SKILL.md/)).toBeNull();
    expect(
      (screen.getByLabelText("输出尺寸（像素）") as HTMLInputElement).value,
    ).toBe("1024x1536");
    await user.click(screen.getByRole("button", { name: "查看验证报价" }));
    const confirm = await screen.findByRole("button", {
      name: "确认这一次验证",
    });
    expect((confirm as HTMLButtonElement).disabled).toBe(true);
    expect(
      mock.api.mock.calls.some(([path]) => String(path).endsWith("/confirm")),
    ).toBe(false);
    expect(settings.providers.at(-1)?.cli?.image_mode).toBe("codex");
    expect(settings.providers.at(-1)?.models[0].capabilities).toContain(
      "image",
    );
    expect(
      bindingOptions(settings, settings, "image").some((o) =>
        o.value.startsWith("cli-codex/"),
      ),
    ).toBe(false);
  });
  it("旧 Codex 保留通用工具方式，切换生图方式不会丢失工具草稿", async () => {
    codex();
    const user = userEvent.setup();
    mount();
    const mode = await screen.findByLabelText("生图方式");
    expect((mode as HTMLSelectElement).value).toBe("tool");
    await user.type(
      screen.getByLabelText(/生图工具或 Skill 说明/),
      "已有的图片工具",
    );
    await user.selectOptions(mode, "codex");
    expect(screen.queryByLabelText(/生图工具或 Skill 说明/)).toBeNull();
    expect(
      (
        screen.getByRole("button", {
          name: "查看验证报价",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(false);
    await user.selectOptions(mode, "tool");
    expect(
      (screen.getByLabelText(/生图工具或 Skill 说明/) as HTMLTextAreaElement)
        .value,
    ).toBe("已有的图片工具");
    expect(screen.getByPlaceholderText(/本机 SKILL.md/)).toBeTruthy();
  });
  it("已配置 Codex 自动加载真实目录，参数随模型联动且保存后可恢复", async () => {
    codex();
    settings.bindings.text = { provider: "cli-codex", model: "test-model-a" };
    const user = userEvent.setup();
    mount();
    await screen.findByRole("option", { name: /Test A/ });
    await user.selectOptions(screen.getByLabelText("推理强度"), "high");
    await user.selectOptions(screen.getByLabelText("服务档位"), "priority");
    await user.click(screen.getByRole("button", { name: "保存配置" }));
    await waitFor(() =>
      expect(settings.providers.at(-1)?.models[0].cli_options).toEqual({
        reasoning: "high",
        service_tier: "priority",
      }),
    );
    cleanup();
    mount();
    await screen.findByRole("option", { name: /Test A/ });
    expect((screen.getByLabelText("推理强度") as HTMLSelectElement).value).toBe(
      "high",
    );
    expect((screen.getByLabelText("服务档位") as HTMLSelectElement).value).toBe(
      "priority",
    );
    await user.type(screen.getByLabelText("搜索模型"), "test-model-b");
    expect(screen.queryByRole("option", { name: /test-c/ })).toBeNull();
    await user.selectOptions(screen.getByLabelText("模型"), "test-model-b");
    expect((screen.getByLabelText("推理强度") as HTMLSelectElement).value).toBe(
      "default",
    );
    expect((screen.getByLabelText("服务档位") as HTMLSelectElement).value).toBe(
      "default",
    );
    await user.click(screen.getByRole("button", { name: "保存配置" }));
    await waitFor(() =>
      expect(settings.bindings.text).toEqual({
        provider: "cli-codex",
        model: "test-model-b",
      }),
    );
  });
  it("路径变更丢弃旧目录响应，失败不覆盖模型草稿", async () => {
    codex();
    let resolveOld!: (value: CLICatalog) => void;
    const original = mock.api.getMockImplementation()!;
    mock.api.mockImplementation(async (path, body, method) => {
      if (path === "/cli/models") {
        if (!(body as { path: string }).path)
          return new Promise<CLICatalog>((resolve) => {
            resolveOld = resolve;
          });
        return {
          models: [],
          options: [],
          source: "unavailable",
          stale: false,
          error: "自定义路径不可用",
        };
      }
      return original(path, body, method);
    });
    const user = userEvent.setup();
    mount();
    await waitFor(() => expect(resolveOld).toBeDefined());
    await user.type(
      screen.getByPlaceholderText(/留空从 PATH/),
      "/different/codex",
    );
    await screen.findByText("自定义路径不可用");
    resolveOld(catalogFixture());
    await user.click(screen.getByRole("button", { name: "保存配置" }));
    await screen.findByText("配置已保存，旧报价自动失效。");
    expect(screen.queryByRole("option", { name: /Test A/ })).toBeNull();
    expect(settings.providers.at(-1)?.models[0].id).toBe("test-model-a");
    expect(
      screen.getByRole("option", { name: /test-model-a.*当前目录/ }),
    ).toBeTruthy();
  });
  it("刷新失败标记过期目录，不自动改选模型", async () => {
    codex();
    const original = mock.api.getMockImplementation()!;
    let fail = false;
    mock.api.mockImplementation(async (path, body, method) => {
      if (path === "/cli/models" && fail) throw new Error("private error");
      return original(path, body, method);
    });
    const user = userEvent.setup();
    mount();
    await screen.findByRole("option", { name: /Test A/ });
    fail = true;
    await user.click(screen.getByRole("button", { name: "刷新模型列表" }));
    await screen.findByText(/已缓存的 CLI 目录 · 已过期/);
    expect(screen.getByRole("option", { name: /Test A/ })).toBeTruthy();
    expect((screen.getByLabelText("模型") as HTMLSelectElement).value).toBe(
      "test-model-a",
    );
    expect(document.body.textContent).not.toContain("private error");
  });
  it("CLI 模型通过下拉选择，保留手填与默认模型入口", async () => {
    const user = userEvent.setup();
    mount();
    await user.click(await screen.findByRole("button", { name: /配置 Codex/ }));
    const select = screen.getByRole("combobox", { name: "模型" });
    expect((select as HTMLSelectElement).value).toBe("default");
    await screen.findByRole("option", { name: /test-model-b/ });
    expect(mock.api.mock.calls.some(([, , method]) => method === "PUT")).toBe(
      false,
    );
    await user.click(screen.getByRole("button", { name: "刷新模型列表" }));
    await screen.findByRole("option", { name: /test-model-b/ });
    await user.selectOptions(select, "test-model-b");
    await user.click(screen.getByRole("button", { name: "保存配置" }));
    await waitFor(() =>
      expect(
        settings.providers.find((p) => p.id === "cli-codex")?.models[0].id,
      ).toBe("test-model-b"),
    );
    await user.selectOptions(select, "__manual__");
    const custom = screen.getByLabelText("自定义模型 ID");
    await user.clear(custom);
    await user.type(custom, "custom-model");
    await user.click(screen.getByRole("button", { name: "保存配置" }));
    await waitFor(() =>
      expect(
        settings.providers.find((p) => p.id === "cli-codex")?.models[0].id,
      ).toBe("custom-model"),
    );
    await user.selectOptions(select, "default");
    expect(screen.queryByLabelText("自定义模型 ID")).toBeNull();
  });
  it("在 CLI 卡片间保留草稿，并在放弃前提示", async () => {
    const user = userEvent.setup();
    const close = mount();
    await screen.findByRole("button", { name: /配置 Codex/ });
    await user.click(screen.getByRole("button", { name: /配置 Codex/ }));
    const path = await screen.findByPlaceholderText(/留空从 PATH/);
    await user.type(path, "/my/codex");
    await user.click(
      screen.getByRole("button", { name: /Claude Code.*Anthropic/ }),
    );
    await user.click(screen.getByRole("button", { name: /Codex.*OpenAI/ }));
    expect(
      (screen.getByPlaceholderText(/留空从 PATH/) as HTMLInputElement).value,
    ).toBe("/my/codex");
    await user.click(screen.getByRole("button", { name: "关闭模型设置" }));
    expect(await screen.findByRole("alertdialog")).toBeTruthy();
    expect(close).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "继续编辑" }));
    expect(
      (screen.getByPlaceholderText(/留空从 PATH/) as HTMLInputElement).value,
    ).toBe("/my/codex");
  });
  it("Key 保存失败保留输入，成功后清空且不回显", async () => {
    remote();
    const user = userEvent.setup();
    mount();
    await screen.findByRole("tab", { name: /远程 API/ });
    await user.click(screen.getByRole("tab", { name: /远程 API/ }));
    const input = await screen.findByPlaceholderText("填写 API Key");
    await user.type(input, "secret-draft");
    rejectCredential = true;
    await user.click(screen.getByRole("button", { name: "保存配置" }));
    await screen.findByRole("alert");
    expect((input as HTMLInputElement).value).toBe("secret-draft");
    rejectCredential = false;
    await user.click(screen.getByRole("button", { name: "保存配置" }));
    await waitFor(() => expect((input as HTMLInputElement).value).toBe(""));
    expect(window.localStorage.length).toBe(0);
    expect(document.body.textContent).not.toContain("secret-draft");
  });
  it("切换到环境变量后不再提交隐藏的 Key 草稿", async () => {
    remote();
    const user = userEvent.setup();
    mount();
    await user.click(await screen.findByRole("tab", { name: /远程 API/ }));
    await user.type(
      await screen.findByPlaceholderText("填写 API Key"),
      "unsaved-key",
    );
    await user.selectOptions(screen.getByLabelText("凭据来源"), "env");
    await user.type(
      await screen.findByPlaceholderText("例如 IMAGE_API_KEY"),
      "TEST_KEY",
    );
    await user.click(screen.getByRole("button", { name: "保存配置" }));
    await screen.findByText("配置已保存，旧报价自动失效。");
    expect(
      mock.api.mock.calls.some(([path]) =>
        String(path).endsWith("/credential"),
      ),
    ).toBe(false);
    expect(
      settings.providers.find((p) => p.id === "remote")?.credential_source,
    ).toBe("env");
  });
  it("清除 Key 不修改其他配置草稿", async () => {
    const p = remote();
    p.credential_version = "old-version";
    const user = userEvent.setup();
    mount();
    await screen.findByRole("tab", { name: /远程 API/ });
    await user.click(screen.getByRole("tab", { name: /远程 API/ }));
    const name = await screen.findByLabelText("服务名称");
    await user.clear(name);
    await user.type(name, "未保存名称");
    await user.click(
      await screen.findByRole("button", { name: "清除当前 Key 绑定" }),
    );
    await waitFor(() =>
      expect(mock.api).toHaveBeenCalledWith(
        "/providers/remote/credential",
        { version: "old-version" },
        "DELETE",
      ),
    );
    expect((screen.getByLabelText("服务名称") as HTMLInputElement).value).toBe(
      "未保存名称",
    );
  });
  it("生图验证必须明确接受费用并确认，失败不会自动再次提交", async () => {
    settings.providers.push({
      id: "cli-codex",
      name: "Codex",
      adapter: "cli",
      base_url: "",
      key_env: "",
      cli: {
        kind: "codex",
        path: "",
        image_instructions: "existing image tool",
        skill_path: "",
        timeout_seconds: 600,
      },
      models: [{ ...newModel("default"), capabilities: ["text", "image"] }],
    });
    const user = userEvent.setup();
    mount();
    await user.click(
      await screen.findByRole("button", { name: "查看验证报价" }),
    );
    const confirm = await screen.findByRole("button", {
      name: "确认这一次验证",
    });
    expect((confirm as HTMLButtonElement).disabled).toBe(true);
    expect(
      mock.api.mock.calls.some(([path]) => String(path).endsWith("/confirm")),
    ).toBe(false);
    await user.click(screen.getByLabelText(/我接受本次验证费用未知/));
    await user.click(confirm);
    await screen.findByText("CLI 未返回真实图片，不自动重试");
    expect(
      mock.api.mock.calls.filter(([path]) => String(path).endsWith("/confirm")),
    ).toHaveLength(1);
  });
  it("未验证 CLI 生图不能进入制作绑定", () => {
    const p: Provider = {
      id: "cli",
      name: "CLI",
      adapter: "cli",
      base_url: "",
      key_env: "",
      cli: {
        kind: "codex",
        path: "",
        image_instructions: "tool",
        skill_path: "",
        timeout_seconds: 600,
      },
      models: [{ ...newModel("image-model"), capabilities: ["text", "image"] }],
    };
    settings.providers.push(p);
    expect(
      bindingOptions(settings, settings, "image").some((o) =>
        o.value.startsWith("cli/"),
      ),
    ).toBe(false);
    expect(
      bindingOptions(settings, settings, "text").some((o) =>
        o.value.startsWith("cli/"),
      ),
    ).toBe(true);
    expect(supportedKinds("anthropic")).toEqual(["text", "vision"]);
  });
});
