# 现场演示脚本

目标：10 分钟内演示"修改、澄清、失败、撤销"四条路径，全程不联网、不配置模型。

前置：`sh scripts/start.sh`（Windows 用 `scripts/start.ps1`），浏览器打开 `http://127.0.0.1:8090`，点击左侧"加载演示工程"。样例工程含 2 条视频轨、2 条字幕轨（其中 `caption-locked` 锁定）、4 个视频片段、3 条字幕。

## 场景 1：一次修改（缩短 2 秒）

| 步骤 | 操作 | 预期 |
| --- | --- | --- |
| 1 | 输入 `把 clip-a 缩短 2 秒` | 出现"待确认的修改"卡片，基于版本 1，1 项操作 |
| 2 | 观察卡片正文 | `时长将从 5.0 秒改为 3.0 秒`；修改后时间 `0.000—3.000`；时间轴对比条同步变化 |
| 3 | 观察时间轴 | **仍是 5.0 秒**（未确认不写入） |
| 4 | 点"确认修改" | 提示"修改已应用"，卡片按钮全部禁用，时间轴 `clip-a` 变 3.0 秒 |
| 5 | 展开"编辑记录" | 出现一条 `edit` 记录，版本 2，含 before/after 实际值 |

失败判定：确认前时间轴已变化（说明跳过确认写入）；卡片显示"缩短 2 秒"但预览时长不是 3000（说明预览与执行不一致）。

## 场景 2：需要澄清的指令（第二段移到开头）

| 步骤 | 操作 | 预期 |
| --- | --- | --- |
| 1 | 输入 `把第二段移到开头` | 不生成计划，出现候选按钮 |
| 2 | 观察候选 | 两条：`第 1 条视频轨道的第 2 段（产品介绍）` = `clip-b`；`第 2 条视频轨道的第 2 段（结尾镜头）` = `clip-d` |
| 3 | 点击 `clip-b` 候选 | 生成预览：`clip-b` 起点 6.0 秒 → 0.0 秒 |
| 4 | 点"确认修改" | 返回拒绝：同轨重叠（`clip-a` 占 0—5 秒），工程无变化 |

失败判定：未询问就自行选中某条轨道的第二段；或候选只给一条却不说明依据。

## 场景 3：一次失败（锁定轨道）

| 步骤 | 操作 | 预期 |
| --- | --- | --- |
| 1 | 输入 `把 caption-lock 的字幕改为“新文案”` | 拒绝，`TRACK_LOCKED` |
| 2 | 观察提示 | "相关轨道已锁定，本次不能修改。" + 一次"本条指令未执行任何修改"；**不出现**"请检查工程文件" |
| 3 | 观察时间轴 | `caption-lock` 文字未变，其他片段未变 |

补充失败样例（同一条指令内第二条失败）：

| 步骤 | 操作 | 预期 |
| --- | --- | --- |
| 4 | 输入 `把 clip-a 缩短 1 秒；把 clip-zzz 缩短 1 秒` | 整条拒绝，`clip-a` 仍为 5.0 秒 |

失败判定：第一条已生效（说明原子性被破坏）；或提示把编辑问题归因为"工程文件格式错误"。

## 场景 4：撤销（含重做）

| 步骤 | 操作 | 预期 |
| --- | --- | --- |
| 1 | 接场景 1，点"撤销" | 提示"已撤销上一条完整指令"，`clip-a` 回到 5.0 秒，版本变为 3 |
| 2 | 再点"撤销" | 拒绝或撤销更早一条；不会重复撤销同一条 |
| 3 | 点"重做" | `clip-a` 回到 3.0 秒，版本 4 |
| 4 | 导出 JSON | `revision` 为当前值，`clip-a.duration_ms` 与界面一致 |

失败判定：撤销后内容与原始工程不等价；版本号回退；重复点击导致重复应用。

## 补充场景（可选，展示能力边界）

| 输入 | 预期 |
| --- | --- |
| `把 clip-b 移到开头，然后把 clip-a 移到 6000 毫秒处` | 生成两条操作的预览（交换顺序），确认后两段位置互换 |
| `把 clip-a 移到 caption-locked 轨道的 1 秒处` | `TRACK_INVALID`：视频片段不能放入字幕轨道 |
| `删除 clip-d` 后点"取消" | 提示已取消，`clip-d` 仍在，版本未变 |
| `帮我检查画面有没有花屏` | 说明不支持画面检测，工程无变化 |

## 用命令行复现（无需浏览器）

```bash
go build -o /tmp/sagent . && LISTEN_ADDR=127.0.0.1:8099 /tmp/sagent &

curl -s -c /tmp/ck -X POST http://127.0.0.1:8099/api/session -H 'Content-Type: application/json' -d '{}'
curl -s -b /tmp/ck http://127.0.0.1:8099/api/sample -o /tmp/p.json
curl -s -b /tmp/ck -X POST http://127.0.0.1:8099/api/import -H 'Content-Type: application/json' --data-binary @/tmp/p.json

# 生成预览（取返回的 preview.id）
curl -s -b /tmp/ck -X POST http://127.0.0.1:8099/api/message -H 'Content-Type: application/json' \
  -d '{"text":"把 clip-a 缩短 2 秒","mode":"offline"}'

# 确认 / 取消 / 撤销
curl -s -b /tmp/ck -X POST http://127.0.0.1:8099/api/confirm -H 'Content-Type: application/json' -d '{"plan_id":"<id>"}'
curl -s -b /tmp/ck -X POST http://127.0.0.1:8099/api/cancel  -H 'Content-Type: application/json' -d '{"plan_id":"<id>"}'
curl -s -b /tmp/ck -X POST http://127.0.0.1:8099/api/undo    -H 'Content-Type: application/json' -d '{"request_id":"u1"}'
```

中文请求体请确保终端以 UTF-8 传参；Windows 下建议用 `curl --data-binary @file.json` 避免编码问题。
