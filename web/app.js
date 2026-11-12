import { $, state, make } from './state.js';
import { api } from './api.js';
import { invalidateCards } from './preview.js';
import { clearMediaBindings } from './media.js';
import { render } from './timeline.js';
import { refreshHistory, showActualResult } from './history.js';
import { message, action, updateControls, toast } from './ui.js';
import { send, showResponse } from './conversation.js';
import { renderInsertTracks } from './insert.js';
import { installMediaControls } from './media-controls.js';
import { installModelControls, renderModelStatus } from './model-controls.js';

const workspaceLinks = document.querySelectorAll('.side-nav .nav-item');

for (const link of workspaceLinks)
  link.addEventListener('click', () => {
    for (const item of workspaceLinks) {
      item.classList.toggle('active', item === link);
      item.removeAttribute('aria-current');
    }
    link.setAttribute('aria-current', 'location');
    if (link.getAttribute('href') === '#usage') $('usage').open = true;
  });

export async function importProject(data) {
  if (
    state.project &&
    !window.confirm(
      '导入会替换当前工程，并清空待确认计划和撤销历史。未导出的状态将丢失，是否继续？',
    )
  )
    return;
  const result = await api('import', data);
  invalidateCards();
  clearMediaBindings();
  $('messages').replaceChildren();
  state.dirty = false;
  state.selectedClipID = null;
  state.selectedProjectID = null;
  render(result.project);
  await refreshHistory();
  message(result.message + '。现在可以输入修改要求。');
}

$('sample').addEventListener('click', () =>
  action(async () => {
    await importProject(await api('sample'));
  }),
);

$('import-file').addEventListener('change', () => {
  const file = $('import-file').files[0];
  if (!file) return;
  action(async () => {
    try {
      if (file.size > 1024 * 1024) throw new Error('工程文件不能超过 1 MiB');
      await importProject(JSON.parse(await file.text()));
    } finally {
      $('import-file').value = '';
    }
  });
});

$('message-form').addEventListener('submit', (event) => {
  event.preventDefault();
  const text = $('instruction').value.trim();
  if (!text) return;
  action(async () => {
    $('instruction').value = '';
    await send({ text }, text);
  });
});

$('instruction').addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault();
    $('message-form').requestSubmit();
  }
});

$('stop-generation').addEventListener('click', () => state.generationController?.abort());

const roleHelp = $('role-help'),
  roleToggle = $('role-toggle');

roleToggle.addEventListener('click', () => {
  const open = roleHelp.dataset.open !== 'true';
  roleHelp.dataset.open = String(open);
  roleHelp.dataset.dismissed = String(!open);
  roleToggle.setAttribute('aria-expanded', String(open));
});

roleHelp.addEventListener('mouseenter', () => {
  roleHelp.dataset.dismissed = 'false';
});

roleToggle.addEventListener('focus', () => {
  roleHelp.dataset.dismissed = 'false';
});

roleHelp.addEventListener('keydown', (event) => {
  if (event.key === 'Escape') {
    roleHelp.dataset.open = 'false';
    roleHelp.dataset.dismissed = 'true';
    roleToggle.setAttribute('aria-expanded', 'false');
    roleToggle.blur();
  }
});

$('mode').addEventListener('change', () => {
  $('mode-help').textContent =
    $('mode').value === 'model'
      ? '剪辑咨询可直接提问，编辑前会先澄清或预览'
      : '离线模式支持明确句式';
  updateControls();
});

for (const route of ['undo', 'redo'])
  $(route).addEventListener('click', () =>
    action(async () => {
      const request_id = crypto.randomUUID();
      const r = await api(route, { request_id });
      invalidateCards();
      state.dirty = true;
      render(r.project);
      await refreshHistory();
      showActualResult(r);
      toast(route === 'undo' ? '已撤销' : '已重做');
    }),
  );

$('export').addEventListener('click', () =>
  action(async () => {
    const data = await api('export');
    const blob = new Blob([JSON.stringify(data, null, 2) + '\n'], { type: 'application/json' }),
      url = URL.createObjectURL(blob),
      a = make('a');
    a.href = url;
    a.download = `${data.project_id.replace(/[^\w\-\u4e00-\u9fff]/g, '_')}-r${data.revision}.json`;
    document.body.append(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 10000);
    state.dirty = false;
    toast('已发起工程下载，请在浏览器中确认文件');
  }),
);

window.addEventListener('beforeunload', (event) => {
  if (state.dirty) {
    event.preventDefault();
    event.returnValue = '';
  }
});

$('insert-source').addEventListener('change', renderInsertTracks);

$('insert-form').addEventListener('submit', (event) => {
  event.preventDefault();
  action(async () => {
    const source = state.project?.clips.find((c) => c.id === $('insert-source').value),
      track = $('insert-track').value,
      secondsValue = $('insert-time').value;
    if (!source || !track || !/^\d+(?:\.\d{1,3})?$/.test(secondsValue))
      throw new Error('请选择来源和目标轨道，起点最多三位小数。');
    const start = Number(secondsValue) * 1000;
    if (!Number.isSafeInteger(Math.round(start)) || start < 0)
      throw new Error('时间超出可用范围。');
    const data = await api('insert-preview', {
      source_id: source.id,
      track_id: track,
      start_ms: Math.round(start),
    });
    showResponse(data);
  });
});

installMediaControls();
installModelControls();

(async () => {
  try {
    const session = await api('session', {});
    state.modelAvailable = session.model_available;
    state.maxTextChars = session.limits?.max_text_chars || 2000;
    $('instruction').removeAttribute?.('maxlength');
    const option = $('mode').querySelector('[value="model"]');
    option.disabled = !state.modelAvailable;
    option.textContent = state.modelAvailable ? '模型 · 自由表达' : '模型 · 待配置';
    $('mode').value = 'offline';
    $('mode-help').textContent = '离线模式支持明确句式';
    $('connection').textContent = state.modelAvailable
      ? '已连接 · 可切换模型进行剪辑咨询'
      : '已连接 · 离线可用';
    const snapshot = await api('state');
    state.dirty = !!snapshot.project;
    render(snapshot.project);
    await refreshHistory();
    renderModelStatus(await api('model/status'));
  } catch (err) {
    $('connection').textContent = '连接失败';
    message(err.message, 'error');
  }
})();
