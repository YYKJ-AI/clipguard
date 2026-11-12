import { state, make, seconds, durationText, $ } from './state.js';
import { clearDifference, renderDifference } from './difference.js';
import { message, action, toast } from './ui.js';
import { clipName, trackName } from './labels.js';
import { api } from './api.js';
import { render } from './timeline.js';
import { refreshHistory, showActualResult } from './history.js';

export function invalidateCards() {
  for (const card of state.pendingCards) {
    for (const b of card.querySelectorAll('button')) b.disabled = true;
    card.append(make('div', 'preview-meta', '该预览已失效或已处理'));
  }
  state.pendingCards.clear();
  clearDifference();
}

export function preview(data) {
  const p = data.preview,
    e = message(data.message, 'assistant', 'plan'),
    card = make('section', 'preview-card');
  renderDifference(p);
  card.append(
    make('h3', '', '待确认的修改'),
    make('div', 'preview-meta', `基于版本 ${p.revision} · ${p.operations.length} 项操作`),
  );
  let hasDelete = false,
    hasVideo = false;
  for (const change of p.changes) {
    const block = make('div', 'change'),
      before = change.before,
      after = change.after;
    if ((after || before).text === undefined) hasVideo = true;
    block.append(make('strong', '', clipName(after || before)));
    if (!before) {
      block.append(
        make(
          'div',
          '',
          `将插入新副本 · ${trackName(after.track_id)} · ${seconds(after.start_ms)}—${seconds(after.start_ms + after.duration_ms)}`,
        ),
      );
    } else if (!after) {
      block.append(
        make(
          'div',
          'danger-note',
          `将删除此片段（${seconds(before.start_ms)}—${seconds(before.start_ms + before.duration_ms)}）`,
        ),
      );
      hasDelete = true;
    } else {
      for (const [key, label] of [
        ['track_id', '所在轨道'],
        ['start_ms', '起点'],
        ['duration_ms', '时长'],
        ['text', '字幕文字'],
      ]) {
        if (before?.[key] !== after[key]) {
          const format = (v) =>
            v === undefined
              ? '—'
              : key === 'start_ms'
                ? seconds(v)
                : key === 'duration_ms'
                  ? durationText(v)
                  : key === 'track_id'
                    ? trackName(v)
                    : String(v);
          const line = make('div', 'change-line', `${label}将从 ${format(before?.[key])} 改为 `);
          line.append(make('span', '', format(after[key])));
          block.append(line);
        }
      }
      block.append(
        make(
          'div',
          'preview-meta',
          `修改后时间：${seconds(after.start_ms)}—${seconds(after.start_ms + after.duration_ms)}`,
        ),
      );
    }
    card.append(block);
  }
  if (hasVideo)
    card.append(
      make(
        'p',
        'preview-note',
        '字幕不会自动跟随视频移动、缩短或删除；只有本预览明确列出的字幕修改才会执行。',
      ),
    );
  if (!p.changes.length) card.append(make('p', 'preview-note', '与当前工程相比没有实际变化。'));
  if (hasDelete)
    card.append(make('p', 'danger-note', '包含删除操作。确认后移除，可在本次运行中撤销。'));
  const buttons = make('div', 'preview-actions'),
    cancel = make('button', 'button', '取消'),
    confirm = make('button', 'button primary', '确认修改');
  cancel.type = confirm.type = 'button';
  buttons.append(cancel, confirm);
  card.append(buttons);
  e.append(card);
  state.pendingCards.add(card);
  const compare = make('button', 'button', '查看时间轴差异');
  compare.type = 'button';
  compare.addEventListener('click', () => {
    renderDifference(p);
    $('difference-panel').scrollIntoView?.({ behavior: 'smooth', block: 'nearest' });
  });
  buttons.prepend?.(compare);
  confirm.addEventListener('click', () =>
    action(async () => {
      const r = await api('confirm', { plan_id: p.id });
      for (const op of p.operations)
        if (op.type === 'insert' && state.mediaBindings.has(op.source_id))
          state.mediaBindings.set(op.target_id, state.mediaBindings.get(op.source_id));
      invalidateCards();
      state.dirty = true;
      render(r.project);
      await refreshHistory();
      showActualResult(r);
      toast('修改已应用');
    }),
  );
  cancel.addEventListener('click', () =>
    action(async () => {
      const r = await api('cancel', { plan_id: p.id });
      confirm.disabled = cancel.disabled = true;
      state.pendingCards.delete(card);
      compare.disabled = true;
      if (state.activeDifference === p.id) clearDifference();
      card.append(make('div', 'preview-meta', '已取消'));
      message(r.message);
    }),
  );
  $('messages').scrollTop = $('messages').scrollHeight;
}
