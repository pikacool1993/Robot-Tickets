<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, clearToken, token } from './api.ts'
import BusinessSettings from './BusinessSettings.vue'
import FeishuSettings from './FeishuSettings.vue'
import MatchSettings from './MatchSettings.vue'
import TerminalTasks from './TerminalTasks.vue'
import TerminalTaskPage from './TerminalTaskPage.vue'
import AdminOrders from './AdminOrders.vue'
import ViewerRoster from './ViewerRoster.vue'
const matchSettings = ref(false)
const currentMatch = ref<any | null>(null), matchError = ref(''), passwordCopied = ref(false)
const detailTab = computed(() => route.query.tab === 'devices' ? 'devices' : 'basic')
async function loadMatch() { try { currentMatch.value = await api.currentMatch(); matchError.value = '' } catch { matchError.value = '比赛信息加载失败，可点击配置重试' } }
function closeMatch() { matchSettings.value = false; void loadMatch() }
async function copyPassword() { try { await navigator.clipboard.writeText(createdPassword.value); passwordCopied.value = true } catch { error.value = '复制失败，请手动复制新密码' } }
const businessSettings = ref<{ agentId?: string; agentName?: string } | null>(null)

const route = useRoute(); const router = useRouter()
const loggedIn = ref(!!token()); const username = ref(''); const loginForm = ref({ username: '', password: '' }); const error = ref(''); const loading = ref(false)
const agents = ref<any[]>([]); const selected = ref<any | null>(null); const audit = ref<any[]>([]); const showCreate = ref(false); const createForm = ref({ name: '', username: '', max_devices: 1 }); const createdPassword = ref('')
watch(() => createdPassword.value, () => { passwordCopied.value = false })
const showAudit = ref(false); const auditLoading = ref(false); const auditError = ref(''); const auditButton = ref<HTMLButtonElement | null>(null); const auditClose = ref<HTMLButtonElement | null>(null)
let auditRevision = 0
const showEdit = ref(false); const editForm = ref({ name: '', username: '' })
const dialog = ref({ open: false, title: '', message: '', confirmLabel: '确认', danger: false, inputLabel: '', expected: '', value: '' })
let resolveDialog: ((confirmed: boolean) => void) | undefined
const originalBodyOverflow = document.body.style.overflow
watch(() => matchSettings.value || !!businessSettings.value || showAudit.value || showEdit.value || showCreate.value || !!createdPassword.value || dialog.value.open,
  open => { document.body.style.overflow = open ? 'hidden' : originalBodyOverflow })
