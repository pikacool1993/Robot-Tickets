<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { backend, type InventoryView, type InventorySettings } from './bridge'
const emit = defineEmits<{ update: [value: InventoryView] }>()
const view = ref<InventoryView | null>(null), error = ref(''), busy = ref(false), editing = ref(false)
const draft = ref<InventorySettings>({ queryMinMS: 300, queryMaxMS: 800, orderMinMS: 4500, orderMaxMS: 7500 })
const orderMin = computed({ get: () => draft.value.orderMinMS / 1000, set: (v: number) => { draft.value.orderMinMS = Math.round(v * 1000) } })
const orderMax = computed({ get: () => draft.value.orderMaxMS / 1000, set: (v: number) => { draft.value.orderMaxMS = Math.round(v * 1000) } })
let alive = true, polling = false, anchor = 0, sampled = 0
const displayNow = ref(0)
const remaining = computed(() => view.value?.match.sale_at ? Math.max(0, Math.ceil((Date.parse(view.value.match.sale_at) - displayNow.value) / 1000)) : 0)
const time = (v?: string) => !v || v.startsWith('0001-') ? '未配置' : new Date(v).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
const failure = (e: unknown) => String(e instanceof Error ? e.message : e)
async function load() {
  if (polling || !alive) return
  polling = true
  try {
    const data = await backend().GetInventory()
    if (alive) { view.value = data; anchor = performance.now(); sampled = Date.parse(data.now); displayNow.value = sampled; emit('update', data) }
  } catch (e) { if (alive) error.value = failure(e) } finally { polling = false }
}
function edit() { if (!view.value) return; draft.value = { ...view.value.settings }; editing.value = !editing.value; error.value = '' }
async function save() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { await backend().SaveInventorySettings({ ...draft.value }); if (alive) editing.value = false; await load() }
  catch (e) { if (alive) error.value = failure(e) } finally { busy.value = false }
}
async function sync() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { await backend().SyncInventoryClock(); await load() } catch (e) { if (alive) error.value = failure(e) } finally { busy.value = false }
}
let poll: ReturnType<typeof setInterval>, tick: ReturnType<typeof setInterval>
onMounted(() => { void load(); poll = setInterval(load, 1000); tick = setInterval(() => { displayNow.value = sampled + performance.now() - anchor }, 200) })
onUnmounted(() => { alive = false; clearInterval(poll); clearInterval(tick) })
</script>
<template>
  <section class="inventory-panel" aria-label="比赛与库存调度">
    <div class="inventory-heading"><strong>{{ view?.match.name || '当前比赛未配置' }}</strong><span>ID {{ view?.match.id || '—' }} · {{ view?.match.enabled ? '已启用' : '未启用' }}</span><button @click="edit">查询设置</button></div>
    <div class="inventory-meta"><span>比赛 {{ time(view?.match.match_at) }}</span><span>开售 {{ time(view?.match.sale_at) }}（北京时间）</span><span v-if="remaining > 0">距开售 {{ remaining }} 秒</span><span v-else-if="view?.match.id">已到开售时间</span></div>
    <div class="inventory-meta"><span>{{ view?.clockReady ? `时间已校准 · 偏差 ${view.offsetMS}ms` : '尚未校准，暂不可启动' }}</span><span v-if="view?.clockError">{{ view.clockError }}</span><button :disabled="busy" @click="sync">{{ busy ? '处理中…' : '校准时间' }}</button><span>首次提交仅调度，下单接口待接入</span></div>
    <form v-if="editing" class="inventory-settings" @submit.prevent="save">
      <fieldset :disabled="busy"><label>查询最短等待（毫秒）<input v-model.number="draft.queryMinMS" type="number" min="1" max="86400000" step="1" required /></label><label>查询最长等待（毫秒）<input v-model.number="draft.queryMaxMS" type="number" :min="draft.queryMinMS" max="86400000" step="1" required /></label><label>首次下单最早偏移（秒）<input v-model.number="orderMin" type="number" min="0" max="86400" step="0.001" required /></label><label>首次下单最晚偏移（秒）<input v-model.number="orderMax" type="number" :min="orderMin" max="86400" step="0.001" required /></label></fieldset>
      <p>全客户端共用；查询间隔用于下一次等待，下单区间只影响新启动任务。</p><button :disabled="busy">保存设置</button><button type="button" :disabled="busy" @click="editing = false">取消</button>
    </form>
    <p v-if="error || view?.match.message" role="alert">{{ error || view?.match.message }}</p>
  </section>
</template>
<style scoped>
.inventory-panel{padding:14px 18px;margin-bottom:14px;border:1px solid var(--app-line);border-radius:10px;background:var(--app-panel);color:var(--app-ink)}.inventory-heading,.inventory-meta{display:flex;gap:12px;align-items:center;flex-wrap:wrap}.inventory-heading strong{font-size:16px}.inventory-heading>button{margin-left:auto}.inventory-meta{margin-top:8px;font-size:12px;color:var(--app-muted)}button{padding:5px 10px;border:1px solid var(--app-line);border-radius:6px;background:var(--app-soft);color:var(--app-ink);cursor:pointer}button:disabled{opacity:.5;cursor:default}.inventory-settings{margin-top:14px}.inventory-settings fieldset{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px;border:0;padding:0;min-width:0}.inventory-settings label{font-size:12px}.inventory-settings input{display:block;width:100%;min-width:0;box-sizing:border-box;margin-top:5px;padding:7px;background:var(--app-input);color:var(--app-ink);border:1px solid var(--app-line);border-radius:5px}.inventory-settings p{font-size:12px;color:var(--app-muted)}.inventory-settings>button{margin-right:8px}@media(max-width:800px){.inventory-settings fieldset{grid-template-columns:repeat(2,minmax(0,1fr))}}
</style>
