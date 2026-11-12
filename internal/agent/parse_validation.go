package agent

import (
	"errors"
	"strings"
)

func validateParseResult(p Project, result ParseResult) error {
	if len(result.Message) > 6000 {
		return errors.New("解析说明过长")
	}
	switch result.Status {
	case "answer":
		if len(result.Operations) > 0 || len(result.Candidates) > 0 || strings.TrimSpace(result.Message) == "" {
			return errors.New("咨询回复必须有正文，且不能包含操作或候选")
		}
	case "plan":
		if len(result.Operations) == 0 || len(result.Operations) > 64 || len(result.Candidates) > 0 {
			return errors.New("操作计划结构无效")
		}
	case "clarify":
		if len(result.Operations) > 0 || strings.TrimSpace(result.Message) == "" || len(result.Candidates) > 4096 {
			return errors.New("澄清结构无效")
		}
		clipIDs := make(map[string]bool, len(p.Clips))
		for _, c := range p.Clips {
			clipIDs[c.ID] = true
		}
		seen := map[string]bool{}
		for _, candidate := range result.Candidates {
			found := clipIDs[candidate.ID]
			if !found || seen[candidate.ID] || len(candidate.Label) > 512 {
				return errors.New("澄清包含未知或重复目标")
			}
			seen[candidate.ID] = true
		}
	case "unsupported":
		if len(result.Operations) > 0 || len(result.Candidates) > 0 {
			return errors.New("不支持响应不能包含操作或候选")
		}
	default:
		return errors.New("未知解析状态")
	}
	return nil
}
