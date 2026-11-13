# 安全剪辑指令智能体

用自然语言修改简化时间轴：导入工程 → 澄清或生成结构化计划 → 查看实际差异 → 明确确认 → 整体提交 → 撤销/重做。

**默认离线，不需要模型账号、密钥、数据库、公司接口或生产素材。** 本项目编辑 JSON 工程，提供简单本地媒体预览，不编码导出视频成片。后端 Go 1.22+，前端原生 HTML/CSS/JavaScript，运行无第三方模块、npm 安装或 CDN 依赖。

## 拉下来就能运行

安装 [Go 1.22 或更新版](https://go.dev/dl/)，进入拉取的项目根目录。

Windows PowerShell：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/start.ps1
```

macOS/Linux：

```sh
sh scripts/start.sh
```

打开 [本地工作台](http://127.0.0.1:8090)，点击“加载演示工程”。脚本默认强制离线，只改变自己的运行进程环境，不修改用户系统配置。端口冲突时用 `scripts/start.ps1 -Port 8091` 或 `PORT=8091 sh scripts/start.sh`。

通用构建与启动：

```text
go test -count=1 ./...
go build ./...
```

需要独立二进制时，Windows 执行 `go build -o safety-edit-agent.exe .`，macOS/Linux 执行 `go build -o safety-edit-agent .`，再运行对应程序。网页与默认演示工程均内嵌，二进制可脱离源码目录运行；用户只需浏览器，不需安装 Go。下载 Actions 产物后，macOS/Linux 需先给对应程序执行权限（`chmod +x 程序文件名`）。直接运行会读取当前进程的可选模型配置，若要忽略旧配置请使用启动脚本。

GitHub 的发布包工作流可生成 Windows、macOS、Linux 的 amd64/arm64 独立程序及校验和。工作流产物在 Actions 下载，不会自动创建公开 Release；是否发布由仓库所有者控制。

## 能力

| 功能 | 行为 |
| --- | --- |
| 移动 | 同轨或同类型换轨，明确时间，碰撞拒绝 |
| 裁剪 | 修改视频右端，保持时间轴起点和素材入点 |
| 字幕 | 修改文字、起点或时长；不自动跟随视频 |
| 删除 | 预览后确认才删除，可整体撤销 |
| 插入副本 | 从工程中已有视频/字幕复制出新稳定 ID，指定同类型目标轨道与起点；来源不变，不生成新素材 |
| 组合指令 | 按顺序在工作副本计算，全部通过后一次提交；任一步失败整条不生效 |
| 可视化差异 | 修改前后以同一时间比例展示，文字列出字段变化；取消不修改工程 |
| 编辑记录 | 查看已提交的实际变化、版本、时间及撤销/重做；页面刷新可读回 |
| 撤销与重做 | 恢复整条指令；版本继续增加；新编辑清空重做分支；重复请求不会重复消费历史 |
| 简单媒体预览 | 选择视频片段，使用内嵌演示封面或绑定本地视频/图片，按素材入点预览，字幕独立按时间轴叠加 |
| 可选在线模型 | 兼容 Chat Completions JSON/SSE，支持咨询、澄清、候选计划和不支持反馈；仍由 Go 校验并等待确认 |

不支持自动字幕、画面检测/修复、降噪、分镜生成、转场、音频轨道、音画字幕自动联动、波纹编辑、云存储、多人协作、视频编码导出或真实编辑器集成。项目不接入任何外部剪辑器或业务系统。

## 五分钟演示

1. 加载演示工程，输入 `把 clip-a 缩短 2 秒`。查看 5→3 秒的文字和时间轴对比；确认前片段仍为 5 秒。
2. 点击“确认修改”，检查实际变化；点“撤销”恢复，再点“重做”重新应用。展开编辑记录查看真实字段。
3. 重新加载样例，输入 `把第二段移到开头`，显示两个视频轨候选；选择后如发生碰撞则拒绝。
4. 输入 `把 caption-lock 的字幕改为“新文案”`，锁轨拒绝；输入 `把 clip-a 缩短 1 秒；删除 caption-lock`，整条拒绝且第一项也不生效。
5. 输入 `删除 clip-d`，取消后片段和版本不变。
6. 在插入区选择来源、未锁定的同类型轨道和空闲时间，先预览再确认；也可以输入 `在 video-2 轨道的 12 秒处插入 clip-a`。
7. 选择时间轴中的视频片段，点击“使用演示封面”或绑定本地视频/图片，拖动时间位置查看。未绑定媒体时仍能完成全部 JSON 编辑验收。
8. 导出当前工程 JSON，再导入下载文件核对结果。

实现与验证结果见 [整改验证记录](docs/verification/2026-11-13-remediation.md)（最新）与 [2026-11-12 测试记录](docs/verification/2026-11-12-test-report.md)（历史），架构约定见 [架构决策](docs/adr/0001-maintainable-core.md)。

## 离线句式与目标引用

| 操作 | 示例 |
| --- | --- |
| 缩短 | `把 clip-a 缩短 2 秒` |
| 移动 | `把 clip-b 移到 12 秒处` |
| 换轨 | `把 clip-b 移到 video-2 轨道的 12 秒处` |
| 字幕文字 | `把 caption-b 的字幕改为“下期见”` |
| 字幕时间 | `把 caption-a 的时间改为从 3 秒到 5 秒` |
| 删除 | `删除 clip-d` |
| 插入副本 | `在 video-2 轨道的 12 秒处插入 clip-a` |
| 多操作 | `把 clip-a 缩短 1 秒；把 caption-b 的字幕改为“再见”` |

时间使用阿拉伯数字加“秒”或“毫秒”，秒最多三位小数，毫秒为整数。公开句式之外的表达可能无法理解，不自动调用在线模型。组合可用分号、换行、“，然后”或“，再”，字幕引号内的分号属于正文。

支持唯一名称、点选后“这段”、视频轨道内序数，以及已选字幕或最近已确认字幕的“刚刚那个字幕”。名称重复或多轨命中必须澄清；按轨道内起点排序，不按 JSON 数组顺序编号。“把 X 移到 A 和 B 之间”仅移动已有 X 到明确且足够的空隙。重新导入清除选中与旧上下文。

插入表单不依赖 ID 的句式安全性，支持符合工程约定的特殊 ID。来源在锁定轨道也可只读复制，目标必须同类型、未锁定；新副本保留原时长、素材区间/字幕文字与可选名称。同名后按原有歧义规则处理。

## 工程格式与确定性规则

见 [主样例](examples/timeline.json)、[双锁轨样例](examples/timeline3.json)、[多轨样例](examples/aurora-coffee-promo.json)（6 轨 16 片段，用于验证导入不依赖预置 ID 与顺序）和 [JSON Schema](docs/timeline.schema.json)。

顶层 `project_id`、正整数 `revision`、`tracks` 和 `clips`；轨道为唯一 `id`、`kind`（video/caption）、明确的布尔 `locked`。片段必填唯一 `id`、`track_id`、`start_ms`、正数 `duration_ms`；视频还需 `source_in_ms`、`source_duration_ms`，字幕需 `text`，不能混用。可选 `display_name` 最多 128 个 Unicode 码点；可省略或为空字符串，不接受 null。

- 时间为整数毫秒，片段区间 `[start_ms, start_ms + duration_ms)`。同轨不重叠，端点相接允许，不同轨可重叠，碰撞拒绝且不自动推移。
- 一条指令的多项操作按顺序在工作副本上执行：每步只校验片段自身约束（时间、时长、源区间、引用、锁轨），整批结束后再统一校验同轨重叠并提交。因此"交换两段顺序"这类必须经过临时重叠的指令可以表达，而最终提交态仍不允许重叠。
- 时间轴起点表示显示位置，素材入点表示从源素材哪里开始取；视频必须满足 `source_in_ms + duration_ms <= source_duration_ms`。右端裁剪保持起点和素材入点不变。
- 视频变化不自动移动、缩短或删除字幕；字幕操作必须明确出现在计划中。
- 严格拒绝未知 JSON 字段、重复 JSON 字段、错误类型、未知 ID、null、非法源区间和时间溢出。`start_ms:0` 也须明确提供。非法导入不替换工程、不清空已有计划和历史。
- 计划绑定工程 ID、版本和加载标识；确认时再检查，重新导入同一 ID、同一版本也会使旧计划失效。重复确认、取消后确认、处理期间变化均拒绝。
- 每次提交、撤销或重做都让 revision 加一。恢复业务内容时不回退版本。取消、失败、预览和导出不推进版本。
- 模型只生成数据，不能执行脚本、命令或自行确认。报告来自实际提交前后 Diff。

插入操作结构：`{"type":"insert","target_id":"新的唯一ID","source_id":"现有片段ID","track_id":"目标轨道ID","start_ms":12000}`。与四类原操作一样经过独立校验。插入新增目标，其他操作不能携带 `source_id`。

## 保存、媒体与运行边界

- 不覆盖导入文件。导出仅包含已提交工程，不包含待确认计划、媒体文件、编辑记录或撤销栈；发起下载不等于文件已经保存成功。
- 工程、历史、会话和模型连接检查状态在服务进程内存中。同地址同浏览器有效会话刷新可读回工程和编辑记录；聊天及待确认卡片不恢复。匿名会话默认空闲 30 分钟过期，请求到来时清理；达到默认 128 个会话时回收最久未访问项。会话回收、程序重启或重新导入会丢失未导出工程与历史。
- 撤销深度最多 100 条，待确认计划最多 64 个，撤销及重做请求标识各最多 1024 个，实际编辑记录最多 1000 条。达到上限明确拒绝，不静默删除记录。可导出后重新导入开始新历史。
- 最多 128 条轨道、4096 个片段、单请求 1 MiB、单指令 64 项操作。前后端统一最多 2000 个 Unicode 字符（含中文与 emoji），服务端通过会话接口返回上限。
- 媒体仅通过浏览器临时对象 URL 绑定，限 512 MiB 的本地视频或 PNG/JPEG/WebP/GIF，不上传到服务端。刷新或重新导入需重新绑定；不能随工程 JSON 带走本地文件。被复制插入的片段在本页面可继承来源绑定。
- 简单预览展示当前所选视频片段，不做多视频轨合成或音频混音；字幕按独立时间范围显示。实际文件长度不足或编码不受浏览器支持会明确提示，JSON 编辑仍可用。
- 同一连接来源每分钟最多新建 20 次匿名会话，已连接会话不消耗此额度；反向代理转发头不作为可信来源。
- 修改类请求同时校验 `Origin` 与 `Host`：跨站 Origin 直接拒绝，Host 必须在白名单内（默认 `127.0.0.1`、`localhost`、`::1`，可用 `ALLOWED_HOSTS=host1,host2` 扩展）。这样即使域名被重新绑定到本机，请求也会因 Host 不在白名单而被拒。
- 默认只监听 127.0.0.1。不同浏览器会话相互隔离，不承诺多人服务容量或生产部署。

## 在线模型：验收方自行配置，可选

离线验收无需这一节。不要把提交者的密钥、私有网关或账号作为验收前提。

使用兼容 Chat Completions 的服务，程序在基础地址后追加 `/chat/completions`。Windows 可以私密输入自己的密钥：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/start.ps1 -Online -ModelBaseURL 'https://your-provider.example/v1' -Model 'your-model-name'
```

将地址和名称替换为供应商实际配置。未在环境中提供密钥时，脚本使用隐藏输入，密钥只传入子进程，不写入文件或源码。无认证的本地兼容服务可以直接配置地址/模型后 `go run .`。

macOS/Linux：

```sh
export LLM_BASE_URL='https://your-provider.example/v1'
export LLM_MODEL='your-model-name'
printf 'Your model API key: '
read -r -s LLM_API_KEY
printf '\n'
export LLM_API_KEY
sh scripts/start.sh --online
```

`read -s` 适用于 bash/zsh，在对应交互 shell 中输入。不要把真实密钥粘贴到命令历史或上传仓库。`.env.example` 只是字段参考，程序不自动读取 `.env`。

可选 `LLM_JSON_MODE=true`（供应商支持才开）、`LLM_THINKING=enabled/disabled`、`LLM_MAX_TOKENS`、`MODEL_TIMEOUT_SECONDS`、`MODEL_CONCURRENCY`。默认输出上限 4096；供应商不兼容的参数应省略。

页面默认仍为离线。在线状态分为未配置、待适配、已连通和暂不可用。“检查连接”真实请求 JSON 与 SSE 两条咨询路径，不改工程；“已连通”不证明任意剪辑表达均正确。模型生成计划后仍需校验和明确确认。

SSE 要求正常 `finish_reason=stop` 和 `[DONE]`；断流、超时、停止、截断及非法 JSON 均不提交。不支持流式的服务明确失败，不模拟打字或悄悄切换供应商。格式错误最多修复重试一次；认证、限流和网络失败不自动重试。可手动切回离线，不修改工程。

不同供应商的连接和理解效果需要分别验收；本次未使用在线密钥重新验证供应商能力。

## 开发与检查

运行程序只需要 Go 1.22+。开发页面需 Node.js 22.13+ 或 24+；npm 依赖仅用于格式和静态检查，不参与程序运行。

```text
go test -count=1 . ./internal/...
go vet . ./internal/...
go build . ./internal/...
npm ci
npm test
npm run lint
npm run format:check
```

Go 格式检查：`gofmt -l *.go internal` 应无输出；整理用 `gofmt -w *.go internal`。页面整理用 `npm run format`。Go 命令限定源码目录，避免将开发依赖中附带的 Go 示例作为本项目编译。

```text
go test -run '^$' -bench . -benchmem ./internal/agent
go test -run '^$' -fuzz '^FuzzOfflineParser$' -fuzztime=300s ./internal/agent
go test -race -count=1 . ./internal/...
```

竞态检查需要支持 cgo 的 C 编译工具，也在 Linux CI 执行。核心包覆盖率门槛为 85%，不把入口启动逻辑的覆盖率混入核心门槛。实测结果见验证记录。

[CI](.github/workflows/ci.yml) 检查 Go 格式、测试、vet、构建以及页面测试、ESLint 和 Prettier；Linux 稳定版增加覆盖率、竞态和短时 fuzz。[发布工作流](.github/workflows/release.yml) 生成独立二进制，不自动公开发布。云端状态以实际 Actions 为准。

## 代码入口与后续扩展

| 位置 | 责任 |
| --- | --- |
| `main.go` | 配置、资源内嵌、启动与关闭 |
| `internal/agent/engine.go`、`op_*.go`、`operations.go` | 工程校验、深拷贝、操作注册与原子计算 |
| `workspace.go` | 版本绑定、计划确认、撤销与编辑记录 |
| `parser.go`、`grammar.go`、`target_resolver.go` | 离线编排、句式与目标定位 |
| `server.go`、`dialog.go`、`session.go` | 路由、对话编排、会话生命周期 |
| `capability.go`、`feedback.go`、`observability.go` | 能力策略、错误码、请求日志 |
| `model.go`、`stream.go` | 可选模型客户端与流式协议 |
| `web/*.js` | 状态、各面板与控件，原生 ES Module |

新增操作先补回归，再新增 `op_*.go` 并显式注册；字段校验和模型说明由注册项派生。离线句式和新增交互按需求增加，不复制现有业务分支。新增可变字段时补深拷贝隔离测试。持久化、执行器与真实格式适配在具体需求出现后设计，不预建空接口。

产品化接口（多工程、身份、条件写入）不在本任务范围内，已从仓库移除，原因与代价见 [范围声明](SCOPES.md)。

## 文档地图

| 文档 | 内容 |
| --- | --- |
| [设计说明](docs/design.md) | 分层、数据模型、操作定义、校验粒度、版本与撤销、错误码 |
| [设计取舍](docs/design-tradeoffs.md) | 为什么隔离解析与执行、如何避免误操作、没有实现什么（两页） |
| [演示脚本](docs/demo.md) | 修改 / 澄清 / 失败 / 撤销四条路径的逐步操作与判定标准 |
| [验收矩阵](docs/acceptance-matrix.md) | 每条必做场景对应的测试函数与命令 |
| [范围声明](SCOPES.md) | 扩展项、文档外实现、明确不做、与文档的偏差 |
| [架构决策](docs/adr/0001-maintainable-core.md) | 重构层面的历史取舍 |
| [测试与验收记录](docs/verification/2026-11-13-remediation.md) | 本轮整改的实测证据（[整改前记录](docs/verification/2026-11-12-test-report.md)） |

工程格式以 [JSON Schema](docs/timeline.schema.json) 为准，字段与解码器的一致性由 `TestSchemaMatchesDecoder` 守护。来源与复用见 [THIRD_PARTY.md](THIRD_PARTY.md)，项目许可证见 [LICENSE](LICENSE)（默认 MIT，如需内部专有请替换该文件）。

阶段计划、历史截图及临时生成物已清理；现状以本 README 为准，架构取舍在 ADR，验证结果在 `docs/verification/`，版本变化在 [CHANGELOG](CHANGELOG.md)。不提交密钥、会话、下载、缓存、node_modules 或旧二进制。
