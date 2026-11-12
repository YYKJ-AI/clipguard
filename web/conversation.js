import { make, state, $, textLength } from './state.js';
import { candidateLabel } from './labels.js';
import { action, message } from './ui.js';
import { preview } from './preview.js';
import { readReplyStream, api } from './api.js';

export function renderCandidates(container, data) {
  const candidates = data.candidates || [],
    size = 20,
    pages = Math.ceil(candidates.length / size);
  let page = 0;
  const list = make('div', 'candidate-list'),
    controls = make('div', 'candidate-pages');
  container.append(list, controls);
  function draw() {
    list.replaceChildren();
    controls.replaceChildren();
    for (const c of candidates.slice(page * size, (page + 1) * size)) {
      const label = candidateLabel(c),
        b = make('button', 'candidate', label);
      b.type = 'button';
      b.addEventListener('click', () =>
        action(async () => {
          b.disabled = true;
          try {
            await send({ choice: c.id, clarification_id: data.clarification_id }, `选择 ${label}`);
          } finally {
            b.disabled = false;
          }
        }),
      );
      list.append(b);
    }
    if (pages > 1) {
      const previous = make('button', 'button', '上一页'),
        next = make('button', 'button', '下一页');
      previous.type = next.type = 'button';
      previous.disabled = page === 0;
      next.disabled = page === pages - 1;
      previous.addEventListener('click', () => {
        page--;
        draw();
      });
      next.addEventListener('click', () => {
        page++;
        draw();
      });
      controls.append(
        previous,
        make('span', '', `第 ${page + 1} / ${pages} 页，共 ${candidates.length} 项`),
        next,
      );
    }
  }
  draw();
}

export function showResponse(data) {
  if (data.status === 'plan') {
    preview(data);
    return;
  }
  const e = message(data.message, 'assistant', data.status);
  if (data.status === 'clarify') renderCandidates(e, data);
}

export async function requestStream(payload) {
  const article = message('正在连接助手…', 'assistant', 'generating'),
    text = article.children[1];
  let draft = '';
  state.generationController = new AbortController();
  $('stop-generation').hidden = false;
  try {
    const response = await fetch('/api/message/stream', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
      signal: state.generationController.signal,
    });
    return await readReplyStream(response, (event, data) => {
      if (event === 'reset') {
        draft = '';
        text.textContent = data.message;
      } else if (event === 'draft') {
        draft += data.text;
        text.textContent = draft;
      } else if (!draft) text.textContent = data.message;
      $('messages').scrollTop = $('messages').scrollHeight;
    });
  } catch (err) {
    if (state.generationController.signal.aborted)
      throw new Error('已停止生成，本次没有应用修改。', { cause: err });
    throw err;
  } finally {
    article.remove();
    state.generationController = null;
    $('stop-generation').hidden = true;
  }
}

export async function send(payload, label) {
  if (textLength(payload.text || '') > state.maxTextChars)
    throw new Error(`指令最多 ${state.maxTextChars} 个字符，请缩短后重试`);
  message(label, 'user');
  const input = {
    ...payload,
    mode: $('mode').value,
    ...(state.selectedClipID ? { selected_clip_id: state.selectedClipID } : {}),
  };
  const data = input.mode === 'model' ? await requestStream(input) : await api('message', input);
  showResponse(data);
}
