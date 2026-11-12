async (page) => {
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(new URL('/', page.url()).href);
  await page.waitForFunction(() =>
    document.getElementById('connection').textContent.startsWith('已连接'),
  );
  await page.waitForFunction(() => !document.getElementById('send').disabled);
  await page.locator('[data-clip-id="clip-c"]').click();
  await page.locator('#demo-cover').click();
  await page.locator('#mode').selectOption('model');
  await page.locator('#instruction').fill('我县要把轨道二第一段适配删掉');
  await page.locator('#send').click();
  await page.waitForFunction(
    () => !document.getElementById('send').disabled,
    {},
    { timeout: 75000 },
  );
  const answer = page.locator('.message[data-status="clarify"]').last();
  const text = await answer.textContent();
  if (/clip-c|video-2|4000|毫秒|\bID\b/.test(text)) throw Error('界面仍显示内部参数：' + text);
  if (!/[？?]/.test(text)) throw Error('没有反问');
  if (await page.locator('.preview-card').count()) throw Error('含糊指令不应直接生成计划');
  const before = await (await page.request.get(new URL('/api/export', page.url()).href)).json();
  if (before.clips.length !== 7) throw Error('澄清时不应删片段');
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({
    path: 'output/playwright/workspace-customer-language-desktop.png',
    scale: 'css',
  });
  await page.locator('#instruction').fill('对，我要删除第二条视频轨道的第一段视频。');
  await page.locator('#send').click();
  await page.waitForFunction(
    () => !document.getElementById('send').disabled,
    {},
    { timeout: 75000 },
  );
  await page.getByRole('button', { name: '取消', exact: true }).last().click();
  await page.waitForFunction(() => !document.getElementById('send').disabled);
  const after = await (await page.request.get(new URL('/api/export', page.url()).href)).json();
  if (JSON.stringify(before) !== JSON.stringify(after)) throw Error('取消后工程改变');
  if (errors.length) throw Error(errors.join('\n'));
  return {
    passed: 5,
    question: text,
    checks: [
      '真实模型口语化反问',
      '含糊指令无计划',
      '澄清不修改工程',
      '补充后生成计划且取消不修改',
      '页面无脚本异常',
    ],
  };
};
