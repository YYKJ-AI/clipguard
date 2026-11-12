export async function api(path, body) {
  const res = await fetch(`/api/${path}`, {
    method: body === undefined ? 'GET' : 'POST',
    credentials: 'same-origin',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  let data;
  try {
    data = await res.json();
  } catch {
    throw new Error('服务返回异常，请检查程序是否运行。');
  }
  if (!res.ok) throw new Error(data.error || `请求失败 (${res.status})`);
  return data;
}

export async function readReplyStream(response, onEvent) {
  if (!response.ok || !response.headers.get('Content-Type')?.includes('text/event-stream')) {
    let data;
    try {
      data = await response.json();
    } catch {
      throw new Error('服务返回异常，请检查程序是否运行。');
    }
    throw new Error(data.error || '当前服务无法提供流式回复，请重试。');
  }
  const reader = response.body.getReader(),
    decoder = new TextDecoder('utf-8', { fatal: true });
  let buffer = '',
    total = 0,
    lastID = 0,
    lastFrame = '',
    result = null;
  const frame = (raw) => {
    let event = '',
      id = '',
      lines = [];
    for (const line of raw.split(/\r?\n/)) {
      if (line.startsWith('event:')) event = line.slice(6).trim();
      else if (line.startsWith('id:')) id = line.slice(3).trim();
      else if (line.startsWith('data:')) lines.push(line.slice(5).replace(/^ /, ''));
    }
    if (!lines.length) return;
    const n = Number(id),
      signature = event + '\n' + lines.join('\n');
    if (n === lastID && signature === lastFrame) return;
    if (!Number.isSafeInteger(n) || n !== lastID + 1 || result)
      throw new Error('回复传输顺序异常，本次未完成，请重试。');
    lastID = n;
    lastFrame = signature;
    const data = JSON.parse(lines.join('\n'));
    if (event === 'error') throw new Error(data.message || '回复未完成，请重试。');
    if (event === 'result') {
      if (!['answer', 'clarify', 'unsupported', 'plan'].includes(data.status))
        throw new Error('回复未完成，请重试。');
      result = data;
    } else if (['progress', 'draft', 'reset'].includes(event)) onEvent(event, data);
    else throw new Error('回复传输格式异常，本次未完成，请重试。');
  };
  try {
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      total += value.byteLength;
      if (total > 4 * 1024 * 1024) throw new Error('回复过长，本次未完成。请缩短问题后重试。');
      buffer += decoder.decode(value, { stream: true });
      let match;
      while ((match = /\r?\n\r?\n/.exec(buffer))) {
        frame(buffer.slice(0, match.index));
        buffer = buffer.slice(match.index + match[0].length);
      }
    }
    buffer += decoder.decode();
    if (buffer.trim() || !result)
      throw new Error('回复传输中断，本次未完成；工程没有因此改变，请重试。');
    return result;
  } finally {
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}
