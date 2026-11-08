<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { backend, type State, type Task, type TaskView, type ProtocolStatus, type AuthAttempt } from './bridge'
const props = defineProps<{ state: State }>()
defineEmits<{ logout: [] }>()
const view = ref<TaskView>({ catalog: { stadium: '', tiers: [] }, configError: '', tasks: [] })
const error = ref(''), loading = ref(true), creating = ref(false), saving = ref(false), saveError = ref('')
const protocol = ref<ProtocolStatus | null>(null), attempt = ref<AuthAttempt | null>(null), protocolBusy = ref(false), controlAction = ref(''), protocolError = ref('')
const controlDialog = ref<HTMLDialogElement | null>(null), now = ref(Date.now())
const safeImage = computed(() => /^data:image\/(png|jpe?g|gif|webp);base64,/i.test(attempt.value?.image || '') ? attempt.value!.image : '')
const remaining = computed(() => Math.max(0, Math.ceil((Date.parse(attempt.value?.expiresAt || '') - now.value) / 1000)) || 0)
const authLabels: Record<string,string> = { creating: '正在获取二维码', pending: '等待扫码', scanned: '已扫码，等待确认', authorized: '正在确认账号', confirmed: '协议账号已登录', expired: '二维码已过期', timeout: '扫码已超时', cancelled: '扫码已取消', failed: '授权失败', verification: '需要额外验证' }
const serviceLabels: Record<string,string> = { stopped: '已停止', starting: '启动中', running: '运行中', failed: '启动失败' }
let protocolTimer: ReturnType<typeof setInterval> | undefined
let protocolPolling = false
let authGeneration = 0
async function pollProtocol() {
  if (protocolPolling || protocolBusy.value || saving.value || !alive) return
  protocolPolling = true
  try {
    const status = await backend().GetProtocolStatus()
    const data = await backend().GetTasks()
    if (alive) { protocol.value = status; view.value = data }
    if (modal.value === 'auth' && activeID.value !== null) {
      const generation = authGeneration
      const data = await backend().GetAuthorization(activeID.value)
      if (alive && generation === authGeneration && modal.value === 'auth' && activeID.value === data.taskID) attempt.value = data
    }
  } catch (e) { if (alive) protocolError.value = failure(e) }
  finally { protocolPolling = false }
}
async function serviceAction(action: string) {
  controlAction.value = action; protocolError.value = ''
  if (action === 'start') { await controlService(); return }
  await pollProtocol(); controlDialog.value?.showModal()
}
async function controlService() {
  if (protocolBusy.value) return
  protocolBusy.value = true; protocolError.value = ''
  try { await backend().ControlProtocol(controlAction.value, true); controlDialog.value?.close() }
  catch (e) { protocolError.value = failure(e) }
  finally { protocolBusy.value = false; await pollProtocol() }
}
async function beginAuth() {
  if (activeID.value === null || saving.value) return
  const id = activeID.value; saving.value = true; saveError.value = ''; attempt.value = null
  authGeneration++
  try { const result = await backend().BeginAuthorization(id); if (modal.value === 'auth' && activeID.value === id) attempt.value = result }
  catch (e) { saveError.value = failure(e) } finally { saving.value = false }
}
const modal = ref<'seat' | 'people' | 'auth' | 'delete' | null>(null), activeID = ref<number | null>(null)
const dialog = ref<HTMLDialogElement | null>(null)
const draft = ref<{ mode: 'tier' | 'area'; selected: string[] }>({ mode: 'tier', selected: [] })
const active = computed(() => view.value.tasks.find(t => t.id === activeID.value))
const areas = computed(() => view.value.catalog.tiers.flatMap(t => t.areas.filter(a => a.isSaleable).map(a => ({ ...a, tier: t.tier, price: t.price }))))
const groups = computed(() => ['首层', '顶层', 'VIP'].map(name => ({ name, areas: areas.value.filter(a => name === 'VIP' ? a.tier === 'VIP' : a.tier !== 'VIP' && a.level === (name === '首层' ? '首层看台' : '顶层看台')) })))
const selectedLabel = (t: Task) => t.selected.join('、') || '未配置'
const search = ref('')
const searchTerms = computed(() => search.value.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean))
const filteredTasks = computed(() => view.value.tasks.filter(task => {
  const text = [String(task.id), task.wechatNickname || '', ...(task.spectatorNames || [])].join(' ').toLocaleLowerCase()
  return searchTerms.value.every(term => text.includes(term))
}))
const clock = (at: string) => new Date(at).toLocaleTimeString('zh-CN', { hour12: false })
const failure = (e: unknown) => e instanceof Error ? e.message : String(e)
let alive = true
let pendingSeatSave = false
const tip = ref<{ text: string; left: number; top: number } | null>(null)
function tooltip(event: MouseEvent | FocusEvent, text: string) {
  if (!text || modal.value) return
  const r = (event.currentTarget as HTMLElement).getBoundingClientRect()
  tip.value = { text, left: Math.max(8, Math.min(r.left, window.innerWidth - Math.min(340, window.innerWidth - 16) - 8)), top: r.bottom + 6 }
}
async function load() { const data = await backend().GetTasks(); if (alive) view.value = data }
onMounted(async () => { try { await load(); await pollProtocol() } catch (e) { error.value = failure(e) } finally { loading.value = false }; protocolTimer = setInterval(() => { now.value = Date.now(); void pollProtocol() }, 2000) })
onUnmounted(() => { alive = false; if (protocolTimer) clearInterval(protocolTimer); dialog.value?.close(); controlDialog.value?.close() })
async function createTask() {
  if (creating.value) return
  creating.value = true; error.value = ''
  try { const task = await backend().CreateTask(); if (alive) view.value.tasks.push(task) } catch (e) { error.value = failure(e) } finally { creating.value = false }
}
async function open(t: Task, kind: NonNullable<typeof modal.value>) {
  activeID.value = t.id; modal.value = kind; saveError.value = ''; tip.value = null
  draft.value = { mode: t.mode, selected: [...t.selected] }
  await nextTick(); dialog.value?.showModal()
  if (kind === 'auth') await beginAuth()
}
function close(discard = false) {
  if (saving.value || (modal.value === 'seat' && saveError.value && !discard)) return
  dialog.value?.close(); modal.value = null; activeID.value = null; saveError.value = ''
}
async function saveSeats() {
  if (activeID.value === null) return
  pendingSeatSave = true
  if (saving.value) return
  saving.value = true; saveError.value = ''
  const id = activeID.value
  try {
    while (pendingSeatSave && alive) {
      pendingSeatSave = false
      const mode = draft.value.mode, selected = [...draft.value.selected]
      await backend().SaveTaskSeats(id, mode, selected)
      const task = view.value.tasks.find(t => t.id === id)
      if (task) { task.mode = mode; task.selected = selected; task.seatValid = selected.length > 0; task.latest = { at: new Date().toISOString(), text: '已保存座位要求' } }
    }
  } catch (e) { pendingSeatSave = false; if (alive) saveError.value = failure(e) } finally { saving.value = false }
}
function setMode(mode: 'tier' | 'area') {
  if (saving.value || mode === draft.value.mode) return
  draft.value.selected = mode === 'area' ? areas.value.filter(a => draft.value.selected.includes(a.tier)).map(a => a.code) : [...new Set(areas.value.filter(a => draft.value.selected.includes(a.code)).map(a => a.tier))]
  draft.value.mode = mode; void saveSeats()
}
function toggle(value: string, checked: boolean) {
  draft.value.selected = checked ? [...new Set([...draft.value.selected, value])] : draft.value.selected.filter(v => v !== value)
  void saveSeats()
}
function toggleGroup(codes: string[], checked: boolean) {
  draft.value.selected = checked ? [...new Set([...draft.value.selected, ...codes])] : draft.value.selected.filter(v => !codes.includes(v))
  void saveSeats()
}
async function deleteTask() {
  if (saving.value || activeID.value === null) return
  saving.value = true; saveError.value = ''
  try { const id = activeID.value; await backend().DeleteTask(id); view.value.tasks = view.value.tasks.filter(t => t.id !== id); saving.value = false; close() }
  catch (e) { saveError.value = failure(e) } finally { saving.value = false }
}
</script>

