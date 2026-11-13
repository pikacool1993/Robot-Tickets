<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { backend, type InventoryView, type InventorySettings } from './bridge'
defineProps<{ stadium?: string }>()
const emit = defineEmits<{ update: [value: InventoryView] }>()
const view = ref<InventoryView | null>(null), error = ref(''), loadError = ref(''), busy = ref(false)
const modal = ref<'details' | 'settings' | null>(null), dialog = ref<HTMLDialogElement | null>(null)
const settingsForm = ref<HTMLFormElement | null>(null)
const draft = ref<InventorySettings>({ queryMinMS: 300, queryMaxMS: 800, orderMinMS: 4500, orderMaxMS: 7500 })
const orderMin = computed({ get: () => draft.value.orderMinMS / 1000, set: (v: number) => { draft.value.orderMinMS = Math.round(v * 1000) } })
const orderMax = computed({ get: () => draft.value.orderMaxMS / 1000, set: (v: number) => { draft.value.orderMaxMS = Math.round(v * 1000) } })
let alive = true, polling = false, anchor = 0, sampled = 0
const displayNow = ref(0)
const validTime = (v?: string) => !!v && !v.startsWith('0001-') && Number.isFinite(Date.parse(v))
const remaining = computed(() => validTime(view.value?.match.sale_at) ? Math.max(0, Math.ceil((Date.parse(view.value!.match.sale_at) - displayNow.value) / 1000)) : 0)
const time = (v?: string) => !validTime(v) ? '未配置' : new Date(v!).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
const shortTime = (v?: string) => !validTime(v) ? '未配置' : new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(v!))
const countdown = computed(() => {
  const n = remaining.value
  if (n >= 86400) return `${Math.floor(n / 86400)}天 ${Math.floor(n % 86400 / 3600)}小时`
  const h = Math.floor(n / 3600), m = Math.floor(n % 3600 / 60), s = n % 60
  return `${h ? `${h}时 ` : ''}${m ? `${m}分 ` : ''}${s}秒`
})
const failure = (e: unknown) => String(e instanceof Error ? e.message : e)
async function load() {
  if (polling || !alive) return
  polling = true
  try {
    const data = await backend().GetInventory()
    if (alive) { view.value = data; anchor = performance.now(); sampled = Date.parse(data.now); displayNow.value = sampled; loadError.value = ''; emit('update', data) }
  } catch (e) { if (alive) loadError.value = failure(e) } finally { polling = false }
}
async function openModal(kind: 'details' | 'settings') {
  if (busy.value || (kind === 'settings' && !view.value)) return
  if (kind === 'settings') draft.value = { ...view.value!.settings }
  error.value = ''; modal.value = kind
  await nextTick(); dialog.value?.showModal()
}
function closeModal() { if (!busy.value) { dialog.value?.close(); modal.value = null } }
async function save() {
  if (busy.value || !settingsForm.value?.reportValidity()) return
  busy.value = true; error.value = ''
  try { await backend().SaveInventorySettings({ ...draft.value }); await load(); if (alive) { dialog.value?.close(); modal.value = null } }
  catch (e) { if (alive) error.value = failure(e) } finally { busy.value = false }
}
async function sync() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { await backend().SyncInventoryClock(); await load() } catch (e) { if (alive) error.value = failure(e) } finally { busy.value = false }
}
let poll: ReturnType<typeof setInterval>, tick: ReturnType<typeof setInterval>
onMounted(() => { void load(); poll = setInterval(load, 1000); tick = setInterval(() => { displayNow.value = sampled + performance.now() - anchor }, 200) })
onUnmounted(() => { alive = false; clearInterval(poll); clearInterval(tick); dialog.value?.close() })
</script>
<template>
  <section class="inventory-panel" aria-label="比赛与任务工具栏">
    <div class="inventory-heading">
      <div class="inventory-title"><slot name="title" /></div><span class="inventory-divider" aria-hidden="true"></span>
      <button type="button" class="inventory-match" aria-haspopup="dialog" :aria-label="`查看比赛详情：${view?.match.name || '当前比赛未配置'}`" :title="`${view?.match.name || '当前比赛未配置'} · 点击查看详情`" @click="openModal('details')">{{ view?.match.name || '当前比赛未配置' }} <span aria-hidden="true">↗</span></button>
      <span class="inventory-enabled" :class="{ enabled: view?.match.enabled }">{{ view?.match.enabled ? '已启用' : '未启用' }}</span>
      <div class="inventory-clock"><span :class="{ ready: view?.clockReady }" :title="view?.clockReady ? `时间已校准 · 偏差 ${view.offsetMS}ms` : '尚未校准，暂不可启动'"><i aria-hidden="true"></i>{{ view?.clockReady ? `已校准 · ${view.offsetMS}ms` : '未校准' }}</span><button type="button" class="inventory-link" :disabled="busy" @click="sync">{{ busy ? '处理中…' : '校准' }}</button></div>
    </div>
    <div class="inventory-meta">
      <span class="inventory-id" :title="view?.match.id">ID <b>{{ view?.match.id || '—' }}</b></span>
      <span :title="`比赛 ${time(view?.match.match_at)}（北京时间）`">比赛 <b>{{ shortTime(view?.match.match_at) }}</b></span>
      <span :title="`开售 ${time(view?.match.sale_at)}（北京时间）`">开售 <b>{{ shortTime(view?.match.sale_at) }}</b></span>
      <span v-if="validTime(view?.match.sale_at)" class="inventory-sale">{{ remaining > 0 ? `距开售 ${countdown}` : '已开售' }}</span>

    </div>
    <p v-if="loadError || error || view?.clockError || view?.match.message" class="inventory-alert" role="alert">{{ loadError || error || view?.clockError || view?.match.message }}</p>
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
          </fieldset>
        </form>
        <template v-else-if="modal === 'details'">
          <dl class="inventory-details">
            <div><dt>比赛名称</dt><dd>{{ view?.match.name || '未配置' }}</dd></div><div><dt>比赛 ID</dt><dd>{{ view?.match.id || '—' }}</dd></div><div><dt>比赛状态</dt><dd>{{ view?.match.enabled ? '已启用' : '未启用' }}</dd></div>
            <div><dt>座位目录</dt><dd>{{ stadium || '未加载' }}</dd></div><div><dt>比赛时间</dt><dd>{{ time(view?.match.match_at) }}</dd></div><div><dt>开售时间</dt><dd>{{ time(view?.match.sale_at) }}</dd></div><div><dt>时区</dt><dd>北京时间（UTC+8）</dd></div>
            <div><dt>时间校准</dt><dd>{{ view?.clockReady ? `已校准 · 偏差 ${view.offsetMS}ms` : '尚未校准，暂不可启动' }}</dd></div>
          </dl><p class="inventory-help">首次提交仅调度，下单接口待接入。</p>
        </template>
        <p v-if="error || loadError || view?.clockError || view?.match.message" class="inventory-alert" role="alert">{{ error || loadError || view?.clockError || view?.match.message }}</p>
      </div>
      <footer class="inventory-dialog-footer"><span>{{ modal === 'details' ? '比赛信息由云端同步' : '保存后对当前客户端生效' }}</span><div><button type="button" :disabled="busy" @click="closeModal()">{{ modal === 'settings' ? '取消' : '关闭' }}</button><button v-if="modal === 'settings'" type="submit" form="inventory-settings-form" class="inventory-primary" :disabled="busy">{{ busy ? '保存中…' : '保存设置' }}</button></div></footer>
    </dialog>
  </section>