const statusLabel: Record<string, string> = { enabled: '启用', disabled: '禁用', locked: '锁定', suspended: '暂停' }
const pageTitle = computed(() => route.path === '/admin/orders' ? '订单查询' : route.params.agentId ? '' : '代理商管理')
async function run(action: () => Promise<any>) { error.value = ''; loading.value = true; try { return await action() } catch (e: any) { if (e.status === 401) { logout(); error.value = '会话已过期，请重新登录' } else error.value = e.message || '请求失败' } finally { loading.value = false } }
async function login() { const admin = await run(() => api.login(loginForm.value.username, loginForm.value.password)); if (admin) { username.value = admin.username; loggedIn.value = true; await router.push('/admin/agents'); await loadAgents(); void loadMatch() } }
async function loadAgents() { const data = await run(() => api.agents()); if (data) { agents.value = Array.isArray(data) ? data : data.items || []; resetCountdown() } }
let viewRevision = 0
let refreshTimer: ReturnType<typeof setInterval> | undefined
const refreshing = ref(false)
const deviceRefreshError = ref('')
const lastRefreshed = ref('')
const refreshInterval = 60
let refreshDeadline = Date.now() + refreshInterval * 1000
function resetCountdown() { refreshDeadline = Date.now() + refreshInterval * 1000 }
function formatTime(value: string | null) { return value ? new Date(value).toLocaleString() : '—' }
function backToList() { closeAudit(); viewRevision++; selected.value = null; deviceRefreshError.value = ''; void router.push('/admin/agents'); void loadAgents() }
async function openAgent(agent: any) {
  const revision = ++viewRevision
  const data = await run(() => api.agent(agent.id))
  if (revision !== viewRevision || !loggedIn.value || !data) return
  selected.value = data; if (route.params.agentId !== data.id) await router.push(`/admin/agents/${encodeURIComponent(data.id)}`); deviceRefreshError.value = ''; lastRefreshed.value = new Date().toLocaleTimeString(); resetCountdown()
}
function closeAudit() {
  auditRevision++; showAudit.value = false; audit.value = []; auditError.value = ''; auditLoading.value = false
  void nextTick(() => auditButton.value?.focus())
}
async function openAudit() {
  if (!selected.value || auditLoading.value) return
  const id = selected.value.id; const revision = ++auditRevision
  showAudit.value = true; auditLoading.value = true; auditError.value = ''; audit.value = []
  await nextTick(); auditClose.value?.focus()
  try {
    const data = await api.audit(id)
    if (revision === auditRevision && selected.value?.id === id) audit.value = data || []
  } catch (e: any) {
    if (revision !== auditRevision) return
    if (e.status === 401) { await logout(); error.value = '会话已过期，请重新登录' }
    else auditError.value = e.message || '事件加载失败'
  } finally { if (revision === auditRevision) auditLoading.value = false }
}
async function refreshDevices() {
  if (refreshing.value || loading.value || !loggedIn.value || document.hidden) return
  refreshing.value = true
  resetCountdown()
  const revision = viewRevision; const id = selected.value?.id
  try {
    const data = id ? await api.agent(id) : await api.agents()
    if (revision !== viewRevision || !loggedIn.value) return
    if (id) {
      if (selected.value?.id !== id) return
      selected.value = data
    } else if (!selected.value) agents.value = Array.isArray(data) ? data : data.items || []
    deviceRefreshError.value = ''; lastRefreshed.value = new Date().toLocaleTimeString()
  } catch (e: any) {
    if (revision !== viewRevision || !loggedIn.value) return
    if (e.status === 401) { await logout(); error.value = '会话已过期，请重新登录' }
    else if (id && e.status === 404) { backToList(); error.value = '代理商已不存在' }
    else deviceRefreshError.value = '刷新失败，当前显示上次获取的数据，请重试'
  } finally { refreshing.value = false }
}
async function createAgent() { const data = await run(() => api.createAgent(createForm.value)); if (data) { createdPassword.value = data.password; showCreate.value = false; createForm.value = { name: '', username: '', max_devices: 1 }; await loadAgents() } }
function startEdit() { if (!selected.value) return; editForm.value = { name: selected.value.name, username: selected.value.username }; showEdit.value = true }
async function saveAgent() { if (!selected.value) return; const data = await run(() => api.updateAgent(selected.value.id, editForm.value)); if (data) { showEdit.value = false; await openAgent(selected.value); await loadAgents() } }
function askDialog(options: Partial<typeof dialog.value>) { dialog.value = { open: true, title: '', message: '', confirmLabel: '确认', danger: false, inputLabel: '', expected: '', value: '', ...options }; return new Promise<boolean>((resolve) => { resolveDialog = resolve }) }
function closeDialog(confirmed: boolean) { if (confirmed && dialog.value.expected && dialog.value.value !== dialog.value.expected) return; dialog.value.open = false; resolveDialog?.(confirmed); resolveDialog = undefined }
async function exportConfig() {
  if (loading.value || !await askDialog({ title: '导出客户端配置', message: '配置文件包含客户端通信密钥，请仅交付给可信客户。本次导出会记录审计，文件不包含账号密码。', confirmLabel: '导出 .cfg 文件' })) return
  await run(() => api.exportClientConfig())
}
async function updateStatus(status: string) { if (!selected.value || !await askDialog({ title: '更改账号状态', message: status === 'disabled' ? '禁用后该代理商将无法登录，现有登录会话也将被撤销。确定禁用吗？' : '启用后该代理商可以重新登录。确定启用吗？', confirmLabel: status === 'disabled' ? '禁用账号' : '启用账号' })) return; await run(() => api.setStatus(selected.value.id, status)); await openAgent(selected.value); await loadAgents() }
async function resetPassword() { if (!selected.value || !await askDialog({ title: '重置代理商密码', message: '重置后将撤销该代理商现有的登录会话。确定继续吗？', confirmLabel: '重置密码', danger: true })) return; const data = await run(() => api.resetCredential(selected.value.id)); if (data) { createdPassword.value = data.password; await openAgent(selected.value) } }
async function removeAgent() { if (!selected.value || !await askDialog({ title: '删除代理商', message: '账号及关联历史日志将永久删除。用户名之后可重新使用。', confirmLabel: '永久删除', danger: true, inputLabel: `输入代理商名称 ${selected.value.name} 以确认`, expected: selected.value.name })) return; const data = await run(async () => { await api.deleteAgent(selected.value.id); return true }); if (data) backToList() }
async function logout() { matchSettings.value = false; businessSettings.value = null; try { if (token()) await api.logout() } catch {} closeAudit(); viewRevision++; clearToken(); loggedIn.value = false; selected.value = null; createdPassword.value = ''; await router.push('/admin/login') }
let detailScroll = 0
watch(() => route.fullPath, async (_, previous) => {
  const id = route.params.agentId
  if (route.name === 'terminal-tasks') { if (!previous?.includes('/tasks')) detailScroll = window.scrollY; return }
  if (!id) { viewRevision++; selected.value = null; return }
  if (typeof id === 'string' && selected.value?.id !== id && loggedIn.value) await openAgent({ id })
  if (previous?.includes('/tasks') && typeof id === 'string') { await nextTick(); window.scrollTo(0, detailScroll) }
}, { immediate: true })
onUnmounted(() => { auditRevision++; if (refreshTimer) clearInterval(refreshTimer); viewRevision++; document.body.style.overflow = originalBodyOverflow })
onMounted(async () => { refreshTimer = setInterval(() => { if (Date.now() >= refreshDeadline) void refreshDevices() }, 1000); if (loggedIn.value) { const me = await run(() => api.me()); if (me) { username.value = me.username; await loadAgents(); void loadMatch() } } })
</script>

