package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

type Track struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Locked bool   `json:"locked"`
}

func (t *Track) UnmarshalJSON(data []byte) error {
	var value struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Locked *bool  `json:"locked"`
	}
	if err := strictJSON(data, &value); err != nil {
		return err
	}
	if value.Locked == nil {
		return appError(CodeProject, "轨道 locked 必须明确填写 true 或 false，不能省略或为 null")
	}
	*t = Track{ID: value.ID, Kind: value.Kind, Locked: *value.Locked}
	return nil
}

type Clip struct {
	ID               string  `json:"id"`
	DisplayName      *string `json:"display_name,omitempty"`
	TrackID          string  `json:"track_id"`
	StartMS          int64   `json:"start_ms"`
	DurationMS       int64   `json:"duration_ms"`
	SourceInMS       *int64  `json:"source_in_ms,omitempty"`
	SourceDurationMS *int64  `json:"source_duration_ms,omitempty"`
	Text             *string `json:"text,omitempty"`
}

func (c *Clip) UnmarshalJSON(data []byte) error {
	// Zero is a valid timeline start, so decoding into int64 alone cannot
	// distinguish an explicit 0 from a missing or null required field.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, name := range []string{"id", "track_id", "start_ms", "duration_ms"} {
		if _, ok := fields[name]; !ok {
			return errorf(CodeProject, "片段缺少必填字段 %s", name)
		}
	}
	for name, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errorf(CodeProject, "片段字段 %s 不允许 null；不适用的可选字段请省略", name)
		}
	}
	type plain Clip
	var value plain
	if err := strictJSON(data, &value); err != nil {
		return err
	}
	*c = Clip(value)
	return nil
}

type Project struct {
	ID       string  `json:"project_id"`
	Revision int64   `json:"revision"`
	Tracks   []Track `json:"tracks"`
	Clips    []Clip  `json:"clips"`
}
type Operation struct {
	Type        string  `json:"type"`
	TargetID    string  `json:"target_id"`
	SourceID    *string `json:"source_id,omitempty"`
	StartMS     *int64  `json:"start_ms,omitempty"`
	DurationMS  *int64  `json:"duration_ms,omitempty"`
	ShortenByMS *int64  `json:"shorten_by_ms,omitempty"`
	TrackID     *string `json:"track_id,omitempty"`
	Text        *string `json:"text,omitempty"`
}
type Change struct {
	ID     string `json:"id"`
	Before *Clip  `json:"before"`
	After  *Clip  `json:"after"`
}

func strictJSON(data []byte, dest any) error {
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dest); err != nil {
		return fmt.Errorf("JSON 格式或字段错误: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return appError(CodeJSON, "只允许一个 JSON 值")
	}
	return nil
}

// Duplicate keys are legal JSON but ambiguous for a strict contract: two
// readers can disagree on which value wins, so a typo can silently override a
// field. Reject them at the boundary instead of picking the last one.
func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var scan func() error
	scan = func() error {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		switch token {
		case json.Delim('{'):
			seen := make(map[string]bool)
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return appError(CodeJSON, "对象字段名必须是字符串")
				}
				if seen[key] {
					return errorf(CodeJSON, "字段 %s 重复出现，请只保留一处", key)
				}
				seen[key] = true
				if err := scan(); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		case json.Delim('['):
			for decoder.More() {
				if err := scan(); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		default:
			return nil
		}
	}
	if err := scan(); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return appError(CodeJSON, "只允许一个 JSON 值")
	}
	return nil
}
func DecodeProject(data []byte) (Project, error) {
	var p Project
	if err := strictJSON(data, &p); err != nil {
		return p, err
	}
	return p, Validate(p)
}
func Clone(p Project) Project {
	out := p
	if p.Tracks != nil {
		out.Tracks = append([]Track{}, p.Tracks...)
	}
	if p.Clips != nil {
		out.Clips = make([]Clip, len(p.Clips))
		for i, c := range p.Clips {
			out.Clips[i] = cloneClip(c)
		}
	}
	return out
}