<template>
  <main class="tw" aria-label="任务工作台">
    <header class="tw-chrome"><div class="tw-brand">ROBOT <span>/ LOCAL AGENT</span></div><div class="tw-session"><span :class="{ disconnected: state.connection !== '已连接' }">{{ state.connection }}</span><span>客户端 {{ state.clientNumber }}</span><button class="tw-link" @click="$emit('logout')">退出登录</button></div></header>
    <section class="tw-body">
      <div class="tw-heading"><div><h1>任务</h1><p>{{ view.catalog.stadium || '座位目录未加载' }} <span class="tw-divider">/</span> {{ view.tasks.length }} 个任务</p></div><button class="tw-primary" :disabled="creating || loading" @click="createTask">＋ {{ creating ? '正在创建…' : '新建任务' }}</button></div>
      <section class="tw-service" aria-label="协议服务">
        <div><strong>{{ props.state.authProtocol === 'yyb' ? 'YYB' : '微信' }}协议</strong><span>{{ serviceLabels[protocol?.state || ''] || '检查中' }}</span><small>{{ protocol?.address }} · {{ protocol?.proxy || '直连' }}</small></div>
        <div><button :disabled="protocolBusy || protocol?.state === 'running'" @click="serviceAction('start')">启动协议</button><button :disabled="protocolBusy" @click="serviceAction('restart')">重启</button><button :disabled="protocolBusy || protocol?.state === 'stopped'" @click="serviceAction('stop')">关闭协议</button></div>
        <p v-if="protocol?.message || protocolError" role="alert">{{ protocolError || protocol?.message }}</p>
      </section>
      <div v-if="error" class="tw-alert" role="alert">{{ error }} <button class="tw-link" @click="load().then(() => error = '').catch(e => error = failure(e))">重试读取</button></div>
      <div v-if="view.configError" class="tw-alert" role="alert">{{ view.configError }}</div>
      <div v-if="state.notice" class="tw-alert">{{ state.notice }}</div>
      <div class="tw-toolbar" role="search" aria-label="任务搜索">
        <div class="tw-search"><input v-model="search" type="search" aria-label="搜索任务" placeholder="搜索任务编号、微信昵称或观演人" autocomplete="off" :disabled="loading" @input="tip = null"/><button v-if="search" class="tw-search-clear" type="button" aria-label="清空搜索" @click="search = ''; tip = null">清空</button></div>
        <span class="tw-search-count" role="status" aria-live="polite">{{ loading ? '正在读取…' : searchTerms.length ? `找到 ${filteredTasks.length} / ${view.tasks.length} 个任务` : `共 ${view.tasks.length} 个任务` }}</span>
      </div>
      <div class="tw-columns" aria-hidden="true"><span>编号</span><span>微信账号</span><span>座位要求</span><span>观演人</span><span>最新日志</span><span></span></div>
      <div v-if="loading" class="tw-empty" role="status">正在读取本地任务…</div>
      <div v-else-if="!view.tasks.length && !error" class="tw-empty"><h2>创建你的第一个任务</h2><p>座位默认选择 A、B、C、D、E，微信账号可稍后授权。</p><button class="tw-primary" :disabled="creating" @click="createTask">＋ 新建任务</button></div>
      <div v-else-if="view.tasks.length && !filteredTasks.length" class="tw-empty"><h2>没有匹配的任务</h2><p>试试其他任务编号、微信昵称或观演人姓名。</p><button @click="search = ''">清空搜索</button></div>
      <div class="tw-list">
        <article v-for="task in filteredTasks" :key="task.id" class="tw-task" :aria-label="`任务 ${task.id}`">
          <span class="tw-number">{{ task.id }}</span>
          <div class="tw-account"><img v-if="task.protocolAvatar" class="tw-account-avatar" :src="task.protocolAvatar" alt="" @error="($event.target as HTMLImageElement).hidden = true"/><strong>{{ task.wechatNickname || (task.protocolStatus ? '已绑定账号' : '尚未授权') }}</strong><span>{{ task.paused ? '任务已暂停 · ' : '' }}{{ task.protocolStatus === 'alive' ? '协议账号已登录' : task.protocolStatus === 'expired' ? '需要重新授权' : task.protocolStatus ? '登录态待检查' : '扫码后绑定微信账号' }}</span><button class="tw-link" @click="open(task, 'auth')">{{ task.protocolStatus ? '重新授权' : '开始授权' }} · {{ state.authProtocol === 'yyb' ? 'YYB' : '微信' }}</button></div>
          <button class="tw-summary tw-seats" aria-haspopup="dialog" @mouseenter="tooltip($event, `${task.mode === 'tier' ? '指定票类' : '固定区域'}：${selectedLabel(task)}`)" @mouseleave="tip = null" @focus="tooltip($event, selectedLabel(task))" @blur="tip = null" @click="open(task, 'seat')"><span class="tw-field">{{ task.mode === 'tier' ? '指定票类' : '固定区域' }} ↗</span><span class="tw-value">{{ selectedLabel(task) }}</span></button>
          <button class="tw-summary tw-people" aria-haspopup="dialog" @click="open(task, 'people')"><span class="tw-field">观演人 · 0 人 ↗</span><span class="tw-value">选择观演人</span></button>
          <div class="tw-log" @mouseenter="tooltip($event, task.latest?.text || '')" @mouseleave="tip = null"><template v-if="task.latest"><time :datetime="task.latest.at">{{ clock(task.latest.at) }}</time><span>{{ task.latest.text }}</span></template></div>
          <button class="tw-delete" :aria-label="`删除任务 ${task.id}`" @click="open(task, 'delete')">删除</button>
        </article>
      </div>
      <footer class="tw-footer"><span>配置自动保存 · 当前客户端共享</span><span>深海石墨</span></footer>
    </section>
    <div v-if="tip" role="tooltip" class="tw-tooltip" :style="{ left: `${tip.left}px`, top: `${tip.top}px` }">{{ tip.text }}</div>
    <dialog ref="dialog" class="tw-dialog" :class="{ wide: modal === 'seat' }" aria-labelledby="tw-dialog-title" @cancel.prevent="close()">
      <template v-if="modal && active">
        <div class="tw-dialog-head"><div><h2 id="tw-dialog-title">{{ modal === 'seat' ? '座位要求' : modal === 'people' ? '观演人' : modal === 'delete' ? '删除任务' : '授权账号' }}</h2><p>编号 {{ active.id }}<template v-if="modal === 'seat'"> · {{ view.catalog.stadium }}</template></p></div><button class="tw-close" aria-label="关闭弹窗" :disabled="saving || (modal === 'seat' && !!saveError)" @click="close()">×</button></div>
        <div class="tw-dialog-content">
          <div v-if="saveError" class="tw-alert" role="alert">{{ saveError }}<div class="tw-error-actions"><button v-if="modal === 'seat'" @click="saveSeats">重试保存</button><button @click="close(true)">放弃未保存更改</button></div></div>
          <template v-if="modal === 'seat'">
            <div v-if="view.configError" class="tw-alert">{{ view.configError }}</div>
            <div class="tw-switch" role="group" aria-label="座位选择模式"><button :aria-pressed="draft.mode === 'tier'" :disabled="!!view.configError" @click="setMode('tier')">指定票类</button><button :aria-pressed="draft.mode === 'area'" :disabled="!!view.configError" @click="setMode('area')">固定区域</button></div>
            <fieldset :disabled="!!view.configError" class="tw-options" aria-label="座位选项">
              <template v-if="draft.mode === 'tier'"><div class="tw-tier-options"><label v-for="tier in view.catalog.tiers" :key="tier.tier" class="tw-choice" :class="{ checked: draft.selected.includes(tier.tier) }"><input type="checkbox" :checked="draft.selected.includes(tier.tier)" @change="toggle(tier.tier, ($event.target as HTMLInputElement).checked)"/><span>{{ tier.tier }}<small>¥{{ tier.price }}</small></span></label></div><p class="tw-note">支持跨档组合，选择后自动保存。</p><p v-if="!view.catalog.tiers.length" class="tw-note">暂无可选票类。</p></template>
              <template v-else><section v-for="group in groups" :key="group.name" class="tw-area-group"><label class="tw-group-heading"><input type="checkbox" :disabled="!group.areas.length" :checked="group.areas.length > 0 && group.areas.every(a => draft.selected.includes(a.code))" :indeterminate="group.areas.some(a => draft.selected.includes(a.code)) && !group.areas.every(a => draft.selected.includes(a.code))" @change="toggleGroup(group.areas.map(a => a.code), ($event.target as HTMLInputElement).checked)"/><span>{{ group.name }}</span><small>全选 · {{ group.areas.length }} 个看台</small></label><div class="tw-area-options"><label v-for="area in group.areas" :key="area.code" class="tw-choice tw-area" :class="{ checked: draft.selected.includes(area.code) }" :title="`${area.code} · ${area.tier}类 ¥${area.price}${area.specialArea ? ' · ' + area.specialArea : ''}`"><input type="checkbox" :aria-label="`${area.code}，${area.tier}类，${area.price}元，${area.specialArea}`" :checked="draft.selected.includes(area.code)" @change="toggle(area.code, ($event.target as HTMLInputElement).checked)"/><span class="tw-area-copy"><span class="tw-area-top"><span class="tw-area-code">{{ area.code }}</span><span v-if="area.specialArea" class="tw-special">{{ area.specialArea }}</span></span><small>{{ area.tier }} · ¥{{ area.price }}</small></span></label></div></section></template>
            </fieldset>
          </template>
          <template v-else-if="modal === 'people'"><div class="tw-unavailable"><h3>请先授权微信账号</h3><p>授权成功后可获取并勾选账号下的观演人。</p><p>微信／YYB 协议尚未接入，暂时无法获取或刷新观演人。</p><button @click="open(active, 'auth')">前往授权</button></div></template>
          <template v-else-if="modal === 'auth'"><div class="tw-auth"><h3>{{ state.authProtocol === 'yyb' ? 'YYB' : '微信' }}扫码授权</h3><img v-if="safeImage" :src="safeImage" alt="微信授权二维码"/><p role="status">{{ authLabels[attempt?.status || ''] || (saving ? '正在创建扫码会话…' : '等待获取二维码') }}</p><p v-if="attempt?.message">{{ attempt.message }}</p><p v-if="attempt && !['confirmed','failed','cancelled','expired','timeout'].includes(attempt.status)">剩余 {{ Math.floor(remaining / 60) }} 分 {{ remaining % 60 }} 秒</p><button :disabled="saving" @click="beginAuth">重新获取二维码</button><p>关闭弹窗后继续等待，最多 5 分钟；重新打开会重新取码并计时。新授权成功前保留原绑定。</p><p>{{ active.protocolStatus === 'alive' ? '协议账号已登录' : '协议账号尚未确认' }} · 业务授权尚未接入</p></div></template>
          <template v-else><p class="tw-delete-note">确认删除 {{ active.id }}？此任务的配置和本地授权信息将被清除，删除不可恢复，编号不再使用。协议支持时尝试退出账号。</p></template>
        </div>
        <div class="tw-dialog-footer"><span role="status" aria-live="polite">{{ saving ? '正在保存…' : saveError ? '保存失败，修改尚未落盘' : modal === 'seat' ? '修改自动保存' : '' }}</span><div><template v-if="modal === 'delete'"><button :disabled="saving" @click="close()">取消</button><button class="tw-danger" :disabled="saving" @click="deleteTask">确认删除</button></template><button v-else class="tw-primary" :disabled="saving || (modal === 'seat' && !!saveError)" @click="close()">{{ modal === 'seat' ? '完成' : '关闭' }}</button></div></div>
      </template>
    </dialog>
    <dialog ref="controlDialog" class="tw-dialog" aria-labelledby="service-confirm" @cancel.prevent="!protocolBusy && controlDialog?.close()">
      <div class="tw-dialog-head"><h2 id="service-confirm">确认{{ controlAction === 'stop' ? '关闭' : '重启' }}协议</h2></div>
      <div class="tw-dialog-content"><p>受影响任务：{{ protocol?.affected.length ? protocol.affected.join('、') : '暂无已绑定任务' }}。正在等待的扫码也会取消。执行后将取消相关请求并暂停任务，账号凭据保留。</p><p v-if="controlAction === 'restart'">服务恢复后重新检查账号，任务保持暂停。</p><p v-if="protocolError" class="tw-alert" role="alert">{{ protocolError }}</p></div>
      <div class="tw-dialog-footer"><span>{{ protocolBusy ? '处理中…' : '' }}</span><div><button :disabled="protocolBusy" @click="controlDialog?.close()">取消</button><button class="tw-primary" :disabled="protocolBusy" @click="controlService">确认{{ controlAction === 'stop' ? '关闭' : '重启' }}</button></div></div>
    </dialog>
  </main>