<template>
  <main v-if="route.path === '/admin/login'" class="login-page"><form class="login-card" @submit.prevent="login"><div class="brand">ROBOT</div><h1>管理员登录</h1><label>用户名<input v-model="loginForm.username" autocomplete="username" required /></label><label>密码<input v-model="loginForm.password" type="password" autocomplete="current-password" required /></label><p v-if="error" class="error">{{ error }}</p><button :disabled="loading">{{ loading ? '登录中…' : '登录' }}</button></form></main>
<ViewerRoster v-else-if="route.path === '/admin/orders/viewers'" />
<main v-else class="shell"><aside><div class="brand">ROBOT <span>管理后台</span></div><nav><RouterLink to="/admin/agents" @click="selected=null" :class="{ active: route.path.startsWith('/admin/agents') }">代理商管理</RouterLink><RouterLink to="/admin/orders" :class="{ active: route.path === '/admin/orders' }">订单查询</RouterLink></nav></aside><section class="content"><header class="admin-header"><h1 v-if="pageTitle">{{ pageTitle }}</h1><div class="account">{{ username }} <button class="link" @click="logout">退出</button></div></header><div v-if="error" class="alert">{{ error }}</div><AdminOrders v-if="route.path === '/admin/orders'" /><TerminalTaskPage v-else-if="route.name === 'terminal-tasks'" :key="`${route.params.agentId}:${route.params.instance}`" />
<section v-else-if="!selected" class="agent-list-page">
  <section class="match-card"><div class="match-card-info"><small v-if="currentMatch?.name">当前比赛</small><h2>{{ currentMatch?.name || '尚未设置当前比赛' }}</h2><p v-if="matchError" role="alert">{{ matchError }}</p><p v-else-if="currentMatch?.id">场次 ID：{{ currentMatch.id }} · {{ currentMatch.enabled ? '已启用' : '未启用' }}</p></div><button @click="matchSettings = true">{{ currentMatch?.id ? '配置当前比赛' : '设置比赛' }} →</button></section>
  <section class="panel"><div class="toolbar"><h2>代理商列表</h2><button @click="showCreate = true">＋ 创建代理商</button></div>
  <div class="agent-tools"><button class="secondary" @click="businessSettings = {}">管理设备模板</button><button class="secondary" :disabled="loading" @click="exportConfig">导出客户端配置</button><button class="secondary list-refresh" :disabled="loading || refreshing" @click="refreshDevices">{{ refreshing ? '刷新中…' : '刷新' }}</button></div><p v-if="deviceRefreshError" role="alert" class="error">{{ deviceRefreshError }}</p><div v-if="!agents.length" class="empty">暂无代理商</div><table v-else class="responsive-table agents-table"><thead><tr><th>名称</th><th>用户名</th><th>状态</th><th>设备</th><th>最后登录</th><th></th></tr></thead><tbody><tr v-for="agent in agents" :key="agent.id"><td data-label="名称">{{ agent.name }}</td><td data-label="用户名">{{ agent.username }}</td><td data-label="状态"><span :class="['status', agent.status]">{{ statusLabel[agent.status] || agent.status }}</span></td><td data-label="设备">{{ agent.active_devices ?? 0 }} / {{ agent.max_devices }}</td><td data-label="最后登录">{{ agent.last_login_at ? new Date(agent.last_login_at).toLocaleString() : '从未登录' }}</td><td data-label="操作"><button class="link" @click="openAgent(agent)">详情</button></td></tr></tbody></table></section></section><section v-else class="panel detail"><button class="back link" @click="backToList">← 返回列表</button><div class="detail-head"><div class="detail-identity"><div class="detail-title"><h1>{{ selected.name }}</h1><button ref="auditButton" class="event-link" @click="openAudit"><svg aria-hidden="true" viewBox="0 0 16 16" fill="none"><path d="M8 3a5 5 0 1 1-4.6 3M2 2.8v3.5h3.5M8 5v3l2 1.2" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg><span>查看事件</span></button></div><p>{{ selected.username }} · {{ selected.id }}</p></div><div class="detail-primary-actions"><span :class="['status', selected.status]">{{ statusLabel[selected.status] }}</span><button @click="startEdit">编辑资料</button></div></div><div class="detail-tabs" role="tablist" aria-label="代理商详情"><button role="tab" :aria-selected="detailTab === 'basic'" :class="{active: detailTab === 'basic'}" @click="router.replace({query:{tab:'basic'}})">基本信息</button><button role="tab" :aria-selected="detailTab === 'devices'" :class="{active: detailTab === 'devices'}" @click="router.replace({query:{tab:'devices'}})">业务登录设备（{{ selected.devices?.length || 0 }}）</button></div><section v-if="detailTab === 'basic'" role="tabpanel" aria-label="基本信息"><div class="facts"><div><small>创建时间</small><strong>{{ selected.created_at ? new Date(selected.created_at).toLocaleString() : '—' }}</strong></div><div><small>最后登录 IP</small><strong>{{ selected.last_login_ip || '—' }}</strong></div><div><small>已登录终端 / 设备上限</small><strong>{{ selected.active_devices }} / {{ selected.max_devices }}</strong></div></div><section class="security-section"><h3>登录安全</h3><div class="setting-row"><div><strong>登录密码</strong></div><button class="secondary" :disabled="loading" @click="resetPassword">重置密码</button></div><div class="setting-row"><div><strong>账号状态</strong></div><button class="secondary" :disabled="loading" @click="updateStatus(selected.status === 'enabled' ? 'disabled' : 'enabled')">{{ selected.status === 'enabled' ? '禁用账号' : '启用账号' }}</button></div></section>