func clonePtr[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func cloneClip(c Clip) Clip {
	c.DisplayName = clonePtr(c.DisplayName)
	c.SourceInMS = clonePtr(c.SourceInMS)
	c.SourceDurationMS = clonePtr(c.SourceDurationMS)
	c.Text = clonePtr(c.Text)
	return c
}

func validID(id string) bool   { return strings.TrimSpace(id) != "" && len(id) <= 128 }
func Validate(p Project) error { return validateProject(p, true) }

// validateStructure 只检查轨道、片段自身字段与引用完整性，不检查同轨重叠。
// 批次内的中间态允许暂时重叠（交换两段顺序必须经过重叠的瞬间），
// 整条指令提交前再由 Validate 统一检查重叠。
func validateStructure(p Project) error { return validateProject(p, false) }

func validateProject(p Project, checkOverlap bool) error {
	if !validID(p.ID) || p.Revision < 1 {
		return appError(CodeProject, "project_id 不能为空，revision 必须为正整数")
	}
	if len(p.Tracks) == 0 || len(p.Tracks) > 128 || p.Clips == nil || len(p.Clips) > 4096 {
		return appError(CodeProject, "工程需要 1—128 条轨道及 clips 数组，片段最多 4096 个")
	}
	tracks := map[string]Track{}
	for _, tr := range p.Tracks {
		if !validID(tr.ID) || (tr.Kind != "video" && tr.Kind != "caption") {
			return appError(CodeProject, "轨道 ID 或类型无效")
		}
		if _, ok := tracks[tr.ID]; ok {
			return appError(CodeProject, "轨道 ID 重复")
		}
		tracks[tr.ID] = tr
	}
	ids := map[string]bool{}
	byTrack := map[string][]Clip{}
	for _, c := range p.Clips {
		if c.DisplayName != nil && (!utf8.ValidString(*c.DisplayName) || utf8.RuneCountInString(*c.DisplayName) > 128) {
			return appError(CodeProject, "片段名称必须是有效文字，且不能超过 128 个字符")
		}
		if !validID(c.ID) || ids[c.ID] {
			return appError(CodeProject, "片段 ID 为空或重复")
		}
		ids[c.ID] = true
		tr, ok := tracks[c.TrackID]
		if !ok {
			return errorf(CodeProject, "片段 %s 引用了未知轨道", c.ID)
		}
		if c.StartMS < 0 || c.DurationMS <= 0 || c.StartMS > math.MaxInt64-c.DurationMS {
			return errorf(CodeTime, "片段 %s 时间非法或溢出", c.ID)
		}
		if tr.Kind == "video" {
			if c.Text != nil || c.SourceInMS == nil || c.SourceDurationMS == nil || *c.SourceInMS < 0 || *c.SourceDurationMS <= 0 || *c.SourceInMS > *c.SourceDurationMS || c.DurationMS > *c.SourceDurationMS-*c.SourceInMS {
				return errorf(CodeTime, "视频 %s 素材源区间非法", c.ID)
			}
		} else {
			if c.Text == nil || len(*c.Text) > 20000 || c.SourceInMS != nil || c.SourceDurationMS != nil {
				return errorf(CodeProject, "字幕 %s 字段非法", c.ID)
			}
		}
		byTrack[c.TrackID] = append(byTrack[c.TrackID], c)
	}
	if !checkOverlap {
		return nil
	}
	for id, clips := range byTrack {
		sort.Slice(clips, func(i, j int) bool { return clips[i].StartMS < clips[j].StartMS })
		for i := 1; i < len(clips); i++ {
			if clips[i-1].StartMS+clips[i-1].DurationMS > clips[i].StartMS {
				return errorf(CodeOverlap, "轨道 %s 的 %s 与 %s 重叠", id, clips[i-1].ID, clips[i].ID)
			}
		}
	}
	return nil
}

func (o *Operation) UnmarshalJSON(data []byte) error {
	type plain Operation
	var v plain
	if err := strictJSON(data, &v); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for k, b := range fields {
		if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
			return errorf(CodeInput, "操作字段 %s 不允许 null", k)
		}
	}
	allowed := map[string]bool{"type": true, "target_id": true}
	spec, ok := operationRegistry[v.Type]
	if !ok {
		return appError(CodeInput, "未知操作类型")
	}
	for _, field := range spec.fields {
		allowed[field] = true
	}

	for k := range fields {
		if !allowed[k] {
			return errorf(CodeInput, "操作 %s 不允许字段 %s", v.Type, k)
		}
	}
	*o = Operation(v)
	return nil
}

func Apply(p Project, ops []Operation) (Project, []Change, error) {
	return applyBatch(p, ops, true, true)
}

// Parsing uses the same validation for a prefix, which may have no net change.
// A complete submitted plan must still contain an actual change.
func applyOperations(p Project, ops []Operation, requireChange bool) (Project, []Change, error) {
	return applyBatch(p, ops, requireChange, true)
}

// applyBatch runs operations in order on a working copy. Committed results must
// pass the full validation; an intermediate prefix may temporarily overlap, so
// parsing passes finalCheck=false while execution always checks.
func applyBatch(p Project, ops []Operation, requireChange, finalCheck bool) (Project, []Change, error) {
	if err := Validate(p); err != nil {
		return Project{}, nil, err
	}
	if len(ops) == 0 || len(ops) > 64 {
		return Project{}, nil, appError(CodeInput, "每条指令需要 1—64 个操作")
	}
	work := Clone(p)
	tracks := map[string]Track{}
	for _, tr := range work.Tracks {
		tracks[tr.ID] = tr
	}
	for n, o := range ops {
		spec, ok := operationRegistry[o.Type]
		if !ok {
			return Project{}, nil, appError(CodeInput, "未知操作类型")
		}
		if err := spec.apply(&work, tracks, o); err != nil {
			return Project{}, nil, fmt.Errorf("操作 %d: %w", n+1, err)
		}
		// 中间态只校验片段自身约束；同轨重叠留到整条指令结束时统一检查，
		// 否则交换、整体挪位这类需要经过临时重叠的指令永远无法表达。
		if err := validateStructure(work); err != nil {
			return Project{}, nil, fmt.Errorf("操作 %d: %w", n+1, err)
		}
	}
	if finalCheck {
		if err := Validate(work); err != nil {
			return Project{}, nil, fmt.Errorf("整条指令校验失败: %w", err)
		}
	}

	changes := Diff(p, work)
	if requireChange && len(changes) == 0 {
		return Project{}, nil, appError(CodeInput, "指令没有产生实际变化")
	}
	return work, changes, nil
}

func Diff(before, after Project) []Change {
	var changes []Change
	seen := map[string]bool{}
	afterByID := make(map[string]Clip, len(after.Clips))
	for _, c := range after.Clips {
		afterByID[c.ID] = c
	}
	for _, b := range before.Clips {
		seen[b.ID] = true
		var a *Clip
		if c, ok := afterByID[b.ID]; ok {
			v := cloneClip(c)
			a = &v
		}
		if a == nil || !reflect.DeepEqual(b, *a) {
			v := cloneClip(b)
			changes = append(changes, Change{ID: b.ID, Before: &v, After: a})
		}
	}
	for _, a := range after.Clips {
		if !seen[a.ID] {
			v := cloneClip(a)
			changes = append(changes, Change{ID: a.ID, After: &v})
		}
	}
	return changes
}
