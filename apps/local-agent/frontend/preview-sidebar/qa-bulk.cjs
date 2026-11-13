const fs = require('fs');
const ts = require('../node_modules/typescript');
const { chromium } = require('C:/Users/onele/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright');

async function check() {
  const browser = await chromium.launch({ headless: true, channel: 'msedge' });
  try {
    const page = await browser.newPage({ viewport: { width: 1100, height: 700 } });
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    let mock = fs.readFileSync('preview-sidebar/src/main.ts', 'utf8')
      .replace(/^import .*$/gm, '').replace("createApp(App).mount('#app')", '');
    mock += `
      window.__inventory = inventory;
      const originalSync = window.go.main.App.SyncInventoryClock;
      window.go.main.App.SyncInventoryClock = async () => { await originalSync(); inventory.clockReady = true; };
      window.__calls = {start: [], stop: [], refresh: []};
      for (const [action, method] of [['start', 'StartInventory'], ['stop', 'StopInventory'], ['refresh', 'RefreshTaskPeople']]) {
        const original = window.go.main.App[method];
        window.go.main.App[method] = async (...args) => {
          window.__calls[action].push(args[0]);
          await new Promise(resolve => setTimeout(resolve, 70));
          if (action === 'start' && args[0] === 1) throw Error('演示启动失败');
          if (action === 'refresh' && args[0] === 1) throw Error('演示刷新失败');
          return original(...args);
        };
      }
    `;
    await page.addInitScript({ content: ts.transpileModule(mock, {
      compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None }
    }).outputText });
    await page.goto('http://127.0.0.1:5186/');
    await page.getByRole('article').first().waitFor();
    await page.locator('.inventory-id').getByText('DEMO-01', { exact: true }).waitFor();
    if (await page.locator('.tw-search-count').count()) throw Error('Duplicate task count exists');
    if (await page.getByRole('button', { name: '详情', exact: true }).count()) throw Error('Separate details button exists');
    await page.locator('.inventory-match').focus();
    await page.keyboard.press('Enter');
    await page.getByRole('dialog', { name: '比赛详情', exact: true }).waitFor();
    await page.keyboard.press('Escape');
    const actions = page.getByRole('group', { name: '任务批量操作', exact: true });
    if (await actions.getByRole('button').count() !== 5) throw Error('Expected five sibling actions');
    await actions.getByRole('button', { name: '设置', exact: true }).click();
    await page.getByRole('dialog', { name: '查询设置', exact: true }).waitFor();
    await page.keyboard.press('Escape');
    await page.locator('.inventory-title').click();
    await page.screenshot({ path: 'preview-sidebar/toolbar-bulk-1100.png' });

    await page.getByRole('searchbox').fill('橙子');
    await actions.getByRole('button', { name: '全部启动', exact: true }).click();
    if (!await actions.getByRole('button', { name: '全部启动', exact: true }).isDisabled()) throw Error('Batch double-submit enabled');
    await page.locator('.tw-bulk-report').filter({ hasText: '全部启动完成' }).waitFor();
    const started = await page.evaluate(() => window.__calls.start);
    if (JSON.stringify(started) !== '[1,2]') throw Error('Batch used filtered tasks or failed to continue: ' + started);
    await page.locator('.tw-bulk-errors').filter({ hasText: '任务 1：演示启动失败' }).waitFor();
    await actions.getByRole('button', { name: '全部停止', exact: true }).click();
    await page.locator('.tw-bulk-report').filter({ hasText: '全部停止完成' }).waitFor();
    const stopped = await page.evaluate(() => window.__calls.stop);
    if (JSON.stringify(stopped) !== '[2]') throw Error('Stop targeted inactive tasks: ' + stopped);
    await actions.getByRole('button', { name: '全部刷新', exact: true }).click();
    await page.locator('.tw-bulk-report').filter({ hasText: '全部刷新完成' }).waitFor();
    const refreshed = await page.evaluate(() => window.__calls.refresh);
    if (JSON.stringify(refreshed) !== '[1,2]') throw Error('Refresh targeted offline tasks or stopped early: ' + refreshed);
    await page.locator('.tw-bulk-errors').filter({ hasText: '任务 1：演示刷新失败' }).waitFor();
    await page.getByRole('button', { name: '清空搜索', exact: true }).click();

    await page.evaluate(() => {
      window.__inventory.clockReady = false;
      window.__inventory.runs[1] = { status: 'paused', message: '演示暂停', target: '' };
    });
    await page.locator('.inventory-clock').getByText('未校准', { exact: true }).waitFor();
    if (await actions.getByRole('button', { name: '全部启动', exact: true }).isEnabled()) throw Error('Uncalibrated batch start enabled');
    await actions.getByRole('button', { name: '全部停止', exact: true }).click();
    await page.locator('.tw-bulk-report').filter({ hasText: '全部停止完成' }).waitFor();
    if (JSON.stringify(await page.evaluate(() => window.__calls.stop)) !== '[2,1]') throw Error('Paused task not stopped');
    await page.getByRole('button', { name: '校准', exact: true }).click();
    await page.locator('.inventory-clock').getByText('已校准 · 0ms', { exact: true }).waitFor();
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.screenshot({ path: 'preview-sidebar/toolbar-bulk-1440.png' });
    await page.getByRole('button', { name: '收起侧边栏', exact: true }).click();
    await page.locator('.sidebar-service-mini').waitFor();
    if (await page.locator('.client-logout').isVisible()) throw Error('Collapsed logout visible');
    await page.getByRole('button', { name: '展开侧边栏', exact: true }).click();
    await page.setViewportSize({ width: 1100, height: 700 });
    await actions.evaluate(el => {
      for (let i = 1; i <= 5; i++) {
        const button = document.createElement('button');
        button.textContent = '后续操作 ' + i;
        el.append(button);
      }
    });
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw Error('Future buttons cause overflow');
    const rows = await actions.getByRole('button').evaluateAll(es => new Set(es.map(e => e.getBoundingClientRect().top)).size);
    if (rows < 2) throw Error('Future actions did not wrap');
    if (errors.length) throw Error(errors.join('\n'));
    console.log(JSON.stringify({ started, stopped, refreshed, futureActionRows: rows, errors, checks: 'passed' }));
  } finally { await browser.close(); }
}
check().catch(e => { console.error(e); process.exit(1); });
