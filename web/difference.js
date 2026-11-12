import { state, $, make, seconds } from './state.js';
import { clipName, trackName, changeDescription } from './labels.js';
import { selectClip } from './timeline.js';

export function clearDifference() {
  state.activeDifference = null;
  $('difference-panel').hidden = true;
  $('difference-content').replaceChildren();
}

export function renderDifference(plan) {
  state.activeDifference = plan.id;
  $('difference-panel').hidden = false;
  const container = $('difference-content');
  container.replaceChildren();
  const clips = plan.changes.flatMap((c) => [c.before, c.after].filter(Boolean));
  const end = Math.max(1000, ...clips.map((c) => c.start_ms + c.duration_ms)) * 1.05;
  for (const change of plan.changes) {
    const block = make('div', 'difference-item'),
      target = change.after || change.before;
    const title = make('button', 'text-button', target.display_name || clipName(target));
    title.type = 'button';
    title.addEventListener('click', () => selectClip(target.id));
    block.append(title);
    for (const [clip, label, kind] of [
      [change.before, '修改前', 'before'],
      [change.after, '修改后', 'after'],
    ]) {
      const row = make('div', 'difference-row');
      row.append(make('span', '', label));
      const rail = make('div', 'difference-rail');
      if (clip) {
        const bar = make(
          'div',
          'difference-bar ' + kind,
          `${seconds(clip.start_ms)}—${seconds(clip.start_ms + clip.duration_ms)}`,
        );
        bar.style.left = `${(clip.start_ms / end) * 100}%`;
        bar.style.width = `${(clip.duration_ms / end) * 100}%`;
        bar.title = `${trackName(clip.track_id)} · ${bar.textContent}`;
        rail.append(bar);
      } else rail.append(make('span', 'muted', kind === 'before' ? '尚无此片段' : '将删除'));
      row.append(rail);
      block.append(row);
    }
    block.append(make('p', 'muted', changeDescription(change)));
    container.append(block);
  }
}
