const fs = require('fs');
const ts = require('../node_modules/typescript');
const { chromium } = require('C:/Users/onele/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright');

async function check() {
  const browser = await chromium.launch({ headless: true, channel: 'msedge' });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 720 } });
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    let mock = fs.readFileSync('preview-sidebar/src/main.ts', 'utf8')
      .replace(/^import .*$/gm, '').replace("createApp(App).mount('#app')", '');
    mock += `
      window.__inventory = inventory;
      window.__saved = [];
      const saveOriginal = window.go.main.App.SaveInventorySettings;
      window.go.main.App.SaveInventorySettings = async v => {
        if (window.__saveFail) throw Error('演示保存失败');
        window.__saved.push({...v});
        return saveOriginal(v);
      };
    `;
    await page.addInitScript({ content: ts.transpileModule(mock, {
      compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None }
    }).outputText });
    await page.goto('http://127.0.0.1:5186/');
    await page.getByRole('article').first().waitFor();
    await page.getByRole('button', { name: '展开侧边栏', exact: true }).click();
    await page.locator('.inventory-match').filter({ hasText: '主队' }).waitFor();
    await page.screenshot({ path: 'preview-sidebar/integrated-toolbar.png' });

    await page.locator('.inventory-match').click();
    await page.getByRole('dialog', { name: '比赛详情', exact: true }).waitFor();
    await page.getByText('北京时间（UTC+8）', { exact: true }).waitFor();
    await page.screenshot({ path: 'preview-sidebar/inventory-details-dialog.png' });
    await page.keyboard.press('Escape');
    if (await page.getByRole('dialog').isVisible()) throw Error('Escape failed');

    const openSettings = () => page.getByRole('button', { name: '设置', exact: true }).click();
    const save = () => page.getByRole('button', { name: '保存设置', exact: true }).click();
    const cancel = () => page.getByRole('button', { name: '取消', exact: true }).click();
    const minInput = page.getByLabel('查询最短等待（毫秒）', { exact: true });
    const maxInput = page.getByLabel('查询最长等待（毫秒）', { exact: true });
    const settings = page.getByRole('dialog', { name: '客户端设置', exact: true });
    await openSettings();
    await settings.waitFor();
    await page.screenshot({ path: 'preview-sidebar/inventory-settings-dialog.png' });
    await minInput.fill('500');
    await maxInput.fill('400');
    await save();
    if ((await page.evaluate(() => window.__saved)).length) throw Error('Invalid settings submitted');
    await maxInput.fill('900');
    await page.getByLabel('首次下单最早偏移（秒）', { exact: true }).fill('5.25');
    await save();
    await settings.waitFor({ state: 'hidden' });
    const saved = await page.evaluate(() => window.__saved[0]);
    if (saved.queryMinMS !== 500 || saved.queryMaxMS !== 900 || saved.orderMinMS !== 5250) {
      throw Error('Saved units or values incorrect');
    }
    await openSettings();
    await minInput.fill('600');
    await cancel();
    await openSettings();
    if (await minInput.inputValue() !== '500') throw Error('Cancelled draft persisted');
    await page.evaluate(() => { window.__saveFail = true; });
    await save();
    await settings.getByRole('alert').filter({ hasText: '演示保存失败' }).waitFor();
    if (!await settings.isVisible()) throw Error('Failed save closed dialog');
    await cancel();
    await page.getByRole('button', { name: '校准', exact: true }).click();
    await page.locator('.inventory-clock').getByText('已校准 · 0ms', { exact: true }).waitFor();
    await page.getByRole('searchbox').fill('橙子');
    if (await page.getByRole('article').count() !== 1) throw Error('Search failed');
    await page.getByRole('button', { name: '清空搜索', exact: true }).click();
    await page.getByRole('button', { name: '收起侧边栏', exact: true }).click();
    if (await page.locator('.client-logout').isVisible()) throw Error('Collapsed logout visible');
    await page.locator('.sidebar-service-mini').waitFor();
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.screenshot({ path: 'preview-sidebar/integrated-collapsed.png' });
    await page.getByRole('button', { name: '展开侧边栏', exact: true }).click();
    for (const width of [1280, 1440]) {
      await page.setViewportSize({ width, height: 700 });
      if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) {
        throw Error('Overflow at ' + width);
      }
    }
    await page.evaluate(() => {
      window.__inventory.clockReady = false;
      window.__inventory.clockError = '校准服务暂不可用';
      window.__inventory.match.sale_at = new Date(Date.now() + 60000).toISOString();
    });
    await page.locator('.inventory-clock').getByText('未校准', { exact: true }).waitFor();
    await page.locator('.inventory-panel>.inventory-alert').filter({ hasText: '校准服务暂不可用' }).waitFor();
    await page.locator('.inventory-sale').filter({ hasText: '距开售' }).waitFor();
    if (await page.getByRole('button', { name: '启动', exact: true }).first().isEnabled()) {
      throw Error('Uncalibrated start enabled');
    }
    if (errors.length) throw Error(errors.join('\n'));
    console.log(JSON.stringify({ saved, errors, checks: 'layout, dialogs, Escape, validation, save, cancel, failure, sync, search, collapsed sidebar, countdown, clock failure passed' }));
  } finally { await browser.close(); }
}
check().catch(e => { console.error(e); process.exit(1); });
