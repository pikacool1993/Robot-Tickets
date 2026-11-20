<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { backend, type InventoryView, type InventorySettings } from './bridge'
import TaskToast from './TaskToast.vue'
import { watch } from 'vue'
defineProps<{ stadium?: string }>()
const emit = defineEmits<{ update: [value: InventoryView]; notice: [value: string] }>()
const view = ref<InventoryView | null>(null), error = ref(''), loadError = ref(''), busy = ref(false)
const modal = ref<'details' | 'settings' | null>(null), dialog = ref<HTMLDialogElement | null>(null)
const settingsForm = ref<HTMLFormElement | null>(null)
const saleStopBusy = ref(false)
const draft = ref<InventorySettings>({ queryMinMS: 300, queryMaxMS: 800, orderMinMS: 4500, orderMaxMS: 7500 })
const refreshDraft = ref({ enabled: false, intervalMinutes: 30 })
const simulationDraft = ref({ enabled:false, orderFailurePercents:[6,6,6,6,6,5,5,5,5,5,5,5], queryFailurePercent:0, cancelFailurePercent:0, refundFailurePercent:0, successPercent:35, orderSuccessPercent:35, paidOrderPercent:50, emptyPercent:30, unavailablePercent:15, networkErrorPercent:10, businessErrorPercent:10 })
const orderFailureLabels = ['库存不足（code=3，默认等待）','抢座失败（code=5，默认等待）','成功码缺少订单 ID','有未支付订单','重复购票','超过最高购票量','请求超时','响应无法解析','库存不足（timeout=2秒）','抢座失败（timeout=2秒）','连接中断','其他业务失败（code=996）']
const orderProbabilityTotal = computed(() => simulationDraft.value.orderSuccessPercent + simulationDraft.value.orderFailurePercents.reduce((sum, value) => sum + value, 0))
function resetDefaults() {
  refreshDraft.value = { enabled: false, intervalMinutes: 30 }
  draft.value = { queryMinMS:300, queryMaxMS:800, orderMinMS:4500, orderMaxMS:7500 }
  simulationDraft.value = { enabled:false, queryFailurePercent:0, cancelFailurePercent:0, refundFailurePercent:0, successPercent:35, orderSuccessPercent:35, paidOrderPercent:50, emptyPercent:30, unavailablePercent:15, networkErrorPercent:10, businessErrorPercent:10, orderFailurePercents:[6,6,6,6,6,5,5,5,5,5,5,5] }
  error.value = ''
}
const orderMin = computed({ get: () => draft.value.orderMinMS / 1000, set: (v: number) => { draft.value.orderMinMS = Math.round(v * 1000) } })
const orderMax = computed({ get: () => draft.value.orderMaxMS / 1000, set: (v: number) => { draft.value.orderMaxMS = Math.round(v * 1000) } })
let alive = true, polling = false, anchor = performance.now(), sampled = Date.now(), saleRevision = 0
const displayNow = ref(sampled)
const liveClockFormat = new Intl.DateTimeFormat('en-GB', { timeZone: 'Asia/Shanghai', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' })
const liveClock = computed(() => liveClockFormat.format(new Date(displayNow.value)))
const validTime = (v?: string) => !!v && !v.startsWith('0001-') && Number.isFinite(Date.parse(v))
const refreshNow = ref(Date.now())
const refreshOverview = computed(() => {
  if (!view.value) return { text: '定时刷新 · 加载中', detail: '正在获取定时刷新设置' }
  if (loadError.value) return { text: '定时刷新 · 状态待更新', detail: loadError.value }
  if (!view.value.autoRefresh.enabled) return { text: '定时刷新已关闭', detail: '可在设置中开启，覆盖所有已授权任务' }
  const runs = Object.entries(view.value.refreshRuns || {})
  const detail = `每 ${view.value.autoRefresh.intervalMinutes} 分钟刷新；各任务独立计时，显示最近一项安排`
  if (runs.some(([, r]) => r.status === 'refreshing')) return { text: '定时刷新已开启 · 资料刷新中', detail }
  const upcoming = runs.filter(([, r]) => !['blocked', 'waiting', 'disabled'].includes(r.status) && validTime(r.next)).sort((a, b) => Date.parse(a[1].next) - Date.parse(b[1].next))
  const first = upcoming[0]
  if (!first) return { text: `定时刷新已开启 · ${runs.some(([, r]) => r.status === 'blocked') ? '等待人工处理' : '等待任务就绪'}`, detail }
  const [id, run] = first
  const seconds = Math.max(0, Math.ceil((Date.parse(run.next) - refreshNow.value) / 1000))
  const reason = `${detail}；任务 ${id}：${run.message}`
  if (!seconds) return { text: `定时刷新已开启 · ${run.status === 'deferred' ? '已到期，暂缓刷新' : '已到期，等待刷新'}`, detail: reason }
  const h = Math.floor(seconds / 3600), m = Math.floor(seconds % 3600 / 60), s = seconds % 60
  const clock = [h, m, s].map(n => String(n).padStart(2, '0')).join(':')
  return { text: `定时刷新已开启 · 距${run.status === 'retry' ? '重试' : '最近刷新'} ${clock}`, detail: reason }
})
const remaining = computed(() => validTime(view.value?.match.sale_at) ? Math.max(0, Math.ceil((Date.parse(view.value!.match.sale_at) - displayNow.value) / 1000)) : 0)
const dateFormat = new Intl.DateTimeFormat('en-GB', {
  timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit',
  hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23',
})
function time(value?: string) {
  if (!value || !validTime(value)) return '未配置'
  const parts = Object.fromEntries(dateFormat.formatToParts(new Date(value)).map(part => [part.type, part.value]))
  return `${parts.year}/${parts.month}/${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`
}
const countdown = computed(() => {
  const n = remaining.value
  if (n >= 86400) return `${Math.floor(n / 86400)}天 ${Math.floor(n % 86400 / 3600)}小时`
  const h = Math.floor(n / 3600), m = Math.floor(n % 3600 / 60), s = n % 60
  return `${h ? `${h}时 ` : ''}${m ? `${m}分 ` : ''}${s}秒`
})
const failure = (e: unknown) => String(e instanceof Error ? e.message : e)
const inventoryNotice = computed(() => error.value || loadError.value || view.value?.clockError || view.value?.match.message || '')
const taskNotice = computed(() => modal.value ? '' : inventoryNotice.value)
watch(taskNotice, value => emit('notice', value), { immediate: true })
async function load() {
  if (polling || !alive) return
  polling = true
  const revision = saleRevision
  try {
    const data = await backend().GetInventory()
    if (alive && revision === saleRevision) { view.value = data; anchor = performance.now(); sampled = Date.parse(data.now); displayNow.value = sampled; loadError.value = ''; emit('update', data) }
  } catch (e) { if (alive) loadError.value = failure(e) } finally { polling = false }
}
async function openModal(kind: 'details' | 'settings') {
  if (busy.value || (kind === 'settings' && !view.value)) return
  if (kind === 'settings') { draft.value = { ...view.value!.settings }; simulationDraft.value = { ...view.value!.simulation, orderFailurePercents: [...view.value!.simulation.orderFailurePercents] } }
  error.value = ''; modal.value = kind
  if (kind === 'settings') refreshDraft.value = { ...view.value!.autoRefresh }
  await nextTick(); dialog.value?.showModal()
}
function closeModal() { if (!busy.value) { dialog.value?.close(); modal.value = null } }
async function save() {
  if (busy.value || !settingsForm.value?.reportValidity()) return
  if (view.value?.debugMode && orderProbabilityTotal.value !== 100) { error.value = '下单成功和 12 种失败场景概率合计必须为 100%'; return }
  busy.value = true; error.value = ''
  try { await backend().SaveAutoRefreshSettings({ ...refreshDraft.value }); await backend().SaveInventorySettings({ ...draft.value }); if (view.value?.debugMode) await backend().SaveSimulationSettings({ ...simulationDraft.value }); await load(); if (alive) { dialog.value?.close(); modal.value = null } }
  catch (e) { if (alive) error.value = failure(e) } finally { busy.value = false }
}
async function sync() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { await backend().SyncInventoryClock(); await load() } catch (e) { if (alive) error.value = failure(e) } finally { busy.value = false }
}
async function changeSaleStop(event: Event) {
  const input = event.target as HTMLInputElement
  const value = input.checked
  const matchID = view.value?.match.id
  if (saleStopBusy.value || !matchID) return
  saleStopBusy.value = true; error.value = ''
  try {
    await backend().SetSaleStopped(value)
    if (!alive) return
    saleRevision++
    if (view.value?.match.id === matchID) {
      view.value = { ...view.value, saleStopped: value }
      emit('update', view.value)
    }
    await load()
  }
  catch (e) { if (alive) { input.checked = !!view.value?.saleStopped; error.value = failure(e) } }
  finally { saleStopBusy.value = false }
}
let poll: ReturnType<typeof setInterval>, tick: ReturnType<typeof setInterval>
onMounted(() => { void load(); poll = setInterval(load, 1000); tick = setInterval(() => { displayNow.value = sampled + performance.now() - anchor; refreshNow.value = Date.now() }, 200) })
onUnmounted(() => { alive = false; clearInterval(poll); clearInterval(tick); dialog.value?.close() })
</script>
<template>
  <section class="inventory-panel" aria-label="比赛与任务工具栏">
    <div class="inventory-heading">
      <div class="inventory-title"><slot name="title" /></div><span class="inventory-divider" aria-hidden="true"></span>
      <button type="button" class="inventory-match" aria-haspopup="dialog" :title="view?.match.name || '当前比赛未配置'" @click="openModal('details')">
        <span class="inventory-match-name">{{ view?.match.name || '当前比赛未配置' }}</span>
        <span class="inventory-match-details">查看详情 <span aria-hidden="true">›</span></span>
      </button>
      <span class="inventory-enabled" :class="{ enabled: view?.match.enabled }">{{ view?.match.enabled ? '已启用' : '未启用' }}</span>
      <div class="inventory-clock">
        <time class="inventory-live-time" :datetime="new Date(displayNow).toISOString()" :title="`${time(new Date(displayNow).toISOString())}（北京时间） · ${view?.clockReady ? '使用校准时间' : '使用本机时间，尚未校准'}`"><span>北京时间</span><b>{{ liveClock }}</b></time>
        <span :class="{ ready: view?.clockReady }" :title="view?.clockReady ? `时间已校准 · 偏差 ${view.offsetMS}ms` : '尚未校准，暂不可启动'"><i aria-hidden="true"></i>{{ view?.clockReady ? `已校准 · ${view.offsetMS}ms` : '未校准' }}</span>
        <button type="button" class="inventory-calibrate" :disabled="busy" @click="sync">{{ busy ? '处理中…' : '校准时间' }}</button>
      </div>
    </div>
    <div class="inventory-meta">
      <span class="inventory-id" :title="view?.match.id">ID <b>{{ view?.match.id || '—' }}</b></span>
      <span :title="`比赛 ${time(view?.match.match_at)}（北京时间）`">比赛 <b>{{ time(view?.match.match_at) }}</b></span>
      <span :title="`开售 ${time(view?.match.sale_at)}（北京时间）`">开售 <b>{{ time(view?.match.sale_at) }}</b></span>
      <span v-if="view?.saleStopped" class="inventory-sale stopped">已停止售票</span>
      <span v-else-if="validTime(view?.match.sale_at)" class="inventory-sale">{{ remaining > 0 ? `距开售 ${countdown}` : '已开售' }}</span>
      <span class="inventory-refresh-status" :class="{ enabled: view?.autoRefresh.enabled }" :title="refreshOverview.detail"><i aria-hidden="true"></i>{{ refreshOverview.text }}</span>
    </div>
    <slot name="toolbar" :open-settings="() => openModal('settings')" :settings-disabled="!view || busy" />
    <dialog ref="dialog" class="inventory-dialog" :class="{ 'client-settings-dialog': modal === 'settings' }" aria-labelledby="inventory-dialog-title" @cancel.prevent="closeModal()" @close="modal = null">
      <header class="inventory-dialog-head"><div><h2 id="inventory-dialog-title">{{ modal === 'settings' ? '客户端设置' : '比赛详情' }}</h2><p v-if="modal !== 'settings'">{{ view?.match.name || '当前比赛未配置' }}</p></div><button type="button" class="inventory-close" aria-label="关闭弹窗" :disabled="busy" @click="closeModal()">×</button></header>
      <div class="inventory-dialog-content">
        <form v-if="modal === 'settings'" id="inventory-settings-form" ref="settingsForm" @submit.prevent="save">
          <fieldset :disabled="busy" class="inventory-settings">
            <legend>任务调度</legend>
            <div class="settings-row">
              <span id="query-range-label" class="settings-row-label">查询间隔</span>
              <div class="settings-range" role="group" aria-labelledby="query-range-label">
                <input v-model.number="draft.queryMinMS" aria-label="查询最短等待（毫秒）" title="最短等待" type="number" min="1" max="86400000" step="1" required />
                <span class="settings-range-separator" aria-hidden="true">—</span>
                <input v-model.number="draft.queryMaxMS" aria-label="查询最长等待（毫秒）" title="最长等待" type="number" :min="draft.queryMinMS" max="86400000" step="1" required />
                <span class="settings-unit">毫秒</span>
              </div>
            </div>
            <div class="settings-row">
              <span id="order-range-label" class="settings-row-label">首次下单偏移</span>
              <div class="settings-range" role="group" aria-labelledby="order-range-label">
                <input v-model.number="orderMin" aria-label="首次下单最早偏移（秒）" title="最早偏移" type="number" min="0" max="86400" step="0.001" required />
                <span class="settings-range-separator" aria-hidden="true">—</span>
                <input v-model.number="orderMax" aria-label="首次下单最晚偏移（秒）" title="最晚偏移" type="number" :min="orderMin" max="86400" step="0.001" required />
                <span class="settings-unit">秒</span>
              </div>
            </div>
            <p class="settings-help">从最小值到最大值；下单偏移仅影响新启动任务。</p>
            <h3 class="settings-section-title">定时刷新</h3>
            <div class="settings-row">
              <span class="settings-row-label">自动刷新</span>
              <label class="settings-checkbox" for="auto-refresh-enabled"><input id="auto-refresh-enabled" v-model="refreshDraft.enabled" type="checkbox" aria-describedby="auto-refresh-help" /><span>所有已授权任务</span></label>
            </div>
            <div class="settings-row">
              <label for="auto-refresh-minutes" class="settings-row-label">刷新间隔</label>
              <div class="settings-range settings-range-single"><input id="auto-refresh-minutes" v-model.number="refreshDraft.intervalMinutes" type="number" min="1" max="1440" step="1" required /><span class="settings-unit">分钟</span></div>
            </div>
            <p id="auto-refresh-help" class="settings-help">每次重新取码、登录并更新观演人。运行中短暂维护后恢复；已命中方案及在途订单优先，开售前 30 秒起避让首次提交。停止购票后仍可刷新资料。</p>
            <template v-if="view?.debugMode">
              <legend class="simulation-legend">本地模拟验收</legend>
              <label class="simulation-enable"><input v-model="simulationDraft.enabled" type="checkbox"/>启用模拟验收（不会请求官方库存或下单接口）</label>
              <div class="simulation-grid">
                <label>库存命中概率 %<input v-model.number="simulationDraft.successPercent" type="number" min="0" max="100"/></label>
                <label>成功订单已付款概率 %<input v-model.number="simulationDraft.paidOrderPercent" type="number" min="0" max="100"/></label>
                <label>查单失败概率 %<input v-model.number="simulationDraft.queryFailurePercent" type="number" min="0" max="100"/></label>
                <label>取消失败概率 %<input v-model.number="simulationDraft.cancelFailurePercent" type="number" min="0" max="100"/></label>
                <label>退款失败概率 %<input v-model.number="simulationDraft.refundFailurePercent" type="number" min="0" max="100"/></label>
                <label>空库存概率 %<input v-model.number="simulationDraft.emptyPercent" type="number" min="0" max="100"/></label>
                <label>按钮不可购概率 %<input v-model.number="simulationDraft.unavailablePercent" type="number" min="0" max="100"/></label>
                <label>网络异常概率 %<input v-model.number="simulationDraft.networkErrorPercent" type="number" min="0" max="100"/></label>
                <label>业务异常概率 %<input v-model.number="simulationDraft.businessErrorPercent" type="number" min="0" max="100"/></label>
              </div>
              <h3 class="simulation-legend">下单响应概率（合计 {{ orderProbabilityTotal }}% / 100%）</h3>
              <div class="simulation-grid">
                <label>下单成功 %<input v-model.number="simulationDraft.orderSuccessPercent" type="number" min="0" max="100" step="1" required/></label>
                <label v-for="(label, index) in orderFailureLabels" :key="index">{{ label }} %<input v-model.number="simulationDraft.orderFailurePercents[index]" type="number" min="0" max="100" step="1" required/></label>
              </div>
              <p class="settings-help">库存命中、空库存、按钮不可购、网络异常、业务异常合计须为 100%。下单成功与 12 种失败场景单独合计 100%；某项设为 0 则不会出现。已付款及查单、取消、退款失败概率独立设置。模拟等待开售后 1 秒查询；首轮及开售后一分钟内固定返回完整抓包数据，不受概率影响；下单失败场景随机抽取，沿用实际等待及恢复规则。</p>
            </template>
          </fieldset>
        </form>
        <template v-else-if="modal === 'details'">
          <dl class="inventory-details">
            <div><dt>比赛名称</dt><dd>{{ view?.match.name || '未配置' }}</dd></div><div><dt>比赛 ID</dt><dd>{{ view?.match.id || '—' }}</dd></div><div><dt>比赛状态</dt><dd>{{ view?.match.enabled ? '已启用' : '未启用' }}</dd></div>
            <div><dt>座位目录</dt><dd>{{ stadium || '未加载' }}</dd></div><div><dt>比赛时间</dt><dd>{{ time(view?.match.match_at) }}</dd></div><div><dt>开售时间</dt><dd>{{ time(view?.match.sale_at) }}</dd></div><div><dt>时区</dt><dd>北京时间（UTC+8）</dd></div>
            <div><dt>时间校准</dt><dd>{{ view?.clockReady ? `已校准 · 偏差 ${view.offsetMS}ms` : '尚未校准，暂不可启动' }}</dd></div>
          </dl>
          <label class="sale-stop-toggle"><input type="checkbox" :checked="!!view?.saleStopped" :disabled="saleStopBusy || !view?.match.id" @change="changeSaleStop"/><span>本场已停止售票</span></label>
          <p class="inventory-help">比赛信息由后台统一配置；停止售票开关仅控制当前客户端，不会修改官方售票状态。</p>
        </template>
        <div class="task-toast-stack"><TaskToast :message="modal ? inventoryNotice : ''" warning /></div>
      </div>
      <footer class="inventory-dialog-footer"><button v-if="modal === 'settings'" type="button" :disabled="busy" @click="resetDefaults">恢复默认设置</button><span v-else>比赛信息由云端同步</span><div><button type="button" :disabled="busy" @click="closeModal()">{{ modal === 'settings' ? '取消' : '关闭' }}</button><button v-if="modal === 'settings'" type="submit" form="inventory-settings-form" class="inventory-primary" :disabled="busy">{{ busy ? '保存中…' : '保存设置' }}</button></div></footer>
    </dialog>
  </section>
</template>
<style scoped>
.inventory-panel{color:var(--app-ink);min-width:0}.inventory-heading{display:flex;align-items:center;gap:12px;min-width:0}.inventory-title{flex:none}.inventory-divider{height:17px;width:1px;background:var(--app-line);flex:none}.inventory-match{font-size:13px;font-weight:500;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0;flex:0 1 auto;color:var(--app-muted)}.inventory-enabled{font-size:10px;color:var(--app-muted);white-space:nowrap;flex:none}.inventory-enabled.enabled{color:var(--app-success)}.inventory-clock{margin-left:auto;display:flex;align-items:center;gap:10px;white-space:nowrap;flex:none}.inventory-clock>span{font-size:11px;color:var(--app-warning)}.inventory-clock>span.ready{color:var(--app-success)}.inventory-clock i{display:inline-block;width:5px;height:5px;border-radius:50%;background:currentColor;margin-right:5px;vertical-align:middle}
.inventory-meta{display:flex;align-items:center;flex-wrap:wrap;gap:8px 16px;font-size:11px;color:var(--app-muted);margin-top:5px}.inventory-meta b{font-weight:400;color:var(--app-ink);font-variant-numeric:tabular-nums}.inventory-sale{color:var(--app-accent);font-variant-numeric:tabular-nums}.inventory-actions{margin-left:auto;display:flex;gap:16px}
.inventory-panel button.inventory-calibrate{min-height:31px;padding:5px 10px;border:1px solid var(--app-accent);border-radius:6px;background:var(--app-selected);color:var(--app-accent);font-size:12px;font-weight:600}
.inventory-panel button.inventory-calibrate:hover:not(:disabled){background:var(--app-accent);color:var(--app-on-accent)}
.inventory-dialog{padding:0;width:540px;max-width:calc(100vw - 32px);max-height:85dvh;border:1px solid var(--app-line);border-radius:10px;background:var(--app-panel);color:var(--app-ink);box-shadow:0 18px 55px var(--app-shadow);font:13px/1.5 'Segoe UI','Microsoft YaHei',sans-serif}.inventory-dialog::backdrop{background:var(--app-backdrop)}.inventory-dialog-head{padding:18px 22px;border-bottom:1px solid var(--app-line);display:flex;align-items:center;justify-content:space-between;gap:16px}.inventory-dialog-head h2{font-size:17px;font-weight:600;margin:0}.inventory-dialog-head p{font-size:12px;color:var(--app-muted);margin:5px 0 0;overflow-wrap:anywhere}.inventory-dialog button{font:inherit;font-size:12px;padding:7px 12px;border:1px solid var(--app-line);border-radius:5px;background:var(--app-panel);color:var(--app-ink)}.inventory-dialog button.inventory-close{border:0;background:transparent;color:var(--app-muted);padding:3px 7px;font-size:20px}.inventory-dialog-content{padding:20px 22px}.inventory-settings{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:18px 16px;border:0;padding:0;margin:0;min-width:0}.inventory-settings label{margin:0;font-size:12px;color:var(--app-muted)}.inventory-settings input{display:block;min-width:0;width:100%;margin-top:7px;padding:9px 10px;border:1px solid var(--app-line);border-radius:5px;color:var(--app-ink);background:var(--app-bg);font:inherit}.inventory-help{margin:16px 0 0!important;font-size:12px;color:var(--app-muted);line-height:1.7}.inventory-details{margin:0}.inventory-details>div{display:grid;grid-template-columns:90px minmax(0,1fr);gap:16px;padding:8px 0;border-bottom:1px solid var(--app-line);font-size:12px}.inventory-details>div:first-child{padding-top:0}.inventory-details dt{color:var(--app-muted)}.inventory-details dd{margin:0;text-align:right;overflow-wrap:anywhere}.inventory-dialog-footer{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:14px 22px;border-top:1px solid var(--app-line);font-size:11px;color:var(--app-muted)}.inventory-dialog-footer>div{display:flex;gap:8px}.inventory-dialog button.inventory-primary{background:var(--app-accent);color:var(--app-on-accent);border-color:var(--app-accent)}
@container client-workspace (max-width:760px){.inventory-heading{flex-wrap:wrap;gap:8px}.inventory-match{flex:1}.inventory-clock{margin-left:auto}.inventory-meta{gap:6px 12px}}
@media(pointer:coarse){.inventory-panel button.inventory-calibrate,.inventory-dialog button{min-height:40px}}
</style>
<style scoped>
.inventory-panel button.inventory-match{display:inline-flex;align-items:center;gap:8px;padding:5px 9px;border:1px solid var(--app-line);border-radius:6px;background:var(--app-panel);font-size:13px;font-weight:500;color:var(--app-ink);text-align:left;line-height:1.5}
.inventory-panel button.inventory-match:hover{border-color:var(--app-accent);background:var(--app-soft)}
.inventory-match-name{min-width:0;overflow:hidden;text-overflow:ellipsis}
.inventory-match-details{flex:none;font-size:11px;color:var(--app-accent)}
.inventory-sale.stopped{color:var(--app-warning)}
.inventory-id{max-width:220px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.inventory-dialog .sale-stop-toggle{display:inline-flex;align-items:center;gap:7px;margin-top:16px;color:var(--app-ink);font-size:12px}
.sale-stop-toggle input{accent-color:var(--app-danger);width:16px;height:16px;margin:0}
</style>

<style scoped>
.client-settings-dialog[open]{display:flex;flex-direction:column}
.client-settings-dialog .inventory-dialog-head{padding:14px 18px;flex-shrink:0}
.client-settings-dialog .inventory-dialog-head h2{font-size:16px}
.client-settings-dialog .inventory-dialog-content{padding:14px 18px;overflow:auto;min-height:0}
.client-settings-dialog .inventory-dialog-footer{padding:10px 18px;flex-shrink:0}
.client-settings-dialog .inventory-settings{display:block;border:0;margin:0;padding:0;min-width:0}
.client-settings-dialog .inventory-settings legend{display:block;width:100%;font-size:12px;font-weight:600;color:var(--app-ink);padding:0 0 8px;border-bottom:1px solid var(--app-line);margin:0 0 4px}
.settings-row{display:grid;grid-template-columns:100px minmax(0,1fr);align-items:center;gap:12px;padding:6px 0}
.simulation-legend{margin-top:14px!important}.simulation-enable{display:inline-flex!important;width:max-content;align-items:center;gap:5px;padding:5px 0;color:var(--app-ink)!important;white-space:nowrap}.simulation-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:7px 12px}.simulation-grid label{display:grid;gap:4px;margin:0;font-size:11px;color:var(--app-muted)}.simulation-grid input{height:30px;margin:0!important;padding:4px 8px!important;font-size:12px!important}.simulation-enable input{accent-color:var(--app-accent);width:15px;height:15px;flex:0 0 15px;margin:0!important;padding:0!important}
.settings-row-label{font-size:12px;color:var(--app-muted)}
.inventory-live-time{display:inline-flex;align-items:baseline;gap:6px;font-size:11px;color:var(--app-muted)}
.inventory-live-time b{font-size:14px;font-weight:500;color:var(--app-ink);font-variant-numeric:tabular-nums}
.inventory-refresh-status{display:inline-flex;align-items:center;gap:6px;max-width:100%;color:var(--app-muted);font-variant-numeric:tabular-nums;overflow-wrap:anywhere}
.inventory-refresh-status i{width:5px;height:5px;border-radius:50%;background:currentColor;flex-shrink:0}
.inventory-refresh-status.enabled{color:var(--app-success, #86ad91)}
.client-settings-dialog .settings-section-title{font-size:12px;font-weight:600;color:var(--app-ink);padding:0 0 8px;border-bottom:1px solid var(--app-line);margin:20px 0 4px}
.client-settings-dialog .settings-checkbox{display:flex;align-items:center;gap:8px;min-height:30px;min-width:0;color:var(--app-ink);cursor:pointer}
.client-settings-dialog .settings-checkbox input{display:block;flex:0 0 16px;width:16px;height:16px;margin:0;padding:0;accent-color:var(--app-accent)}
.settings-checkbox span{min-width:0;overflow-wrap:anywhere}
.settings-range-single .settings-unit{grid-column:2 / -1}
.settings-range{display:grid;grid-template-columns:minmax(0,1fr) 14px minmax(0,1fr) 26px;align-items:center;gap:8px;min-width:0}
.client-settings-dialog .settings-range input{height:30px;margin:0;padding:4px 8px;font-size:12px;border-radius:4px;font-variant-numeric:tabular-nums}
.settings-range-separator{text-align:center;color:var(--app-muted);font-size:11px}
.settings-unit{color:var(--app-muted);font-size:11px;white-space:nowrap}
.client-settings-dialog .settings-help{font-size:11px;color:var(--app-muted);margin:7px 0 0;line-height:1.6}
@media(pointer:coarse){.client-settings-dialog .settings-range input{height:36px}}
@media(max-width:480px){.settings-row{grid-template-columns:84px minmax(0,1fr);gap:8px}.settings-range{gap:6px;grid-template-columns:minmax(0,1fr) 10px minmax(0,1fr) 26px}}
@media(max-width:380px){.settings-row{grid-template-columns:minmax(0,1fr);gap:6px;padding:8px 0}}
</style>
