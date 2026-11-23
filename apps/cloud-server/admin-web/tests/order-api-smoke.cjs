// Run after pnpm build. Requires Playwright, optionally via PLAYWRIGHT_MODULE.
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const http = require('node:http')
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright')
const root = path.resolve(__dirname, '../dist')

async function main() {
  const server = http.createServer((req, res) => {
    const name = req.url.split('?')[0]
    if (name === '/runtime-config.js') { res.setHeader('Content-Type', 'text/javascript'); res.end(''); return }
    const file = name.startsWith('/assets/') ? path.join(root, path.basename('assets'), path.basename(name)) : path.join(root, 'index.html')
    res.setHeader('Content-Type', file.endsWith('.js') ? 'text/javascript' : file.endsWith('.css') ? 'text/css' : 'text/html')
    fs.createReadStream(file).on('error', () => { res.statusCode = 404; res.end() }).pipe(res)
  })
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
  let browser
  try {
    browser = await chromium.launch({ headless: true, ...(process.platform === 'win32' ? { channel: 'msedge' } : {}) })
    const page = await browser.newPage()
    const errors = [], calls = []
    page.on('pageerror', e => errors.push(e.message))
    await page.addInitScript(() => sessionStorage.setItem('cloud_admin_token', 'test-token'))
    await page.route('**/api/admin/v1/**', async route => {
      const url = new URL(route.request().url())
      calls.push(url.pathname + url.search)
      const agent = { id: 'a1', name: '测试代理商', username: 'test', status: 'enabled', max_devices: 3, active_devices: 1, devices: [{ id: 'd1', client_number: 'A01', client_instance_id: 'i1', expires_at: '2099-01-01T00:00:00Z', task_summary: { total: 1, online: 1 } }] }
      let data
      if (url.pathname.endsWith('/auth/me')) data = { username: 'admin' }
      else if (url.pathname.endsWith('/current-match')) data = { id: '123', name: '测试比赛', enabled: true }
      else if (url.pathname.endsWith('/business-config')) data = { ready: true, device_type: 'windows', config_revision: 1 }
      else if (url.pathname.endsWith('/terminals/i1/tasks')) data = { synced: true, tasks: [{ task_id: 101, authorization_status: 'online' }] }
      else if (url.pathname.endsWith('/orders/1/reveal-viewers')) data = { order_id: 'order-1', viewers: [{ name: '测试姓名', phone: '13800000000' }] }
      else if (url.pathname.endsWith('/orders/viewer-roster')) {
        assert.equal(route.request().method(), 'POST')
        assert.deepEqual(route.request().postDataJSON(), { confirm: true })
        data = { names: Array.from({ length: 240 }, (_, i) => i < 2 ? '同名观演人' : `观演人${i + 1}`), order_count: 205 }
      }
      else if (url.pathname.endsWith('/orders')) data = { items: [{ id: 1, order_id: 'SIM-order-1', simulated: true, agent_name: '测试代理商', task_id: 101, client_number: 'A01', match_id: 'match-123', match_name: '测试场次名称', created_at: '2026-11-23T06:30:00Z', payment_deadline: '2026-11-23T07:00:00Z', ticket_count: 2, tiers: ['A区'], seats: [{ tier: 'A区', row: 3, seat: 5 }, { tier: 'A区', row: 3, seat: 6 }], viewers: [{ name: '测试姓名', phone: '138****0000' }], detail_status: 'unavailable' }], total: 205, page: Number(url.searchParams.get('page')), limit: Number(url.searchParams.get('limit')) }
      else if (url.pathname.endsWith('/agents/a1')) data = agent
      else if (url.pathname.endsWith('/agents')) data = [agent]
      else throw new Error('Unexpected API: ' + url.pathname)
      await route.fulfill({ json: { code: 'ok', message: '', data } })
    })
    await page.goto(`http://127.0.0.1:${server.address().port}/admin/orders`)
    await page.getByText('SIM-order-1', { exact: true }).waitFor()
    await page.getByText('模拟订单', { exact: true }).waitFor()
    await page.getByLabel('终端编号').fill('A01')
    await page.getByLabel('代理商').selectOption('a1')
    await page.getByLabel('场次（场次 ID）', { exact: true }).fill('match-123')
    await page.getByRole('button', { name: '查询', exact: true }).click()
    await page.waitForURL('**/admin/orders?**match_id=match-123**')
    assert.equal(await page.locator('.order-filters label').count(), 3)
    assert.equal(await page.getByRole('link', { name: '订单查询', exact: true }).count(), 1)
    await page.getByLabel('每页').selectOption('200')
    await page.waitForURL('**limit=200')
    await page.getByRole('button', { name: '下一页', exact: true }).click()
    await page.waitForURL('**page=2**')
    const selectedQuery = new URL(page.url()).search
    // Unapplied edits must not silently alter the roster's scope.
    await page.getByLabel('终端编号').fill('UNAPPLIED')
    if (process.env.SCREENSHOT_DIR) await page.screenshot({ path: path.join(process.env.SCREENSHOT_DIR, 'orders-desktop.png'), fullPage: true })
    await page.getByRole('button', { name: '观演人名单', exact: true }).click()
    assert.equal(calls.some(url => url.includes('/orders/viewer-roster')), false)
    await page.getByRole('button', { name: '确认查看名单', exact: true }).click()
    await page.locator('.roster-names li').last().waitFor()
    assert.equal(await page.locator('.roster-names li').count(), 240)
    assert.equal(await page.getByText('同名观演人', { exact: true }).count(), 2)
    assert.equal(await page.locator('aside, header, button').count(), 0)
    assert.equal((await page.locator('.viewer-roster').innerText()).includes('13800000000'), false)
    const rosterCall = new URL(calls.find(url => url.includes('/orders/viewer-roster')), 'http://test')
    assert.equal(rosterCall.searchParams.get('client_number'), 'A01')
    assert.equal(rosterCall.searchParams.get('match_id'), 'match-123')
    assert.equal(rosterCall.searchParams.has('page'), false)
    assert.equal(rosterCall.searchParams.has('limit'), false)
    assert.equal(await page.locator('.roster-names li').first().evaluate(el => getComputedStyle(el).lineHeight), '24px')
    if (process.env.SCREENSHOT_DIR) await page.screenshot({ path: path.join(process.env.SCREENSHOT_DIR, 'roster-desktop.png'), fullPage: true })
    await page.setViewportSize({ width: 390, height: 844 })
    assert.equal(await page.locator('.roster-names').evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length), 1)
    if (process.env.SCREENSHOT_DIR) await page.screenshot({ path: path.join(process.env.SCREENSHOT_DIR, 'roster-mobile.png'), fullPage: true })
    await page.keyboard.press('Escape')
    await page.getByText('SIM-order-1', { exact: true }).waitFor()
    assert.equal(new URL(page.url()).search, selectedQuery)
    assert.equal(await page.getByLabel('终端编号').inputValue(), 'A01')
    assert.equal(await page.getByLabel('每页').inputValue(), '200')
    assert.equal(await page.locator('.orders-table td').first().evaluate(el => getComputedStyle(el).display), 'block')
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth))
    assert((await page.locator('.orders-table tr').last().boundingBox()).height < 500)
    if (process.env.SCREENSHOT_DIR) await page.screenshot({ path: path.join(process.env.SCREENSHOT_DIR, 'orders-mobile.png'), fullPage: true })
    await page.setViewportSize({ width: 1440, height: 1000 })
    assert(calls.some(path => path.includes('/api/admin/v1/orders')))
    assert.equal(await page.locator('[data-label="票务信息"]').innerText(), 'A区 3排5座\nA区 3排6座')
    const seats = page.locator('[data-label="票务信息"]').getByText(/A区 3排[56]座/)
    const firstSeat = await seats.nth(0).boundingBox()
    const secondSeat = await seats.nth(1).boundingBox()
    assert(secondSeat.y >= firstSeat.y + firstSeat.height, 'Seats should occupy separate lines on desktop')
    assert.equal(await page.locator('[data-label="观演人"]').innerText(), '测试姓名')
    assert.equal(await page.getByRole('columnheader', { name: '支付截止时间', exact: true }).count(), 1)
    assert((await page.locator('.orders-table tbody tr').first().boundingBox()).height < 76)
    await page.getByRole('button', { name: '138****0000', exact: true }).click()
    await page.getByRole('button', { name: '13800000000', exact: true }).click()
    await page.getByRole('button', { name: '138****0000', exact: true }).waitFor()
    await page.goto(`http://127.0.0.1:${server.address().port}/admin/agents`)
    await page.getByRole('button', { name: '详情', exact: true }).click()
    await page.getByRole('tab', { name: /业务登录设备/ }).click()
    await page.getByRole('link', { name: '查看任务 →' }).click()
    await page.getByRole('button', { name: /任务 101，在线/ }).waitFor()
    assert(calls.some(path => path.includes('/api/admin/v1/orders')))
    assert(calls.includes('/api/admin/v1/orders/1/reveal-viewers'))
    assert(calls.some(url => url.startsWith('/api/admin/v1/agents/a1/terminals/i1/tasks')))
    assert.deepEqual(errors, [])
    assert.equal(await page.getByRole('alert').count(), 0)
    console.log('Production bundle: order filters, pagination, all-order roster, duplicates, privacy, return state, mobile layout, viewer reveal, and terminal APIs passed')
  } finally {
    if (browser) await browser.close()
    await new Promise(resolve => server.close(resolve))
  }
}
main().catch(e => { console.error(e); process.exitCode = 1 })
