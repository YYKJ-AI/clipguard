package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Candidate struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type ParseResult struct {
	Status       string      `json:"status"`
	Message      string      `json:"message"`
	Operations   []Operation `json:"operations,omitempty"`
	Candidates   []Candidate `json:"candidates,omitempty"`
	Reference    string      `json:"-"`
	CommandIndex int         `json:"-"`
}
type Parser interface {
	Parse(context.Context, Project, []Turn) (ParseResult, error)
}
type OfflineParser struct {
	choices                   map[int]string
	selectedID, lastCaptionID string
}

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
			return 0, errors.New("毫秒必须是整数")
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
		return nil, errors.New("字幕引号未闭合")
	}
	flush()
	if len(parts) == 0 || len(parts) > 64 {
		return nil, errors.New("需要 1—64 条明确操作")
	}
	return parts, nil
}

func resolve(p Project, ref string) (*Clip, []Candidate) {
	ref = strings.Trim(strings.TrimSpace(ref), "`")
	for _, c := range p.Clips {
		if c.ID == ref {
			v := c
			return &v, nil
		}
	}
	var names []Clip
	for _, c := range p.Clips {
		if c.DisplayName != nil && strings.TrimSpace(*c.DisplayName) == ref {
			names = append(names, c)
		}
	}
	if len(names) == 1 {
		result := names[0]
		return &result, nil
	}
	if len(names) > 1 {
		return nil, clipCandidates(p, names)
	}
	ordinal := 0
	if m := ordinalPattern.FindStringSubmatch(ref); m != nil {
		ordinal, _ = strconv.Atoi(m[1])
		if ordinal == 0 {
			ordinal = strings.Index("一二三四五六七八九十", m[1])/3 + 1
		}
	} else if ref == "开场片段" {
		ordinal = 1
	}
	var candidates []Candidate
	if ordinal > 0 {
		for _, tr := range p.Tracks {
			if tr.Kind != "video" {
				continue
			}
			var clips []Clip
			for _, c := range p.Clips {
				if c.TrackID == tr.ID {
					clips = append(clips, c)
				}
			}
			sort.Slice(clips, func(i, j int) bool {
				if clips[i].StartMS == clips[j].StartMS {
					return clips[i].ID < clips[j].ID
				}
				return clips[i].StartMS < clips[j].StartMS
			})
			if len(clips) >= ordinal {
				c := clips[ordinal-1]
				candidates = append(candidates, Candidate{c.ID, fmt.Sprintf("%s / %s（%d—%d ms）", tr.ID, c.ID, c.StartMS, c.StartMS+c.DurationMS)})
			}
		}
	}
	if ref == "结尾字幕" {
		var maxStart int64 = -1
		for _, c := range p.Clips {
			if c.Text != nil && c.StartMS > maxStart {
				maxStart = c.StartMS
			}
		}
		for _, c := range p.Clips {
			if c.Text != nil && c.StartMS == maxStart {
				candidates = append(candidates, Candidate{c.ID, c.TrackID + " / " + c.ID})
			}
		}
	}
	if len(candidates) == 1 {
		for _, c := range p.Clips {
			if c.ID == candidates[0].ID {
				v := c
				return &v, nil
			}
		}
	}
	return nil, candidates
}

func clipCandidates(p Project, clips []Clip) []Candidate {
	trackOrder := map[string]int{}
	for n, tr := range p.Tracks {
		trackOrder[tr.ID] = n
	}
	sort.Slice(clips, func(i, j int) bool {
		left, right := clips[i], clips[j]
		if trackOrder[left.TrackID] != trackOrder[right.TrackID] {
			return trackOrder[left.TrackID] < trackOrder[right.TrackID]
		}
		if left.StartMS != right.StartMS {
			return left.StartMS < right.StartMS
		}
		return left.ID < right.ID
	})
	candidates := make([]Candidate, 0, len(clips))
	for _, c := range clips {
		candidates = append(candidates, Candidate{ID: c.ID, Label: fmt.Sprintf("%s / %s（%d—%d ms）", c.TrackID, c.ID, c.StartMS, c.StartMS+c.DurationMS)})
	}
	return candidates
}

