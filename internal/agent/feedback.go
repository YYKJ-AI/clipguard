package agent

import "strings"

// Engine errors retain their diagnostic detail internally; browser feedback uses
// the same cause without exposing JSON types, field names, or opaque IDs.
func publicError(text string) string {
	if strings.HasPrefix(text, "相关轨道已锁定，") || strings.HasPrefix(text, "修改会造成同一轨道") {
		return text
	}
	switch {
	case strings.Contains(text, "JSON 格式"), strings.Contains(text, "json:"), strings.Contains(text, "只允许一个 JSON"):
		return "文件或请求格式有误。请检查工程文件，或重新输入本次要求。"
	case strings.Contains(text, "锁定"):
		return "相关轨道已锁定，本次不能修改。请核对目标片段；本条指令未执行任何修改。"
	case strings.Contains(text, "重叠"):
		return "修改会造成同一轨道的片段重叠，当前不会自动推移其他片段。请调整目标位置或修改量；本条指令未执行任何修改。"
	case strings.Contains(text, "未知片段"), strings.Contains(text, "无法定位目标"):
		return "没有找到要修改的片段。请在当前时间轴选中片段，或使用唯一片段名称后重试。"
	case strings.Contains(text, "duration_ms"), strings.Contains(text, "shorten_by_ms"), strings.Contains(text, "start_ms"):
		return "修改的时间参数不完整或不符合当前操作。请明确目标时间或缩短量后重试。"
	case strings.Contains(text, "project_id"), strings.Contains(text, "revision"), strings.Contains(text, "clips"), strings.Contains(text, "locked"), strings.Contains(text, "字段"):
		return "工程或修改内容的必填信息不完整或格式不正确。请检查工程文件后重试。"
	case strings.Contains(text, "LLM_"), strings.Contains(text, "256 KiB"), strings.Contains(text, "Chat Completions"):
		return "模型服务配置或返回内容不符合要求，请重试；仍失败时请联系程序维护者检查服务。"
	default:
		return text
	}
}
