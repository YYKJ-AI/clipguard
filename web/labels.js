import { state, orderedClips, seconds, durationText } from './state.js';

export function clipName(c, p = state.project) {
  if (c.display_name && c.display_name.trim()) return c.display_name;
  const track = p?.tracks.find((t) => t.id === c.track_id),
    clips = track ? orderedClips(p, track) : [c],
    index = clips.findIndex((item) => item.id === c.id) + 1;
  return c.text !== undefined ? `第 ${index} 条字幕` : `第 ${index} 段`;
}

export function trackName(id, p = state.project) {
  const track = p?.tracks.find((t) => t.id === id);
  if (!track) return '目标轨道';
  const tracks = p.tracks.filter((t) => t.kind === track.kind);
  return `${track.kind === 'caption' ? '字幕' : '视频'}轨道 ${tracks.indexOf(track) + 1}`;
}

export function candidateLabel(candidate) {
  const clip = state.project?.clips.find((c) => c.id === candidate.id);
  return clip
    ? `${clipName(clip)} · ${trackName(clip.track_id)} · ${seconds(clip.start_ms)}—${seconds(clip.start_ms + clip.duration_ms)}`
    : '候选片段';
}

export function changeDescription(change) {
  const b = change.before,
    a = change.after,
    c = a || b;
  const name =
    c.display_name ||
    (state.project?.clips.some((item) => item.id === c.id)
      ? clipName(c)
      : c.text !== undefined
        ? '字幕'
        : '片段');
  if (!b)
    return `插入${name} · ${trackName(a.track_id)} · ${seconds(a.start_ms)}—${seconds(a.start_ms + a.duration_ms)}`;
  if (!a) return `删除${name} · ${seconds(b.start_ms)}—${seconds(b.start_ms + b.duration_ms)}`;
  const fields = [];
  if (b.track_id !== a.track_id) fields.push(`${trackName(b.track_id)} → ${trackName(a.track_id)}`);
  if (b.start_ms !== a.start_ms)
    fields.push(`起点 ${seconds(b.start_ms)} → ${seconds(a.start_ms)}`);
  if (b.duration_ms !== a.duration_ms)
    fields.push(`时长 ${durationText(b.duration_ms)} → ${durationText(a.duration_ms)}`);
  if (b.text !== a.text) fields.push(`字幕“${b.text}” → “${a.text}”`);
  return `${name}：${fields.join('；')}`;
}
