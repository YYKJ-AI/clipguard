async (page) => {
  const base = new URL(page.url()).origin,
    checks = [],
    errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('dialog', (d) => d.accept());
  const assert = (condition, label) => {
    if (!condition) throw new Error(label);
    checks.push(label);
  };
  const state = async () => await (await page.request.get(base + '/api/export')).json();
  const history = async () => await (await page.request.get(base + '/api/history')).json();
  const idle = () => page.waitForFunction(() => !document.getElementById('send').disabled);
  const send = async (text) => {
    await page.locator('#instruction').fill(text);
    await page.getByRole('button', { name: '发送 ↑', exact: true }).click();
    await idle();
  };
  const confirm = async () => {
    await page.getByRole('button', { name: '确认修改', exact: true }).last().click();
    await idle();
  };
  const click = async (id) => {
    await page.locator('#' + id).click();
    await idle();
  };
  const sample = await (await page.request.get(base + '/api/sample')).json();
  const load = async (p = sample) => {
    const r = await page.request.post(base + '/api/import', { data: p });
    if (!r.ok()) throw new Error(await r.text());
    await page.evaluate(async () => {
      (await import('/state.js')).state.dirty = false;
    });
    await page.reload();
    await idle();
    await page.evaluate(() => {
      window.confirm = () => true;
    });
  };
  const normalized = (p) => JSON.stringify({ ...p, revision: 1 });
  const clip = (p, id) => p.clips.find((c) => c.id === id);
  await load();
  assert((await page.locator('#mode').inputValue()) === 'offline', '无密钥启动默认离线');
  assert(await page.locator('#check-model').isDisabled(), '未配置在线模型不影响离线验收');
  await send('把 clip-a 缩短 2 秒');
  assert(clip(await state(), 'clip-a').duration_ms === 5000, '预览前后工程未变');
  assert(await page.locator('#difference-panel').isVisible(), '前后时间轴差异可见');
  assert(
    (await page.locator('.difference-bar.before').count()) === 1 &&
      (await page.locator('.difference-bar.after').count()) === 1,
    '前后位置分别绘制',
  );
  await confirm();
  assert(clip(await state(), 'clip-a').duration_ms === 3000, '确认后裁剪准确');
  assert(
    (await page.locator('.actual-change').last().textContent()).includes('3 秒'),
    '执行报告列出实际时长',
  );
  await click('undo');
  assert(normalized(await state()) === normalized(sample), '撤销恢复全部业务字段，版本继续推进');
  await click('redo');
  assert(clip(await state(), 'clip-a').duration_ms === 3000, '重做恢复完整修改');
  const savedHistory = await history();
  await page.evaluate(async () => {
    (await import('/state.js')).state.dirty = false;
  });
  await page.reload();
  await idle();
  await page.evaluate(() => {
    window.confirm = () => true;
  });
  assert(
    (await page.locator('#history-list details').count()) === savedHistory.entries.length,
    '刷新读回已提交记录',
  );
  await load();
  await send('把第二段移到开头');
  assert((await page.locator('.candidate').count()) === 2, '多轨序数先澄清');
  await page.locator('.candidate').first().click();
  await idle();
  assert(normalized(await state()) === normalized(sample), '澄清后碰撞拒绝且不变');
  await send('把 caption-lock 的字幕改为“不能修改”');
  assert(normalized(await state()) === normalized(sample), '锁轨保护');
  await send('把 clip-a 缩短 99 秒');
  assert(normalized(await state()) === normalized(sample), '非法缩短不生效');
  await send('删除 clip-d');
  await page.getByRole('button', { name: '取消', exact: true }).last().click();
  await idle();
  assert(normalized(await state()) === normalized(sample), '取消删除工程不变');
  assert(await page.locator('#difference-panel').isHidden(), '取消时清除差异视图');
  assert((await history()).entries.length === 0, '预览取消及失败不记成功记录');
  await send('把 clip-a 缩短 1 秒；删除 caption-lock');
  assert(normalized(await state()) === normalized(sample), '第二项失败整体拒绝');
  await send('把 clip-a 缩短 1 秒');
  await page.request.post(base + '/api/import', { data: sample });
  await confirm();
  assert(normalized(await state()) === normalized(sample), '相同ID和版本重载后旧计划拒绝');
  await load();
  await send('把 clip-a 缩短 1 秒');
  await confirm();
  await send('把 caption-b 的字幕改为“新片尾”');
  await confirm();
  await click('undo');
  await click('undo');
  assert(normalized(await state()) === normalized(sample), '连续两次撤销正确恢复各自范围');
  await click('redo');
  await send('删除 clip-d');
  await confirm();
  assert(await page.locator('#redo').isDisabled(), '新提交清除旧重做分支');
  await load();
  await page.locator('#insert-source').selectOption('clip-a');
  await page.locator('#insert-track').selectOption('video-2');
  await page.locator('#insert-time').fill('12');
  await click('insert-preview');
  assert((await state()).clips.length === 7, '插入预览不提交');
  await confirm();
  const inserted = (await state()).clips.find((c) => !sample.clips.some((s) => s.id === c.id));
  assert(
    inserted && inserted.start_ms === 12000 && inserted.track_id === 'video-2',
    '插入产生新稳定ID和准确位置',
  );
  await click('undo');
  assert((await state()).clips.length === 7, '撤销移除整个插入');
  await click('redo');
  assert(!!clip(await state(), inserted.id), '重做保留插入时的稳定ID');
  await page.locator('[data-clip-id="clip-a"]').click();
  await page.locator('#demo-cover').click();
  await page.waitForFunction(() => document.getElementById('media-image').naturalWidth > 0);
  assert(await page.locator('#media-image').isVisible(), '内嵌演示封面可预览');
  await page.locator('#media-cursor').evaluate((el) => {
    el.value = '1000';
    el.dispatchEvent(new Event('input'));
  });
  assert(
    (await page.locator('#media-caption').textContent()).includes('欢迎观看'),
    '字幕按时间轴独立显示',
  );
  await page.locator('#media-cursor').evaluate((el) => {
    el.value = '5000';
    el.dispatchEvent(new Event('input'));
  });
  assert(await page.locator('#media-image').isHidden(), '片段右端采用左闭右开区间');
  // Real file download is used as the next file import, not an in-memory shortcut.
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.locator('#export').click(),
  ]);
  const downloaded = await download.path();
  assert(!!downloaded, '工程导出产生实际下载');
  await page.locator('#import-file').setInputFiles(downloaded);
  await idle();
  assert(!!clip(await state(), inserted.id), '导出文件可重新导入');
  const external = JSON.parse(JSON.stringify(sample));
  external.project_id = 'third-party-project';
  for (const t of external.tracks) t.id = 'custom-' + t.id;
  for (const c of external.clips) {
    c.id = 'custom-' + c.id;
    c.track_id = 'custom-' + c.track_id;
  }
  external.clips.reverse();
  await page.locator('#import-file').setInputFiles({
    name: 'external.json',
    mimeType: 'application/json',
    buffer: Buffer.from(JSON.stringify(external)),
  });
  await idle();
  await send('把 custom-clip-a 缩短 1 秒');
  await confirm();
  assert(clip(await state(), 'custom-clip-a').duration_ms === 4000, '外部ID和乱序工程可编辑');
  await click('undo');
  assert(normalized(await state()) === normalized(external), '外部工程撤销不丢字段');
  for (const width of [360, 600]) {
    await page.setViewportSize({ width, height: 1000 });
    assert(
      await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      width + 'px 页面无整体横向溢出',
    );
    await page.screenshot({ path: `output/playwright/extensions-${width}.png`, fullPage: true });
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.screenshot({ path: 'output/playwright/extensions-desktop.png', fullPage: true });
  assert(errors.length === 0, '没有页面脚本异常');
  return { passed: checks.length, checks, errors };
};
