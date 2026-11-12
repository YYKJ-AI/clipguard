import { message, updateControls } from './ui.js';
import { make, state, $ } from './state.js';
import { changeDescription } from './labels.js';
import { api } from './api.js';

export function showActualResult(result) {
  const e = message(
    `${result.message}。当前版本 ${result.project.revision}。`,
    'assistant',
    'executed',
  );
  for (const change of result.changes)
    e.append(make('p', 'actual-change', changeDescription(change)));
}

export function renderHistory(data) {
  state.auditState = { entries: [], can_undo: false, can_redo: false, ...data };
  const list = $('history-list');
  list.replaceChildren();
  const entries = state.auditState.entries || [];
  if (!entries.length)
    list.append(make('p', 'muted', '还没有已提交的修改。预览、取消和失败不会记为执行成功。'));
  for (const entry of [...entries].reverse()) {
    const item = make('details', 'history-entry'),
      label = { edit: '已执行', undo: '已撤销', redo: '已重做' }[entry.action] || '记录';
    item.append(
      make('summary', '', `${label} · 版本 ${entry.revision} · ${entry.changes.length} 个片段`),
    );
    item.append(
      make(
        'p',
        'muted',
        new Date(entry.time).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' }),
      ),
    );
    for (const change of entry.changes) item.append(make('p', '', changeDescription(change)));
    list.append(item);
  }
  $('undo').title = state.auditState.can_undo
    ? '撤销：' + (state.auditState.undo_summary || '上一条完整指令')
    : '没有可撤销的修改';
  $('redo').title = state.auditState.can_redo
    ? '重做：' + (state.auditState.redo_summary || '上一条完整指令')
    : '没有可重做的修改';
  updateControls();
}

export async function refreshHistory() {
  try {
    renderHistory(await api('history'));
  } catch {
    state.auditState = { entries: [], can_undo: false, can_redo: false };
    updateControls();
    $('history-list').textContent = '记录暂时无法读取，请刷新后重试。当前工程不会因此回滚。';
  }
}
