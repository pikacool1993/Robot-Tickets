const fs = require("fs");
const ts = require("../node_modules/typescript");
const {chromium} = require("C:/Users/onele/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright");
(async () => {
 let mock = fs.readFileSync("preview-sidebar/src/main.ts", "utf8").replace(/^import .*$/gm, "").replace("createApp(App).mount('#app')", "");
 mock += `
 inventory.debugMode=false; inventory.simulation={enabled:false,orderFailurePercents:[6,6,6,6,6,5,5,5,5,5,5,5]};
 inventory.autoRefresh={enabled:false,intervalMinutes:30};
 inventory.refreshRuns={1:{lastSuccess:new Date().toISOString(),next:new Date(Date.now()+600000).toISOString(),retryAt:"",status:"scheduled",message:"按间隔自动刷新"}};
 window.go.main.App.SaveAutoRefreshSettings=async value=>{inventory.autoRefresh={...value};window.__savedRefresh={...value}};
 `;
 const js=ts.transpileModule(mock,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.None}}).outputText;
 const browser=await chromium.launch({headless:true,channel:"msedge"});
 try {
  const page=await browser.newPage({viewport:{width:1440,height:960}}); const errors=[];page.on("pageerror",e=>errors.push(e.message));
  await page.addInitScript({content:js});await page.goto("http://127.0.0.1:5187/");
  await page.getByRole("article").first().waitFor();
  await page.getByText('定时刷新已关闭',{exact:true}).waitFor();
  await page.getByRole("button",{name:"设置",exact:true}).click();
  const dialog=page.getByRole("dialog",{name:"客户端设置"});await dialog.waitFor();
  await dialog.getByLabel("所有已授权任务").check();
  await dialog.getByLabel("刷新间隔",{exact:true}).fill("15");
  await page.screenshot({path:"preview-sidebar/auto-refresh-settings.png",fullPage:true});
  await dialog.getByRole("button",{name:"保存设置",exact:true}).click();await dialog.waitFor({state:"hidden"});
  const saved=await page.evaluate(()=>window.__savedRefresh);
  if (!saved?.enabled||saved.intervalMinutes!==15)throw Error("refresh settings did not reach bridge");
  const overview=page.locator('.inventory-refresh-status');
  if(!(await overview.innerText()).includes('距最近刷新'))throw Error('enabled countdown missing');
  const before=await overview.innerText();
  await page.waitForTimeout(1200);
  if((await overview.innerText())===before)throw Error('countdown did not advance');
  await page.screenshot({path:'preview-sidebar/auto-refresh-header.png',fullPage:true});
  await page.getByRole("button",{name:"设置",exact:true}).click();
  if(await dialog.getByLabel("刷新间隔",{exact:true}).inputValue()!=="15")throw Error("saved settings not reloaded");
  await dialog.getByRole("button",{name:"取消",exact:true}).click();
  for(const width of [1440,600,480,375,320]){
   await page.setViewportSize({width,height:960});
   await page.getByRole("button",{name:"设置",exact:true}).click();
   const layout=await dialog.evaluate(el=>{
    const checkbox=el.querySelector('#auto-refresh-enabled').getBoundingClientRect();
    const interval=el.querySelector('#auto-refresh-minutes').getBoundingClientRect();
    const option=el.querySelector('.settings-checkbox').getBoundingClientRect();
    return {overflow:el.scrollWidth>el.clientWidth,aligned:Math.abs(checkbox.left-interval.left)<1,centered:Math.abs((checkbox.top+checkbox.height/2)-(option.top+option.height/2))<1};
   });
   if(layout.overflow||!layout.aligned||!layout.centered)throw Error('settings alignment '+width+' '+JSON.stringify(layout));
   if(width===375)await page.screenshot({path:'preview-sidebar/auto-refresh-settings-mobile.png',fullPage:true});
   await dialog.getByRole("button",{name:"取消",exact:true}).click();
  }
  for(const width of [1440,1100,800,600]){await page.setViewportSize({width,height:960});if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error("overflow "+width)}
  const status=page.locator(".tw-auto-refresh").first();if(!(await status.getAttribute("title")).includes("上次完整成功"))throw Error("refresh history missing");
  if(errors.length)throw Error(errors.join("\n"));
  console.log("Auto refresh settings/save/reopen/status/responsive checks passed; mock backend only");
 } finally {await browser.close()}
})().catch(e=>{console.error(e);process.exit(1)});
