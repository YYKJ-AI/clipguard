# 验收矩阵

每条任务书必做场景对应到自动化测试与可执行命令。命令均在仓库根目录运行。

## 一、必做验收场景

| # | 场景 | 测试函数 | 命令 | 预期证据 |
| --- | --- | --- | --- | --- |
| 1 | "把 `clip-a` 缩短 2 秒"：预览精确新时长，确认后应用，撤销后与原始 JSON 等价 | `TestEditingOperations`（right trim）<br>`TestConfirmCancelAndUndo` | `go test ./internal/agent -run 'TestEditingOperations\|TestConfirmCancelAndUndo' -v` | 预览 `duration_ms` 5000→3000；确认后 `revision=2`；撤销后内容与原工程逐字段相等、`revision=3` |
| 2 | "把第二段移到开头"存在多条可选轨道：澄清，不擅自移动 | `TestOfflineAmbiguity` | `go test ./internal/agent -run TestOfflineAmbiguity -v` | 返回 `clarify`，候选 2 条（`clip-b`/`clip-d`），`Reference="第二段"`，无 `operations` |
| 3 | 修改锁定轨道字幕：拒绝并给出准确原因，其他片段不受影响 | `TestEngineErrorsHaveStableCodes/locked`<br>`TestTrackProblemsAreEditProblems`<br>`TestInstructionFeedbackStatesReasonOnce` | `go test ./internal/agent -run 'TestEngineErrors\|TestTrackProblems\|TestInstructionFeedback' -v` | 错误码 `TRACK_LOCKED`；用户文案只出现一次"本条指令未执行任何修改"；工程 diff 为空 |
| 4 | 模型/解析器给出未知操作、额外字段、负时长、越界源区间：独立校验并拒绝，不执行脚本 | `TestInvalidOperationsNeverMutate`（13 个子用例）<br>`TestOperationFieldErrorsAreInputProblems` | `go test ./internal/agent -run 'TestInvalidOperationsNeverMutate\|TestOperationFieldErrors' -v` | 全部返回错误且 `project` 未变；未知类型 `INVALID_INPUT`；多余字段 `INVALID_INPUT` |
| 5 | 查看计划后工程变化/切换项目：旧计划失效，不能应用到新状态 | `TestParserSnapshotCannotBindToNewState`<br>`TestReloadInvalidatesSameProjectAndVersion`<br>`TestConcurrentPlansOnlyCommitOnce` | `go test ./internal/agent -run 'TestParserSnapshot\|TestReloadInvalidates\|TestConcurrentPlans' -v` | 确认返回 `STATE_CONFLICT`；同 ID 同版本重新导入后旧计划仍失效（epoch 生效） |
| 6 | 取消删除：无状态变化、无"已删除"提示 | `TestConfirmCancelAndUndo` | `go test ./internal/agent -run TestConfirmCancelAndUndo -v` | 取消后工程与版本均未变；取消后再确认返回 409 |
| 7 | 一条指令两个修改，第二个失败：整条不生效，报告与实际一致 | `TestRelativeTrimInvalidBatchIsAtomic`<br>`TestBatchEndingInOverlapAppliesNothing` | `go test ./internal/agent -run 'TestRelativeTrimInvalidBatch\|TestBatchEndingInOverlap' -v` | 返回 `OVERLAP`，`changes` 为空，工程未变 |
| 8 | 连续两次撤销 / 执行一次撤销：不重复应用、不丢失无关片段 | `TestTwoUndosAndIsolation`<br>`TestExtensionHistoryAndRedoLifecycle` | `go test ./internal/agent -run 'TestTwoUndos\|TestExtensionHistoryAndRedo' -v` | 第二次撤销消费上一条；其余片段数值不变；版本单调递增 |

## 二、格式与边界（任务书"工程数据准备"与"技术栈"）

| 检查 | 测试 | 命令 |
| --- | --- | --- |
| 样例工程满足轨道/片段/锁轨要求 | `TestExamplesRespectSchema` | `go test ./internal/agent -run TestExamplesRespectSchema -v` |
| 导入不依赖预置 ID 或顺序 | `TestImportedProjectIndependentOfIDsAndOrder` | `go test ./internal/agent -run TestImportedProjectIndependent -v` |
| Schema 与解码器字段/必填一致 | `TestSchemaMatchesDecoder` | `go test ./internal/agent -run TestSchemaMatchesDecoder -v` |
| 严格拒绝未知字段、null、缺字段 | `TestImportRejectsInvalidData`<br>`TestImportRejectsMissingOrNullClipFields` | `go test ./internal/agent -run TestImportRejects -v` |
| Go 构建、静态检查、全量测试 | — | `go vet ./... && go build ./... && go test -count=1 ./...` |

## 三、批次语义（本轮调整）

| 行为 | 测试 | 说明 |
| --- | --- | --- |
| 交换两段顺序可表达 | `TestBatchCanSwapOrder`<br>`TestOfflineParserCanExpressSwap` | 批次内允许临时重叠，提交态仍严格 |
| 真碰撞仍拒绝 | `TestSingleMoveIntoOverlapStillRejected` | 单条移动落到已有片段区间 → `OVERLAP` |
| 批次末尾非法则不生效 | `TestBatchEndingInOverlapAppliesNothing` | 中间步骤成功也不提交 |

## 四、请求边界

| 行为 | 测试 | 说明 |
| --- | --- | --- |
| 重复 JSON 字段拒绝 | `TestDuplicateJSONKeysAreRejected` | 顶层、嵌套、操作数组三处均覆盖 |
| 伪造 Host 拒绝 | `TestForeignHostIsRejected` | DNS rebinding 场景返回 403 |
| 回环主机放行 | `TestLoopbackHostsAreAccepted` | `127.0.0.1` / `localhost` / `[::1]` |
| 跨站 Origin 拒绝 | `TestHTTPRejectsCrossOriginAndUnknownFields` | 403，且工程不变 |

## 五、前端与页面

| 检查 | 命令 | 预期 |
| --- | --- | --- |
| 页面逻辑与流式解析 | `npm test` | 20 项通过 |
| 代码风格 | `npm run lint && npm run format:check` | 无输出 |

## 六、一键回归

```bash
gofmt -l . && go vet ./... && go test -count=1 ./... && go build ./... && npm test
```

`gofmt -l .` 必须无输出。若本机缺少 C 编译器，`go test -race` 变红是环境问题而非代码问题；CI 的 Ubuntu 任务会执行 race 与 fuzz。