</template>
<style scoped>
.inventory-panel{color:var(--app-ink);min-width:0}.inventory-heading{display:flex;align-items:center;gap:12px;min-width:0}.inventory-title{flex:none}.inventory-divider{height:17px;width:1px;background:var(--app-line);flex:none}.inventory-match{font-size:13px;font-weight:500;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0;flex:0 1 auto;color:var(--app-muted)}.inventory-enabled{font-size:10px;color:var(--app-muted);white-space:nowrap;flex:none}.inventory-enabled.enabled{color:var(--app-success)}.inventory-clock{margin-left:auto;display:flex;align-items:center;gap:10px;white-space:nowrap;flex:none}.inventory-clock>span{font-size:11px;color:var(--app-warning)}.inventory-clock>span.ready{color:var(--app-success)}.inventory-clock i{display:inline-block;width:5px;height:5px;border-radius:50%;background:currentColor;margin-right:5px;vertical-align:middle}
.inventory-meta{display:flex;align-items:center;flex-wrap:wrap;gap:8px 16px;font-size:11px;color:var(--app-muted);margin-top:5px}.inventory-meta b{font-weight:400;color:var(--app-ink);font-variant-numeric:tabular-nums}.inventory-sale{color:var(--app-accent);font-variant-numeric:tabular-nums}.inventory-actions{margin-left:auto;display:flex;gap:16px}.inventory-panel button.inventory-link{padding:3px 0;border:0;border-radius:3px;background:transparent;color:var(--app-muted);font-size:11px}.inventory-panel button.inventory-link:hover:not(:disabled){color:var(--app-accent);background:transparent}
.inventory-alert{margin:10px 0!important;padding:9px 11px;border:1px solid var(--app-warning-line);border-radius:5px;background:var(--app-warning-bg);color:var(--app-warning)!important;font-size:12px;overflow-wrap:anywhere}
.inventory-dialog{padding:0;width:540px;max-width:calc(100vw - 32px);max-height:85dvh;border:1px solid var(--app-line);border-radius:10px;background:var(--app-panel);color:var(--app-ink);box-shadow:0 18px 55px var(--app-shadow);font:13px/1.5 'Segoe UI','Microsoft YaHei',sans-serif}.inventory-dialog::backdrop{background:var(--app-backdrop)}.inventory-dialog-head{padding:18px 22px;border-bottom:1px solid var(--app-line);display:flex;align-items:center;justify-content:space-between;gap:16px}.inventory-dialog-head h2{font-size:17px;font-weight:600;margin:0}.inventory-dialog-head p{font-size:12px;color:var(--app-muted);margin:5px 0 0;overflow-wrap:anywhere}.inventory-dialog button{font:inherit;font-size:12px;padding:7px 12px;border:1px solid var(--app-line);border-radius:5px;background:var(--app-panel);color:var(--app-ink)}.inventory-dialog button.inventory-close{border:0;background:transparent;color:var(--app-muted);padding:3px 7px;font-size:20px}.inventory-dialog-content{padding:20px 22px}.inventory-settings{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:18px 16px;border:0;padding:0;margin:0;min-width:0}.inventory-settings label{margin:0;font-size:12px;color:var(--app-muted)}.inventory-settings input{display:block;min-width:0;width:100%;margin-top:7px;padding:9px 10px;border:1px solid var(--app-line);border-radius:5px;color:var(--app-ink);background:var(--app-bg);font:inherit}.inventory-help{margin:16px 0 0!important;font-size:12px;color:var(--app-muted);line-height:1.7}.inventory-details{margin:0}.inventory-details>div{display:grid;grid-template-columns:90px minmax(0,1fr);gap:16px;padding:8px 0;border-bottom:1px solid var(--app-line);font-size:12px}.inventory-details>div:first-child{padding-top:0}.inventory-details dt{color:var(--app-muted)}.inventory-details dd{margin:0;text-align:right;overflow-wrap:anywhere}.inventory-dialog-footer{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:14px 22px;border-top:1px solid var(--app-line);font-size:11px;color:var(--app-muted)}.inventory-dialog-footer>div{display:flex;gap:8px}.inventory-dialog button.inventory-primary{background:var(--app-accent);color:var(--app-on-accent);border-color:var(--app-accent)}
@container client-workspace (max-width:760px){.inventory-heading{flex-wrap:wrap;gap:8px}.inventory-match{flex:1}.inventory-clock{margin-left:auto}.inventory-meta{gap:6px 12px}}
@media(pointer:coarse){.inventory-panel button.inventory-link,.inventory-dialog button{min-height:40px}}
</style>
<style scoped>
.inventory-panel button.inventory-match{padding:0;border:0;border-radius:3px;background:transparent;font-size:13px;font-weight:500;color:var(--app-muted);text-align:left;line-height:1.6}
.inventory-panel button.inventory-match:hover{color:var(--app-accent);background:transparent}
.inventory-match>span{font-size:10px;color:var(--app-muted)}
.inventory-id{max-width:220px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
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
.settings-row-label{font-size:12px;color:var(--app-muted)}
.settings-range{display:grid;grid-template-columns:minmax(0,1fr) 14px minmax(0,1fr) 26px;align-items:center;gap:8px;min-width:0}
.client-settings-dialog .settings-range input{height:30px;margin:0;padding:4px 8px;font-size:12px;border-radius:4px;font-variant-numeric:tabular-nums}
.settings-range-separator{text-align:center;color:var(--app-muted);font-size:11px}
.settings-unit{color:var(--app-muted);font-size:11px;white-space:nowrap}
.client-settings-dialog .settings-help{font-size:11px;color:var(--app-muted);margin:7px 0 0;line-height:1.6}
@media(pointer:coarse){.client-settings-dialog .settings-range input{height:36px}}
</style>
