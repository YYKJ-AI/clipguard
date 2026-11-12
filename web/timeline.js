import { $, make, orderedClips, state, seconds, durationText } from './state.js';
import { clipName, trackName } from './labels.js';
import { updateMedia } from './media.js';
import { renderInsertOptions } from './insert.js';
import { updateControls } from './ui.js';

export function renderExamples(p) {
  const suggestions = $('suggestions'),
    usage = $('usage-examples');
  suggestions.replaceChildren();
  usage.replaceChildren();
  $('instruction').placeholder = '说说你的修改想法…';
  if (!p) {
    usage.append(make('p', '', '载入工程后，这里会显示适用于当前片段的示例。'));
    return;
  }
  const available = p.tracks
    .filter((t) => !t.locked)
    .flatMap((t) => orderedClips(p, t))
    .filter((c) => /^[A-Za-z0-9][A-Za-z0-9_.:-]*$/.test(c.id));
  const video = available.find((c) => c.text === undefined && c.duration_ms > 1),
    caption = available.find((c) => c.text !== undefined);
  const examples = [];
  if (video) {
    const amount = video.duration_ms > 1000 ? '1 秒' : '1 毫秒';
    examples.push({
      target: video.id,
      label: `缩短${clipName(video, p)}`,
      text: `把这段缩短 ${amount}`,
    });
  }
  if (caption)
    examples.push({
      target: caption.id,
      label: `修改${clipName(caption, p)}`,
      text: '把这段的字幕改为“下期见”',
    });
  if (available.length) {
    const target = available.find((c) => c !== video && c !== caption) || available[0];
    examples.push({ target: target.id, label: `删除${clipName(target, p)}`, text: '删除这段' });
  }
  for (const example of examples) {
    const button = make('button', '', example.label);
    button.type = 'button';
    button.dataset.example = example.text;
    button.addEventListener('click', () => {
      selectClip(example.target);
      $('instruction').value = example.text;
      $('instruction').focus();
    });
    suggestions.append(button);
    usage.append(make('p', '', `${example.label}：选中对应片段后，输入“${example.text}”`));
  }
  if (examples.length)
    $('instruction').placeholder = '说说你的修改想法…\n点击上方示例可填入当前片段的指令';
  else
    usage.append(
      make('p', '', '当前没有适用的离线示例。锁定片段或含特殊符号的标识不会生成示例指令。'),
    );
}

export function updateSelection() {
  const clip = state.project?.clips.find((c) => c.id === state.selectedClipID);
  $('selected-context').textContent = clip
    ? '当前选中：' + clipName(clip)
    : '未选择片段；点击时间轴里的镜头或字幕';
  for (const element of document.querySelectorAll('[data-clip-id]'))
    element.classList.toggle('selected', element.dataset.clipId === state.selectedClipID);
}

export function selectClip(id) {
  if (!state.project?.clips.some((c) => c.id === id)) return;
  state.selectedClipID = id;
  state.selectedProjectID = state.project.project_id;
  updateSelection();
  updateMedia(true);
}

export function render(p) {
  renderPanels(p);
  if (
    !p ||
    state.selectedProjectID !== p.project_id ||
    !p.clips.some((c) => c.id === state.selectedClipID)
  )
    state.selectedClipID = null;
  state.selectedProjectID = p?.project_id ?? null;
  state.project = p;
  renderExamples(p);
  updateSelection();
  renderInsertOptions();
  updateMedia();
  $('empty').hidden = !!p;
  $('timeline-panel').hidden = !p;
  $('details-panel').hidden = !p;
  if (!p) {
    updateControls();
    return;
  }
  $('project-title').textContent = '当前时间轴工程';
  $('project-meta').textContent =
    `版本 ${p.revision} · ${p.tracks.length} 条轨道 · ${p.clips.length} 个片段`;
  $('clip-count').textContent = `${p.clips.length} 个片段`;
  $('timeline').replaceChildren();
  $('clip-rows').replaceChildren();
  const end = Math.max(10000, ...p.clips.map((c) => c.start_ms + c.duration_ms)) * 1.08;
  const ruler = make('div', 'ruler');
  ruler.append(make('div', 'lane-name', '时间 / 分:秒'));
  const marks = make('div', 'ruler-track');
  for (let i = 0; i <= 5; i++) marks.append(make('span', '', seconds(Math.round((end * i) / 5))));
  ruler.append(marks);
  $('timeline').append(ruler);
  for (const track of p.tracks) {
    const lane = make('div', 'lane'),
      name = make('div', 'lane-name');
    name.append(
      make('strong', '', trackName(track.id)),
      make(
        'small',
        '',
        track.locked ? '🔒 已锁定' : track.kind === 'video' ? '视频轨道' : '字幕轨道',
      ),
    );
    const rail = make('div', 'lane-track');
    for (const c of orderedClips(p, track)) {
      const labelText = clipName(c, p),
        bar = make(
          'div',
          `clip ${track.kind === 'caption' ? 'caption' : ''} ${track.locked ? 'locked' : ''}`,
        );
      bar.style.left = `${(c.start_ms / end) * 100}%`;
      bar.style.width = `${(c.duration_ms / end) * 100}%`;
      bar.title = `${labelText} · ${seconds(c.start_ms)}—${seconds(c.start_ms + c.duration_ms)}${c.text !== undefined ? ' · ' + c.text : ''}`;
      bar.append(
        make('strong', '', labelText),
        make('small', '', c.text ?? durationText(c.duration_ms)),
      );
      bar.dataset.clipId = c.id;
      bar.role = 'button';
      bar.tabIndex = 0;
      bar.addEventListener('click', () => selectClip(c.id));
      bar.addEventListener('keydown', (event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          selectClip(c.id);
        }
      });
      rail.append(bar);
      const row = make('tr');
      const label = make('td', '', labelText);
      if (c.text !== undefined) label.append(make('small', '', c.text));
      else
        label.append(
          make(
            'small',
            '',
            `素材入点 ${seconds(c.source_in_ms)} / 源时长 ${durationText(c.source_duration_ms)}`,
          ),
        );
      row.append(
        label,
        make('td', '', trackName(track.id) + (track.locked ? ' 🔒' : '')),
        make('td', '', `${seconds(c.start_ms)}—${seconds(c.start_ms + c.duration_ms)}`),
        make('td', '', durationText(c.duration_ms)),
      );
      $('clip-rows').append(row);
    }
    lane.append(name, rail);
    $('timeline').append(lane);
  }
  updateSelection();
  updateControls();
}

export function renderPanels(p) {
  for (const id of ['insert-panel', 'media-panel', 'history-panel']) $(id).hidden = !p;
}
