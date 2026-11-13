# 设计说明

本文描述当前实现的结构与行为约定，供评审者在不读代码的情况下判断时序与边界。行为细节以代码与测试为准，本文与实现不一致时以实现为准并须修订本文。

## 1. 目标与非目标

目标是建立"自然语言 → 结构化计划 → 人工确认 → 确定性执行 → 可撤销"的闭环，验证自然语言与确定性编辑之间的可靠边界。

不做：视频编码导出、多轨合成、字幕识别、自动分镜、云存储、多人协作、接入任何真实剪辑器。

## 2. 分层

| 层 | 文件 | 职责 | 依赖方向 |
| --- | --- | --- | --- |
| 传输 | `server.go`、`stream.go` | HTTP 路由、请求限额、Host/Origin 校验、SSE | → dialog / workspace |
| 编排 | `dialog.go`、`session.go` | 会话、澄清上下文、模式选择、模型并发闸门 | → parser / workspace |
| 解析 | `parser.go`、`grammar.go`、`target_resolver.go`、`model.go` | 自然语言 → `ParseResult`（只产出数据） | → engine（只读） |
| 执行 | `engine.go`、`operations.go`、`op_*.go` | 校验、应用操作、差异计算 | 无外部依赖 |
| 状态 | `workspace.go` | 工程、计划、撤销栈、版本号 | → engine |
| 前端 | `web/*.js` | 渲染、预览卡片、确认按钮 | → HTTP |

关键约束：`engine` 不感知 HTTP、不感知模型、不感知时钟，因此可被单测、基准与 fuzz 直接驱动。

## 3. 数据模型

时间一律为整数毫秒，片段区间为左闭右开 `[start_ms, start_ms + duration_ms)`。

| 字段 | 必填 | 规则 |
| --- | --- | --- |
| `project_id` | 是 | 非空、≤128 字节 |
| `revision` | 是 | 正整数，每次提交/撤销/重做 +1 |
| `tracks[].id/kind/locked` | 是 | kind ∈ {video, caption}；locked 必须显式布尔，不接受省略或 null |
| `clips[].id/track_id/start_ms/duration_ms` | 是 | id 全局唯一；start ≥ 0；duration > 0 |
| `clips[].source_in_ms/source_duration_ms` | 视频是 | 需满足 `source_in + duration ≤ source_duration` |
| `clips[].text` | 字幕是 | ≤20000 字节；与视频字段互斥 |
| `clips[].display_name` | 否 | ≤128 Unicode 码点，仅用于展示与名称定位 |

时间轴时间（`start_ms`）与素材源时间（`source_in_ms`）是两个概念：前者是片段在时间轴上出现的位置，后者是从源素材的哪一帧开始取。右端裁剪只改 `duration_ms`，二者都不动。

同轨不重叠（端点相接允许），不同轨可重叠；移动碰撞直接拒绝，不做波纹推移。视频改动不自动带动字幕。

## 4. 操作定义

所有操作经同一注册表（`operations.go`）声明字段白名单、执行函数与说明；严格解码、执行与模型提示读取同一份定义。

| 操作 | 字段 | 前置条件 | 主要失败码 |
| --- | --- | --- | --- |
| `move` | `start_ms` 必填，`track_id` 可选 | 目标片段存在且未锁；目标轨道同类型且未锁；终点不与同轨他人重叠 | `TARGET_NOT_FOUND`、`TRACK_LOCKED`、`TRACK_INVALID`、`OVERLAP` |
| `trim` | `duration_ms` 与 `shorten_by_ms` 二选一 | 仅视频；缩短量为正且小于当前时长；源区间仍合法 | `INVALID_TIME`、`INVALID_INPUT` |
| `caption` | `text` / `start_ms` / `duration_ms` 至少一个 | 仅字幕轨；结果时长 > 0 | `INVALID_INPUT`、`INVALID_TIME` |
| `delete` | 无 | 目标存在且未锁 | `TARGET_NOT_FOUND`、`TRACK_LOCKED` |
| `insert` | `source_id`、`track_id`、`start_ms` 必填，`target_id` 为新 ID | 来源存在；目标轨道与来源同类型且未锁；新 ID 未被占用 | `TARGET_NOT_FOUND`、`TRACK_INVALID`、`INVALID_INPUT` |

