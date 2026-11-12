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
		return errors.New("轨道 locked 必须明确填写 true 或 false，不能省略或为 null")
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
			return fmt.Errorf("片段缺少必填字段 %s", name)
		}
	}
	for name, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("片段字段 %s 不允许 null；不适用的可选字段请省略", name)
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
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dest); err != nil {
		return fmt.Errorf("JSON 格式或字段错误: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("只允许一个 JSON 值")
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
	b, _ := json.Marshal(p)
	var out Project
	_ = json.Unmarshal(b, &out)
	return out
}
func validID(id string) bool { return strings.TrimSpace(id) != "" && len(id) <= 128 }
func Validate(p Project) error {
	if !validID(p.ID) || p.Revision < 1 {
		return errors.New("project_id 不能为空，revision 必须为正整数")
	}
	if len(p.Tracks) == 0 || len(p.Tracks) > 128 || p.Clips == nil || len(p.Clips) > 4096 {
		return errors.New("工程需要 1—128 条轨道及 clips 数组，片段最多 4096 个")
	}
	tracks := map[string]Track{}
	for _, tr := range p.Tracks {
		if !validID(tr.ID) || (tr.Kind != "video" && tr.Kind != "caption") {
			return errors.New("轨道 ID 或类型无效")
		}
		if _, ok := tracks[tr.ID]; ok {
			return errors.New("轨道 ID 重复")
		}
		tracks[tr.ID] = tr
	}
	ids := map[string]bool{}
	byTrack := map[string][]Clip{}
	for _, c := range p.Clips {
		if c.DisplayName != nil && (!utf8.ValidString(*c.DisplayName) || utf8.RuneCountInString(*c.DisplayName) > 128) {
			return errors.New("片段名称必须是有效文字，且不能超过 128 个字符")
		}
		if !validID(c.ID) || ids[c.ID] {
			return errors.New("片段 ID 为空或重复")
		}
		ids[c.ID] = true
		tr, ok := tracks[c.TrackID]
		if !ok {
			return fmt.Errorf("片段 %s 引用了未知轨道", c.ID)
		}
		if c.StartMS < 0 || c.DurationMS <= 0 || c.StartMS > math.MaxInt64-c.DurationMS {
			return fmt.Errorf("片段 %s 时间非法或溢出", c.ID)
		}
		if tr.Kind == "video" {
			if c.Text != nil || c.SourceInMS == nil || c.SourceDurationMS == nil || *c.SourceInMS < 0 || *c.SourceDurationMS <= 0 || *c.SourceInMS > *c.SourceDurationMS || c.DurationMS > *c.SourceDurationMS-*c.SourceInMS {
				return fmt.Errorf("视频 %s 素材源区间非法", c.ID)
			}
		} else {
			if c.Text == nil || len(*c.Text) > 20000 || c.SourceInMS != nil || c.SourceDurationMS != nil {
				return fmt.Errorf("字幕 %s 字段非法", c.ID)
			}
		}
		byTrack[c.TrackID] = append(byTrack[c.TrackID], c)
	}
	for id, clips := range byTrack {
		sort.Slice(clips, func(i, j int) bool { return clips[i].StartMS < clips[j].StartMS })
		for i := 1; i < len(clips); i++ {
			if clips[i-1].StartMS+clips[i-1].DurationMS > clips[i].StartMS {
				return fmt.Errorf("轨道 %s 的 %s 与 %s 重叠", id, clips[i-1].ID, clips[i].ID)
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
			return fmt.Errorf("操作字段 %s 不允许 null", k)
		}
	}
	allowed := map[string]bool{"type": true, "target_id": true}
	switch v.Type {
	case "insert":
		allowed["source_id"] = true
		allowed["start_ms"] = true
		allowed["track_id"] = true
	case "move":
		allowed["start_ms"] = true
		allowed["track_id"] = true
	case "trim":
		allowed["duration_ms"] = true
		allowed["shorten_by_ms"] = true
	case "caption":
		allowed["start_ms"] = true
		allowed["duration_ms"] = true
		allowed["text"] = true
	case "delete":
	default:
		return errors.New("未知操作类型")
	}
	for k := range fields {
		if !allowed[k] {
			return fmt.Errorf("操作 %s 不允许字段 %s", v.Type, k)
		}
	}
	*o = Operation(v)
	return nil
}

func Apply(p Project, ops []Operation) (Project, []Change, error) {
	return applyOperations(p, ops, true)
}

// Parsing uses the same validation for a prefix, which may have no net change.
// A complete submitted plan must still contain an actual change.
func applyOperations(p Project, ops []Operation, requireChange bool) (Project, []Change, error) {
	if err := Validate(p); err != nil {
		return Project{}, nil, err
	}
	if len(ops) == 0 || len(ops) > 64 {
		return Project{}, nil, errors.New("每条指令需要 1—64 个操作")
	}
	work := Clone(p)
	tracks := map[string]Track{}
	for _, tr := range work.Tracks {
		tracks[tr.ID] = tr
	}
	for n, o := range ops {
		if o.Type == "insert" {
			if o.SourceID == nil || o.StartMS == nil || o.TrackID == nil || !validID(o.TargetID) || o.DurationMS != nil || o.ShortenByMS != nil || o.Text != nil {
				return Project{}, nil, errors.New("插入需要新片段 ID、source_id、track_id 和 start_ms，不能附带其他字段")
			}
			var source *Clip
			for _, clip := range work.Clips {
				if clip.ID == o.TargetID {
					return Project{}, nil, errors.New("新片段 ID 已存在")
				}
				if clip.ID == *o.SourceID {
					copy := clip
					source = &copy
				}
			}
			if source == nil {
				return Project{}, nil, errors.New("插入来源片段不存在")
			}
			target, ok := tracks[*o.TrackID]
			if !ok || target.Kind != tracks[source.TrackID].Kind {
				return Project{}, nil, errors.New("目标轨道不存在或类型不同")
			}
			if target.Locked {
				return Project{}, nil, errors.New("目标轨道已锁定")
			}
			source.ID, source.TrackID, source.StartMS = o.TargetID, *o.TrackID, *o.StartMS
			work.Clips = append(work.Clips, *source)
			if err := Validate(work); err != nil {
				return Project{}, nil, fmt.Errorf("操作 %d: %w", n+1, err)
			}
			continue
		}
		if o.SourceID != nil {
			return Project{}, nil, errors.New("只有插入操作允许 source_id")
		}
		index := -1
		for i, c := range work.Clips {
			if c.ID == o.TargetID {
				index = i
				break
			}
		}
		if index < 0 {
			return Project{}, nil, fmt.Errorf("操作 %d: 未知片段 %s", n+1, o.TargetID)
		}
		c := &work.Clips[index]
		tr := tracks[c.TrackID]
		if tr.Locked {
			return Project{}, nil, fmt.Errorf("轨道 %s 已锁定，不能修改 %s", tr.ID, c.ID)
		}
		switch o.Type {
		case "move":
			if o.StartMS == nil || o.DurationMS != nil || o.Text != nil || o.ShortenByMS != nil {
				return Project{}, nil, errors.New("移动需要 start_ms，不能包含时长或文字")
			}
			if o.TrackID != nil {
				target, ok := tracks[*o.TrackID]
				if !ok || target.Kind != tr.Kind {
					return Project{}, nil, errors.New("目标轨道不存在或类型不同")
				}
				if target.Locked {
					return Project{}, nil, errors.New("目标轨道已锁定")
				}
				c.TrackID = target.ID
			}
			c.StartMS = *o.StartMS
		case "trim":
			if tr.Kind != "video" || (o.DurationMS == nil) == (o.ShortenByMS == nil) || o.StartMS != nil || o.Text != nil || o.TrackID != nil {
				return Project{}, nil, errors.New("裁剪仅支持视频右端，duration_ms 与 shorten_by_ms 必须且只能提供一个")
			}
			if o.ShortenByMS != nil {
				if *o.ShortenByMS <= 0 || *o.ShortenByMS >= c.DurationMS {
					return Project{}, nil, errors.New("缩短量必须为正整数且小于当前片段时长")
				}
				c.DurationMS -= *o.ShortenByMS
			} else {
				c.DurationMS = *o.DurationMS
			}
		case "caption":
			if tr.Kind != "caption" || o.TrackID != nil || o.ShortenByMS != nil || (o.Text == nil && o.StartMS == nil && o.DurationMS == nil) {
				return Project{}, nil, errors.New("字幕操作需要字幕目标和文字或时间参数")
			}
			if o.Text != nil {
				c.Text = o.Text
			}
			if o.StartMS != nil {
				c.StartMS = *o.StartMS
			}
			if o.DurationMS != nil {
				c.DurationMS = *o.DurationMS
			}
		case "delete":
			if o.StartMS != nil || o.DurationMS != nil || o.TrackID != nil || o.Text != nil || o.ShortenByMS != nil {
				return Project{}, nil, errors.New("删除只接受目标 ID")
			}
			work.Clips = append(work.Clips[:index], work.Clips[index+1:]...)
		default:
			return Project{}, nil, errors.New("未知操作类型")
		}
		if err := Validate(work); err != nil {
			return Project{}, nil, fmt.Errorf("操作 %d: %w", n+1, err)
		}
	}
	changes := Diff(p, work)
	if requireChange && len(changes) == 0 {
		return Project{}, nil, errors.New("指令没有产生实际变化")
	}
	return work, changes, nil
}

func Diff(before, after Project) []Change {
	var changes []Change
	seen := map[string]bool{}
	for _, b := range before.Clips {
		seen[b.ID] = true
		var a *Clip
		for _, c := range after.Clips {
			if c.ID == b.ID {
				v := c
				a = &v
				break
			}
		}
		if a == nil || !reflect.DeepEqual(b, *a) {
			v := b
			changes = append(changes, Change{ID: b.ID, Before: &v, After: a})
		}
	}
	for _, a := range after.Clips {
		if !seen[a.ID] {
			v := a
			changes = append(changes, Change{ID: a.ID, After: &v})
		}
	}
	return changes
}
