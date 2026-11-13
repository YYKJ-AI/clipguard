package agent

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	CodeJSON     ErrorCode = "INVALID_JSON"
	CodeLocked   ErrorCode = "TRACK_LOCKED"
	CodeOverlap  ErrorCode = "OVERLAP"
	CodeTarget   ErrorCode = "TARGET_NOT_FOUND"
	CodeTrack    ErrorCode = "TRACK_INVALID"
	CodeTime     ErrorCode = "INVALID_TIME"
	CodeProject  ErrorCode = "INVALID_PROJECT"
	CodeModel    ErrorCode = "MODEL_FAILURE"
	CodeInput    ErrorCode = "INVALID_INPUT"
	CodeConflict ErrorCode = "STATE_CONFLICT"
	CodeUser     ErrorCode = "USER_MESSAGE"
)

// Message is diagnostic detail; public messages are selected by Code.
type AppError struct {
	Code      ErrorCode
	Message   string
	Retryable bool
	Cause     error
}

func (e *AppError) Error() string                   { return e.Message }
func (e *AppError) Unwrap() error                   { return e.Cause }
func (e *AppError) ErrorCode() string               { return string(e.Code) }
func appError(code ErrorCode, message string) error { return &AppError{Code: code, Message: message} }
func errorf(code ErrorCode, format string, args ...any) error {
	cause := fmt.Errorf(format, args...)
	return &AppError{Code: code, Message: cause.Error(), Cause: cause}
}
func userError(message string) error { return appError(CodeUser, message) }

// Each entry states the cause only. The "本次未修改工程" tail is appended once
// by instructionFeedback; do not repeat it here.
var errorMessages = map[ErrorCode]string{
	CodeJSON:     "文件或请求格式有误。请检查工程文件，或重新输入本次要求。",
	CodeLocked:   "相关轨道已锁定，本次不能修改。",
	CodeOverlap:  "修改会造成同一轨道的片段重叠，当前不会自动推移其他片段。请调整目标位置或修改量。",
	CodeTarget:   "没有找到要修改的片段。请在当前时间轴选中片段，或使用唯一片段名称后重试。",
	CodeTrack:    "目标轨道不存在，或与片段类型不符：视频片段只能放入视频轨道，字幕只能放入字幕轨道。请核对轨道后重试。",
	CodeTime:     "修改的时间参数不完整或不符合当前操作。请明确目标时间或缩短量后重试。",
	CodeProject:  "工程格式不正确，请检查工程文件后重试。",
	CodeModel:    "模型服务配置或返回内容不符合要求，请重试；仍失败时请联系程序维护者检查服务。",
	CodeInput:    "修改内容不完整或不符合当前操作，请核对目标和参数后重试。",
	CodeConflict: "工程或计划已变化，请重新读取工程并生成预览。",
}

func publicError(err error) string {
	var app *AppError
	if errors.As(err, &app) {
		if app.Code == CodeUser {
			return app.Message
		}
		if message, ok := errorMessages[app.Code]; ok {
			return message
		}
	}
	return "本次处理未完成，请重试；仍失败时请联系程序维护者。"
}

func instructionFeedback(reason error) string {
	return publicError(reason) + " 本条指令未执行任何修改。请核对选中的片段与修改量，参考页面示例重新提交完整指令。离线模式用数字加秒或毫秒；组合操作用分号分隔，字幕正文使用双引号。仍需查看预览并确认。"
}
