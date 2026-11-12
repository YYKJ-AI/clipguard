package agent

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var spokenTime = regexp.MustCompile(`([0-9]+)[ \t]*(毫秒|ms)`)
var spokenIDPrefix = regexp.MustCompile(`(?i)\bID\s*(为|是|[:：])?\s*`)
var spokenToken = regexp.MustCompile(`['"]?[A-Za-z0-9_.:-]+['"]?`)

func customerNames(p Project) map[string]string {
	names := map[string]string{}
	counts := map[string]int{}
	for _, track := range p.Tracks {
		counts[track.Kind]++
		kind := "视频"
		if track.Kind == "caption" {
			kind = "字幕"
		}
		name := fmt.Sprintf("第 %d 条%s轨道", counts[track.Kind], kind)
		names[track.ID] = name
		clips := []Clip{}
		for _, clip := range p.Clips {
			if clip.TrackID == track.ID {
				clips = append(clips, clip)
			}
		}
		sort.Slice(clips, func(i, j int) bool {
			if clips[i].StartMS == clips[j].StartMS {
				return clips[i].ID < clips[j].ID
			}
			return clips[i].StartMS < clips[j].StartMS
		})
		for i, clip := range clips {
			label := fmt.Sprintf("%s的第 %d 段", name, i+1)
			if track.Kind == "caption" {
				label = fmt.Sprintf("%s的第 %d 条字幕", name, i+1)
			}
			if clip.DisplayName != nil && strings.TrimSpace(*clip.DisplayName) != "" {
				label += "（" + *clip.DisplayName + "）"
			}
			names[clip.ID] = label
		}
	}
	return names
}

// Only presentation changes here. IDs, operations and pending question state
// remain intact so that a friendly label cannot grant editing authority.
func customerClarification(p Project, result ParseResult) ParseResult {
	if result.Status != "clarify" {
		return result
	}
	names := customerNames(p)
	text := spokenIDPrefix.ReplaceAllString(result.Message, "")
	text = spokenTime.ReplaceAllStringFunc(text, func(raw string) string {
		parts := spokenTime.FindStringSubmatch(raw)
		ms, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return raw
		}
		return strconv.FormatFloat(float64(ms)/1000, 'f', -1, 64) + " 秒"
	})
	result.Message = spokenToken.ReplaceAllStringFunc(text, func(raw string) string {
		if name, ok := names[strings.Trim(raw, "\"'")]; ok {
			return name
		}
		return raw
	})
	for i, candidate := range result.Candidates {
		if name, ok := names[candidate.ID]; ok {
			result.Candidates[i].Label = name
		}
	}
	return result
}

const customerConversation = `
面向用户的对话规则：message 和 candidates.label 是给剪辑用户看的，必须简短、自然、口语化。使用片段名称、第几条视频/字幕轨道、第几段，以及秒或分秒来描述；不向用户展示 clip-c、video-2、ID、target_id、start_ms 等内部标识、字段或整数毫秒参数。内部 ID 和毫秒只放在 operations 与 candidates.id 中。用户明确询问技术参数时才可解释参数。
下面 customer_names 给出了稳定 ID 对应的用户称呼，轨道编号按工程中同类型轨道顺序，片段编号按轨道内时间顺序。名称重复时加上轨道和序号区分，不依靠名称猜测。
当错字、语音识别错误、指代或语义导致操作、目标或范围不确定，必须返回 clarify，用一个具体、容易回答的问题反问用户，不能默默纠错并生成编辑计划。能确定的部分沿用，只问尚不确定的部分。例如“我县要把轨道二第一段适配删掉”中的“适配删掉”含义不清，应问“你是想把第二条视频轨道的第一段视频删掉吗？”；不要念出内部编号和毫秒。若用户下一轮明确答是，再生成待确认计划。这个回答不等于执行授权，仍须预览并点击确认。
用户明确说“删除第二条视频轨道的第一段视频”，且目标唯一时直接生成待确认计划，不重复追问。用户问删除还是缩短时须问清操作；缺少缩短量时问“你想缩短几秒？”，不可只让用户选片段后便猜测时长。
`
