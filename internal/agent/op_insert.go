package agent

func insertOperation() operationSpec {
	return operationSpec{
		fields:      []string{"source_id", "start_ms", "track_id"},
		description: "插入已有视频/字幕的独立副本；target_id 为新的唯一 ID，source_id 为已有片段，track_id/start_ms 必填；不能生成素材",
		apply: func(p *Project, tracks map[string]Track, o Operation) error {
			if o.SourceID == nil || o.StartMS == nil || o.TrackID == nil || !validID(o.TargetID) || o.DurationMS != nil || o.ShortenByMS != nil || o.Text != nil {
				return appError(CodeTime, "插入需要新片段 ID、source_id、track_id 和 start_ms，不能附带其他字段")
			}
			var source *Clip
			for _, c := range p.Clips {
				if c.ID == o.TargetID {
					return appError(CodeInput, "新片段 ID 已存在")
				}
				if c.ID == *o.SourceID {
					copy := cloneClip(c)
					source = &copy
				}
			}
			if source == nil {
				return appError(CodeTarget, "插入来源片段不存在")
			}
			if _, err := destinationTrack(tracks, *o.TrackID, tracks[source.TrackID].Kind); err != nil {
				return err
			}
			source.ID, source.TrackID, source.StartMS = o.TargetID, *o.TrackID, *o.StartMS
			p.Clips = append(p.Clips, *source)
			return nil
		},
	}
}
