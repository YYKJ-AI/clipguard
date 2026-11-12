package agent

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

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
	return clipCandidates(p, clips)
}
