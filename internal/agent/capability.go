package agent

import (
	"regexp"
	"strings"
)

func mediaCapabilityGap(text string) bool {
	// Quoted caption content is data. Negated clauses are not capability requests.
	var plain strings.Builder
	var quote rune
	escaped := false
	for _, r := range text {
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if quote == '"' && r == '\\' {
				escaped = true
				continue
			}
			if (quote == '“' && r == '”') || (quote == '"' && r == '"') {
				quote = 0
			}
			continue
		}
		if r == '“' || r == '"' {
			quote = r
			continue
		}
		plain.WriteRune(r)
	}
	for _, clause := range strings.FieldsFunc(plain.String(), func(r rune) bool { return strings.ContainsRune("，,；;。\n", r) }) {
		if mediaCapabilityClause(clause) {
			return true
		}
	}
	return false
}

func mediaCapabilityClause(text string) bool {
	visual := strings.Contains(text, "画面") || strings.Contains(text, "花屏") || strings.Contains(text, "噪点") || strings.Contains(text, "黑屏") || strings.Contains(text, "蓝屏")
	action := mediaActionPattern.MatchString(strings.TrimSpace(text))
	problem := strings.Contains(text, "异常") || strings.Contains(text, "纯色") || strings.Contains(text, "花屏") || strings.Contains(text, "噪点") || strings.Contains(text, "黑屏") || strings.Contains(text, "蓝屏")
	return visual && action && problem
}

var mediaActionPattern = regexp.MustCompile(`^(?:请)?(?:帮我|麻烦)?(?:自动)?(?:检查|检测|修复|处理|去掉)`)

func generalCapabilityQuestion(text string) bool {
	return strings.Contains(text, "有哪些功能") || strings.Contains(text, "能做什么") || strings.Contains(text, "支持哪些功能")
}

func exportCapabilityQuestion(text string) bool {
	return strings.Contains(text, "导出") && (strings.Contains(text, "吗") || strings.Contains(text, "能") || strings.Contains(text, "支持") || strings.Contains(text, "功能"))
}

// Capability rules own both prompt wording and normalized public answers.
type capabilityRule struct {
	kind    string
	matches func(string) bool
	answer  string
	reject  bool
}

var capabilityRules = []capabilityRule{
	{"media_analysis", mediaCapabilityGap, "暂时不支持自动检测或修复异常画面，也不能在去除画面时单独保留对应旁白；当前工程只支持片段移动、视频右端缩短、字幕调整和删除。工程没有变化。", true},
	{"export", exportCapabilityQuestion, "可以导出当前工程 JSON，用于保存时间轴修改；当前不支持把工程编码导出为视频成片。", false},
	{"overview", generalCapabilityQuestion, "我可以移动已有片段、从视频右端缩短、修改字幕、删除，以及插入已有片段的副本。修改前会展示差异，确认后才执行；可以整体撤销和重做，查看编辑记录，绑定本地视频或图片预览。可以保存工程 JSON，不能导出视频成片；画面检测修复、降噪、自动字幕和音画联动暂时不支持。", false},
}

func capabilityMatch(text string) *capabilityRule {
	for i := range capabilityRules {
		if capabilityRules[i].matches(text) {
			return &capabilityRules[i]
		}
	}
	return nil
}
func normalizeCapability(text, mode string, result ParseResult) ParseResult {
	if rule := capabilityMatch(text); rule != nil {
		if rule.reject {
			return ParseResult{Status: "unsupported", Message: rule.answer}
		}
		if mode == "model" && result.Status == "answer" {
			result.Message = rule.answer
		}
	}
	return result
}
func capabilityDescription() string {
	var b strings.Builder
	b.WriteString("当前能力以注册表为准，所有编辑必须预览并等待明确确认。\n")
	b.WriteString(operationDescriptions())
	for _, rule := range capabilityRules {
		b.WriteString(rule.answer)
		b.WriteByte('\n')
	}
	b.WriteString("不能凭空生成素材、添加分镜或转场、自动生成字幕、降噪或调用音乐库。超范围编辑返回 unsupported，说明原因，不追问位置，不给绕行建议或声称执行。流式回复先写 status，再写 message。媒体绑定仅在浏览器页面保留；工程和历史仅在内存。撤销重做请用户点击对应按钮，不能用新编辑模拟撤销。\n")
	return b.String()
}
