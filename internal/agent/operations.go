package agent

import (
	"fmt"
	"sort"
	"strings"
)

// A single explicit registry supplies strict decoding, execution and model
// documentation. Avoid init registration so assembly is visible and deterministic.
type operationSpec struct {
	fields      []string
	description string
	apply       func(*Project, map[string]Track, Operation) error
}

var operationRegistry = map[string]operationSpec{
	"move": moveOperation(), "trim": trimOperation(), "caption": captionOperation(),
	"delete": deleteOperation(), "insert": insertOperation(),
}

func operationDescriptions() string {
	var b strings.Builder
	kinds := make([]string, 0, len(operationRegistry))
	for kind := range operationRegistry {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		spec := operationRegistry[kind]
		fmt.Fprintf(&b, "%s：%s；允许字段 type、target_id、%s。\n", kind, spec.description, strings.Join(spec.fields, "、"))
	}
	return b.String()
}
func editableClip(p *Project, tracks map[string]Track, o Operation) (int, error) {
	if o.SourceID != nil {
		return -1, appError(CodeInput, "只有插入操作允许 source_id")
	}
	for i, c := range p.Clips {
		if c.ID == o.TargetID {
			if tracks[c.TrackID].Locked {
				return -1, errorf(CodeLocked, "轨道 %s 已锁定，不能修改 %s", c.TrackID, c.ID)
			}
			return i, nil
		}
	}
	return -1, errorf(CodeTarget, "未知片段 %s", o.TargetID)
}
func destinationTrack(tracks map[string]Track, id, kind string) (Track, error) {
	target, ok := tracks[id]
	if !ok || target.Kind != kind {
		return Track{}, appError(CodeProject, "目标轨道不存在或类型不同")
	}
	if target.Locked {
		return Track{}, appError(CodeLocked, "目标轨道已锁定")
	}
	return target, nil
}
