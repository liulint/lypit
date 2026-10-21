# Hypit 独立 Seedance 扩展

把这个文件夹交给支持终端和 Skill 的 AI，即可让它安装和使用。用于官方 Hypit 完成剧本后，通过 Seedance 生成视频素材；不修改官方 Hypit、不覆盖官方 Skill、不自动拆分剧本或替代 Hypit 的剪辑编排。

本文件夹的 `AGENTS.md` 提供项目入口：先使用**官方 `hypit` Skill** 分析参考、发展创意与剧本，再在需要 Seedance 素材时读取随包扩展。“生成视频吧”会从官方主流程进入报价和确认，不需要用户记住 Seedance 或 Skill 名称。扩展无需先全局安装即可在本目录读取，但官方 Skill 仍须单独核实与加载。其他 Agent 请先让它阅读本说明和项目入口。

## 直接发给 AI

> 请阅读这个文件夹的 README.md，按“AI 安装流程”分别核实官方 hypit Skill、官方 CLI 和独立 hypit-seedance 扩展，缺少的组件由你安装。npm 使用淘宝镜像，Go 按文档的下载源设置执行；若我指定 Go 安装包地址或模块代理，则使用我指定的源。复用已有安装，不修改官方文件。指导我在本地设置 ARK_API_KEY，不要让我在对话中发送密钥。安装后运行无付费演练，告诉我各组件状态；不要创建付费生成任务。以后看参考视频、改编创意和写剧本先使用官方 Hypit Skill，需要 Seedance 素材时再调用扩展，展示全部最终参数和报价，等我确认再生成。

用户无需自己执行下面的命令；命令供 AI 操作及排查时使用。

## AI 安装流程

