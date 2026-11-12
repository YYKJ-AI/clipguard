package agent

import (
	"regexp"
	"strconv"
	"strings"
)

var numberUnit = `([0-9]+(?:\.[0-9]{1,3})?)\s*(毫秒|秒)`
var trimPattern = regexp.MustCompile(`^(?:把|将)?\s*(.+?)\s*(?:缩短|减短)\s*` + numberUnit + `$`)
var trimMissingAmountPattern = regexp.MustCompile(`^(?:把|将)?\s*(.+?)\s*(?:缩短|减短)$`)
var moveTrackPattern = regexp.MustCompile(`^(?:把|将)?\s*(.+?)\s*移到\s*(.+?)\s*轨道的\s*` + numberUnit + `(?:处)?$`)
var movePattern = regexp.MustCompile(`^(?:把|将)?\s*(.+?)\s*移到\s*(?:第\s*)?` + numberUnit + `(?:处)?$`)
var startPattern = regexp.MustCompile(`^(?:把|将)?\s*(.+?)\s*移到开头$`)
var moveBetweenPattern = regexp.MustCompile(`^(?:把|将)?\s*(.+?)\s*移到\s*(.+?)\s*(?:和|与)\s*(.+?)\s*之间$`)
var captionPattern = regexp.MustCompile(`^(?:把|将)?\s*(.+?)\s*的字幕改为\s*(.+)$`)
var recentCaptionPattern = regexp.MustCompile(`^(?:把|将)?\s*(刚刚那个字幕)\s*改为\s*(.+)$`)
var captionTimePattern = regexp.MustCompile(`^(?:把|将)?\s*(.+?)\s*的时间改为从\s*` + numberUnit + `\s*到\s*` + numberUnit + `$`)
var deletePattern = regexp.MustCompile(`^删除\s*(.+)$`)
var insertPattern = regexp.MustCompile(`^在\s*(.+?)\s*轨道的\s*` + numberUnit + `(?:处)?插入\s*(.+)$`)
var ordinalPattern = regexp.MustCompile(`^第([一二三四五六七八九十]|[0-9]+)(?:个片段|段|个镜头|镜头)$`)

func milliseconds(n, unit string) (int64, error) {
	parts := strings.Split(n, ".")
	if unit == "毫秒" {
		if len(parts) > 1 {
			return 0, userError("毫秒必须是整数")
		}
		return strconv.ParseInt(n, 10, 64)
	}
	fraction := "000"
	if len(parts) > 1 {
		fraction = (parts[1] + "000")[:3]
	}
	return strconv.ParseInt(parts[0]+fraction, 10, 64)
}
func splitCommands(text string) ([]string, error) {
	var parts []string
	var b strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if s := strings.TrimSpace(b.String()); s != "" {
			parts = append(parts, s)
		}
		b.Reset()
	}
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote != 0 {
			b.WriteRune(r)
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' && quote == '"' {
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
			b.WriteRune(r)
			continue
		}
		if r == ';' || r == '；' || r == '\n' {
			flush()
			continue
		}
		if r == '，' || r == ',' {
			rest := string(runes[i+1:])
			connector := ""
			for _, c := range []string{"然后", "再", "只"} {
				if strings.HasPrefix(rest, c) {
					connector = c
					break
				}
			}
			if connector != "" {
				flush()
				i += len([]rune(connector))
				continue
			}
		}
		b.WriteRune(r)
	}
	if quote != 0 {
		return nil, userError("字幕引号未闭合")
	}
	flush()
	if len(parts) == 0 || len(parts) > 64 {
		return nil, userError("需要 1—64 条明确操作")
	}
	return parts, nil
}
