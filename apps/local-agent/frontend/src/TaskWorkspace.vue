<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import InventoryPanel from './InventoryPanel.vue'
import TaskToast from './TaskToast.vue'
import type { InventoryView, OrderDisplay } from './bridge'
import ScanAuthorization from './ScanAuthorization.vue'
import { confirmDialog } from './dialogKeyboard'
import { backend, type State, type Person, type Task, type TaskView, type ProtocolStatus, type AuthAttempt, type AddressForm } from './bridge'
const inventoryView = ref<InventoryView | null>(null)
const historyOrders = ref<OrderDisplay[]>([])
const orderSearch = ref('')
const ordersDialog = ref<HTMLDialogElement | null>(null)
const ordersBusy = ref(false), ordersError = ref('')
const selectedOrder=ref<OrderDisplay|null>(null)
const filteredOrders = computed(() => {
 const needle = orderSearch.value.trim().toLocaleLowerCase()
 if (!needle) return historyOrders.value
 return historyOrders.value.filter(order => order.viewers.some(viewer => viewer.name.toLocaleLowerCase().includes(needle)))
})
async function showOrders() {
 ordersBusy.value = true; ordersError.value = ''
 try { historyOrders.value = await backend().GetOrderHistory() }
 catch (e) { ordersError.value = failure(e) } finally { ordersBusy.value = false }
}
async function deleteOrder(record: OrderDisplay) {
 if (!window.confirm(`删除本地订单 ${record.id}、对应 token 和详情？不会取消或更改官方订单，此操作不可撤销。`)) return
 ordersBusy.value = true; ordersError.value = ''
 try { await backend().DeleteOrderRecord(record.taskId, record.id); historyOrders.value = historyOrders.value.filter(o => !(o.taskId === record.taskId && o.id === record.id)) }
 catch (e) { ordersError.value = failure(e) } finally { ordersBusy.value = false }
}
async function deleteAllOrders() {
 const count = historyOrders.value.length
 if (!count || ordersBusy.value) return
 if (!window.confirm(`将删除全部 ${count} 条本地订单记录及对应 token 和详情，不会取消或更改官方订单。此操作不可撤销。确定全部删除吗？`)) return
 ordersBusy.value = true; ordersError.value = ''
 try { await backend().DeleteAllOrderRecords(); historyOrders.value = []; selectedOrder.value = null }
 catch (e) { ordersError.value = failure(e) } finally { ordersBusy.value = false }
}
async function openOrder(record: OrderDisplay) {
 selectedOrder.value=record;ordersError.value='';await nextTick();ordersDialog.value?.showModal()
}
async function refreshOrder(record: OrderDisplay) {
 if (ordersBusy.value) return
 ordersBusy.value=true;ordersError.value=''
 try { await backend().RefreshOrderDetail(record.taskId,record.id);historyOrders.value=await backend().GetOrderHistory();if(selectedOrder.value?.taskId===record.taskId&&selectedOrder.value.id===record.id)selectedOrder.value=historyOrders.value.find(o=>o.taskId===record.taskId&&o.id===record.id)||selectedOrder.value }
 catch(e) { ordersError.value=failure(e);try{historyOrders.value=await backend().GetOrderHistory()}catch{} }
 finally { ordersBusy.value=false }
}
async function runSimulation(task: Task) {
 if (ordersBusy.value || running(task.id)) return
 ordersBusy.value=true
 try { await backend().StartSimulation(task.id); inventoryView.value=await backend().GetInventory() }
 catch(e) { error.value=failure(e) }
 finally { ordersBusy.value=false }
}
async function refreshSelectedOrder(){if(!selectedOrder.value||ordersBusy.value)return;ordersBusy.value=true;ordersError.value='';try{await backend().RefreshOrderDetail(selectedOrder.value.taskId,selectedOrder.value.id);historyOrders.value=await backend().GetOrderHistory();selectedOrder.value=historyOrders.value.find(o=>o.id===selectedOrder.value?.id&&o.taskId===selectedOrder.value?.taskId)||selectedOrder.value}catch(e){ordersError.value=failure(e)}finally{ordersBusy.value=false}}
async function afterSale(action:'cancel'|'refund') {let o=selectedOrder.value;if(!o||ordersBusy.value)return;ordersBusy.value=true;ordersError.value='';try{await backend().RefreshOrderDetail(o.taskId,o.id);historyOrders.value=await backend().GetOrderHistory();o=historyOrders.value.find(x=>x.id===o?.id&&x.taskId===o?.taskId)||o;selectedOrder.value=o;const d=o.details;if(action==='cancel'&&d.status!==5)throw new Error('订单最新状态不是待支付，无法取消');if(action==='refund'&&(d.status!==6||d.pay_type!=='微信支付'||!d.refundEnabled))throw new Error('订单最新状态不符合整单退款条件');const message=action==='cancel'?`订单 ${o.id} 当前待支付，确认取消整个订单？`:`订单 ${o.id} 当前符合整单退款条件，确认申请退掉本订单全部座位？`;if(!window.confirm(message))return;await backend().OrderAfterSale(o.taskId,o.id,action);historyOrders.value=await backend().GetOrderHistory();selectedOrder.value=historyOrders.value.find(x=>x.id===o?.id&&x.taskId===o?.taskId)||o}catch(e){ordersError.value=failure(e);if(selectedOrder.value){try{historyOrders.value=await backend().GetOrderHistory();selectedOrder.value=historyOrders.value.find(x=>x.id===selectedOrder.value?.id&&x.taskId===selectedOrder.value?.taskId)||selectedOrder.value}catch{}}}finally{ordersBusy.value=false}}
const executionBusy = ref<number[]>([])
type BulkAction = 'start' | 'simulate' | 'stop' | 'refresh'
const bulkAction = ref<BulkAction | null>(null), bulkDone = ref(0), bulkTotal = ref(0)
const bulkReport = ref(''), bulkErrors = ref<string[]>([])
const bulkLabels: Record<BulkAction, string> = { simulate: '批量模拟验收', start: '全部启动', stop: '全部停止', refresh: '全部刷新' }
const bulkBlocked = computed(() => !!bulkAction.value || loading.value || creating.value || saving.value || protocolBusy.value || executionBusy.value.length > 0 || refreshingTasks.value.length > 0)
function bulkTargets(action: BulkAction) {
 return view.value.tasks.filter(task => {
  if (action === 'stop') return !!inventoryView.value?.runs[task.id] && !['stopped','order_created','order_verifying','order_blocked'].includes(inventoryView.value.runs[task.id].status)
  if ((action !== 'refresh' && running(task.id)) || (action === 'start' && inventoryView.value?.runs[task.id]?.status === 'order_created')) return false
  if (action === 'simulate') return !!inventoryView.value?.debugMode && !!inventoryView.value.simulation.enabled && !running(task.id) && task.businessStatus === 'ready' && task.seatValid && !!task.peopleSelected?.length
  if (action === 'refresh') return !['order_verifying','order_blocked','order_review','stopping'].includes(inventoryView.value?.runs[task.id]?.status || '') && task.protocolStatus === 'alive' && task.businessStatus !== 'loading'
  return !!inventoryView.value?.clockReady && !!inventoryView.value?.match.enabled && !view.value.configError && !task.paused && task.protocolStatus === 'alive' && task.businessStatus === 'ready' && task.seatValid && task.selected.length > 0 && task.peopleUsable && task.peopleComplete !== false && !!task.peopleSelected?.length
 })
}
async function runBulk(action: BulkAction) {
 if (bulkBlocked.value) return
 const targets = bulkTargets(action)
 if (!targets.length) return
 const skipped = view.value.tasks.length - targets.length
 bulkAction.value = action; bulkDone.value = 0; bulkTotal.value = targets.length; bulkReport.value = ''; bulkErrors.value = []
 let succeeded = 0
 try {
  for (const task of targets) {
   if (!alive) return
   try {
    if (action === 'simulate') await backend().StartSimulation(task.id)
    else if (action === 'start') await backend().StartInventory(task.id)
    else if (action === 'stop') await backend().StopInventory(task.id)
    else await backend().RefreshTaskPeople(task.id, true)
    succeeded++
   } catch (e) { bulkErrors.value.push(`任务 ${task.id}：${failure(e)}`) }
   bulkDone.value++
  }
  const results = await Promise.allSettled([backend().GetTasks(), backend().GetInventory()])
  if (!alive) return
  if (results[0].status === 'fulfilled') updateView(results[0].value)
  else bulkErrors.value.push(`任务状态同步失败：${failure(results[0].reason)}`)
  if (results[1].status === 'fulfilled') inventoryView.value = results[1].value
  else bulkErrors.value.push(`执行状态同步失败：${failure(results[1].reason)}`)
  bulkReport.value = `${bulkLabels[action]}完成：${action === 'refresh' ? '已提交刷新' : '成功'} ${succeeded} 个${targets.length - succeeded ? `，失败 ${targets.length - succeeded} 个` : ''}${skipped ? `，跳过 ${skipped} 个不符合条件的任务` : ''}`
 } finally { bulkAction.value = null }
}
const executionLabels: Record<string,string> = {waiting:'等待开售',monitoring:'库存监控中',ready:'待下单',pending_order:'等待下单参数',submitting:'正在创建订单',stopping:'正在停止，等待下单结果',maintenance:'维护中',maintenance_failed:'维护失败',order_cooldown:'等待恢复查询',order_created:'创建成功／待支付',order_review:'下单待核实',order_verifying:'订单核实中',order_blocked:'购票已暂停',stopped:'已停止',paused:'异常暂停'}
function running(id: number) { return ['waiting','monitoring','ready','pending_order','submitting','stopping','maintenance','maintenance_failed','order_cooldown','order_review','order_verifying','order_blocked'].includes(inventoryView.value?.runs[id]?.status || '') }
async function toggleExecution(task: Task) {
 if(bulkAction.value || executionBusy.value.includes(task.id)) return
 const run = inventoryView.value?.runs[task.id]
 if (run?.status === 'order_created') { await prepareRepurchase(task); return }
 if (run?.status === 'order_verifying' || run?.status === 'order_blocked') { await recoverRun(task.id, run.status); return }
 executionBusy.value.push(task.id); error.value = ''
 try { if(running(task.id)) await backend().StopInventory(task.id); else await backend().StartInventory(task.id); inventoryView.value = await backend().GetInventory(); await load() }
 catch(e) { error.value = `任务 ${task.id}：${failure(e)}` } finally {executionBusy.value=executionBusy.value.filter(id=>id!==task.id)}
}
async function prepareRepurchase(task: Task) {
 if (executionBusy.value.includes(task.id)) return
 executionBusy.value.push(task.id); error.value = ''
 try {
  await backend().PrepareRepurchase(task.id, [...(task.peopleSelected || [])], [...task.selected], task.mode)
  inventoryView.value = await backend().GetInventory()
  await load()
 } catch (e) { error.value = `任务 ${task.id}：${failure(e)}` }
 finally { executionBusy.value = executionBusy.value.filter(id => id !== task.id) }
}
async function recoverRun(id: number, status?: string) {
 if (saving.value || executionBusy.value.includes(id)) return
 const action = status === 'order_verifying' ? 'confirm-not-created' : 'resolved'
 executionBusy.value.push(id); error.value = ''
 try { await backend().RecoverInventory(id, action); inventoryView.value = await backend().GetInventory(); await load() }
 catch (e) { error.value = `任务 ${id}：${failure(e)}` } finally { executionBusy.value = executionBusy.value.filter(item => item !== id) }
}
const crossSaving = ref(false), crossError = ref('')
async function setCross(task: Task, value: boolean) {
 if (crossSaving.value || saving.value || running(task.id)) return
 crossSaving.value = true; crossError.value = ''
 try {
  await backend().SaveTaskCross(task.id, value)
  const current = view.value.tasks.find(t => t.id === task.id)
  if (current) current.allowCross = value
 } catch (e) { if (alive) crossError.value = failure(e) }
 finally { crossSaving.value = false; if (alive) await pollProtocol() }
}
const props = defineProps<{ state: State; page: 'tasks' | 'orders' }>()
watch(() => props.page, page => { if (page === 'orders') { orderSearch.value=''; void showOrders() } })

