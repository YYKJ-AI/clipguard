package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type streamSinkKey struct{}
type streamSink func(string, any)

// Streaming uses the same coordinator as the JSON endpoint, including all
// version, clarification, preview and confirmation checks.
func (s *Server) streamMessage(w http.ResponseWriter, r *http.Request, u *userSession) {
	writer := &messageStream{destination: w, header: make(http.Header), status: http.StatusOK}
	ctx := context.WithValue(r.Context(), streamSinkKey{}, streamSink(writer.event))
	s.message(writer, r.WithContext(ctx), u)
}

type messageStream struct {
	destination      http.ResponseWriter
	header           http.Header
	status, sequence int
}

func (w *messageStream) Header() http.Header    { return w.header }
func (w *messageStream) WriteHeader(status int) { w.status = status }
func (w *messageStream) Write(data []byte) (int, error) {
	if w.status >= 400 {
		w.destination.Header().Set("X-Error-Code", w.header.Get("X-Error-Code"))
		var failure struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(data, &failure); err != nil {
			return 0, err
		}
		w.event("error", map[string]string{"message": failure.Error})
	} else {
		w.event("result", json.RawMessage(data))
	}
	return len(data), nil
}

func (w *messageStream) event(kind string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	w.destination.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.destination.Header().Set("Cache-Control", "no-store")
	w.destination.Header().Set("X-Accel-Buffering", "no")
	w.sequence++
	_, _ = fmt.Fprintf(w.destination, "id: %d\nevent: %s\ndata: %s\n\n", w.sequence, kind, data)
	_ = http.NewResponseController(w.destination).Flush()
}

var errStreamIncomplete = errors.New("回复传输中断或未正常结束，本次未完成，请重试；工程没有因此改变")

// readModelStream requires both a normal finish and [DONE]. Network packets and
// UTF-8 characters may split anywhere; Scanner assembles complete SSE lines.
func readModelStream(ctx context.Context, body io.Reader) (string, error) {
	scanner := bufio.NewScanner(io.LimitReader(body, (1<<20)+1))
	scanner.Buffer(make([]byte, 4096), 256<<10)
	var content strings.Builder
	var lines []string
	finished, done := false, false
	lastDraft := ""
	sink, _ := ctx.Value(streamSinkKey{}).(streamSink)
	process := func() error {
		if len(lines) == 0 {
			return nil
		}
		data := strings.Join(lines, "\n")
		lines = nil
		if data == "[DONE]" {
			if !finished {
				return errStreamIncomplete
			}
			done = true
			return nil
		}
		if done {
			return errStreamIncomplete
		}
		var chunk struct {
			Error   json.RawMessage `json:"error"`
			Usage   json.RawMessage `json:"usage"`
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Content      string          `json:"content"`
					Refusal      string          `json:"refusal"`
					ToolCalls    json.RawMessage `json:"tool_calls"`
					FunctionCall json.RawMessage `json:"function_call"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Error) > 0 {
			return errStreamIncomplete
		}
		if len(chunk.Choices) == 0 && len(chunk.Usage) > 0 && finished {
			return nil
		}
		if len(chunk.Choices) != 1 || chunk.Choices[0].Index != 0 || finished {
			return errStreamIncomplete
		}
		choice := chunk.Choices[0]
		if choice.Delta.Refusal != "" || len(choice.Delta.ToolCalls) > 0 || len(choice.Delta.FunctionCall) > 0 {
			return errStreamIncomplete
		}
		if choice.Delta.Content != "" {
			content.WriteString(choice.Delta.Content)
			if content.Len() > 256<<10 {
				return errStreamIncomplete
			}
			if sink != nil {
				sink("progress", map[string]string{"message": "正在接收助手回复…"})
				if draft := answerDraft(content.String()); draft != "" && draft != lastDraft {
					if !strings.HasPrefix(draft, lastDraft) {
						return errStreamIncomplete
					}
					sink("draft", map[string]string{"text": draft[len(lastDraft):]})
					lastDraft = draft
				}
			}
		}
		if choice.FinishReason != nil {
			if *choice.FinishReason != "stop" {
				return errStreamIncomplete
			}
			finished = true
		}
		return nil
	}
	for scanner.Scan() {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		line := scanner.Text()
		if line == "" {
			if err := process(); err != nil {
				return "", err
			}
			if done {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			lines = append(lines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if scanner.Err() != nil || !done || !finished || content.Len() == 0 {
		return "", errStreamIncomplete
	}
	return content.String(), nil
}

// Only a provisional consultation message may be exposed while generating.
// Plans, candidate IDs and raw JSON never leave this path as draft text.
func answerDraft(raw string) string {
	d := json.NewDecoder(strings.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return ""
	}
	status := ""
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return ""
		}
		if key == "message" && status == "answer" {
			rest := strings.TrimSpace(raw[d.InputOffset():])
			if !strings.HasPrefix(rest, ":") {
				return ""
			}
			return jsonStringDraft(strings.TrimSpace(rest[1:]))
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return ""
		}
		if key == "status" {
			if json.Unmarshal(value, &status) != nil {
				return ""
			}
		}
	}
	return ""
}

// Decode only complete JSON string characters, including surrogate pairs.
func jsonStringDraft(raw string) string {
	if !strings.HasPrefix(raw, `"`) {
		return ""
	}
	var out strings.Builder
	for i := 1; i < len(raw); {
		if raw[i] == '"' {
			return out.String()
		}
		if raw[i] == '\\' {
			if i+1 >= len(raw) {
				break
			}
			if raw[i+1] == 'u' {
				if i+6 > len(raw) {
					break
				}
				n, err := strconv.ParseUint(raw[i+2:i+6], 16, 16)
				if err != nil {
					break
				}
				r := rune(n)
				size := 6
				if utf16.IsSurrogate(r) {
					if r < 0xD800 || r > 0xDBFF || i+12 > len(raw) || raw[i+6:i+8] != `\u` {
						break
					}
					low, err := strconv.ParseUint(raw[i+8:i+12], 16, 16)
					if err != nil || low < 0xDC00 || low > 0xDFFF {
						break
					}
					r = utf16.DecodeRune(r, rune(low))
					size = 12
				}
				out.WriteRune(r)
				i += size
				continue
			}
			var decoded string
			if json.Unmarshal([]byte(`"`+raw[i:i+2]+`"`), &decoded) != nil {
				break
			}
			out.WriteString(decoded)
			i += 2
			continue
		}
		r, size := utf8.DecodeRuneInString(raw[i:])
		if (r == utf8.RuneError && size == 1) || r < 0x20 {
			break
		}
		out.WriteRune(r)
		i += size
	}
	return out.String()
}
