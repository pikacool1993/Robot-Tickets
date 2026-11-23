<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type AgentTask } from './api.ts'
import { taskStatuses, taskLabels, taskStatus, type TaskStatus } from './taskStatus.ts'
import { rememberTerminalView, terminalView } from './terminalTaskNavigation.ts'
const route = useRoute(), router = useRouter()
const agentId = String(route.params.agentId), instance = String(route.params.instance)
const agentName = ref(''), clientNumber = ref(instance)
const tasks = ref<AgentTask[]>([]), counts = ref<Partial<Record<TaskStatus, number>>>({})
const total = ref(0), synced = ref(false), syncedAt = ref(''), loading = ref(false), error = ref('')
const search = ref(typeof route.query.q === 'string' ? route.query.q : '')
const selectedTask = ref<AgentTask | null>(null), drawerClose = ref<HTMLButtonElement | null>(null)
let trigger: HTMLElement | null = null, revision = 0, alive = true
const originalOverflow = document.body.style.overflow
const page = computed(() => Math.max(1, Math.min(100000, Number(route.query.page) || 1)))
const limit = computed(() => Number(route.query.limit) === 120 ? 120 : 60)
const status = computed(() => taskStatuses.includes(route.query.status as TaskStatus) ? route.query.status as TaskStatus : '')
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / limit.value)))
const numberWidth = computed(() => `${Math.max(108, ...tasks.value.map(task => String(task.task_id).length * 9 + 24))}px`)
const allCount = computed(() => taskStatuses.reduce((n, key) => n + (counts.value[key] || 0), 0))
let lastQuery = Object.fromEntries(Object.entries(route.query).filter((entry): entry is [string,string] => typeof entry[1] === 'string'))
let restoreScroll: number | undefined = terminalView(agentId,instance)?.scroll
function remember() { rememberTerminalView(agentId,instance,lastQuery,window.scrollY) }
const back = () => { remember(); return router.push({ path: `/admin/agents/${encodeURIComponent(agentId)}`, query: { tab: 'devices' } }) }
function stamp(value?: string) { return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—' }
async function load() {
  const current = ++revision
  loading.value = true; error.value = ''
  try {
    const data = await api.terminalTasks(agentId, instance, { page: page.value, limit: limit.value, status: status.value, q: typeof route.query.q === 'string' ? route.query.q : '' })
    if (!alive || current !== revision) return
    tasks.value = data.tasks || []; counts.value = data.counts || {}; total.value = data.total || 0; synced.value = data.synced; syncedAt.value = data.tasks_synced_at || ''
  } catch (e) { if (alive && current === revision) error.value = e instanceof Error ? e.message : '任务加载失败' }
  finally { if (alive && current === revision) { loading.value = false; if (restoreScroll !== undefined) { await nextTick(); window.scrollTo(0,restoreScroll); restoreScroll = undefined } } }
}
async function change(query: Record<string, string | number>) {
  await router.replace({ query: { ...route.query, ...query } })
  document.getElementById('terminal-task-grid')?.scrollIntoView({ block: 'start' })
}
async function openTask(task: AgentTask, event: MouseEvent) {
  trigger = event.currentTarget as HTMLElement; selectedTask.value = task; document.body.style.overflow = 'hidden'
  await nextTick(); drawerClose.value?.focus()
}
function closeTask() { selectedTask.value = null; document.body.style.overflow = originalOverflow; trigger?.focus() }
function onKey(e: KeyboardEvent) {
  if (!selectedTask.value) return
  if (e.key === 'Escape') { e.preventDefault(); closeTask() }
  if (e.key === 'Tab') { e.preventDefault(); drawerClose.value?.focus() }
}
watch(() => route.fullPath, () => { if (route.name !== 'terminal-tasks' || route.params.agentId !== agentId || route.params.instance !== instance) { remember(); return }; lastQuery = Object.fromEntries(Object.entries(route.query).filter((entry): entry is [string,string] => typeof entry[1] === 'string')); search.value = typeof route.query.q === 'string' ? route.query.q : ''; void load() })
onMounted(async () => {
  document.addEventListener('keydown', onKey)
  void load()
  try { const data = await api.agent(agentId); if (alive) { agentName.value = data.name; clientNumber.value = data.devices.find(d => d.client_instance_id === instance)?.client_number || instance } } catch { /* The task API independently reports access errors. */ }
})
onUnmounted(() => { alive = false; revision++; document.body.style.overflow = originalOverflow; document.removeEventListener('keydown', onKey) })
</script>
<template>
  <section class="panel terminal-task-page">
    <button class="back link" @click="back">← 返回代理商详情</button>
    <div class="toolbar"><div><small class="task-breadcrumb">{{ agentName || '代理商' }}</small><h1>{{ clientNumber }} 的任务</h1><small v-if="syncedAt" class="task-sync-time">上报于 {{ stamp(syncedAt) }}</small></div><button class="secondary" :disabled="loading" @click="load">{{ loading ? '刷新中…' : '刷新' }}</button></div>
    <div class="task-filter-tabs" role="group" aria-label="任务状态筛选"><button :class="{active: !status}" :aria-pressed="!status" :disabled="loading" @click="change({status:'',page:1})">全部 {{ allCount }}</button><button v-for="key in taskStatuses" :key="key" :class="['task-status-chip', key, {active: status === key}]" :aria-pressed="status === key" :disabled="loading" @click="change({status:key,page:1})">{{ taskLabels[key] }} {{ counts[key] || 0 }}</button></div>
    <form class="task-search" @submit.prevent="change({q:search,page:1})"><label><span class="sr-only">搜索任务编号</span><input v-model.trim="search" placeholder="搜索任务编号" inputmode="numeric" pattern="[0-9]*" maxlength="20" /></label><button :disabled="loading">查询</button><button type="button" class="secondary" :disabled="loading" @click="search='';change({q:'',status:'',page:1})">重置</button></form>
    <p v-if="error" class="error" role="alert">{{ error }} <button class="link" @click="load">重试</button></p>
    <div id="terminal-task-grid" class="task-grid-region" :aria-busy="loading">
      <div v-if="loading" class="empty" role="status">正在加载任务…</div><div v-else-if="!synced && !error" class="empty">任务状态待终端上报</div><div v-else-if="!tasks.length && !error" class="empty">{{ allCount ? '没有符合条件的任务' : '当前没有任务' }}</div>
      <div v-else-if="!error" class="task-number-grid" :style="{'--task-number-width':numberWidth}"><button v-for="task in tasks" :key="task.task_id" :class="['task-number',taskStatus(task.authorization_status)]" :data-tooltip="taskLabels[taskStatus(task.authorization_status)]" :title="`任务 ${task.task_id} · ${taskLabels[taskStatus(task.authorization_status)]}`" :aria-label="`任务 ${task.task_id}，${taskLabels[taskStatus(task.authorization_status)]}，查看详情`" @click="openTask(task,$event)">{{ task.task_id }}</button></div>
    </div>
    <div class="task-pagination"><span v-if="total !== allCount" class="task-filtered-total">共 {{ total }} 个</span><label>每页 <select :value="limit" :disabled="loading" @change="change({limit:($event.target as HTMLSelectElement).value,page:1})"><option value="60">60</option><option value="120">120</option></select> 个</label><button class="secondary" :disabled="loading || page <= 1" @click="change({page:page-1})">上一页</button><span>{{ page }} / {{ pageCount }}</span><button class="secondary" :disabled="loading || page >= pageCount" @click="change({page:page+1})">下一页</button></div>
    <div v-if="selectedTask" class="task-drawer-overlay" @click.self="closeTask"><section class="task-drawer" role="dialog" aria-modal="true" aria-labelledby="task-detail-heading"><button ref="drawerClose" class="close" aria-label="关闭任务详情" @click="closeTask">×</button><h2 id="task-detail-heading">任务详情</h2><dl><dt>任务编号</dt><dd>{{ selectedTask.task_id }}</dd><dt>授权状态</dt><dd><span :class="['task-status-chip',taskStatus(selectedTask.authorization_status)]">{{ taskLabels[taskStatus(selectedTask.authorization_status)] }}</span></dd><dt>所属终端</dt><dd>{{ clientNumber }}</dd><dt>终端标识</dt><dd>{{ instance }}</dd><dt>状态更新时间</dt><dd>{{ stamp(selectedTask.updated_at) }}</dd></dl></section></div>
  </section>
</template>
<style scoped>
.terminal-task-page{min-width:0}.task-breadcrumb{display:block;color:var(--text-secondary);margin-bottom:8px}.task-filter-tabs{display:flex;flex-wrap:wrap;gap:8px;padding-bottom:16px;border-bottom:1px solid var(--line)}.task-filter-tabs button:not(.task-status-chip){background:#f5f5f7;color:#6e6e73;min-height:34px;padding:0 12px;font-size:12px}.task-filter-tabs button.task-status-chip { min-height: 34px; padding-inline: 12px; }
.task-filter-tabs button.active, .task-filter-tabs button:hover:not(:disabled) { box-shadow: inset 0 0 0 1px currentColor; }
.task-filter-tabs button:not(.task-status-chip):hover:not(:disabled) { background: #e8f2ff; }
.task-search{display:flex;align-items:center;gap:8px;margin:18px 0}.task-search label{flex:1;max-width:400px;min-width:0}.task-sync-time{display:block;margin-top:4px;font-size:12px;color:var(--text-secondary);line-height:1.6}.task-number-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(min(100%,var(--task-number-width,108px)),1fr));gap:10px}.task-number{min-height:42px;position:relative;padding:8px;font-weight:500;white-space:nowrap;font-variant-numeric:tabular-nums;border:1px solid transparent;border-radius:7px;transition:box-shadow .15s}.task-number.online{background:#e8f7ed;color:#248a3d}.task-number.expired{background:#fff0f0;color:#c62828}.task-number.pending_authorization{background:#fff5e5;color:#a85d00}.task-number.maintenance_paused{background:#e8f2ff;color:#235a95}.task-number.protocol_offline{background:#f3edfb;color:#7450a8}.task-number.unknown{background:#f1f1f4;color:#6e6e73}.task-number:hover:not(:disabled){background:var(--task-hover);box-shadow:inset 0 0 0 1px currentColor}.task-number.online{--task-hover:#e8f7ed}.task-number.expired{--task-hover:#fff0f0}.task-number.pending_authorization{--task-hover:#fff5e5}.task-number.maintenance_paused{--task-hover:#e8f2ff}.task-number.protocol_offline{--task-hover:#f3edfb}.task-number.unknown{--task-hover:#f1f1f4}.task-number:focus-visible:after{content:attr(data-tooltip);position:absolute;bottom:calc(100% + 4px);left:50%;transform:translateX(-50%);padding:4px 7px;border-radius:4px;background:#1d1d1f;color:#fff;font-size:11px;z-index:2}.task-number:focus-visible{outline:2px solid var(--blue);outline-offset:2px}.task-grid-region{scroll-margin-top:20px;min-height:120px}.task-pagination{display:flex;align-items:center;justify-content:flex-end;gap:10px;flex-wrap:wrap;margin-top:22px;font-size:12px;color:var(--text-secondary)}.task-pagination .task-filtered-total{margin-right:auto}.task-pagination select{border:1px solid #d2d2d7;background:#fff;border-radius:6px;min-height:32px;padding:0 8px}.task-drawer-overlay{position:fixed;z-index:25;inset:0;background:#1d1d1f55;display:flex;justify-content:flex-end}.task-drawer{width:min(440px,100%);height:100dvh;overflow-y:auto;position:relative;background:white;padding:30px 24px;box-shadow:-8px 0 32px #00000016}.task-drawer dt{font-size:12px;color:var(--text-secondary);margin-top:24px}.task-drawer dd{margin:8px 0 0;overflow-wrap:anywhere}.sr-only{position:absolute;width:1px;height:1px;overflow:hidden;clip-path:inset(50%)}@media(max-width:720px){.task-search{flex-wrap:wrap}.task-search label{flex-basis:100%;max-width:none}.task-search button{flex:1}.task-pagination .task-filtered-total{width:100%}}
</style>
