package agent

func trimOperation() operationSpec {
	return operationSpec{
		fields:      []string{"duration_ms", "shorten_by_ms"},
		description: "只调整视频右端；duration_ms（最终时长）和 shorten_by_ms（缩短量）必须且只能提供一个；保持起点和素材入点",
		apply: func(p *Project, tracks map[string]Track, o Operation) error {
			index, err := editableClip(p, tracks, o)
			if err != nil {
				return err
			}
			c := &p.Clips[index]
			if tracks[c.TrackID].Kind != "video" || (o.DurationMS == nil) == (o.ShortenByMS == nil) || o.StartMS != nil || o.Text != nil || o.TrackID != nil {
				return appError(CodeTime, "裁剪仅支持视频右端，duration_ms 与 shorten_by_ms 必须且只能提供一个")
			}
			if o.ShortenByMS != nil {
				if *o.ShortenByMS <= 0 || *o.ShortenByMS >= c.DurationMS {
					return appError(CodeTime, "缩短量必须为正整数且小于当前片段时长")
				}
				c.DurationMS -= *o.ShortenByMS
			} else {
				c.DurationMS = *o.DurationMS
			}
			return nil
		},
	}
}