</template>

<style scoped>
.tw-service{display:flex;flex-wrap:wrap;justify-content:space-between;align-items:center;gap:12px;background:var(--panel);border:1px solid var(--line);border-radius:8px;padding:14px;margin-bottom:18px}.tw-service>div{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.tw-service span,.tw-service small{color:var(--muted);font-size:12px}.tw-service p{width:100%;font-size:12px;color:var(--app-warning)}.tw-auth{text-align:center}.tw-auth img{display:block;width:220px;max-width:100%;height:auto;margin:14px auto;background:white}.tw-auth p{font-size:13px;margin:12px 0;overflow-wrap:anywhere}
.tw{--bg:var(--app-bg);--panel:var(--app-panel);--ink:var(--app-ink);--muted:var(--app-muted);--line:var(--app-line);--soft:var(--app-soft);--active:var(--app-accent);--selected:var(--app-selected);max-width:none;min-height:100vh;margin:0;padding:0;background:var(--bg);color:var(--ink);font:14px/1.5 "Segoe UI","Microsoft YaHei",sans-serif}
.tw button,.tw select{font:inherit}.tw button{color:var(--ink);background:var(--panel);border:1px solid var(--line);border-radius:6px;padding:7px 11px}.tw button:hover:not(:disabled){background:var(--soft)}.tw button:focus-visible,.tw input:focus-visible,.tw select:focus-visible{outline:2px solid var(--active);outline-offset:3px}.tw button.tw-primary{background:var(--active);border-color:var(--active);color:var(--app-on-accent)}.tw button.tw-danger{background:var(--app-danger);border-color:var(--app-danger);color:var(--app-on-danger)}.tw button:disabled{cursor:default;opacity:.55}.tw h1,.tw h2,.tw h3,.tw p{margin:0;letter-spacing:normal}.tw h1{font-size:22px;font-weight:600}.tw h2{font-size:17px;font-weight:600}.tw h3{font-size:14px;font-weight:600}.tw p{color:var(--muted);line-height:1.6}.tw label{color:inherit;font-size:inherit;margin:0}.tw input[type=checkbox]{accent-color:var(--active);width:15px;height:15px;margin:0;flex-shrink:0}.tw-chrome{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;padding:18px 24px;background:var(--panel);border-bottom:1px solid var(--line)}.tw-brand{font-weight:600;letter-spacing:.06em}.tw-brand span{font-size:11px;font-weight:400;color:var(--muted)}.tw-session{display:flex;align-items:center;gap:16px;font-size:12px;color:var(--muted)}.tw-session .disconnected{color:var(--app-warning)}.tw button.tw-link{padding:0;border:0;background:transparent;color:var(--active);font-size:12px;text-align:left}.tw-body{padding:26px 22px 18px}.tw-heading{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:24px}.tw-heading p{font-size:12px;margin-top:4px}.tw-divider{margin:0 8px;color:var(--line)}.tw-columns,.tw-task{display:grid;grid-template-columns:40px minmax(110px,1fr) minmax(150px,1.35fr) minmax(105px,1fr) minmax(125px,1.4fr) 48px;gap:12px;align-items:center}.tw-columns{padding:0 14px 9px;font-size:12px;color:var(--muted)}.tw-task{padding:14px;height:96px;background:var(--panel);border:1px solid var(--line);border-radius:8px;margin-bottom:10px}.tw-task>*{min-width:0}.tw-number{font-size:17px;color:var(--muted);font-variant-numeric:tabular-nums}.tw-account{position:relative;padding-left:38px;display:flex;flex-direction:column;align-items:flex-start;gap:3px;overflow:hidden}.tw-account-avatar{position:absolute;left:0;top:2px;width:30px;height:30px;object-fit:cover;border-radius:50%;background:var(--soft)}.tw-account strong{font-weight:600;max-width:100%;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.tw-account>span{font-size:12px;color:var(--muted);max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.tw button.tw-summary{height:60px;width:100%;background:var(--soft);border-color:transparent;text-align:left;padding:8px 9px;min-width:0}.tw button.tw-summary:hover{border-color:var(--active);background:var(--selected)}.tw-field,.tw-value{display:block;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.tw-field{font-size:12px;color:var(--muted);margin-bottom:3px}.tw-log{font-size:12px;color:var(--muted);overflow:hidden}.tw-log time{display:block;font-size:11px;margin-bottom:3px}.tw-log>span{display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}.tw button.tw-delete{border:0;background:transparent;color:var(--muted);font-size:12px;padding:5px 0;white-space:nowrap}.tw-footer{display:flex;justify-content:space-between;gap:12px;border:0;padding-top:6px;font-size:12px;color:var(--muted)}.tw-empty{text-align:center;padding:65px 16px;background:var(--panel);border:1px solid var(--line);border-radius:8px;margin-bottom:14px}.tw-empty p{margin:10px 0 22px}.tw-alert{border:1px solid var(--app-warning-line);background:var(--app-warning-bg);color:var(--app-warning);font-size:12px;padding:10px 12px;border-radius:6px;margin-bottom:16px;overflow-wrap:anywhere}.tw-error-actions{display:flex;gap:8px;flex-wrap:wrap;margin-top:8px}.tw-tooltip{position:fixed;max-width:min(340px,calc(100vw - 16px));max-height:240px;overflow:auto;background:var(--panel);border:1px solid var(--line);color:var(--ink);box-shadow:0 5px 18px var(--app-shadow);border-radius:7px;padding:10px 12px;font-size:12px;overflow-wrap:anywhere;z-index:20;pointer-events:none}.tw-dialog{padding:0;color:var(--ink);background:var(--panel);border:1px solid var(--line);border-radius:12px;width:420px;max-width:calc(100vw - 24px);max-height:85vh;box-shadow:0 15px 50px var(--app-shadow)}.tw-dialog.wide{width:780px}.tw-dialog::backdrop{background:var(--app-backdrop)}.tw-dialog-head{position:sticky;top:0;z-index:2;background:var(--panel);padding:18px 20px;border-bottom:1px solid var(--line);display:flex;align-items:center;justify-content:space-between;gap:12px}.tw-dialog-head p{font-size:12px;margin-top:4px}.tw button.tw-close{border:0;padding:5px 9px;color:var(--muted)}.tw-dialog-content{padding:20px}.tw-dialog-footer{position:sticky;bottom:0;background:var(--panel);border-top:1px solid var(--line);padding:14px 20px;display:flex;align-items:center;justify-content:space-between;gap:12px;font-size:12px;color:var(--muted);z-index:2}.tw-dialog-footer>div{display:flex;gap:8px}.tw-switch{display:inline-flex;border:1px solid var(--line);border-radius:7px;padding:3px;gap:2px;margin-bottom:16px}.tw-switch button{border:0;background:transparent;padding:5px 12px;font-size:13px}.tw-switch button[aria-pressed=true]{color:var(--app-on-accent);background:var(--active)}.tw-options{border:0;padding:0;margin:0;min-width:0}.tw-tier-options{display:flex;gap:8px;flex-wrap:wrap}.tw label.tw-choice{display:flex;align-items:center;gap:7px;border:1px solid var(--line);background:var(--panel);padding:9px 12px;border-radius:6px;min-width:90px;cursor:pointer}.tw label.tw-choice.checked{background:var(--selected);border-color:var(--active)}.tw-choice small{display:block;font-size:11px;color:var(--muted)}.tw-note{font-size:12px;margin-top:12px!important}.tw-area-group+.tw-area-group{margin-top:16px}.tw label.tw-group-heading{display:flex;align-items:center;gap:8px;font-size:13px;margin-bottom:8px;cursor:pointer}.tw-group-heading small{color:var(--muted);font-size:12px}.tw-area-options{display:grid;grid-template-columns:repeat(auto-fill,minmax(140px,1fr));gap:7px}.tw label.tw-area{height:56px;min-width:0;padding:7px 8px}.tw-area-copy{flex:1;min-width:0}.tw-area-top{display:flex;gap:6px;align-items:center;min-width:0;white-space:nowrap}.tw-area-code{flex-shrink:0}.tw-special{font-size:11px;color:var(--muted);min-width:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.tw-area small{white-space:nowrap}.tw-unavailable{background:var(--soft);padding:18px;border-radius:7px}.tw-unavailable p{font-size:13px;margin:8px 0 14px}.tw-delete-note{font-size:13px}.tw-protocol{display:block;margin-bottom:20px!important;font-size:13px!important}.tw select{display:block;width:100%;margin-top:8px;padding:9px;background:var(--panel);color:var(--ink);border:1px solid var(--line);border-radius:6px}
.tw button.tw-primary:hover:not(:disabled){background:var(--app-accent-hover)}.tw button.tw-danger:hover:not(:disabled){background:var(--app-danger-hover)}
.tw-toolbar{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;margin-bottom:18px}.tw-search{position:relative;flex:1;max-width:420px;min-width:0}.tw .tw-search input{display:block;width:100%;min-width:0;margin:0;padding:10px 58px 10px 12px;border:1px solid var(--line);border-radius:7px;background:var(--panel);color:var(--ink);font:inherit;appearance:none}.tw-search input::-webkit-search-cancel-button{appearance:none}.tw .tw-search input::placeholder{color:var(--muted);font-size:12px}.tw .tw-search input:hover:not(:disabled){border-color:var(--active)}.tw button.tw-search-clear{position:absolute;right:5px;top:50%;transform:translateY(-50%);padding:5px 8px;border:0;background:transparent;color:var(--active);font-size:12px}.tw-search-count{color:var(--muted);font-size:12px;white-space:nowrap}
@media(max-width:480px){.tw-search{flex-basis:100%;max-width:none}.tw-toolbar{gap:8px}}
@media(max-width:760px){.tw-chrome{padding:16px;gap:12px}.tw-session{width:100%;gap:12px;flex-wrap:wrap}.tw-body{padding:20px 12px 14px}.tw-columns{display:none}.tw-task{height:190px;grid-template-columns:30px minmax(0,1fr) minmax(0,1fr) 34px;grid-template-rows:66px 60px 32px;align-content:center;gap:10px}.tw-number{grid-row:1}.tw-account{grid-column:2/4}.tw-seats{grid-column:2}.tw-people{grid-column:3}.tw-delete{grid-column:4;grid-row:1}.tw-log{grid-column:2/5;display:flex;gap:6px;align-items:center}.tw-log time{flex-shrink:0;margin:0}.tw-log>span{-webkit-line-clamp:1}.tw-dialog-head,.tw-dialog-content,.tw-dialog-footer{padding:16px}.tw-area-options{grid-template-columns:repeat(auto-fill,minmax(125px,1fr))}.tw-footer{font-size:11px}}
@media(pointer:coarse){.tw button,.tw label.tw-choice,.tw-group-heading{min-height:44px}.tw input,.tw select{font-size:16px}.tw button.tw-delete{min-width:44px;justify-self:end}}
</style>
