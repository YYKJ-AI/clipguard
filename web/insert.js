import { $, state, orderedClips, make } from './state.js';
import { clipName, trackName } from './labels.js';

export function renderInsertOptions() {
  const sources = $('insert-source'),
    tracks = $('insert-track'),
    previous = sources.value;
  sources.replaceChildren();
  tracks.replaceChildren();
  if (!state.project) return;
  for (const track of state.project.tracks)
    for (const clip of orderedClips(state.project, track)) {
      const o = make('option', '', `${clipName(clip)} · ${trackName(clip.track_id)}`);
      o.value = clip.id;
      sources.append(o);
    }
  if (state.project.clips.some((c) => c.id === previous)) sources.value = previous;
  renderInsertTracks();
}

export function renderInsertTracks() {
  const tracks = $('insert-track'),
    previous = tracks.value;
  tracks.replaceChildren();
  const source =
    state.project?.clips.find((c) => c.id === $('insert-source').value) || state.project?.clips[0];
  if (!source) return;
  const kind = state.project.tracks.find((t) => t.id === source.track_id).kind;
  for (const track of state.project.tracks.filter((t) => t.kind === kind && !t.locked)) {
    const o = make('option', '', trackName(track.id));
    o.value = track.id;
    tracks.append(o);
  }
  if (state.project.tracks.some((t) => t.id === previous && t.kind === kind && !t.locked))
    tracks.value = previous;
}
