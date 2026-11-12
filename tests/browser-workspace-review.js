async (page) => {
  page.on('dialog', (dialog) => dialog.accept().catch(() => {}));
  const checks = [];
  const verify = (ok, label) => {
    if (!ok) throw Error(label);
    checks.push(label);
  };
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.context().clearCookies();
  await page.goto(new URL('/', page.url()).href);
  await page.waitForFunction(() =>
    document.getElementById('connection').textContent.startsWith('已连接'),
  );
  await page.locator('#sample').click();
  await page.waitForFunction(() => !document.getElementById('send').disabled);
  await page.locator('.side-nav a[href="#usage"]').click();
  verify(await page.locator('#usage').evaluate((el) => el.open), '左侧操作说明直接展开帮助');
  verify(
    (await page.locator('.side-nav a[href="#usage"]').getAttribute('aria-current')) === 'location',
    '左侧按钮提示当前入口',
  );
  await page.locator('#usage summary').click();
  const media = await page.locator('#media-panel').boundingBox(),
    timeline = await page.locator('#timeline-panel').boundingBox();
  verify(media.y < timeline.y, '媒体预览位于时间轴上方');
  verify(
    await page.locator('#details-panel').evaluate((el) => el.tagName === 'DETAILS' && !el.open),
    '片段明细默认折叠',
  );
  verify((await page.locator('.topbar #hero-title').count()) === 1, '标语移到顶部栏');
  await page.locator('#details-panel summary').click();
  verify((await page.locator('#clip-rows tr').count()) === 7, '展开明细保留全部片段');
  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
  const chat = await page.locator('#assistant').boundingBox(),
    composer = await page.locator('#instruction').boundingBox();
  verify(chat.y >= 0 && chat.y + chat.height <= 901, '滚动到底部时助手完整留在窗口内');
  verify(composer.y >= 0 && composer.y + composer.height < 900, '滚动后输入框仍可见');
  await page.locator('#instruction').fill('把 clip-a 缩短 1 秒');
  await page.locator('#send').click();
  await page.waitForFunction(() => !document.getElementById('send').disabled);
  verify(
    (await page.getByRole('button', { name: '确认修改', exact: true }).count()) === 1,
    '跟随窗口的助手可发送并展示待确认计划',
  );
  await page.getByRole('button', { name: '取消', exact: true }).click();
  await page.waitForFunction(() => !document.getElementById('send').disabled);
  for (const width of [600, 360]) {
    await page.setViewportSize({ width, height: 800 });
    verify(
      await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      width + 'px 无页面横向溢出',
    );
    await page.locator('.assistant-jump').click();
    await page.waitForTimeout(350);
    verify(
      await page.locator('#assistant').evaluate((el) => {
        const r = el.getBoundingClientRect();
        return r.top < innerHeight && r.bottom > 0;
      }),
      width + 'px 可快捷到达助手',
    );
  }
  return { passed: checks.length, checks };
};
