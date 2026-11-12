package agent

func deleteOperation() operationSpec {
	return operationSpec{
		fields: []string{}, description: "删除已有片段；只接受 type 和 target_id；必须预览确认",
		apply: func(p *Project, tracks map[string]Track, o Operation) error {
			index, err := editableClip(p, tracks, o)
			if err != nil {
				return err
			}
			if o.StartMS != nil || o.DurationMS != nil || o.TrackID != nil || o.Text != nil || o.ShortenByMS != nil {
				return appError(CodeInput, "删除只接受目标 ID")
			}
			p.Clips = append(p.Clips[:index], p.Clips[index+1:]...)
			return nil
		},
	}
}