1. 确定用户的视频项目绝对路径和这个工具文件夹的绝对路径。检查 `uname -sm`、`go version`、`node --version`、`command -v hypit`。本交付验证目标为 macOS Apple Silicon。其他平台不宣称已验证，不下载本包没有提供的可执行文件。
2. 复用已有环境。缺少 Go 时按下文“下载源设置”从 [Go 国内官方入口](https://golang.google.cn/dl/) 或用户指定的安装包源下载适合本机架构的 Go，最低 1.22，建议当前稳定版；安装方法见 [官方文档](https://golang.google.cn/doc/install)。Node.js 最低 22.15.0；已有更新受支持版本可复用，缺少时用现有版本管理器或 [Node.js 官方渠道](https://nodejs.org/en/download) 安装。系统安装需要权限时遵循当前 Agent 的权限机制，不擅自修改全局目录权限。
3. **分别检查官方 CLI 和官方 Skill，不可相互替代。** 缺少 CLI 时执行 `npm install -g @hypit/hypit --registry=https://registry.npmmirror.com`，运行 `hypit --version` 检查。无论 CLI 是否存在，都必须定位官方 `hypit/SKILL.md`；找不到则按 [官方说明](https://github.com/hypit-ai/hypit/blob/main/docs/guide/agents.md) 使用 `env npm_config_registry=https://registry.npmmirror.com npx skills add hypit-ai/hypit -g`，根据当前 Agent 选择正确目标目录。安装结果必须包含官方 Skill 的全部 references 和支持文件，而不只是单个 SKILL.md；安装后读取官方入口及当前任务所需 references。若官方 Skill 已存在则复用，不更新、改写或覆盖它。本扩展不要求克隆 Hypit 仓库。
4. 读取当前 Agent 的 Skill 安装规范，把本文件夹的 `skills/hypit-seedance/` 复制到独立 Skill 目录。当前 Codex 官方文档使用 `~/.agents/skills/hypit-seedance`（用户级）或视频项目下的 `.agents/skills/hypit-seedance`（项目级）；已有环境也可能加载 `${CODEX_HOME:-$HOME/.codex}/skills`，先核实实际加载目录再选一个，不要重复安装。Claude Code 通常为 `~/.claude/skills/hypit-seedance`；以实际 Agent 规范为准。已有同名**扩展**时，先复制到带时间戳且不重名的备份路径，确认备份完整后才更新；不删除备份。不要将内容放进官方 `hypit` Skill。
5. 在安装后的扩展目录新增 `installation-location.txt`，只写本工具源码文件夹的绝对路径。保留完整源码文件夹，移动后更新这个路径。不要把本机路径写回原始分发文件夹。必要时提醒用户刷新 Skill 列表或重新打开 Agent 会话。

   同时在用户的视频项目 `AGENTS.md` 中合并一段简短入口：先使用官方 `hypit` Skill 分析参考、评估改编和发展剧本；进入 Seedance 素材生成时再读取独立 `hypit-seedance` Skill，先报价、集中展示最终参数、用户确认后才提交，素材返回后交回官方工作流。本地工具未检查前不要断言缺少生成能力或要求连接其他服务。入口写入该用户**实际解析后的官方 Skill、扩展 Skill 和工具绝对路径**，保留已有项目规则，不整份覆盖，也不修改全局规则或官方 Skill。若只在本扩展文件夹试用，可以直接读取随包扩展，不必先完成扩展的全局安装；官方 Skill 仍需单独安装并读取。
6. 检查 AI 的终端进程能否读取 `ARK_API_KEY`，只检查是否存在，**不打印值**。让用户在本地终端、安全环境配置或 Agent 的安全设置中提供密钥；不要让用户把它贴进聊天，不要用含真实密钥的 shell 命令写入历史。需开启对应火山方舟模型权限和额度。安装 Go/Hypit 不包含模型额度。
7. 纯文本和 JPEG/PNG/GIF 图片不需要 FFmpeg。使用参考视频或其他受支持图片格式时检查 `ffprobe -version`；缺少时按需从 [FFmpeg 官方渠道](https://ffmpeg.org/download.html) 或已有 Homebrew 安装 FFmpeg。不要仅为文本生成安装视频工具。源码无第三方 Go 依赖。
8. 在工具目录运行 `go test ./...` 和 `go run . -dry-run -json`，演练不需要密钥、不创建任务。若仅 Go 默认编译缓存目录因沙箱不可写，将 `GOCACHE` 指向当前环境允许写入的独立缓存目录，不修改系统目录权限。最后分别报告 Go/Node、官方 CLI 版本、官方 Skill 路径及 references 完整性、扩展 Skill 路径、源码路径、密钥是否可用（不显示内容），以及可选依赖缺失项。不要只凭 `hypit --version` 就宣称整个官方工作流就绪，不要为了验收发起付费请求。

## 下载源设置（供 AI）

以下设置用于 AI 安装和执行命令，默认只对当前命令生效，不写用户的全局 npm 配置、Go 配置或 shell 启动文件。用户明确指定其他源时，以用户选择为准，并在安装结果中报告实际使用的源。

| 下载内容 | 默认来源 | 指定方式 |
|---|---|---|
| npm 包，包括 Hypit 和 skills CLI | 淘宝镜像 `https://registry.npmmirror.com` | npm 使用 `--registry`；npx 使用 `npm_config_registry` 环境变量 |
| Go 本体安装包 | 国内官方入口 `https://golang.google.cn/dl/` | 可指定完整 HTTPS 安装包 URL 或镜像目录；AI 核实实际文件地址后下载 |
| Go 模块及按需自动下载的工具链 | `https://goproxy.cn,direct` | 每次执行 Go 命令时传入 `GOPROXY`，也支持用户指定其他代理 |

**Go 安装包源与 GOPROXY 是两件事。** 尚未安装 Go 时，`GOPROXY` 不会替 AI 下载初始安装包。AI 从下载页选择实际版本与架构；Apple Silicon 使用文件名含 `darwin-arm64` 的安装包。用户指定镜像时使用相同版本与架构，并用 Go 官方发布信息中的 SHA-256 校验下载文件；不能只更换域名后假定文件存在，也不要硬编码一个“最新版本号”。

Go 模块代理的命令示例：

```sh
env GOPROXY=https://goproxy.cn,direct go version
env GOPROXY=https://goproxy.cn,direct go -C "$TOOL_ROOT" test ./...
env GOPROXY=https://goproxy.cn,direct go -C "$TOOL_ROOT" run . -dry-run -json
```

后续报价、生成和恢复等 Go 命令也使用同一选定的 `GOPROXY`；不同工具调用可能运行在不同 shell，不依赖上一条命令的 `export`。本工具没有第三方 Go 依赖，主要下载需求是 Go 安装包，以及当前 Go 版本确有需要时的自动工具链下载。保留 Go 的校验机制，不将 `GOSUMDB` 设为 `off`。`direct` 仅在代理返回 404/410 时回源，不表示所有网络错误都会自动回源。

淘宝 npm 镜像只影响 npm 包下载，**不会加速 GitHub 上的官方 Hypit Skill 仓库，也不会替换 Node.js 安装包下载源**。这几条链路失败时分别排查。来源：[npm 镜像项目](https://github.com/cnpm/cnpmcore)、[Goproxy.cn](https://goproxy.cn/)、[Go 工具链说明](https://go.dev/doc/toolchain)、[Go 模块代理规则](https://go.dev/ref/mod#goproxy-protocol)。

## 对话使用

用官方 Hypit 确定剧本后，可以说：

> 使用 hypit-seedance，把刚才确定的提示词生成一条视频，先给我看最终参数和报价。

在已配置入口的项目内，直接说“生成视频吧”也应触发同一流程。这个请求只是启动报价，AI 仍须在展示全部最终参数后取得付费确认。已有会话未读取新入口时，明确让它读取当前项目的 `AGENTS.md` 和随包 Skill；也可以在该项目新建会话。项目指令和 Skill 的发现方式见 [Codex 官方定制说明](https://learn.chatgpt.com/docs/customization/overview)。

例如“看这个视频前 15 秒，想改成教资内容”也属于官方 Hypit 的参考理解与改编阶段，应先读取官方 Skill 及其 reference-video、brief、transformations 等当前问题相关指引；这个阶段不调用 Seedance 报价或付费生成。抽帧可以是官方工作流中的工具操作，但单独用 ffmpeg 抽帧、给自由创意建议，不等于已使用官方 Hypit Skill。

AI 会展示模型、分辨率、声音、时长、比例、完整提示词、参考素材（没有则为“无”）、数量、单价和预估费用。全部默认值也必须确认。用户修改任何生成参数或素材，AI 重新报价并取得确认。用户确认的是**这一次、这一批**生成，不代表无限次授权。

默认 Mini + 480p + **有声** + 5 秒 + 16:9 + 1 条。声音开关使用 `-audio=true` 或 `-audio=false`；不要写成 `-audio false`。固定比例支持 16:9、4:3、1:1、3:4、9:16、21:9；时长仅支持 4–15 秒整数；数量 1–100，每条分别计费。不自动切分剧本。

## 命令接口（供 AI）

下列变量由 AI 根据实际路径设置；始终正确引用，不要把提示词插入 shell 命令。

```sh
TOOL_ROOT='/实际的工具源码绝对路径'
JOB_DIR='/实际的视频项目绝对路径/seedance-output/本次任务唯一目录'
PROMPT_FILE='/实际的视频项目绝对路径/最终提示词.txt'

# 提示词先通过文件工具写入 UTF-8 文件；报价不需要密钥
go -C "$TOOL_ROOT" run . -quote -json -prompt-file "$PROMPT_FILE" -out "$JOB_DIR"

# 用户确认后，使用上述报价返回的 confirmation_id；其余参数保持一致
go -C "$TOOL_ROOT" run . -json -prompt-file "$PROMPT_FILE" -out "$JOB_DIR" \
  -confirm '上述报价返回的完整confirmation_id'

# 恢复仅查询和下载已保存 ID，不需要重新确认生成
go -C "$TOOL_ROOT" run . -json -resume "$JOB_DIR/run.json"
```

`go -C` 会切换工作目录，因此必须传**绝对**素材路径、提示词路径和 `-out`，避免把运行输出写进工具分发文件夹。直接在项目目录运行本地构建的工具时，不传 `-out` 默认使用 `./seedance-output/<confirmation_id>`。

完整可选参数示例（报价和提交使用同一组）：

```sh
go -C "$TOOL_ROOT" run . -quote -json -prompt-file "$PROMPT_FILE" -out "$JOB_DIR" \
  -model fast -resolution 720p -audio=false -duration 8 -ratio 9:16 -count 2 \
  -image '/绝对路径/参考一.png' -image '/绝对路径/参考二.jpg' \
  -video-url 'https://你的公网地址/reference.mp4'
```

模型支持 `mini`、`fast`、`standard`，分别解析为官方完整 ID：`doubao-seedance-2-0-mini-260615`、`doubao-seedance-2-0-fast-260128`、`doubao-seedance-2-0-260128`。也可直接传完整 ID。接入点或未知版本使用 `-model ep-xxx -price-model mini` 等方式显式关联真实模型；AI 必须先核实映射，不得猜测。未知模型未关联时仍可查看报价，但费用为未知且禁止提交。

`-quote` 和 `-dry-run` 永不创建任务，但带公网视频参考时会匿名读取素材做校验；不带素材的 `go run . -dry-run` 仅本地运行。JSON 提供规范化参数、参考素材及内容哈希、报价、来源、核实日期、`can_submit` 和 `confirmation_id`。确认标识绑定参数、素材内容和计价依据；不是用户授权证明，AI 仍须取得对话中的明确确认。

## 素材校验

- 图片为本地 JPEG、PNG、GIF；WebP、BMP、TIFF、HEIC、HEIF 需 ffprobe 核实尺寸。每张小于 30 MB，最多 9 张；宽高均为 300–6000，宽高比 0.4–2.5；Base64 编码后总请求体不超过 64 MB。图片固定为 `reference_image`，不使用首尾帧模式。
- 视频仅接受无用户名密码的 HTTP(S) 公网 URL，不上传本地视频、不传入账户 Authorization。先匿名读取至临时文件做哈希和 ffprobe 校验，使用后删除该单个临时文件。最多 3 个，每个不超过 200 MB、2–15 秒，总时长不超过 15 秒；MP4/MOV，H.264 或 HEVC，24–60 fps；尺寸和像素数按官方限制校验。
- 素材不可读取、缺少 ffprobe 或不合规时，报价明确标记无法核实且 `can_submit=false`，不能提交。用户修改素材后重新报价。公网视频提交给 Ark 的仍是原始 URL，请确保生成期间地址有效且内容不变；客户端无法锁定服务端最终读取的远程内容。
- 不支持参考音频、自动时长、自动比例、本地视频上传、首尾帧或样片模式。账户和内容资格仍由官方服务审核。

## 计价依据

官方资料核实日期 **2026-10-21**，价目表集中在 `pricing.go`。以下均为元/百万 token，有声和无声相同，480p/720p 相同，不自动使用账户优惠：

| 模型 | 无视频输入（含图片参考） | 含视频输入 |
|---|---:|---:|
| Mini | 23 | 14 |
| Fast | 37 | 22 |
| 标准版 | 46 | 28 |

来源：[官方价格](https://ark.volcengine.com/region:cn-beijing/docs/model-pricing.md)、[API 和尺寸表](https://ark.volcengine.com/region:cn-beijing/docs/create-video-generation-task-api.md)、[模型 ID 与教程](https://ark.volcengine.com/region:cn-beijing/docs/video-generation-tutorial.md)。本次核实与需求一致。价格会变化，后续更新必须同步核实日期和测试，不能宣称永久有效。

估算 token =（参考视频总秒数 + 输出秒数）× 输出宽 × 输出高 × 24 / 1024，向上取整。Seedance 2.0 的 480p 16:9 为 **864×496**，720p 16:9 为 **1280×720**。默认无视频参考的预估为 50,220 token，约 **¥1.1551/条**。

有视频输入时还须与官方最低 token 量取较大值。[最低量表](https://bytedance.larkoffice.com/wiki/FXaYwxzJ5i5Zdik32ipcWzt7nxd?table=tblheIYDXXwYM43b&view=vew2CR2JYM) 尚未完整核实：

- 5 秒、16:9、已通过素材校验时，使用官方价格页同规格的**四舍五入示例范围**，不是伪造精确金额。Mini 480p 示例为 ¥1.27–2.81/条，覆盖输入总时长 2–15 秒。
- 其他规格显示公式下界和未知上界；缺少时长时费用未知。JSON 中 `max=null` 表示上界未知，**不能把 min 当作最终费用或封顶价**。AI 必须把这一限制展示给用户并确认；若用户要求明确费用上限，停止提交，直到核实官方最低量或用户改用可明确估算的规格。

生成成功后按实际 **`usage.completion_tokens` × 已保存单价 / 1,000,000** 计算刊例价估算；`total_tokens` 仅展示，不代替计费字段。结果不等于账户账单。实际模型或分辨率与确认依据不一致时显示费用未知；旧记录缺少单价或用量也不猜测。

## 恢复、失败和记录

每批独立目录存放 `run.json`，每条成功视频按任务 ID 命名。记录先于下载保存，包含最终参数、引用素材哈希、计价依据、实际模型和用量；包含提示词及素材地址，请按私人项目文件保管。新目录和记录分别使用 0700/0600 权限。

- **未确认不付费**：提交必须带本次报价的完整 `-confirm`；修改参数或素材后旧标识失效。
- **禁止覆盖**：已有 `run.json` 时拒绝新建，请恢复。已有视频不会被覆盖；只有记录确认该文件已下载时才跳过下载。相同参数的另一批生成必须是用户明确提出的新请求，使用新目录并重新确认。
- **POST 不重试**：任务创建前保存 `submission_unknown` 意图，成功返回即保存任务 ID。网络失败、HTTP 错误、进程崩溃或成功后写盘失败都不自动重提。记录提示的 ID 应保留；没有 ID 时到官方控制台核实，无法自动找回。
- **部分失败**：`-count` 按顺序提交，失败就停止。已创建、结果未知、未提交的项分别保留；恢复会处理已知 ID，并明确报告其余项，绝不补提交。剩余项需要用户新的确认。
- **恢复**：`-resume` 只接受记录路径及 `-json`、`-poll`、`-wait`；只执行查询和下载。默认查询间隔 10 秒、等待 30 分钟；超时或 Ctrl-C 后仍可恢复。
- **下载失败**：记录保留用量和费用，恢复可重试下载；结果地址过期时查询能否刷新由官方服务决定。重复恢复更新每条记录，不做累加计费。
- **并发和异常退出**：同一目录使用空目录 `.seedance-lock` 防并发。正常退出会释放；强制终止可能遗留。AI 必须确认旧进程已退出，再仅移除这个明确的空锁目录，不能清空任务目录。
- **旧记录**：支持没有版本字段的本工具批次记录，以及含 `id` 或 `task_id` 的旧单任务 JSON；没有返回用量明确显示“未返回”。不保证识别其他工具的任意格式。先备份外来旧记录再恢复。
- **凭据隔离**：Ark 固定官方 API 地址，不接受环境变量替换到未知网关；API 不跟随重定向。媒体读取和下载用独立匿名客户端，拒绝内网地址。错误响应不原样打印。密钥仅来自 `ARK_API_KEY`。

## 开发验证

```sh
go test -race ./...
go vet ./...
go run . -dry-run
go build -o /tmp/hypit-seedance-check .
/tmp/hypit-seedance-check -dry-run -json
```

模拟测试不调用真实付费服务。视频测试使用本地 ffmpeg 生成临时样例，缺少 ffmpeg/ffprobe 时会明确跳过相关测试；纯文本和图片测试仍执行。开发用二进制不要放进分发文件夹。Skill 可使用 skill-creator 的 `quick_validate.py` 校验，行为还需审阅确认流程。测试结束关闭模拟 HTTP 服务，不遗留监听进程。
