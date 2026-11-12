import { $, make, state } from './state.js';

export function toast(text) {
  $('toast').textContent = text;
  $('toast').hidden = false;
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => ($('toast').hidden = true), 5000);
}

export const statusLabels = {
  answer: '咨询回复',
  clarify: '需要补充信息',
  plan: '待确认',
  executed: '已执行',
  error: '本次未完成',
  unsupported: '暂时不支持',
  generating: '生成中 · 尚未完成',
};

export function message(text, role = 'assistant', status = role === 'error' ? 'error' : 'answer') {
  const e = make('article', `message ${role}`);
  e.dataset.status = status;
  e.append(
    make(
      'span',
      'message-label',
      role === 'user' ? '你' : `剪辑助手 · ${statusLabels[status] || '说明'}`,
    ),
    make('p', '', text),
  );
  $('messages').append(e);
  $('messages').scrollTop = $('messages').scrollHeight;
  return e;
}

export function friendlyError(text) {
  if (/failed to fetch|network|encoded data|load failed|connection|terminated/i.test(String(text)))
    return '连接或回复传输中断，本次未完成；工程没有因此改变。请确认程序仍在运行后重试。';
  if (
    /json:|unmarshal|ParseResult|JSON 格式|SyntaxError|Unexpected token|unknown field/i.test(
      String(text),
    )
  )
    return '本次未能读懂回复或工程文件，工程没有因此改变。请重试；导入文件时请检查文件格式。';
  return String(text || '本次未完成，请稍后重试。');
}

export function updateControls() {
  for (const id of ['undo', 'redo', 'export', 'insert-preview', 'media-file', 'demo-cover'])
    $(id).disabled = state.busy || !state.project;
  $('undo').disabled = state.busy || !state.project || !state.auditState.can_undo;
  $('redo').disabled = state.busy || !state.project || !state.auditState.can_redo;
  $('check-model').disabled = state.busy || !state.modelAvailable;
  $('send').disabled =
    state.busy || (!state.project && !($('mode').value === 'model' && state.modelAvailable));
  $('sample').disabled = state.busy;
  $('import-file').disabled = state.busy;
  $('mode').disabled = state.busy;
  $('send').textContent = state.busy ? '处理中…' : '发送 ↑';
  $('save-note').classList.toggle('dirty', state.dirty);
  $('save-note').textContent = state.dirty
    ? '当前工程有尚未导出的状态。点击“导出工程”下载；会话空闲超时、容量回收或程序重启后无法恢复未导出的修改及撤销历史。'
    : '点击导出才能保留工程文件；会话空闲超时、容量回收或程序重启后，未导出的工程和撤销历史将丢失。';
}

export async function action(fn) {
  if (state.busy) return;
  state.busy = true;
  updateControls();
  try {
    await fn();
  } catch (err) {
    const text = friendlyError(err.message);
    message(text, 'error');
    toast(text);
  } finally {
    state.busy = false;
    updateControls();
  }
}
