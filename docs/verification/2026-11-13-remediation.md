# 整改验证记录（2026-11-13）

范围：按《交付整改清单》处理缺失项 M-1~M-5 与整改项 R-1~R-5、R-7、R-9、R-10，并复核全链路。

环境：Windows amd64、Go 1.22 语法基线（本机工具链较新）、Node.js 22.22.1。

## 一、整改内容与证据

| 编号 | 整改 | 证据 |
| --- | --- | --- |
| R-7 | 删除产品化 v1 接口（`product.go`、`product_auth.go`、`workspace_product.go`、`product_config.go`、OpenAPI、接口文档及其测试） | 生产 Go 代码由 3232 行降至 2992 行；`grep` 无残留引用 |
| R-3 | 批次校验改为两级：逐操作校验片段自身约束，整批结束后统一校验重叠 | `TestBatchCanSwapOrder`：`clip-a` 0→6000、`clip-b` 6000→0 一次通过；`TestSingleMoveIntoOverlapStillRejected`：真碰撞仍 `OVERLAP`；`TestBatchEndingInOverlapAppliesNothing`：中间步成功但末尾非法时零修改 |
| R-1 | 错误文案单一来源 | `TestInstructionFeedbackStatesReasonOnce`：10 个错误码的用户文案中"本条指令未执行任何修改"均只出现 1 次；实测锁轨响应重复次数由 2 → 1 |
| R-2 | 新增 `TRACK_INVALID` | `TestTrackProblemsAreEditProblems`：错误码 `TRACK_INVALID`，文案含真实原因（字幕轨道/不存在），不再出现"检查工程文件" |
| R-5 | 重复 JSON 字段拒绝 | `TestDuplicateJSONKeysAreRejected`：顶层、嵌套、操作数组三处均拒绝；实测导入 `{"revision":1,"revision":2}` 返回 `INVALID_JSON` |
| R-4 | Host 白名单 | `TestForeignHostIsRejected`（403）、`TestLoopbackHostsAreAccepted`（`127.0.0.1`/`localhost`/`[::1]`/大小写） |
| R-9 | 不再忽略快照错误 | `captureRecentCaption` 在快照失败时清空引用，不写入零值绑定；既有 `TestReferenceRecentCaptionSurvivesUnrelatedVideoSelection` 仍通过 |
| R-10 | 契约漂移测试 | `TestSchemaMatchesDecoder`：Schema 与 `Project`/`Clip`/`Track` 的字段集、必填集双向一致；`TestExamplesRespectSchema`：三个样例工程的字段与必填项均符合 Schema 且可解码 |
| M-1 | `docs/design-tradeoffs.md` | 两页内回答三问：解析/执行隔离、误操作防线（五层表 + 两处放宽）、未实现清单（含代价与触发条件） |
| M-2 | `docs/design.md` | 分层、数据模型、操作定义、校验粒度、版本与撤销、错误码、请求边界、已知限制 |
| M-3 | `LICENSE`（MIT） | `THIRD_PARTY.md` 改为引用该文件，消除"许可证由所有者选择"的自相矛盾 |
| M-4 | `docs/demo.md` | 四条路径的逐步操作、预期界面反馈与失败判定，附命令行复现 |
| M-5 | `docs/acceptance-matrix.md` | 8 条必做场景 → 测试函数 → 命令 → 预期证据，另附格式、边界、批次、前端四张表 |
| — | `SCOPES.md` | 四节：扩展项、文档外实现（含代价与可否摘除）、明确不做、与文档的偏差（`shorten_by_ms`、`display_name`、批次临时重叠） |

## 二、回归结果

| 检查 | 结果 |
| --- | --- |
| `gofmt -l .` | 无输出 |
| `go vet ./...` | 通过 |
| `go build ./...` | 通过 |
| `go test -count=1 ./...` | 两个包通过；测试函数 98 个 |
| `go test -cover ./internal/agent` | 87.1%（整改前 86.1%，产品接口移除后分母变小） |
| `npm test` | 20 项通过 |

## 三、端到端复核（实际启动服务）

| 场景 | 结果 |
| --- | --- |
| 交换顺序 `把 clip-b 移到开头，然后把 clip-a 移到 6000 毫秒处` | 预览两项变更；确认后 revision 2、位置互换；撤销后内容与原工程等价、revision 3；重复撤销 409 |
| 锁轨 `把 caption-lock 的字幕改为"测试"` | `TRACK_LOCKED`，"本条指令未执行任何修改"出现 1 次 |
| 轨道类型 `把 clip-a 移到 caption-locked 轨道的 1 秒处` | `TRACK_INVALID`，说明视频/字幕不能混放 |
| 重复字段导入 | `INVALID_JSON`，工程未变 |
| 伪造 `Host: attacker.example` | 403 |

## 四、本轮未处理（需决策或环境支持）

| 编号 | 原因 |
| --- | --- |
| R-6 模型计划的目标一致性校验 | 需要先定义"指令中出现的合法目标集合"的判定规则，否则会误拒"按名称或上下文定位"的合法计划。建议单独一轮实现并配对抗样本集 |
| R-8 离线缺单位/缺引号改澄清 | 属交互优化，不影响验收场景；改动会改变既有句式测试的预期，建议与 R-6 同批做 |
| R-11 提交粒度 | 需要按功能拆分历史提交，属仓库操作，本次不代做 |
| R-12 本地 `go test -race` | 本机缺 C 编译器（cgo 关闭），仍需 CI 或安装 gcc 后验证 |

## 五、一致性说明

`docs/verification/2026-11-12-test-report.md` 记录的是整改前的实测数据（86.1% 覆盖率、含产品接口测试），保留为历史证据；当前状态以本文件为准。