const view = ref<TaskView>({ catalog: { stadium: '', tiers: [] }, configError: '', tasks: [] })
const error = ref(''), loading = ref(true), creating = ref(false), saving = ref(false), saveError = ref('')
const readError = ref('')
const retryingRead = ref(false)
const protocol = ref<ProtocolStatus | null>(null), attempt = ref<AuthAttempt | null>(null), protocolBusy = ref(false), controlAction = ref(''), protocolError = ref('')
const controlDialog = ref<HTMLDialogElement | null>(null), now = ref(Date.now())
const safeImage = computed(() => /^data:image\/(png|jpe?g|gif|webp);base64,/i.test(attempt.value?.image || '') ? attempt.value!.image : '')
const remaining = computed(() => Math.max(0, Math.ceil((Date.parse(attempt.value?.expiresAt || '') - now.value) / 1000)) || 0)
const serviceLabels: Record<string,string> = { stopped: '已停止', starting: '启动中', running: '运行中', failed: '启动失败' }
let protocolTimer: ReturnType<typeof setInterval> | undefined
let protocolPolling = false
let authGeneration = 0
async function pollProtocol() {
  if (protocolPolling || protocolBusy.value || saving.value || bulkAction.value || !alive) return
  protocolPolling = true
  try {
    const status = await backend().GetProtocolStatus()
    const data = await backend().GetTasks()
    if (alive) { protocol.value = status; updateView(data) }
    if (modal.value === 'auth' && activeID.value !== null) {
      const generation = authGeneration
      const data = await backend().GetAuthorization(activeID.value)
      if (alive && generation === authGeneration && modal.value === 'auth' && activeID.value === data.taskID) attempt.value = data
    }
  } catch (e) { if (alive) protocolError.value = failure(e) }
  finally { protocolPolling = false }
}
async function serviceAction(action: string) {
  if (bulkAction.value) return
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
const modal = ref<'note' | 'address' | 'logs' | 'seat' | 'people' | 'auth' | 'reauth' | 'delete' | null>(null), activeID = ref<number | null>(null)
const dialog = ref<HTMLDialogElement | null>(null)
const draft = ref<{ mode: 'tier' | 'area'; selected: string[] }>({ mode: 'tier', selected: [] })
const emptyAddress = (): AddressForm => ({ province: '', city: '', district: '', address: '', name: '', phone: '', region: [] })
const noteDraft = ref(''), addressDraft = ref<AddressForm>(emptyAddress())
const noteLength = computed(() => [...noteDraft.value].length)
const personDetails = ref<Record<string, Person>>({})
const personLoading = ref<string[]>([])
let personGeneration = 0
function clearPersonDetails() {
 personGeneration++; personDetails.value = {}; personLoading.value = []
}
async function togglePersonDetails(personID: string) {
 if (personDetails.value[personID]) { delete personDetails.value[personID]; return }
 if (activeID.value === null || personLoading.value.includes(personID)) return
 const taskID = activeID.value, generation = personGeneration
 personLoading.value.push(personID); saveError.value = ''
 try {
  const details = await backend().GetTaskPersonDetails(taskID, personID)
  if (generation === personGeneration && modal.value === 'people' && activeID.value === taskID) personDetails.value[personID] = details
 } catch (e) { if (generation === personGeneration) saveError.value = failure(e) }
 finally { if (generation === personGeneration) personLoading.value = personLoading.value.filter(id => id !== personID) }
}
const refreshingTasks = ref<number[]>([])
async function refreshTask(task: Task) {
 if (bulkAction.value || refreshingTasks.value.includes(task.id) || task.businessStatus === 'loading' || task.protocolStatus !== 'alive') return
 refreshingTasks.value.push(task.id); error.value = ''
 try { await backend().RefreshTaskPeople(task.id, true); await load() }
 catch (e) { if (alive) error.value = `任务 ${task.id}：${failure(e)}` }
 finally { refreshingTasks.value = refreshingTasks.value.filter(id => id !== task.id) }
}
async function refreshPeople(login = false) {
 clearPersonDetails()
 if(activeID.value === null || saving.value) return
 saving.value = true; saveError.value = ''
 try { await backend().RefreshTaskPeople(activeID.value, login); await load() }
 catch(e) {saveError.value = failure(e)} finally {saving.value = false}
}
async function togglePerson(id: string, checked: boolean) {
 if (!active.value || saving.value) return
 const task = active.value; const available = new Set((task.people || []).map(p => p.id)); const ids = (task.peopleSelected || []).filter(value => value !== id && available.has(value))
 if(checked) ids.push(id)
 saving.value = true; saveError.value = ''
 try {await backend().SaveTaskPeople(task.id, ids); await load()}
 catch(e){saveError.value=failure(e)} finally {saving.value=false}
}
async function saveDetails(clear = false) {
  if (activeID.value === null || saving.value) return
  const id = activeID.value, kind = modal.value
  saving.value = true; saveError.value = ''
  try {
    if (kind === 'note') await backend().SaveTaskNote(id, noteDraft.value)
    else await backend().SaveTaskAddress(id, clear ? null : { form: { ...addressDraft.value, region: [addressDraft.value.province, addressDraft.value.city, addressDraft.value.district] } })
    await load(); saving.value = false; close()
  } catch (e) { saveError.value = failure(e) } finally { saving.value = false }
}
const active = computed(() => view.value.tasks.find(t => t.id === activeID.value))
const areas = computed(() => view.value.catalog.tiers.flatMap(t => t.areas.filter(a => a.isSaleable).map(a => ({ ...a, tier: t.tier, price: t.price }))))
const groups = computed(() => ['首层', '顶层', 'VIP'].map(name => ({ name, areas: areas.value.filter(a => name === 'VIP' ? a.tier === 'VIP' : a.tier !== 'VIP' && a.level === (name === '首层' ? '首层看台' : '顶层看台')) })))
const selectedLabel = (t: Task) => t.selected.join('、') || '未配置'
function taskStatus(task: Task) {
 const run=inventoryView.value?.runs[task.id]; if(run && run.status!=="stopped") return {label:executionLabels[run.status] || run.status,tone:['paused','order_review','order_cooldown','order_verifying','order_blocked'].includes(run.status)?"warning":run.status==='order_created'?"ready":"pending",detail:run.message + (run.retryAt && !run.retryAt.startsWith("0001-") ? " · 恢复查询 " + new Date(run.retryAt).toLocaleTimeString("zh-CN",{timeZone:"Asia/Shanghai",hour12:false}) : "") + (run.target && !run.target.startsWith("0001-") ? " · 首次提交 " + new Date(run.target).toLocaleTimeString("zh-CN",{timeZone:"Asia/Shanghai",hour12:false}) : "") + (run.plan ? " · " + run.plan.parts.map(p=>`${p.region.name}区 ${p.quantity}张`).join(" + ") : "")}
 if (task.protocolStatus === 'expired') return { label: '需重新授权', tone: 'warning', detail: task.businessStatus === 'blocked' ? (task.businessMessage || '授权已失效，请重新扫码') : '授权已失效，请重新扫码' }
 if (!task.protocolStatus) return { label: '待授权', tone: 'neutral', detail: '扫码绑定微信账号' }
 if (task.paused) return { label: '维护已暂停', tone: 'neutral', detail: '点击刷新可恢复业务维护' }
 if (task.protocolStatus !== 'alive') return { label: '检查中', tone: 'pending', detail: '等待确认微信登录状态' }
 if (task.businessStatus === 'loading' || refreshingTasks.value.includes(task.id)) return { label: '准备中', tone: 'pending', detail: '正在登录业务并获取观演人' }
 if (task.businessStatus === 'failed' || task.businessStatus === 'config_error' || task.businessStatus === 'blocked') return { label: '需处理', tone: 'warning', detail: task.businessMessage || '业务暂不可用，请刷新重试' }
 if (!task.seatValid || !task.selected.length) return { label: '待配置', tone: 'warning', detail: '请选择座位要求' }
 if (!task.peopleUsable) return { label: '待就绪', tone: 'pending', detail: task.businessMessage || '等待业务登录' }
 if (task.peopleComplete === false) return { label: '待配置', tone: 'warning', detail: '已选观演人已不存在或不可用，请重新选择' }
 if (!task.peopleSelected?.length) return { label: '待配置', tone: 'warning', detail: '请选择观演人' }
 return { label: '已就绪', tone: 'ready', detail: '账号与配置已就绪' }
}
const search = ref('')
const searchTerms = computed(() => search.value.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean))
const filteredTasks = computed(() => view.value.tasks.filter(task => {
  const text = [String(task.id), task.wechatNickname || '', task.note || '', ...(task.spectatorNames || [])].join(' ').toLocaleLowerCase()
  return searchTerms.value.every(term => text.includes(term))
}))
const logLabels: Record<string, string> = {debug: '调试', info: '普通', success: '成功', warning: '提醒', error: '失败' }
const logEntries = computed(() => active.value?.logs || (active.value?.latest ? [active.value.latest] : []))
const copyNotice = ref(''), copying = ref(false)
const logList = ref<HTMLElement | null>(null)
const logAtTop = ref(true), newLogs = ref(false)
const fullTime = (at: string) => new Date(at).toLocaleString('zh-CN', { hour12: false })
async function copyLogs() {
 if (activeID.value === null || copying.value) return
 copying.value = true; copyNotice.value = ''
 try { await backend().CopyTaskLogs(activeID.value); copyNotice.value = '已复制日志' }
 catch { copyNotice.value = '复制失败，请重试' }
 finally { copying.value = false }
}
function showNewLogs() { if (logList.value) logList.value.scrollTop = 0; logAtTop.value = true; newLogs.value = false }
function updateView(data: TaskView) {
 const old = active.value?.latest?.at
 const scroll = logList.value?.scrollTop || 0
 const oldHeight = logList.value?.scrollHeight || 0
 view.value = data
 if (modal.value === 'logs' && old !== active.value?.latest?.at) {
  if (logAtTop.value) void nextTick(showNewLogs)
  else { newLogs.value = true; void nextTick(() => { if (logList.value) logList.value.scrollTop = scroll + logList.value.scrollHeight - oldHeight }) }
 }
}
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
async function retryRead() {
  retryingRead.value = true
  try { await load(); readError.value = '' }
  catch (e) { if (alive) readError.value = failure(e) }
  finally { retryingRead.value = false }
}
onMounted(async () => { try { await load(); await pollProtocol() } catch (e) { readError.value = failure(e) } finally { loading.value = false }; protocolTimer = setInterval(() => { now.value = Date.now(); void pollProtocol() }, 2000) })
onUnmounted(() => { alive = false; if (protocolTimer) clearInterval(protocolTimer); dialog.value?.close(); controlDialog.value?.close() })
async function createTask() {
  if (creating.value) return
  creating.value = true; error.value = ''
  try { const task = await backend().CreateTask(); if (alive) view.value.tasks.push(task) } catch (e) { error.value = failure(e) } finally { creating.value = false }
}
async function open(t: Task, kind: NonNullable<typeof modal.value>) {

  if (running(t.id) && ['seat','people','delete'].includes(kind)) { error.value='请先停止任务，再修改执行配置'; return }
  if (kind === 'auth' && t.protocolStatus === 'alive') kind = 'reauth'
  clearPersonDetails()
  crossError.value = ''; copyNotice.value = ''; newLogs.value = false; logAtTop.value = true; activeID.value = t.id; modal.value = kind; saveError.value = ''; tip.value = null
  draft.value = { mode: t.mode, selected: [...t.selected] }
  noteDraft.value = t.note || ''
  addressDraft.value = t.shippingAddress ? { ...t.shippingAddress.form, region: [...t.shippingAddress.form.region] } : emptyAddress()
  await nextTick(); dialog.value?.showModal()
  if (running(t.id) && ['seat','people','delete'].includes(kind)) { error.value='请先停止任务，再修改执行配置'; return }
  if (kind === 'auth') await beginAuth()
}
async function confirmReauthorization() {
  if (saving.value || modal.value !== 'reauth') return
  modal.value = 'auth'
  await beginAuth()
}
function confirmTaskDialog() {
  if (saving.value || crossSaving.value) return
  if (modal.value === 'reauth') { void confirmReauthorization(); return }
  if (modal.value === 'delete') { void deleteTask(); return }
  if (modal.value === 'note' || modal.value === 'address') return
  close()
}
function close(discard = false) {
  if (saving.value || crossSaving.value || (modal.value === 'seat' && saveError.value && !discard)) return
  clearPersonDetails(); dialog.value?.close(); modal.value = null; activeID.value = null; saveError.value = ''
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
      if (task) { task.mode = mode; task.selected = selected; task.seatValid = selected.length > 0; /* Logs and timestamps come from the persisted backend snapshot. */ }
    }
  } catch (e) { pendingSeatSave = false; if (alive) saveError.value = failure(e) } finally { saving.value = false; if (alive) await pollProtocol() }
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
    <Teleport defer to="#client-sidebar-protocol">
      <section class="tw-service sidebar-service" aria-label="协议服务">
        <div><strong>{{ props.state.authProtocol === 'yyb' ? 'YYB' : '微信' }}协议</strong><span class="sidebar-service-status" :data-state="protocol?.state" role="status">{{ serviceLabels[protocol?.state || ''] || '检查中' }}</span><small>{{ protocol?.address }} · {{ protocol?.proxy || '直连' }}</small></div>
        <div><button :disabled="!!bulkAction || protocolBusy || protocol?.state === 'running'" @click="serviceAction('start')">启动协议</button><button :disabled="!!bulkAction || protocolBusy" @click="serviceAction('restart')">重启</button><button :disabled="!!bulkAction || protocolBusy || protocol?.state === 'stopped'" @click="serviceAction('stop')">关闭协议</button></div>
        <p v-if="protocol?.message || protocolError" role="alert">{{ protocolError || protocol?.message }}</p>
        <div class="sidebar-service-mini" role="status" :aria-label="`${props.state.authProtocol === 'yyb' ? 'YYB' : '微信'}协议：${protocolError ? '状态异常' : serviceLabels[protocol?.state || ''] || '检查中'}`" :title="protocolError || protocol?.message || `${protocol?.address || ''} · ${protocol?.proxy || '直连'}`">
          <strong>{{ props.state.authProtocol === 'yyb' ? 'YYB' : '微信' }}</strong>
          <span><i class="sidebar-service-dot" :data-state="protocolError ? 'failed' : protocol?.state" aria-hidden="true"></i>{{ protocolError ? '异常' : serviceLabels[protocol?.state || ''] || '检查中' }}</span>
        </div>
      </section>
      </Teleport>
    <section v-if="props.page === 'tasks'" class="tw-body">
      <section class="tw-task-controls" aria-label="任务操作">
      <InventoryPanel :stadium="view.catalog.stadium" @update="inventoryView = $event">
        <template #title><h1>任务 <span class="tw-task-count">{{ view.tasks.length }}</span></h1></template>
        <template #toolbar="{ openSettings, settingsDisabled, inventoryNotice }">
          <div class="tw-toolbar" role="search" aria-label="任务搜索">
            <div class="tw-search"><input v-model="search" type="search" aria-label="搜索任务" placeholder="搜索任务" title="支持搜索任务编号、微信昵称、备注或观演人" autocomplete="off" :disabled="loading" @input="tip = null"/><button v-if="search" class="tw-search-clear" type="button" aria-label="清空搜索" @click="search = ''; tip = null">清空</button></div>
            <div class="tw-task-actions" role="group" aria-label="任务批量操作" title="批量操作作用于当前客户端的全部任务，不受搜索过滤影响">
              <button class="tw-primary" :disabled="creating || loading || !!bulkAction" @click="createTask">＋ {{ creating ? '正在创建…' : '新建任务' }}</button>
              <button :disabled="bulkBlocked || !bulkTargets('start').length" title="启动全部已就绪且未运行的任务，不受搜索过滤影响" @click="runBulk('start')">全部启动</button>
              <button v-if="inventoryView?.debugMode && inventoryView.simulation.enabled" :disabled="bulkBlocked || !bulkTargets('simulate').length" title="按比赛开售时间批量启动已就绪任务的模拟验收" @click="runBulk('simulate')">批量模拟验收</button>
              <button :disabled="bulkBlocked || !bulkTargets('stop').length" title="停止全部尚未停止的任务，不受搜索过滤影响" @click="runBulk('stop')">全部停止</button>
              <button :disabled="bulkBlocked || !bulkTargets('refresh').length" title="刷新全部可维护的在线任务，原运行任务完成后自动恢复" @click="runBulk('refresh')">全部刷新</button>
              <button :disabled="settingsDisabled || !!bulkAction" aria-haspopup="dialog" @click="openSettings()">设置</button>
            </div>
          </div>
          <Teleport to="body">
            <div class="task-toast-stack" role="region" aria-label="任务通知">
              <TaskToast v-if="!retryingRead" :message="readError" warning>
                <template #actions><button type="button" class="tw-toast-retry" @click="retryRead">重试读取</button></template>
              </TaskToast>
              <TaskToast :message="error" warning />
              <TaskToast :message="view.configError" warning />
              <TaskToast :message="state.notice || ''" />
              <TaskToast :message="inventoryNotice" warning />
              <TaskToast
                :message="bulkAction ? `${bulkLabels[bulkAction]} ${bulkDone} / ${bulkTotal}…` : bulkReport"
                :pending="!!bulkAction"
                :warning="!bulkAction && bulkErrors.length > 0"
              >
                <div v-if="!bulkAction && bulkErrors.length" class="tw-toast-details">
                  <div v-for="(message, index) in bulkErrors" :key="index">{{ message }}</div>
                </div>
              </TaskToast>
            </div>
          </Teleport>
        </template>
      </InventoryPanel>
      </section>
      <div class="tw-columns" aria-hidden="true"><span>编号</span><span>微信账号 / 备注</span><span>座位要求</span><span>观演人</span><span>操作</span></div>
      <div v-if="loading" class="tw-empty" role="status">正在读取本地任务…</div>
      <div v-else-if="!view.tasks.length && !readError && !error" class="tw-empty"><h2>创建你的第一个任务</h2><p>座位默认选择 A、B、C、D、E，微信账号可稍后授权。</p><button class="tw-primary" :disabled="creating" @click="createTask">＋ 新建任务</button></div>
      <div v-else-if="view.tasks.length && !filteredTasks.length" class="tw-empty"><h2>没有匹配的任务</h2><p>试试其他任务编号、微信昵称、备注或观演人姓名。</p><button @click="search = ''">清空搜索</button></div>
      <div class="tw-list">
        <article v-for="task in filteredTasks" :key="task.id" class="tw-task" :data-tone="taskStatus(task).tone" :aria-label="`任务 ${task.id}`">
          <span class="tw-number">{{ task.id }}</span>
          <div class="tw-account">
            <div class="tw-account-avatar" aria-hidden="true">{{ (task.wechatNickname || '微').slice(0, 1) }}<img v-if="task.protocolAvatar" :src="task.protocolAvatar" alt="" @error="($event.target as HTMLImageElement).hidden = true"/></div>
            <div class="tw-account-title"><strong :title="task.wechatNickname">{{ task.wechatNickname || (task.protocolStatus ? '已绑定账号' : '尚未授权') }}</strong></div>
            <div class="tw-account-badges">
              <span class="tw-status" :title="taskStatus(task).detail"><span class="tw-status-dot" aria-hidden="true"></span>{{ taskStatus(task).label }}</span>
              <span class="tw-account-connection"><span class="tw-account-state" :class="{ online: task.protocolStatus === 'alive' }">{{ task.protocolStatus === 'alive' ? '微信在线' : task.protocolStatus === 'expired' ? '授权失效' : task.protocolStatus ? '微信待检查' : '未绑定微信' }}</span><span class="tw-account-protocol">{{ state.authProtocol === 'yyb' ? 'YYB' : '微信协议' }}</span></span>
            </div>
          </div>
          <button :disabled="!!bulkAction || running(task.id)" class="tw-summary tw-seats" aria-haspopup="dialog" @mouseenter="tooltip($event, `${task.mode === 'tier' ? '指定票类' : '固定区域'}：${selectedLabel(task)}`)" @mouseleave="tip = null" @focus="tooltip($event, selectedLabel(task))" @blur="tip = null" @click="open(task, 'seat')"><span class="tw-field">{{ task.mode === 'tier' ? '指定票类' : '固定区域' }} ↗</span><span class="tw-value">{{ selectedLabel(task) }}</span></button>
          <button :disabled="!!bulkAction || running(task.id)" class="tw-summary tw-people" aria-haspopup="dialog" @click="open(task, 'people')"><span class="tw-field">已选观演人 {{ task.peopleSelected?.length || 0 }}人 ↗</span><span v-if="!task.spectatorNames?.length && task.businessStatus === 'ready' && task.peopleUsable" class="tw-value tw-people-ready" :title="`业务已登录，已获取${task.people?.length || 0}位观演人`"><svg class="tw-business-ready-icon" viewBox="0 0 16 16" fill="none" role="img" aria-label="业务已登录"><circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="1.5"/><path d="m5 8 2 2 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg><span>已获取{{ task.people?.length || 0 }}位观演人</span></span><span v-else class="tw-value" :title="task.spectatorNames?.join('、') || task.businessMessage">{{ task.spectatorNames?.join('、') || task.businessMessage || '选择观演人' }}</span></button>
          <button class="tw-summary tw-note-summary" aria-haspopup="dialog" :title="task.note || '添加备注或特殊说明'" @click="open(task, 'note')"><span class="tw-field">备注</span><span class="tw-value">{{ task.note || '添加备注或特殊说明' }}</span></button>
          <button class="tw-log" :class="{ 'tw-log-error': task.latest?.level === 'error' || task.latest?.level === 'warning' }" type="button" aria-haspopup="dialog" :aria-label="`查看任务 ${task.id} 的操作日志`" :title="task.latest?.text || '查看操作日志'" @click="open(task, 'logs')"><template v-if="task.latest"><time :datetime="task.latest.at">{{ clock(task.latest.at) }}</time><span>{{ task.latest.text }}</span></template><span v-else>暂无操作日志</span></button>
          <div class="tw-task-operations" role="group" :aria-label="`任务 ${task.id} 操作`">
            <button class="tw-start" :disabled="!!bulkAction || executionBusy.includes(task.id) || (inventoryView?.runs[task.id]?.status === 'stopping') || (!running(task.id) && inventoryView?.runs[task.id]?.status !== 'order_created' && inventoryView?.runs[task.id]?.status !== 'order_verifying' && inventoryView?.runs[task.id]?.status !== 'order_blocked' && (!inventoryView?.clockReady || !inventoryView?.match.enabled))" @click="toggleExecution(task)">{{ inventoryView?.runs[task.id]?.status === 'order_created' ? '再次购票' : ['order_verifying','order_blocked'].includes(inventoryView?.runs[task.id]?.status || '') ? '恢复监控' : inventoryView?.runs[task.id]?.status === 'stopping' ? '正在停止…' : running(task.id) ? '停止' : '启动' }}</button>
            <button :disabled="!!bulkAction || refreshingTasks.includes(task.id) || task.businessStatus === 'loading' || task.protocolStatus !== 'alive'" :title="task.paused ? '恢复业务维护并刷新观演人' : '重新登录业务并刷新观演人'" @click="refreshTask(task)">{{ refreshingTasks.includes(task.id) || task.businessStatus === 'loading' ? '刷新中…' : '刷新' }}</button>
            <button :disabled="!!bulkAction || task.businessStatus === 'loading'" @click="open(task, 'auth')">{{ task.protocolStatus ? '重新授权' : '开始授权' }}</button>
            <button v-if="inventoryView?.debugMode && inventoryView.simulation.enabled" :disabled="!!bulkAction || running(task.id) || !task.peopleSelected?.length" title="本地模拟库存、下单和订单售后，不访问官方接口" @click="runSimulation(task)">模拟验收</button>
            <button :title="task.shippingAddress ? '收货地址已配置' : '收货地址未配置'" @click="open(task, 'address')">收货地址</button>
            <button :disabled="!!bulkAction || running(task.id)" class="tw-operation-delete" @click="open(task, 'delete')">删除</button>
          </div>
        </article>
      </div>
      <footer class="tw-footer"><span>配置自动保存 · 当前客户端共享</span><span>曜石橙</span></footer>
    </section>
    <section v-else class="tw-orders-page" aria-labelledby="orders-page-title">
      <header class="tw-orders-heading">
        <div><h1 id="orders-page-title">订单 <span class="tw-task-count">{{ historyOrders.length }}</span></h1><p>本地订单汇总 · 详情仅在手动刷新时向服务器查询</p></div>
        <button class="tw-operation-delete" :disabled="ordersBusy || !historyOrders.length" @click="deleteAllOrders">全部删除</button>
      </header>
      <div class="tw-orders-toolbar">
        <label class="tw-orders-search"><span>按观演人筛选</span><input v-model="orderSearch" type="search" placeholder="输入观演人姓名" autocomplete="off"/><button v-if="orderSearch" type="button" aria-label="清空筛选" @click="orderSearch=''">清空</button></label>
        <span class="tw-orders-count">显示 {{ filteredOrders.length }} / {{ historyOrders.length }} 条</span>
      </div>
      <p v-if="ordersError" class="tw-alert" role="alert">{{ ordersError }}</p>
      <div v-if="ordersBusy && !historyOrders.length" class="tw-empty" role="status">正在读取本地订单…</div>
      <div v-else-if="!historyOrders.length" class="tw-empty"><h2>暂无订单记录</h2><p>创建订单后，记录会保存在这里。</p></div>
      <div v-else-if="!filteredOrders.length" class="tw-empty"><h2>没有匹配的订单</h2><p>试试其他观演人姓名。</p><button @click="orderSearch=''">清空筛选</button></div>
      <div v-else class="tw-orders-table-wrap">
        <table class="tw-orders-table">
          <thead><tr><th>订单 ID / 任务</th><th>观演人</th><th>状态</th><th>票区 / 座位</th><th>支付截止</th><th>金额</th><th>操作</th></tr></thead>
          <tbody><tr v-for="record in filteredOrders" :key="`${record.taskId}-${record.id}`">
            <td><button class="tw-order-id" @click="openOrder(record)">{{ record.id }}</button><small v-if="record.simulated" class="tw-order-simulated">本地模拟订单</small><small>任务 {{ record.taskId }} · {{ record.match.name }}</small><small>{{ fullTime(record.createdAt) }}</small><small v-if="record.detailError" class="tw-order-error">详情查询失败：{{ record.detailError }}</small></td>
            <td>{{ record.viewers.map(v => v.name).join('、') || '—' }}</td>
            <td><span class="tw-order-payment" :data-paid="record.details?.id ? record.details.status === 6 : null">{{ record.details?.id ? (record.details.status === 6 ? '已付款' : '未付款') : '—' }}</span></td>
            <td><template v-if="record.details?.bill?.length"><div v-for="ticket in record.details.bill" :key="ticket.id">{{ ticket.region }}区<template v-if="ticket.estateName"> · {{ ticket.estateName }}</template><template v-if="ticket.row"> · {{ ticket.row }}排{{ ticket.seat }}座</template></div></template><span v-else>—</span></td>
            <td>{{ record.details?.id ? (record.details.order_end_time || '—') : '—' }}</td>
            <td>{{ record.details?.id && record.details.payable ? `¥${record.details.payable}` : '—' }}</td>
            <td><div class="tw-orders-row-actions"><button :disabled="ordersBusy" @click="refreshOrder(record)">{{ ordersBusy ? '处理中…' : '刷新' }}</button><button @click="openOrder(record)">详情</button><button class="tw-operation-delete" :disabled="ordersBusy" @click="deleteOrder(record)">删除</button></div></td>
          </tr></tbody>
        </table>
      </div>
      <footer class="tw-orders-footer">订单列表仅展示本地记录；刷新操作只查询对应订单。</footer>
    </section>
    <div v-if="tip" role="tooltip" class="tw-tooltip" :style="{ left: `${tip.left}px`, top: `${tip.top}px` }">{{ tip.text }}</div>
    <dialog ref="dialog" class="tw-dialog" :class="{ wide: modal === 'seat' || modal === 'logs', 'tw-log-dialog': modal === 'logs', 'tw-scan-dialog': modal === 'auth' }" aria-labelledby="tw-dialog-title" @cancel.prevent="close()" @keydown="!['auth', 'note', 'address', 'people', 'recover'].includes(modal || '') && confirmDialog($event, confirmTaskDialog)">
      <template v-if="modal && active">
      <div class="tw-dialog-head"><div><h2 id="tw-dialog-title">{{ modal === 'note' ? '任务备注' : modal === 'address' ? '收货地址' : modal === 'logs' ? '操作日志' : modal === 'seat' ? '座位要求' : modal === 'people' ? '观演人' : modal === 'delete' ? '删除任务' : modal === 'reauth' ? '确认重新授权' : '授权账号' }}</h2><p>编号 {{ active.id }}<template v-if="modal === 'logs'"> · {{ logEntries.length }} / 500 条</template><template v-if="modal === 'seat'"> · {{ view.catalog.stadium }}</template></p></div><button class="tw-close" aria-label="关闭弹窗" :disabled="saving || crossSaving || (modal === 'seat' && !!saveError)" @click="close()">×</button></div>
        <div class="tw-dialog-content">
          <form v-if="modal === 'note' || modal === 'address'" id="task-details-form" @submit.prevent="saveDetails()">
            <fieldset :disabled="saving" class="tw-details-form">
              <template v-if="modal === 'note'">
                <label for="task-note">备注或特殊说明</label>
                <textarea id="task-note" v-model="noteDraft" rows="6" placeholder="填写本任务的备注或特殊说明" :aria-invalid="noteLength > 500" autofocus></textarea>
                <p class="tw-note-count" :class="{ 'tw-over-limit': noteLength > 500 }">{{ noteLength }} / 500 字</p>
              </template>
              <template v-else>
                <div class="tw-address-grid">
                  <label>收货人<input v-model="addressDraft.name" required maxlength="50" autocomplete="shipping name"/></label>
                  <label>手机号<input v-model="addressDraft.phone" required type="tel" inputmode="numeric" pattern="1[0-9]{10}" maxlength="11" autocomplete="shipping tel"/></label>
                </div>
                <div class="tw-address-grid tw-regions">
                  <label>省份<input v-model="addressDraft.province" required maxlength="50" autocomplete="shipping address-level1"/></label>
                  <label>城市<input v-model="addressDraft.city" required maxlength="50" autocomplete="shipping address-level2"/></label>
                  <label>区／县<input v-model="addressDraft.district" required maxlength="50" autocomplete="shipping address-level3"/></label>
                </div>
                <label>详细地址<textarea v-model="addressDraft.address" required maxlength="200" rows="3" autocomplete="shipping street-address" placeholder="街道、小区、楼栋和房间号"></textarea></label>
              </template>
            </fieldset>
          </form>
          <template v-if="modal === 'logs'">
            <button v-if="newLogs" class="tw-new-logs" @click="showNewLogs">有新日志，点击查看</button>
            <div ref="logList" class="tw-history" @scroll="logAtTop = ($event.target as HTMLElement).scrollTop < 8">
              <p v-if="!logEntries.length" class="tw-note">暂无操作日志</p>
              <article v-for="(entry, index) in [...logEntries].reverse()" :key="`${entry.at}-${index}`" class="tw-history-entry">
                <div><time :datetime="entry.at">{{ fullTime(entry.at) }}</time><span class="tw-log-level" :class="entry.level || 'info'">{{ logLabels[entry.level || 'info'] || '普通' }}</span></div>
                <p>{{ entry.text }}</p>
              </article>
            </div>
            <div class="tw-copy"><span role="status">{{ copyNotice }}</span><button :disabled="!logEntries.length || copying" @click="copyLogs">{{ copying ? '正在复制…' : '复制日志' }}</button></div>
          </template>

          <div v-if="saveError && modal !== 'auth'" class="tw-alert" role="alert">{{ saveError }}<div v-if="modal === 'seat'" class="tw-error-actions"><button @click="saveSeats">重试保存</button><button @click="close(true)">放弃未保存更改</button></div></div>
          <template v-if="modal === 'seat'">
            <div v-if="view.configError" class="tw-alert">{{ view.configError }}</div>
            <div class="tw-switch" role="group" aria-label="座位选择模式"><button :aria-pressed="draft.mode === 'tier'" :disabled="!!view.configError || crossSaving" @click="setMode('tier')">指定票类</button><button :aria-pressed="draft.mode === 'area'" :disabled="!!view.configError || crossSaving" @click="setMode('area')">固定区域</button></div>
            <div class="tw-cross-setting">
              <div><strong>允许跨区域</strong><small>开启后可跨区域、跨票类凑齐全部观演人数，仅使用已选座位范围。</small></div>
              <button type="button" role="switch" aria-label="允许跨区域" :aria-checked="!!active.allowCross" class="tw-cross-toggle" :class="{ enabled: active.allowCross }" :disabled="crossSaving || saving || running(active.id)" @click="setCross(active, !active.allowCross)"><span aria-hidden="true"></span></button>
            </div>
            <p v-if="crossError" class="tw-alert" role="alert">{{ crossError }} <button type="button" :disabled="crossSaving || saving" @click="setCross(active, !active.allowCross)">重试保存跨区域设置</button></p>
            <fieldset :disabled="!!view.configError || crossSaving" class="tw-options" aria-label="座位选项">
              <template v-if="draft.mode === 'tier'"><div class="tw-tier-options"><label v-for="tier in view.catalog.tiers" :key="tier.tier" class="tw-choice" :class="{ checked: draft.selected.includes(tier.tier) }"><input type="checkbox" :checked="draft.selected.includes(tier.tier)" @change="toggle(tier.tier, ($event.target as HTMLInputElement).checked)"/><span>{{ tier.tier }}<small>¥{{ tier.price }}</small></span></label></div><p class="tw-note">支持跨档组合，选择后自动保存。</p><p v-if="!view.catalog.tiers.length" class="tw-note">暂无可选票类。</p></template>
              <template v-else><section v-for="group in groups" :key="group.name" class="tw-area-group"><label class="tw-group-heading"><input type="checkbox" :disabled="!group.areas.length" :checked="group.areas.length > 0 && group.areas.every(a => draft.selected.includes(a.code))" :indeterminate="group.areas.some(a => draft.selected.includes(a.code)) && !group.areas.every(a => draft.selected.includes(a.code))" @change="toggleGroup(group.areas.map(a => a.code), ($event.target as HTMLInputElement).checked)"/><span>{{ group.name }}</span><small>全选 · {{ group.areas.length }} 个看台</small></label><div class="tw-area-options"><label v-for="area in group.areas" :key="area.code" class="tw-choice tw-area" :class="{ checked: draft.selected.includes(area.code) }" :title="`${area.code} · ${area.tier}类 ¥${area.price}${area.specialArea ? ' · ' + area.specialArea : ''}`"><input type="checkbox" :aria-label="`${area.code}，${area.tier}类，${area.price}元，${area.specialArea}`" :checked="draft.selected.includes(area.code)" @change="toggle(area.code, ($event.target as HTMLInputElement).checked)"/><span class="tw-area-copy"><span class="tw-area-top"><span class="tw-area-code">{{ area.code }}</span><span v-if="area.specialArea" class="tw-special">{{ area.specialArea }}</span></span><small>{{ area.tier }} · ¥{{ area.price }}</small></span></label></div></section></template>
            </fieldset>
          </template>
          <template v-else-if="modal === 'people'">
 <p role="status">{{ active.businessMessage || '授权账号后自动获取观演人' }}</p>
 <p v-if="active.peopleComplete === false" class="tw-note">已选观演人中有人已不存在或不可用，请重新选择完整购票人员。</p>
 <p v-if="!active.peopleUsable" class="tw-note">{{ active.paused && active.protocolStatus === 'alive' ? '账号已在线，点击任务卡片操作区的“刷新”可恢复任务并获取观演人。' : '当前观演人暂不可选，原列表及勾选保留，业务登录和刷新成功后恢复。' }}</p>
 <div class="tw-people-actions"><button :disabled="saving || active.businessStatus === 'loading' || active.protocolStatus !== 'alive' || active.paused" @click="refreshPeople(false)">刷新观演人</button><button v-if="active.protocolStatus !== 'alive'" @click="open(active, 'auth')">前往授权</button></div>
 <p v-if="active.peopleUsable && !active.people?.length">此账号暂无观演人。</p>
 <div v-for="person in active.people || []" :key="person.id" class="tw-person"><label class="tw-person-choice"><input type="checkbox" :disabled="saving || !active.peopleUsable" :checked="active.peopleSelected?.includes(person.id)" @change="togglePerson(person.id, ($event.target as HTMLInputElement).checked)"/><span><strong>{{ person.name }}{{ person.self ? '（本人）' : '' }}</strong><small v-if="!personDetails[person.id]">{{ person.card }} · {{ person.phone }}</small><template v-else><small>证件号码：{{ personDetails[person.id].card || '未提供' }}</small><small>手机号：{{ personDetails[person.id].phone || '未提供' }}</small><small v-if="personDetails[person.id].card.includes('*') || personDetails[person.id].phone.includes('*')">接口仅返回脱敏信息，暂无法显示完整内容。</small></template></span></label><button type="button" class="tw-person-details" :disabled="personLoading.includes(person.id)" :aria-expanded="!!personDetails[person.id]" @click="togglePersonDetails(person.id)">{{ personLoading.includes(person.id) ? '读取中…' : personDetails[person.id] ? '收起' : '显示完整信息' }}</button></div>
 <p class="tw-note">首次获取默认不勾选；选择后自动保存。</p><p v-if="saveError" class="tw-alert" role="alert">{{ saveError }}</p>
 </template>
          <ScanAuthorization v-else-if="modal === 'auth'" :task-id="active.id" :client-number="state.clientNumber" :protocol="attempt?.protocol || state.authProtocol" :image="safeImage" :status="attempt?.status || ''" :message="saveError || attempt?.message || ''" :remaining="remaining" :busy="saving" :logged-in="active.protocolStatus === 'alive'" @refresh="beginAuth"/>
          <template v-else-if="modal === 'reauth'"><p>任务 {{ state.clientNumber }}-{{ active.id }} 已登录协议账号{{ active.wechatNickname ? `「${active.wechatNickname}」` : '' }}，确认重新扫码授权吗？</p><p class="tw-note">新授权成功后才会替换绑定；取消、过期或失败时保留原绑定，原账号是否可用单独检查。</p></template>
          <template v-else-if="modal === 'delete'"><p class="tw-delete-note">确认删除 {{ active.id }}？此任务的配置和本地授权信息将被清除，删除不可恢复，编号不再使用。协议支持时尝试退出账号。</p></template>
        </div>
        <div class="tw-dialog-footer"><span role="status" aria-live="polite">{{ saving || crossSaving ? '正在保存…' : saveError ? '操作失败，请查看提示' : modal === 'seat' ? '修改自动保存' : '' }}</span><div><template v-if="modal === 'note' || modal === 'address'"><button v-if="modal === 'address' && active.shippingAddress" :disabled="saving" @click="saveDetails(true)">清除地址</button><button :disabled="saving" @click="close()">取消</button><button type="submit" form="task-details-form" class="tw-primary" :disabled="saving || (modal === 'note' && noteLength > 500)">{{ saving ? '正在保存…' : '保存' }}</button></template><template v-else-if="modal === 'delete'"><button :disabled="saving" @click="close()">取消</button><button class="tw-danger" :disabled="saving" @click="deleteTask">确认删除</button></template><template v-else-if="modal === 'reauth'"><button :disabled="saving" @click="close()">取消</button><button class="tw-primary" :disabled="saving" @click="confirmReauthorization">确认重新授权</button></template><button v-else class="tw-primary" :disabled="saving || crossSaving || (modal === 'seat' && !!saveError)" @click="close()">{{ modal === 'seat' ? '完成' : '关闭' }}</button></div></div>
      </template>
    </dialog>
    <dialog ref="controlDialog" class="tw-dialog" aria-labelledby="service-confirm" @keydown="confirmDialog($event, controlService)" @cancel.prevent="!protocolBusy && controlDialog?.close()">
      <div class="tw-dialog-head"><h2 id="service-confirm">确认{{ controlAction === 'stop' ? '关闭' : '重启' }}协议</h2></div>
      <div class="tw-dialog-content">
          <p>受影响任务：{{ protocol?.affected.length ? protocol.affected.join('、') : '暂无已绑定任务' }}。正在等待的扫码也会取消。执行后将取消相关请求并暂停任务，账号凭据保留。</p><p v-if="controlAction === 'restart'">服务恢复后重新检查账号，任务保持暂停。</p><p v-if="protocolError" class="tw-alert" role="alert">{{ protocolError }}</p></div>
      <div class="tw-dialog-footer"><span>{{ protocolBusy ? '处理中…' : '' }}</span><div><button :disabled="!!bulkAction || protocolBusy" @click="controlDialog?.close()">取消</button><button class="tw-primary" :disabled="!!bulkAction || protocolBusy" @click="controlService">确认{{ controlAction === 'stop' ? '关闭' : '重启' }}</button></div></div>
    </dialog>
    <dialog ref="ordersDialog" class="tw-dialog wide" aria-labelledby="orders-title" @cancel.prevent="ordersDialog?.close();selectedOrder=null">
      <div class="tw-dialog-head"><div><h2 id="orders-title">订单 ID {{ selectedOrder?.id }} <small v-if="selectedOrder?.simulated" class="tw-order-simulated">本地模拟</small></h2><p>{{ selectedOrder ? `任务 ${selectedOrder.taskId} · ${selectedOrder.match.name}` : '' }}</p></div><div class="tw-order-head-actions"><button class="tw-close" aria-label="关闭弹窗" @click="ordersDialog?.close();selectedOrder=null">×</button></div></div>
      <div class="tw-dialog-content tw-order-history">
       <template v-if="selectedOrder"><p v-if="ordersError || selectedOrder.detailError" class="tw-alert" role="alert">{{ ordersError || `订单详情获取失败：${selectedOrder.detailError}` }}</p><div class="tw-order-toolbar"><span class="tw-order-status">{{ ({1:'已取消（付款超时）',2:'退款申请中',3:'已退款',4:'已取消',5:'待支付',6:'待观赛',7:'已入场',8:'已完成'} as Record<number,string>)[selectedOrder.details?.status] || '详情尚未获取' }}</span><button :disabled="ordersBusy" @click="refreshSelectedOrder">{{ ordersBusy?'查询中…':'刷新详情' }}</button></div>
        <div class="tw-order-facts"><div><small>订单 ID</small><strong>{{ selectedOrder.id }}</strong></div><div><small>下单时间</small><strong>{{ selectedOrder.details?.create_time || fullTime(selectedOrder.createdAt) }}</strong></div><div><small>支付截止</small><strong>{{ selectedOrder.details?.order_end_time || '—' }}</strong></div><div><small>订单金额</small><strong>¥{{ selectedOrder.details?.payable || '—' }}</strong></div><div><small>实付金额</small><strong>¥{{ selectedOrder.details?.payed || '—' }}</strong></div><div><small>退款金额</small><strong>¥{{ selectedOrder.details?.refund || '—' }}</strong></div><div><small>座位数量</small><strong>{{ selectedOrder.details?.count_bill ?? '—' }}</strong></div><div><small>已退座位</small><strong>{{ selectedOrder.details?.refund_bill ?? '—' }}</strong></div></div>
        <section class="tw-ticket-section"><h3>门票明细</h3><article v-for="ticket in selectedOrder.details?.bill || []" :key="ticket.id" class="tw-ticket"><div><strong>{{ ticket.region }}区 · {{ ticket.estateName || ticket.no }}</strong><small>{{ ticket.realname }} · {{ ticket.phone }} · {{ ({5:'待使用',6:'已锁定',7:'已入场'} as Record<number,string>)[ticket.status] || `状态 ${ticket.status}` }}</small></div><div><b>{{ ticket.row ? `${ticket.row} 排 ${ticket.seat} 座` : '未分配座位' }}</b><small>¥{{ ticket.price }}</small></div></article><p v-if="selectedOrder.details && !selectedOrder.details.bill?.length" class="tw-note">尚无已查询的票券明细。</p></section>
        <section class="tw-ticket-section"><h3>已退票明细 · {{ selectedOrder.details?.tuiBill?.length || 0 }} 张</h3><article v-for="ticket in selectedOrder.details?.tuiBill || []" :key="ticket.id" class="tw-ticket"><div><strong>{{ ticket.region }}区 · {{ ticket.estateName || ticket.no }}</strong><small>已退票</small></div><div><b>{{ ticket.row ? `${ticket.row} 排 ${ticket.seat} 座` : '' }}</b><small>¥{{ ticket.price }}</small></div></article></section>
       </template>
      </div>
      <div class="tw-dialog-footer"><span>订单状态以服务端最新查询为准</span><div v-if="selectedOrder"><button v-if="selectedOrder.details?.status===5" :disabled="ordersBusy" class="tw-danger" @click="afterSale('cancel')">取消未支付订单</button><button v-if="selectedOrder.details?.status===6 && selectedOrder.details?.pay_type==='微信支付' && selectedOrder.details?.refundEnabled" :disabled="ordersBusy" class="tw-danger" @click="afterSale('refund')">整单退款</button><button @click="ordersDialog?.close();selectedOrder=null">关闭详情</button></div></div>
    </dialog>
  </main>
