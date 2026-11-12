package agent

func moveOperation() operationSpec {
	return operationSpec{
		fields:      []string{"start_ms", "track_id"},
		description: "移动已有片段；必须提供 start_ms，可选同类型 track_id；不能重叠或自动推移",
		apply: func(p *Project, tracks map[string]Track, o Operation) error {
			index, err := editableClip(p, tracks, o)
			if err != nil {
				return err
			}
			c := &p.Clips[index]
			if o.StartMS == nil || o.DurationMS != nil || o.Text != nil || o.ShortenByMS != nil {
				return appError(CodeTime, "移动需要 start_ms，不能包含时长或文字")
			}
			if o.TrackID != nil {
				target, err := destinationTrack(tracks, *o.TrackID, tracks[c.TrackID].Kind)
				if err != nil {
					return err
				}
				c.TrackID = target.ID
			}
			c.StartMS = *o.StartMS
			return nil
		},
	}
}
