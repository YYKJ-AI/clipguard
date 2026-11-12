package agent

func captionOperation() operationSpec {
	return operationSpec{
		fields:      []string{"text", "start_ms", "duration_ms"},
		description: "修改已有字幕；text/start_ms/duration_ms 至少一个；字幕不自动跟随视频",
		apply: func(p *Project, tracks map[string]Track, o Operation) error {
			index, err := editableClip(p, tracks, o)
			if err != nil {
				return err
			}
			c := &p.Clips[index]
			if tracks[c.TrackID].Kind != "caption" || o.TrackID != nil || o.ShortenByMS != nil || (o.Text == nil && o.StartMS == nil && o.DurationMS == nil) {
				return appError(CodeInput, "字幕操作需要字幕目标和文字或时间参数")
			}
			if o.Text != nil {
				c.Text = clonePtr(o.Text)
			}
			if o.StartMS != nil {
				c.StartMS = *o.StartMS
			}
			if o.DurationMS != nil {
				c.DurationMS = *o.DurationMS
			}
			return nil
		},
	}
}