</template>

<style scoped>
.tw-value.tw-people-ready{display:flex;align-items:center;gap:5px}.tw-people-ready>span{min-width:0;overflow:hidden;text-overflow:ellipsis}.tw-business-ready-icon{width:14px;height:14px;flex-shrink:0;color:var(--active)}
.tw-people-actions{display:flex;gap:8px;flex-wrap:wrap;margin:12px 0}.tw-person{display:flex;align-items:center;gap:12px;padding:10px 0;border-bottom:1px solid var(--line)}.tw-person-choice{display:flex;align-items:center;gap:12px;flex:1;min-width:0}.tw-person-details{flex-shrink:0}.tw-person small{overflow-wrap:anywhere}.tw-person{flex-wrap:wrap}.tw-person span{display:flex;flex-direction:column;gap:4px}.tw-person small{color:var(--muted)}
.tw-order-head-actions{display:flex;align-items:center;gap:8px;flex-shrink:0}.tw-order-head-actions .tw-operation-delete{color:var(--app-error)}
.tw-orders-page{min-width:0;padding:26px 22px 18px}.tw-orders-heading{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:18px}.tw-orders-heading p{margin-top:4px;font-size:12px}.tw-orders-heading .tw-operation-delete,.tw-orders-row-actions .tw-operation-delete{color:var(--app-error)}
.tw-orders-toolbar{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:12px}.tw-orders-search{display:flex;align-items:center;gap:8px;min-width:280px;color:var(--muted);font-size:12px}.tw-orders-search input{box-sizing:border-box;width:min(300px,45vw);padding:7px 9px;border:1px solid var(--line);border-radius:6px;background:var(--panel);color:var(--ink);font:inherit}.tw-orders-search button{padding:5px 8px;font-size:12px}.tw-orders-count,.tw-orders-footer{font-size:12px;color:var(--muted);font-variant-numeric:tabular-nums}.tw-orders-table-wrap{width:100%;overflow:auto;border:1px solid var(--line);border-radius:8px;background:var(--panel)}.tw-orders-table{width:100%;min-width:980px;table-layout:fixed;border-collapse:collapse;font-size:12px}.tw-orders-table th,.tw-orders-table td{box-sizing:border-box;padding:7px 9px;border-bottom:1px solid var(--line);text-align:left;vertical-align:middle;overflow-wrap:anywhere}.tw-orders-table th{position:sticky;top:0;background:var(--soft);color:var(--muted);font-weight:550;white-space:nowrap}.tw-orders-table tbody tr:last-child td{border-bottom:0}.tw-orders-table tbody tr:hover{background:var(--soft)}.tw-orders-table th:nth-child(1){width:17%}.tw-orders-table th:nth-child(2){width:13%}.tw-orders-table th:nth-child(3){width:9%}.tw-orders-table th:nth-child(4){width:22%}.tw-orders-table th:nth-child(5){width:16%}.tw-orders-table th:nth-child(6){width:9%}.tw-orders-table th:nth-child(7){width:14%}.tw-orders-table td:first-child{font-variant-numeric:tabular-nums}.tw-orders-table td:first-child small{display:block;margin-top:2px;color:var(--muted);font-size:10px}.tw button.tw-order-id{display:block;padding:0;border:0;background:transparent;color:var(--active);font-weight:600;font-variant-numeric:tabular-nums}.tw button.tw-order-id:hover{background:transparent;text-decoration:underline}.tw-order-payment{white-space:nowrap;color:var(--muted)}.tw-order-payment[data-paid=true]{color:var(--app-success);font-weight:600}.tw-orders-row-actions{display:flex;align-items:center;flex-wrap:wrap;gap:4px}.tw-orders-row-actions button{padding:4px 6px;font-size:11px;white-space:nowrap}.tw-orders-footer{padding:10px 2px}.tw-orders-page .tw-empty{margin-top:14px;padding:42px 16px}.tw-orders-page .tw-empty p{margin:8px 0 14px}
.tw-orders-table td:first-child small.tw-order-error{color:var(--app-warning)}
.tw-order-simulated{color:var(--app-warning)!important;font-weight:600}
.tw-order-history{max-height:65vh;overflow:auto}.tw-order-record{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:6px 16px;padding:12px 0;border-bottom:1px solid var(--line);align-items:center}.tw-order-record>div{min-width:0;overflow-wrap:anywhere}.tw-order-record>div:first-child{display:flex;flex-direction:column}.tw-order-record .tw-muted{font-size:12px;color:var(--muted)}.tw-order-actions{grid-column:2;grid-row:1/3;display:flex;gap:6px}.tw-order-actions .tw-operation-delete{color:var(--app-error)}.tw-order-toolbar{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:16px}.tw-order-status{font-size:13px;font-weight:600;color:var(--app-accent)}.tw-order-facts{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));border-top:1px solid var(--line);border-left:1px solid var(--line)}.tw-order-facts>div{min-width:0;padding:9px 11px;border-right:1px solid var(--line);border-bottom:1px solid var(--line)}.tw-order-facts small,.tw-ticket small{display:block;font-size:11px;color:var(--muted);overflow-wrap:anywhere}.tw-order-facts strong{font-size:12px;font-weight:550;overflow-wrap:anywhere}.tw-ticket-section{margin-top:20px}.tw-ticket-section h3{padding-bottom:8px;border-bottom:1px solid var(--line)}.tw-ticket{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:12px;padding:11px 2px;border-bottom:1px solid var(--line)}.tw-ticket strong,.tw-ticket b{font-size:12px;font-weight:550}.tw-ticket>div:last-child{text-align:right}.tw-danger{color:var(--app-error)!important}
.tw-task-controls{position:sticky;top:0;z-index:10;margin:0 -22px 18px;padding:12px 22px;background:var(--bg);border-bottom:1px solid var(--line);box-shadow:0 6px 12px var(--app-shadow)}
.tw-task-controls .tw-heading{margin-bottom:10px;flex-wrap:wrap}
.tw-task-controls .tw-toolbar{margin-bottom:0}
.tw-task-actions{display:flex;align-items:center;justify-content:flex-end;gap:8px;flex-wrap:wrap;margin-left:auto}
.tw-task-count{font-size:18px;font-weight:400;color:var(--muted);margin-left:6px;font-variant-numeric:tabular-nums}
@media(max-width:760px){.tw-task-controls{margin-left:-12px;margin-right:-12px;padding:10px 12px}}
@media(max-width:760px){.tw-orders-page{padding:20px 12px 14px}.tw-orders-heading{align-items:flex-start}.tw-orders-toolbar{align-items:flex-start;flex-direction:column}.tw-orders-search{width:100%;min-width:0;flex-wrap:wrap}.tw-orders-search input{flex:1;width:auto}}
.tw-dialog.tw-scan-dialog{box-sizing:border-box;width:370px;height:fit-content;min-height:0;max-height:none;overflow:visible}
.tw-scan-dialog .tw-dialog-head{position:static;padding:6px 12px;border-radius:12px 12px 0 0}.tw-scan-dialog .tw-dialog-head h2{font-size:13px}.tw-scan-dialog .tw-dialog-head p{display:none}.tw-scan-dialog .tw-dialog-content{padding:6px 10px;overflow:visible}.tw-scan-dialog .tw-dialog-footer{display:none}
.tw-service{display:flex;flex-wrap:wrap;justify-content:space-between;align-items:center;gap:12px;background:var(--panel);border:1px solid var(--line);border-radius:8px;padding:14px;margin-bottom:18px}.tw-service>div{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.tw-service span,.tw-service small{color:var(--muted);font-size:12px}.tw-service p{width:100%;font-size:12px;color:var(--app-warning)}.tw-auth{text-align:center}.tw-auth img{display:block;width:220px;max-width:100%;height:auto;margin:14px auto;background:white}.tw-auth p{font-size:13px;margin:12px 0;overflow-wrap:anywhere}
.tw{--bg:var(--app-bg);--panel:var(--app-panel);--ink:var(--app-ink);--muted:var(--app-muted);--line:var(--app-line);--soft:var(--app-soft);--active:var(--app-accent);--selected:var(--app-selected);max-width:none;min-height:100dvh;margin:0;padding:0;background:var(--bg);color:var(--ink);font:14px/1.5 "Segoe UI","Microsoft YaHei",sans-serif}
.tw button,.tw select{font:inherit}.tw button{color:var(--ink);background:var(--panel);border:1px solid var(--line);border-radius:6px;padding:7px 11px}.tw button:hover:not(:disabled){background:var(--soft)}.tw button:focus-visible,.tw input:focus-visible,.tw select:focus-visible{outline:2px solid var(--active);outline-offset:3px}.tw button.tw-primary{background:var(--active);border-color:var(--active);color:var(--app-on-accent)}.tw button.tw-danger{background:var(--app-danger);border-color:var(--app-danger);color:var(--app-on-danger)}.tw button:disabled{cursor:default;opacity:.55}.tw h1,.tw h2,.tw h3,.tw p{margin:0;letter-spacing:normal}.tw h1{font-size:22px;font-weight:600}.tw h2{font-size:17px;font-weight:600}.tw h3{font-size:14px;font-weight:600}.tw p{color:var(--muted);line-height:1.6}.tw label{color:inherit;font-size:inherit;margin:0}.tw input[type=checkbox]{accent-color:var(--active);width:15px;height:15px;margin:0;flex-shrink:0}.tw-chrome{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;padding:18px 24px;background:var(--panel);border-bottom:1px solid var(--line)}.tw-brand{font-weight:600;letter-spacing:.06em}.tw-brand span{font-size:11px;font-weight:400;color:var(--muted)}.tw-session{display:flex;align-items:center;gap:16px;font-size:12px;color:var(--muted)}.tw-session .disconnected{color:var(--app-warning)}.tw button.tw-link{padding:0;border:0;background:transparent;color:var(--active);font-size:12px;text-align:left}.tw-body{padding:26px 22px 18px}.tw-heading{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:24px}.tw-heading p{font-size:12px;margin-top:4px}.tw-divider{margin:0 8px;color:var(--line)}.tw-columns,.tw-task{display:grid;grid-template-columns:32px minmax(140px,1.1fr) minmax(115px,1fr) minmax(85px,.7fr) minmax(140px,1.2fr) minmax(150px,1.3fr) 36px;gap:12px;align-items:center}.tw-columns{padding:0 14px 9px;font-size:12px;color:var(--muted)}.tw-task{padding:8px 10px;height:104px;background:var(--panel);border:1px solid var(--line);border-radius:8px;margin-bottom:10px}.tw-task>*{min-width:0}.tw-number{font-size:17px;color:var(--muted);font-variant-numeric:tabular-nums}.tw-account{position:relative;padding-left:38px;display:flex;flex-direction:column;align-items:flex-start;gap:3px;overflow:hidden}.tw-account-avatar{position:absolute;left:0;top:2px;width:30px;height:30px;object-fit:cover;border-radius:50%;background:var(--soft)}.tw-account strong{font-weight:600;max-width:100%;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.tw-account>span{font-size:12px;color:var(--muted);max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.tw button.tw-summary{height:60px;width:100%;background:var(--soft);border-color:transparent;text-align:left;padding:8px 9px;min-width:0}.tw button.tw-summary:hover{border-color:var(--active);background:var(--selected)}.tw-field,.tw-value{display:block;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.tw-field{font-size:12px;color:var(--muted);margin-bottom:3px}.tw button.tw-log{display:flex;flex-direction:column;align-items:flex-start;justify-content:flex-start;text-align:left;align-self:stretch;min-height:0;max-height:100%;padding:4px 0;border:0;background:transparent;font-size:12px;font-weight:400;color:var(--muted);overflow:hidden;white-space:normal;line-height:18px}.tw button.tw-log:hover{background:var(--soft)}.tw-log time{display:block;flex-shrink:0;font-size:11px;margin-bottom:3px}.tw-log>span{flex-shrink:0;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}.tw button.tw-delete{border:0;background:transparent;color:var(--muted);font-size:12px;padding:5px 0;white-space:nowrap}.tw-footer{display:flex;justify-content:space-between;gap:12px;border:0;padding-top:6px;font-size:12px;color:var(--muted)}.tw-empty{text-align:center;padding:65px 16px;background:var(--panel);border:1px solid var(--line);border-radius:8px;margin-bottom:14px}.tw-empty p{margin:10px 0 22px}.tw-alert{border:1px solid var(--app-warning-line);background:var(--app-warning-bg);color:var(--app-warning);font-size:12px;padding:10px 12px;border-radius:6px;margin-bottom:16px;overflow-wrap:anywhere}.tw-error-actions{display:flex;gap:8px;flex-wrap:wrap;margin-top:8px}.tw-tooltip{position:fixed;max-width:min(340px,calc(100vw - 16px));max-height:240px;overflow:auto;background:var(--panel);border:1px solid var(--line);color:var(--ink);box-shadow:0 5px 18px var(--app-shadow);border-radius:7px;padding:10px 12px;font-size:12px;overflow-wrap:anywhere;z-index:20;pointer-events:none}.tw-dialog{padding:0;color:var(--ink);background:var(--panel);border:1px solid var(--line);border-radius:12px;width:420px;max-width:calc(100vw - 24px);max-height:85vh;box-shadow:0 15px 50px var(--app-shadow)}.tw-dialog.wide{width:780px}.tw-dialog::backdrop{background:var(--app-backdrop)}.tw-dialog-head{position:sticky;top:0;z-index:2;background:var(--panel);padding:18px 20px;border-bottom:1px solid var(--line);display:flex;align-items:center;justify-content:space-between;gap:12px}.tw-dialog-head p{font-size:12px;margin-top:4px}.tw button.tw-close{border:0;padding:5px 9px;color:var(--muted)}.tw-dialog-content{padding:20px}.tw-dialog-footer{position:sticky;bottom:0;background:var(--panel);border-top:1px solid var(--line);padding:14px 20px;display:flex;align-items:center;justify-content:space-between;gap:12px;font-size:12px;color:var(--muted);z-index:2}.tw-dialog-footer>div{display:flex;gap:8px}.tw-switch{display:inline-flex;border:1px solid var(--line);border-radius:7px;padding:3px;gap:2px;margin-bottom:16px}.tw-switch button{border:0;background:transparent;padding:5px 12px;font-size:13px}.tw-switch button[aria-pressed=true]{color:var(--app-on-accent);background:var(--active)}.tw-options{border:0;padding:0;margin:0;min-width:0}.tw-tier-options{display:flex;gap:8px;flex-wrap:wrap}.tw label.tw-choice{display:flex;align-items:center;gap:7px;border:1px solid var(--line);background:var(--panel);padding:9px 12px;border-radius:6px;min-width:90px;cursor:pointer}.tw label.tw-choice.checked{background:var(--selected);border-color:var(--active)}.tw-choice small{display:block;font-size:11px;color:var(--muted)}.tw-note{font-size:12px;margin-top:12px!important}.tw-area-group+.tw-area-group{margin-top:16px}.tw label.tw-group-heading{display:flex;align-items:center;gap:8px;font-size:13px;margin-bottom:8px;cursor:pointer}.tw-group-heading small{color:var(--muted);font-size:12px}.tw-area-options{display:grid;grid-template-columns:repeat(auto-fill,minmax(140px,1fr));gap:7px}.tw label.tw-area{height:56px;min-width:0;padding:7px 8px}.tw-area-copy{flex:1;min-width:0}.tw-area-top{display:flex;gap:6px;align-items:center;min-width:0;white-space:nowrap}.tw-area-code{flex-shrink:0}.tw-special{font-size:11px;color:var(--muted);min-width:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.tw-area small{white-space:nowrap}.tw-unavailable{background:var(--soft);padding:18px;border-radius:7px}.tw-unavailable p{font-size:13px;margin:8px 0 14px}.tw-delete-note{font-size:13px}.tw-protocol{display:block;margin-bottom:20px!important;font-size:13px!important}.tw select{display:block;width:100%;margin-top:8px;padding:9px;background:var(--panel);color:var(--ink);border:1px solid var(--line);border-radius:6px}
.tw button.tw-primary:hover:not(:disabled){background:var(--app-accent-hover)}.tw button.tw-danger:hover:not(:disabled){background:var(--app-danger-hover)}
.tw-toolbar{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;margin-bottom:18px}.tw-search{position:relative;flex:1;max-width:420px;min-width:0}.tw .tw-search input{display:block;width:100%;min-width:0;margin:0;padding:10px 58px 10px 12px;border:1px solid var(--line);border-radius:7px;background:var(--panel);color:var(--ink);font:inherit;appearance:none}.tw-search input::-webkit-search-cancel-button{appearance:none}.tw .tw-search input::placeholder{color:var(--muted);font-size:12px}.tw .tw-search input:hover:not(:disabled){border-color:var(--active)}.tw button.tw-search-clear{position:absolute;right:5px;top:50%;transform:translateY(-50%);padding:5px 8px;border:0;background:transparent;color:var(--active);font-size:12px}.tw-search-count{color:var(--muted);font-size:12px;white-space:nowrap}
@media(max-width:480px){.tw-search{flex-basis:100%;max-width:none}.tw-toolbar{gap:8px}}
@media(max-width:1000px){.tw-chrome{padding:16px;gap:12px}.tw-session{width:100%;gap:12px;flex-wrap:wrap}.tw-body{padding:20px 12px 14px}.tw-columns{display:none}.tw-log time{flex-shrink:0;margin:0}.tw-log>span{-webkit-line-clamp:2;line-height:16px}.tw-dialog-head,.tw-dialog-content,.tw-dialog-footer{padding:16px}.tw-area-options{grid-template-columns:repeat(auto-fill,minmax(125px,1fr))}.tw-footer{font-size:11px}}
@media(pointer:coarse){.tw button,.tw label.tw-choice,.tw-group-heading{min-height:44px}.tw input,.tw select{font-size:16px}.tw button.tw-delete{min-width:44px;justify-self:end}}
.tw-history{max-height:48vh;overflow:auto;overflow-anchor:none}.tw-history-entry{padding:12px 0;border-bottom:1px solid var(--line)}.tw-history-entry>div{display:flex;gap:12px;align-items:center;font-size:12px;color:var(--muted)}.tw-history-entry p{margin-top:6px;white-space:pre-wrap;overflow-wrap:anywhere;font-size:13px}.tw-log-level.success{color:var(--active)}.tw-log-level.warning,.tw-log-level.error{color:var(--app-warning)}.tw-copy{display:flex;justify-content:space-between;align-items:center;gap:12px;margin-top:14px;font-size:12px}.tw-new-logs{margin-bottom:8px}.tw-log-dialog .tw-dialog-content{min-height:0}.tw button.tw-log{min-height:0;min-width:0}

.tw button.tw-address-status{font-size:11px;padding:1px 0;min-height:0;max-width:100%;white-space:nowrap;color:var(--muted)}.tw button.tw-address-status.configured{color:var(--active)}.tw-note-summary .tw-value{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;white-space:normal;overflow:hidden;overflow-wrap:anywhere;line-height:18px}.tw-details-form{border:0;margin:0;padding:0;min-width:0}.tw-details-form label{display:block;font-size:12px;color:var(--muted)}.tw-details-form input,.tw-details-form textarea{box-sizing:border-box;display:block;width:100%;margin:6px 0 14px;padding:9px 10px;color:var(--ink);background:var(--soft);border:1px solid var(--line);border-radius:6px;font:inherit;line-height:1.6}.tw-details-form textarea{resize:vertical;max-height:30vh}.tw-address-grid{display:grid;grid-template-columns:1fr 1fr;gap:12px}.tw-regions{grid-template-columns:1fr 1fr 1fr}.tw-note-count{text-align:right;font-size:12px;color:var(--muted)}.tw-over-limit{color:var(--app-warning)}
@media(max-width:480px){.tw-address-grid{grid-template-columns:1fr}.tw-details-form input{margin-bottom:9px}.tw-details-form input,.tw-details-form textarea{font-size:16px}}

/* Three columns of visible actions (up to nine), with a prominent centered task number. */
.tw-columns,.tw-task{grid-template-columns:64px minmax(225px,1.25fr) minmax(145px,1fr) minmax(120px,.85fr) 244px;column-gap:16px}
.tw-columns{padding:0 16px 8px;font-size:11px}
.tw-task{--status-color:var(--muted);position:relative;height:140px;padding:12px 16px;grid-template-rows:74px 28px;row-gap:8px;align-content:center;margin-bottom:8px;border-radius:8px;transition:border-color .15s}
.tw-task::before{content:'';position:absolute;left:-1px;top:18px;bottom:18px;width:3px;border-radius:2px;background:var(--status-color)}
.tw-task[data-tone=ready]{--status-color:var(--app-success)}
.tw-task[data-tone=pending]{--status-color:var(--active)}
.tw-task[data-tone=warning]{--status-color:var(--app-warning)}
.tw-task:hover{border-color:color-mix(in srgb,var(--line) 60%,var(--muted))}
.tw-number{grid-column:1;grid-row:1/3;align-self:stretch;display:flex;align-items:center;justify-content:center;padding:0;font-size:28px;font-weight:600;line-height:1.1;color:var(--ink);overflow-wrap:anywhere;text-align:center;font-variant-numeric:tabular-nums}.tw-columns>span:first-child{text-align:center}
.tw-account{grid-column:2;grid-row:1;gap:2px;padding-left:42px;overflow:hidden;max-height:none;align-self:center}
.tw-account-title{display:flex;align-items:center;gap:8px;max-width:100%;min-width:0}
.tw-account-title strong{min-width:0;font-size:14px}
.tw-account-avatar{position:absolute;left:0;top:3px;width:32px;height:32px;display:grid;place-items:center;border:1px solid var(--line);border-radius:9px;background:var(--soft);font-size:13px;color:var(--muted);overflow:hidden}
.tw-account-avatar img{position:absolute;width:100%;height:100%;object-fit:cover}
.tw-status{display:inline-flex;align-items:center;gap:5px;flex-shrink:0;padding:1px 6px;border-radius:4px;font-size:11px;line-height:18px;color:var(--status-color);background:color-mix(in srgb,var(--status-color) 11%,transparent);white-space:nowrap}
.tw-status-dot{width:5px;height:5px;flex-shrink:0;border-radius:50%;background:currentColor}
.tw-account>span{font-size:11px;line-height:16px}
.tw-account-badges{display:flex;flex-direction:column;align-items:flex-start;gap:3px;min-width:0;max-width:100%;margin-top:2px}
.tw-account-connection{display:flex;align-items:center;gap:6px;min-width:0}
.tw-account-state{display:inline-flex;align-items:center;padding:1px 6px;border-radius:4px;font-size:11px;line-height:18px;white-space:nowrap;color:var(--muted);background:color-mix(in srgb,var(--muted) 11%,transparent)}
.tw-account-state.online{color:var(--app-success);background:color-mix(in srgb,var(--app-success) 11%,transparent)}
.tw-account-badges .tw-account-protocol{font-size:11px;line-height:18px}
.tw-account-protocol{color:var(--muted)}

.tw-seats{grid-column:3;grid-row:1}.tw-people{grid-column:4;grid-row:1}
.tw button.tw-summary{height:54px;padding:6px 9px;background:transparent;border-color:transparent}
.tw button.tw-summary:hover{background:var(--soft);border-color:var(--line)}
.tw-field{font-size:11px;margin-bottom:4px}.tw-value{font-size:13px}
.tw button.tw-note-summary{grid-column:2;grid-row:2;display:flex;align-items:center;gap:7px;height:20px;padding:0;border:0;background:transparent;overflow:hidden;color:var(--muted);text-align:left}
.tw-note-summary .tw-field{font-size:11px;flex-shrink:0;margin:0}
.tw-note-summary .tw-value{display:block;font-size:11px;line-height:18px;white-space:nowrap;min-width:0}
.tw button.tw-log{grid-column:3/5;grid-row:2;display:flex;flex-direction:row;align-items:center;gap:8px;height:20px;padding:0 9px;line-height:18px}
.tw-log time{line-height:18px;margin:0;flex-shrink:0;font-size:11px}
.tw-log>span{display:block;line-height:18px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0;font-size:11px;flex-shrink:1}
.tw button.tw-log-error{color:var(--app-warning)}
.tw-task-operations{grid-column:5;grid-row:1/3;align-self:stretch;display:grid;grid-template-columns:repeat(3,minmax(0,72px));align-content:center;justify-content:center;align-items:center;justify-items:center;gap:7px;padding:0 10px;border-left:1px solid var(--line);min-width:0}
.tw-task-operations button{width:100%;min-width:0;min-height:30px;padding:5px 6px;font-size:12px;white-space:nowrap;display:flex;align-items:center;justify-content:center;text-align:center}
.tw button.tw-start{background:var(--soft)}
.tw button.tw-operation-delete{color:var(--app-error);background:transparent;border-color:transparent}
.tw button.tw-operation-delete:hover{background:var(--app-error-bg)}
@container client-workspace (max-width:1000px){.tw-task-controls{margin-left:-12px;margin-right:-12px;padding-left:12px;padding-right:12px}.tw-columns{display:grid}.tw-columns,.tw-task{grid-template-columns:56px minmax(180px,1.25fr) minmax(110px,1fr) minmax(85px,.8fr) 234px;column-gap:10px}.tw-account{padding-left:0}.tw-account-avatar{display:none}.tw-task-operations{padding-left:10px}}
@container client-workspace (max-width:920px){.tw-columns{display:none}.tw-task{height:auto;min-height:160px;grid-template-columns:56px minmax(0,1fr) minmax(0,1fr) 234px;grid-template-rows:54px 44px 20px;gap:6px 8px;padding:10px}.tw-number{grid-column:1;grid-row:1/5}.tw-account{grid-column:2/4;grid-row:1}.tw-account-avatar{display:grid}.tw-account{padding-left:42px}.tw-seats{grid-column:2;grid-row:2}.tw-people{grid-column:3;grid-row:2}.tw button.tw-summary{height:44px;padding:3px 0}.tw button.tw-note-summary{grid-column:2/4;grid-row:3}.tw button.tw-log{grid-column:2/4;grid-row:4;padding:0;height:18px}.tw-task-operations{grid-column:4;grid-row:1/5;padding-left:8px}.tw-account-protocol{display:none}}
@container client-workspace (max-width:600px){.tw-task{grid-template-columns:44px minmax(0,1fr) minmax(0,1fr);padding:10px;row-gap:8px}.tw-account{grid-column:2/4}.tw-task-operations{grid-column:2/4;grid-row:5;display:grid;grid-template-columns:repeat(3,minmax(0,72px));padding:8px 0 0;border-left:0;border-top:1px solid var(--line)}.tw-account-title{gap:6px}}
@media(pointer:coarse){.tw-task-operations button{min-height:44px}.tw-task{height:auto;min-height:164px;grid-template-rows:minmax(74px,auto) minmax(28px,auto)}.tw button.tw-note-summary,.tw button.tw-log{height:auto;min-height:44px}}
@media(pointer:coarse) and (max-width:760px){.tw-task{grid-template-rows:58px 44px 44px 44px}}
@media(prefers-reduced-motion:reduce){.tw-task{transition:none}}
</style>

<style scoped>
.tw-summary:disabled{cursor:default;opacity:.7}
.tw-cross-setting{display:flex;align-items:center;justify-content:space-between;gap:20px;padding:14px 0;margin-bottom:16px;border-bottom:1px solid var(--line)}
.tw-cross-setting>div{min-width:0}.tw-cross-setting strong{display:block;font-size:13px;font-weight:500}.tw-cross-setting small{display:block;margin-top:5px;color:var(--muted);font-size:12px;line-height:1.6}
.tw button.tw-cross-toggle{flex-shrink:0;position:relative;width:38px;height:22px;border-radius:12px;padding:2px;background:var(--soft);border-color:var(--line)}
.tw button.tw-cross-toggle span{display:block;width:16px;height:16px;border-radius:50%;background:var(--muted);transition:transform .15s}
.tw button.tw-cross-toggle.enabled{background:var(--active);border-color:var(--active)}.tw button.tw-cross-toggle.enabled span{transform:translateX(16px);background:var(--app-on-accent)}
</style>

<style scoped>
.sidebar-service{display:flex;flex-direction:column;align-items:stretch;justify-content:flex-start;gap:16px;margin:0;padding:0 8px 22px;border:0;border-bottom:1px solid var(--app-line);border-radius:0;background:transparent;color:var(--app-ink);font-size:12px}.sidebar-service>div:first-child{display:flex;flex-wrap:wrap;align-items:center;gap:10px}.sidebar-service strong{font-size:13px;font-weight:600}.sidebar-service>div:first-child>span{margin-left:auto;font-size:11px;color:var(--app-success);background:#9bcaac12;border:1px solid #9bcaac30;border-radius:4px;padding:2px 6px}.sidebar-service small{flex-basis:100%;font-size:10px;line-height:1.7;overflow-wrap:anywhere;color:var(--app-muted)}.sidebar-service>div:nth-child(2){display:grid;grid-template-columns:1fr 1fr;gap:8px}.sidebar-service button{font:inherit;font-size:11px;padding:8px 4px;border:1px solid var(--app-line);border-radius:5px;background:var(--app-soft);color:var(--app-ink)}.sidebar-service button:first-child{grid-column:1/-1}.sidebar-service button:hover:enabled{border-color:var(--app-accent);color:var(--app-accent)}.sidebar-service p{font-size:11px;line-height:1.6;margin:0;color:var(--app-warning)}
</style>

<style scoped>
.sidebar-service>div:first-child>span:not([data-state="running"]){color:var(--app-warning);background:var(--app-warning-bg);border-color:var(--app-warning-line)}
.sidebar-service button:focus-visible{outline:2px solid var(--app-accent);outline-offset:3px}
@media(pointer:coarse){.sidebar-service button{min-height:44px}}
</style>

<style scoped>
.sidebar-service .sidebar-service-mini{display:none}
.sidebar-service-dot{width:5px;height:5px;border-radius:50%;background:var(--app-warning);flex:none}
.sidebar-service-dot[data-state="running"]{background:var(--app-success)}
.sidebar-service-dot[data-state="stopped"]{background:var(--app-muted)}
.sidebar-service-dot[data-state="failed"]{background:var(--app-error)}
</style>

<style scoped>
.tw-task-controls .tw-toolbar{margin-top:12px;gap:12px}
.tw-task-controls .tw-toolbar .tw-task-actions{margin-left:0}
</style>

<style scoped>
.tw-task-controls .tw-toolbar{display:grid;grid-template-columns:minmax(180px,220px) minmax(0,1fr);align-items:center;gap:12px}
.tw-task-controls .tw-search{max-width:220px;width:100%}
.tw-task-controls .tw-task-actions{display:flex;flex-wrap:wrap;justify-content:flex-end;gap:8px;min-width:0}
.tw-task-controls .tw-task-actions button{min-height:36px;padding:7px 10px;font-size:12px;white-space:nowrap}
.tw-toast-details{margin-top:4px}
.tw-toast-retry{padding:0;border:0;background:transparent;color:inherit;font:inherit;text-decoration:underline}
@container client-workspace (max-width:720px){.tw-task-controls .tw-toolbar{grid-template-columns:minmax(0,1fr)}.tw-task-controls .tw-task-actions{justify-content:flex-start}}
</style>

<style scoped>
.tw .tw-search input{height:32px;padding:5px 44px 5px 10px;font-size:12px;border-radius:5px}
.tw button.tw-search-clear{right:3px;padding:3px 5px;font-size:11px}
</style>