<FeishuSettings :key="selected.id" :agent-id="selected.id" @unauthorized="logout" />
<section class="danger-zone"><h3>危险操作</h3><button class="danger-button" :disabled="loading" @click="removeAgent">删除账号</button></section></section>
<section v-else role="tabpanel" aria-label="业务登录设备"><BusinessSettings :key="selected.id" :agent-id="selected.id" embedded @unauthorized="logout" /><TerminalTasks :agent-id="selected.id" :devices="selected.devices || []" :last-refreshed="lastRefreshed" :refreshing="refreshing" :loading="loading" @refresh="refreshDevices" /></section></section></section><MatchSettings v-if="matchSettings" @close="closeMatch" @unauthorized="logout" /><BusinessSettings v-if="businessSettings" v-bind="businessSettings" @close="businessSettings = null" @unauthorized="logout" /><div v-if="showAudit && selected" class="overlay" @click.self="closeAudit" @keydown.esc.stop="closeAudit"><section class="modal audit-modal" role="dialog" aria-modal="true" aria-labelledby="audit-title"><button ref="auditClose" class="close" aria-label="关闭事件" @click="closeAudit">×</button><h2 id="audit-title">事件 · {{ selected.name }}</h2><p v-if="auditLoading" role="status">正在加载事件…</p><div v-else-if="auditError" role="alert"><p class="error">{{ auditError }}</p><button class="secondary" @click="openAudit">重试</button></div><div v-else-if="!audit.length" class="empty">暂无事件</div><div v-else class="audit-table"><table class="responsive-table"><thead><tr><th>事件</th><th>结果</th><th>时间</th><th>来源 IP</th></tr></thead><tbody><tr v-for="(item, index) in audit" :key="index"><td data-label="事件">{{ item.event_type }}</td><td data-label="结果">{{ item.result }}</td><td data-label="时间">{{ formatTime(item.occurred_at) }}</td><td data-label="来源 IP">{{ item.source_ip || '—' }}</td></tr></tbody></table></div><div class="modal-actions"><button class="secondary" @click="closeAudit">关闭</button></div></section></div><div v-if="showEdit" class="overlay" @click.self="showEdit = false"><form class="modal" @submit.prevent="saveAgent"><button type="button" class="close" aria-label="关闭" @click="showEdit = false">×</button><h2>编辑代理商资料</h2><label>代理商名称<input v-model="editForm.name" maxlength="200" required /></label><label>用户名<input v-model="editForm.username" pattern="[a-z0-9_.-]{3,64}" maxlength="64" required /></label><div class="modal-actions"><button type="button" class="secondary" @click="showEdit = false">取消</button><button :disabled="loading">保存修改</button></div></form></div><div v-if="showCreate" class="overlay" @click.self="showCreate = false"><form class="modal" @submit.prevent="createAgent"><button type="button" class="close" aria-label="关闭" @click="showCreate = false">×</button><h2>创建代理商</h2><label>代理商名称<input v-model="createForm.name" maxlength="200" required /></label><label>用户名<input v-model="createForm.username" pattern="[a-z0-9_.-]{3,64}" maxlength="64" required /></label><label>设备上限<input v-model.number="createForm.max_devices" type="number" min="1" max="10000" required /></label><div class="modal-actions"><button type="button" class="secondary" @click="showCreate = false">取消</button><button :disabled="loading">创建并生成密码</button></div></form></div><div v-if="createdPassword" class="overlay" @click.self="createdPassword = ''"><div class="modal"><button class="close" aria-label="关闭" @click="createdPassword = ''">×</button><h2>新密码</h2><p>关闭后将无法在此查看，请先复制。</p><code>{{ createdPassword }}</code><button class="full-width secondary" @click="copyPassword">{{ passwordCopied ? '已复制' : '复制新密码' }}</button><button class="full-width" @click="createdPassword = ''">关闭</button></div></div><div v-if="dialog.open" class="overlay" @click.self="closeDialog(false)" @keydown.esc="closeDialog(false)"><form class="modal" role="alertdialog" aria-modal="true" @submit.prevent="closeDialog(true)"><button type="button" class="close" aria-label="关闭" @click="closeDialog(false)">×</button><h2>{{ dialog.title }}</h2><p>{{ dialog.message }}</p><label v-if="dialog.inputLabel">{{ dialog.inputLabel }}<input v-model="dialog.value" autocomplete="off" /></label><div class="modal-actions"><button type="button" class="secondary" @click="closeDialog(false)">取消</button><button :class="{ danger: dialog.danger }" :disabled="!!dialog.expected && dialog.value !== dialog.expected">{{ dialog.confirmLabel }}</button></div></form></div></main>
</template>