## 5. 校验粒度与原子性

一条指令 = 一批操作，按书写顺序在**工作副本**上执行。

- **每个操作之后**只校验局部不变量：片段自身字段合法、时间非负、时长为正、源区间成立、引用存在、轨道未锁。
- **整批结束后**再做一次完整校验，包含同轨重叠与全局唯一性。
- 任一步失败，整批不产生任何修改，工作副本被丢弃；成功时才提交并把 `revision` 加一。

为什么要分成两级：交换两段顺序必须经过"两段同时占位"的瞬间。若每步都要求全局合法，这类指令永远无法表达；若只在末尾校验，又可能把非法状态提交出去。两级校验让中间态可以临时非法，但提交态必须完全合法。

## 6. 计划、确认与版本

```
指令 → 解析 → Preview（生成 plan_id，绑定 project_id + revision + epoch）
     → 用户确认 → 重新 Apply → revision+1 → 记入撤销栈
     → 用户取消 → 删除计划，工程不变
```

- `epoch` 在每次导入时递增，因此"同 ID 同版本重新导入"也会让旧计划失效。
- 确认时先比对绑定，再重算操作；计划只保存操作与绑定，不保存计算结果，避免预览与实际脱节。
- 一次确认会清空所有待确认计划，前端同时禁用旧卡片。

## 7. 撤销与重做

撤销栈保存**提交前的完整快照**，而不是反向操作。撤销恢复快照内容后仍将 `revision` 加一，因此版本号单调递增、不回退。重做栈在新提交时清空。撤销/重做请求带客户端生成的幂等键，重复请求返回 409，不会重复消费历史。

深度上限 100；编辑记录上限 1000；幂等键各上限 1024。到上限明确拒绝，不静默丢弃。

## 8. 歧义与目标定位

定位顺序：精确 ID → 唯一 `display_name` → 序数（每条视频轨按起点排序的第 N 段）→ 特殊指代（"开头"=0 ms、"结尾字幕"=起点最大的字幕）→ 上下文（当前选中片段、"刚刚那个字幕"）。

命中多个候选时返回 `clarify` 并列出候选，绝不自行选择。名称重复、多轨同时命中、上下文缺失都走澄清路径。

## 9. 错误码

| 码 | 含义 | 用户可据此做什么 |
| --- | --- | --- |
| `INVALID_JSON` | 请求或工程 JSON 不合法（含重复字段、未知字段、类型错误） | 修文件或改写指令 |
| `TRACK_LOCKED` | 相关轨道已锁定 | 换目标或先解锁 |
| `OVERLAP` | 结果会造成同轨重叠 | 改位置或改时长 |
| `TARGET_NOT_FOUND` | 找不到目标片段 | 用 ID 或唯一名称重试 |
| `TRACK_INVALID` | 轨道不存在或类型不符 | 换成同类型轨道 |
| `INVALID_TIME` | 时间/时长参数非法或越界 | 调整数值 |
| `INVALID_PROJECT` | 工程结构不合法 | 修工程文件 |
| `INVALID_INPUT` | 指令字段不合法（多余字段、null、未知操作） | 按注册表字段重写 |
| `STATE_CONFLICT` | 工程或计划已变化 | 重新生成预览 |
| `MODEL_FAILURE` | 模型服务异常 | 重试或切离线 |
| `USER_MESSAGE` | 面向用户的普通说明 | 按提示操作 |

错误文案的来源只有一处：`feedback.go` 的码表 + `instructionFeedback` 的统一尾句。

## 10. 请求边界

`POST` 需通过 Origin 与 Host 双重校验：Host 必须在允许列表内（默认 `127.0.0.1`、`localhost`、`::1`，可用 `ALLOWED_HOSTS` 扩展），跨站 Origin 直接拒绝。会话 Cookie 为 `HttpOnly` + `SameSite=Strict`。请求体上限 1 MiB，单指令 2000 字符。

## 11. 已知限制

- 全部状态在内存，进程退出即丢失；不提供持久化。
- 撤销恢复的是整条指令，不支持指令内部的单步回退。
- 模型只产出数据，但仍可能被工程内文本（字幕、名称）影响；最后一道防线是确认预览。
- 不支持波浪推移，因此"整体左移"需要显式给出每个片段的新位置。
