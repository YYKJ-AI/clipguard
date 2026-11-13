package agent

import (
	"context"
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

func (parser OfflineParser) Parse(ctx context.Context, p Project, turns []Turn) (ParseResult, error) {
	if len(turns) == 0 {
		return ParseResult{}, userError("请输入指令")
	}
	if err := validateInstructionText(turns[len(turns)-1].Content); err != nil {
		return ParseResult{}, err
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
			return ParseResult{Status: "unsupported", Message: "离线模式不支持这段表达。请参考页面句式，或配置模型后切换模型模式。"}, nil
		}
		work := p
		if len(ops) > 0 {
			var e error
			work, _, e = applyBatch(p, ops, false, false)
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
			return ParseResult{}, errorf(CodeTarget, "无法定位目标 %q，请提供工程中的片段 ID", ref)
		}
		if betweenA != "" {
			first, _ := resolve(work, betweenA)
			second, _ := resolve(work, betweenB)
			if first == nil || second == nil {
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