func contextCandidates(p Project, wantCaption, wantAny bool) []Candidate {
	clips := make([]Clip, 0)
	for _, c := range p.Clips {
		if wantAny || (c.Text != nil) == wantCaption {
			clips = append(clips, c)
		}
	}
	if len(clips) > 64 {
		return nil
	}
	return clipCandidates(p, clips)
}

func (parser OfflineParser) Parse(ctx context.Context, p Project, turns []Turn) (ParseResult, error) {
	if len(turns) == 0 {
		return ParseResult{}, errors.New("请输入指令")
	}
	parts, err := splitCommands(strings.TrimSpace(turns[len(turns)-1].Content))
	if err != nil {
		return ParseResult{}, err
	}
	var ops []Operation
	negatedRef := ""
	for commandIndex, part := range parts {
		part = strings.TrimSuffix(strings.TrimSpace(part), "。")
		if m := trimMissingAmountPattern.FindStringSubmatch(part); m != nil {
			return ParseResult{Status: "clarify", Message: "你想缩短多少秒？", Reference: m[1], CommandIndex: commandIndex}, nil
		}
		if strings.HasPrefix(part, "不要删除 ") || strings.HasPrefix(part, "不要删除") || strings.HasPrefix(part, "不要删 ") || strings.HasPrefix(part, "不要删") {
			negatedRef = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(part, "不要删除"), "不要删"))
			continue
		}
		var ref string
		var op Operation
		var delta *int64
		var betweenA, betweenB string
		if m := insertPattern.FindStringSubmatch(part); m != nil {
			ref = m[4]
			track := strings.TrimSpace(m[1])
			start, e := milliseconds(m[2], m[3])
			if e != nil {
				return ParseResult{}, e
			}
			op = Operation{Type: "insert", StartMS: &start, TrackID: &track}
		} else if m := trimPattern.FindStringSubmatch(part); m != nil {
			ref = m[1]
			n, e := milliseconds(m[2], m[3])
			if e != nil {
				return ParseResult{}, e
			}
			delta = &n
			op.Type = "trim"
		} else if m := moveTrackPattern.FindStringSubmatch(part); m != nil {
			ref = m[1]
			tr := strings.TrimSpace(m[2])
			n, e := milliseconds(m[3], m[4])
			if e != nil {
				return ParseResult{}, e
			}
			op = Operation{Type: "move", StartMS: &n, TrackID: &tr}
		} else if m := movePattern.FindStringSubmatch(part); m != nil {
			ref = m[1]
			n, e := milliseconds(m[2], m[3])
			if e != nil {
				return ParseResult{}, e
			}
			op = Operation{Type: "move", StartMS: &n}
		} else if m := startPattern.FindStringSubmatch(part); m != nil {
			ref = m[1]
			op = Operation{Type: "move", StartMS: int64ptr(0)}
		} else if m := moveBetweenPattern.FindStringSubmatch(part); m != nil {
			ref, betweenA, betweenB = m[1], m[2], m[3]
			op.Type = "move"
		} else if m := captionTimePattern.FindStringSubmatch(part); m != nil {
			ref = m[1]
			start, e := milliseconds(m[2], m[3])
			if e != nil {
				return ParseResult{}, e
			}
			end, e := milliseconds(m[4], m[5])
			if e != nil {
				return ParseResult{}, e
			}
			duration := end - start
			op = Operation{Type: "caption", StartMS: &start, DurationMS: &duration}
		} else if m := captionPattern.FindStringSubmatch(part); m != nil {
			ref = m[1]
			text := m[2]
			if strings.HasPrefix(text, "“") && strings.HasSuffix(text, "”") {
				text = strings.TrimSuffix(strings.TrimPrefix(text, "“"), "”")
			} else {
				var e error
				text, e = strconv.Unquote(text)
				if e != nil {
					return ParseResult{Status: "unsupported", Message: "字幕内容请放在中文或英文双引号中"}, nil
				}
			}
			op = Operation{Type: "caption", Text: &text}
		} else if m := recentCaptionPattern.FindStringSubmatch(part); m != nil {
			ref = m[1]
			text := m[2]
			if strings.HasPrefix(text, "“") && strings.HasSuffix(text, "”") {
				text = strings.TrimSuffix(strings.TrimPrefix(text, "“"), "”")
			} else {
				var e error
				text, e = strconv.Unquote(text)
				if e != nil {
					return ParseResult{Status: "unsupported", Message: "字幕内容请放在中文或英文双引号中"}, nil
				}
			}
			op = Operation{Type: "caption", Text: &text}
		} else if m := deletePattern.FindStringSubmatch(part); m != nil {
			ref = m[1]
			op.Type = "delete"
		} else {
			return ParseResult{Status: "unsupported", Message: "离线模式不支持这段表达：" + part + "。请参考页面句式，或配置模型后切换模型模式。"}, nil
		}
		work := p
		if len(ops) > 0 {
			var e error
			work, _, e = applyOperations(p, ops, false)
			if e != nil {
				return ParseResult{}, e
			}
		}
		if choice, ok := parser.choices[commandIndex]; ok {
			ref = choice
		}
		if ref == "它" && negatedRef != "" {
			ref = negatedRef
		}
		if ref == "这段" || ref == "这个片段" || ref == "这个镜头" || ref == "刚刚那个字幕" {
			wantCaption := op.Type == "caption" || ref == "刚刚那个字幕"
			selected := parser.selectedID
			if ref == "刚刚那个字幕" {
				selected = ""
				for _, c := range work.Clips {
					if c.ID == parser.selectedID && c.Text != nil {
						selected = c.ID
						break
					}
				}
				if selected == "" {
					selected = parser.lastCaptionID
				}
			}
			if selected != "" {
				for _, c := range work.Clips {
					if c.ID == selected && (op.Type == "delete" || op.Type == "insert" || (c.Text != nil) == wantCaption) {
						ref = selected
						break
					}
				}
			}
			if ref == "这段" || ref == "这个片段" || ref == "这个镜头" || ref == "刚刚那个字幕" {
				return ParseResult{Status: "clarify", Message: "请先选择需要修改的片段", Candidates: contextCandidates(work, wantCaption, op.Type == "delete" || op.Type == "insert"), CommandIndex: commandIndex}, nil
			}
		}
		target, candidates := resolve(work, ref)
		if target == nil {
			if len(candidates) > 0 {
				return ParseResult{Status: "clarify", Message: "请选择“" + strings.TrimSpace(ref) + "”对应的片段", Candidates: candidates, Reference: strings.TrimSpace(ref), CommandIndex: commandIndex}, nil
			}
			return ParseResult{}, fmt.Errorf("无法定位目标 %q，请提供工程中的片段 ID", ref)
		}
		if betweenA != "" {
			first, ca := resolve(work, betweenA)
			second, cb := resolve(work, betweenB)
			if first == nil || second == nil {
				_ = ca
				_ = cb
				return ParseResult{Status: "clarify", Message: "请用明确的片段 ID 指出两段之间的位置；名称重复时不能猜测目标"}, nil
			}
			if first.ID == target.ID || second.ID == target.ID || first.ID == second.ID || first.TrackID != second.TrackID || first.Text != nil || second.Text != nil || target.Text != nil || first.StartMS+first.DurationMS > second.StartMS {
				return ParseResult{Status: "unsupported", Message: "两段必须是同一视频轨道上按时间排列的不同片段，且被移动片段不能是两端之一"}, nil
			}
			if target.DurationMS > second.StartMS-(first.StartMS+first.DurationMS) {
				return ParseResult{Status: "unsupported", Message: "两段之间没有足够空间容纳该片段；当前不会自动推移后续片段"}, nil
			}
			start := first.StartMS + first.DurationMS
			track := first.TrackID
			op.StartMS = &start
			op.TrackID = &track
		}
		op.TargetID = target.ID
		if op.Type == "insert" {
			source := target.ID
			op.SourceID = &source
			op.TargetID = "insert-" + randomID()
		}
		if delta != nil {
			duration := target.DurationMS - *delta
			op.DurationMS = &duration
		}
		ops = append(ops, op)
	}
	if len(ops) == 0 {
		return ParseResult{Status: "unsupported", Message: "没有需要执行的修改；否定表达不会触发删除"}, nil
	}
	return ParseResult{Status: "plan", Message: "已解析，需校验并确认后执行", Operations: ops}, nil
}
