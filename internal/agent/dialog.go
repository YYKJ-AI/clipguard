package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Server) message(w http.ResponseWriter, r *http.Request, u *userSession) {
	var input struct {
		Text            string `json:"text"`
		Mode            string `json:"mode"`
		Choice          string `json:"choice"`
		ClarificationID string `json:"clarification_id"`
		SelectedClipID  string `json:"selected_clip_id"`
	}
	if err := body(r, &input); err != nil {
		fail(w, 400, err)
		return
	}
	if input.Mode == "" {
		input.Mode = "offline"
	}
	if input.Mode != "offline" && input.Mode != "model" {
		fail(w, 400, userError("请选择 offline 或 model 模式"))
		return
	}
	if err := validateInstructionText(input.Text); err != nil {
		fail(w, 400, err)
		return
	}
	if !u.dialogMu.TryLock() {
		fail(w, 409, userError("本会话正在处理上一条指令，请稍后重试"))
		return
	}
	defer u.dialogMu.Unlock()
	p, binding, err := u.workspace.Snapshot()
	hasProject := err == nil
	if input.SelectedClipID != "" {
		found := false
		for _, clip := range p.Clips {
			if clip.ID == input.SelectedClipID {
				found = true
				break
			}
		}
		if !hasProject || !found {
			fail(w, 400, userError("所选片段不在当前工程中，请重新选择"))
			return
		}
	}
	if err != nil && input.Mode != "model" {
		fail(w, 400, err)
		return
	}
	if binding != u.dialogBinding {
		u.turns = nil
		u.pending = nil
		u.dialogBinding = binding
	}
	if u.lastCaptionBinding.ProjectID != binding.ProjectID || u.lastCaptionBinding.Epoch != binding.Epoch {
		u.lastCaptionID = ""
	}
	text := strings.TrimSpace(input.Text)
	if input.Choice == "" && text == "取消" {
		u.turns, u.pending = nil, nil
		respond(w, 200, map[string]string{"status": "answer", "message": "已取消当前澄清，本次没有修改工程。若页面仍有待确认预览，请在预览卡片上点取消。"})
		return
	}
	if input.Choice == "" && u.pending != nil && u.pending.Mode == input.Mode && u.pending.Binding == binding {
		if strings.HasPrefix(text, "改成") {
			text = strings.TrimSpace(strings.TrimPrefix(text, "改成"))
			u.turns, u.pending = nil, nil
		} else if strings.HasPrefix(text, "算了，改成") {
			text = strings.TrimSpace(strings.TrimPrefix(text, "算了，改成"))
			u.turns, u.pending = nil, nil
		} else if input.Mode == "offline" && quantityAnswerPattern.MatchString(text) && strings.HasSuffix(strings.TrimSpace(u.pending.Original), "缩短") {
			text = u.pending.Original + " " + text
			u.pending = nil
		}
	}
	choices := map[int]string{}
	if input.Choice != "" {
		pending := u.pending
		if pending == nil || pending.ID != input.ClarificationID || pending.Binding != binding || pending.Mode != input.Mode {
			fail(w, 409, userError("澄清已失效，请重新输入指令"))
			return
		}
		valid := false
		for _, c := range pending.Result.Candidates {
			if c.ID == input.Choice {
				valid = true
			}
		}
		if !valid {
			fail(w, 400, userError("选择不属于候选列表"))
			return
		}
		if input.Mode == "offline" {
			text = pending.Original
			for index, target := range pending.Choices {
				choices[index] = target
			}
			choices[pending.Result.CommandIndex] = input.Choice
		} else {
			text = "我选择的目标 ID 是 " + input.Choice + "。请结合上一条修改要求生成完整候选计划，不要执行。"
		}
	}
	if text == "" {
		fail(w, 400, userError("请输入指令或补充信息"))
		return
	}
	turns := append([]Turn(nil), u.turns...)
	if input.Mode == "offline" {
		turns = nil
	}
	turns = append(turns, Turn{"user", text})
	if len(turns) > maxDialogTurns+1 {
		fail(w, 400, userError("本轮澄清过长，请重新导入工程后给出完整指令"))
		return
	}
	var parser Parser = OfflineParser{choices: choices, selectedID: input.SelectedClipID, lastCaptionID: u.lastCaptionID}
	if input.Mode == "model" {
		if s.config.Model == nil {
			fail(w, 503, userError("模型尚未配置，请使用离线句式模式"))
			return
		}
		parser = s.config.Model
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		default:
			fail(w, 503, userError("模型服务繁忙，请稍后重试"))
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.config.ModelTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, modelSelectionKey{}, modelSelection{SelectedID: input.SelectedClipID, LastCaptionID: u.lastCaptionID})
	if sink, ok := ctx.Value(streamSinkKey{}).(streamSink); ok {
		sink("progress", map[string]string{"message": "正在理解你的要求…"})
		if capabilityMatch(text) != nil {
			// Capability feedback is normalized below. Do not expose an earlier,
			// contradictory provisional claim from the model.
			ctx = context.WithValue(ctx, streamSinkKey{}, streamSink(func(kind string, data any) {
				if kind != "draft" {
					sink(kind, data)
				}
			}))
		}
	}
	result, err := parser.Parse(ctx, p, turns)
	if ctx.Err() != nil {
		fail(w, 504, userError(instructionFeedback(userError("模型或解析请求超时"))))
		return
	}
	if err != nil {
		if input.Mode == "model" {
			fail(w, 422, appError(CodeModel, err.Error()))
		} else {
			failMessage(w, 422, err, instructionFeedback(err))
		}
		return
	}
	_, current, currentErr := u.workspace.Snapshot()
	if (hasProject && (currentErr != nil || current != binding)) || (!hasProject && currentErr == nil) {
		fail(w, 409, userError("处理期间工程已变化，请重新提交指令"))
		return
	}
	result = normalizeCapability(text, input.Mode, result)
	if err := validateParseResult(p, result); err != nil {
		failMessage(w, 422, err, instructionFeedback(err))
		return
	}
	switch result.Status {
	case "answer":
		u.turns = append(turns, Turn{"assistant", result.Message})
		if len(u.turns) > maxDialogTurns {
			u.turns = append([]Turn(nil), u.turns[len(u.turns)-maxDialogTurns:]...)
		}
		respond(w, 200, map[string]string{"status": "answer", "message": result.Message})
	case "plan":
		if !hasProject {
			u.turns, u.pending = nil, nil
			respond(w, 200, map[string]any{"status": "clarify", "message": "请先加载演示工程或导入 JSON，再告诉我需要怎样修改。你可以点击左侧的“加载演示工程”。本次没有修改工程。", "candidates": []Candidate{}, "clarification_id": randomID()})
			return
		}
		preview, err := u.workspace.Preview(binding, result.Operations, text)
		if err != nil {
			failMessage(w, 422, err, instructionFeedback(err))
			return
		}
		u.turns = nil
		u.pending = nil
		respond(w, 200, map[string]any{"status": "plan", "preview": preview, "message": "请检查实际变更，确认后才会修改工程"})
	case "clarify":
		result = customerClarification(p, result)
		id := randomID()
		original := text
		if u.pending != nil && u.pending.Binding == binding && u.pending.Mode == input.Mode {
			id = u.pending.ID
			if input.Mode == "model" || input.Choice != "" {
				original = u.pending.Original
			}
		}
		u.pending = &clarification{ID: id, Binding: binding, Result: result, Original: original, Mode: input.Mode, Choices: choices}
		payload, marshalErr := json.Marshal(struct {
			Message    string      `json:"message"`
			Candidates []Candidate `json:"candidates"`
		}{result.Message, result.Candidates})
		if marshalErr != nil {
			fail(w, 500, marshalErr)
			return
		}
		u.turns = append(turns, Turn{"assistant", string(payload)})
		respond(w, 200, map[string]any{"status": "clarify", "message": result.Message, "candidates": result.Candidates, "clarification_id": id})
	default:
		if input.Mode == "model" || mediaCapabilityGap(text) {
			respond(w, 200, map[string]string{"status": "unsupported", "message": result.Message})
		} else {
			respond(w, 200, map[string]string{"status": "unsupported", "message": instructionFeedback(userError(result.Message))})
		}
	}
}
