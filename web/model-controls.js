import { $, state } from './state.js';
import { action } from './ui.js';
import { api } from './api.js';

export function renderModelStatus(status) {
  const labels = {
    unconfigured: '模型 · 未配置',
    pending: '模型 · 待适配',
    connected: '模型 · 已连通',
    unavailable: '模型 · 暂不可用',
  };
  const option = $('mode').querySelector('[value="model"]');
  option.textContent = labels[status.state] || '模型 · 待适配';
  option.disabled = !state.modelAvailable;
  $('model-status').textContent = status.message || '在线模型可选，离线功能不依赖密钥。';
  $('check-model').disabled = !state.modelAvailable || state.busy;
}

export function installModelControls() {
  $('check-model').addEventListener('click', () =>
    action(async () => {
      $('model-status').textContent = '正在检查普通回复与流式协议，不会修改工程…';
      renderModelStatus(await api('model/check', {}));
    }),
  );
}
