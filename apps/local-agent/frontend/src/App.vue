<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { backend, type State, type Form } from './bridge'
import TaskWorkspace from './TaskWorkspace.vue'
import ProtocolProxyDialog from './ProtocolProxyDialog.vue'
const state = ref<State | null>(null)
const form = reactive<Form>({ username: '', password: '', client_number: '', remember: false, auth_protocol: '', resetTaskIDs: false })
const error = ref(''); const busy = ref(false); const ready = ref(false); const reveal = ref(false)
const proxyOpen = ref(false), proxyInitial = ref(''), proxyError = ref(''), proxyRestart = ref(false)
const resetDialog = ref<HTMLDialogElement | null>(null)
let timer: ReturnType<typeof setInterval> | undefined
let polling = false
function message(e: unknown) { return e instanceof Error ? e.message : String(e) }
async function remembered() { Object.assign(form, { username: '', password: '', client_number: '', remember: false, auth_protocol: '', resetTaskIDs: false }, await backend().GetRememberedForm()); reveal.value = false }
async function refresh() {
  if (polling) return
  polling = true
  try {
    const next = await backend().GetState()
    if (state.value?.loggedIn && !next.loggedIn) await remembered()
    state.value = next
  } finally { polling = false }
}
async function run(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { await action(); await refresh() } catch (e) { error.value = message(e) } finally { busy.value = false }
}
async function login() {
  if (busy.value || proxyOpen.value) return
  if (form.resetTaskIDs) { resetDialog.value?.showModal(); return }
  await openProxyDialog()
}
async function openProxyDialog() {
  if (busy.value || proxyOpen.value) return
  busy.value = true; error.value = ''; proxyError.value = ''; proxyRestart.value = false
  try { proxyInitial.value = await backend().GetProtocolProxy(form.auth_protocol); proxyOpen.value = true }
  catch (e) { error.value = message(e) } finally { busy.value = false }
}
function confirmReset() { resetDialog.value?.close(); void openProxyDialog() }
async function confirmProxy(proxy: string, confirmRestart: boolean) {
  if (busy.value) return
  busy.value = true; proxyError.value = ''
  try { await backend().Login({ ...form, proxy, confirmRestart }); proxyOpen.value = false; proxyInitial.value = ''; form.password = ''; reveal.value = false; await refresh() }
  catch (e) { proxyError.value = message(e); if (proxyError.value.includes('需要确认重启')) proxyRestart.value = true }
  finally { busy.value = false }
}
function cancelProxy() { if (!busy.value) { proxyOpen.value = false; proxyInitial.value = ''; proxyError.value = ''; proxyRestart.value = false } }
function importConfig() { return run(async () => { await backend().ImportConfig(); await remembered() }) }
onMounted(async () => {
  try { await refresh(); await remembered(); ready.value = true; timer = setInterval(() => { if (!busy.value) void refresh().catch(e => { error.value = message(e) }) }, 1000) }
  catch (e) { error.value = message(e) }
})
onUnmounted(() => { if (timer) clearInterval(timer); form.password = '' })
</script>

<template>
  <main v-if="!state?.loggedIn">
    <header><div class="brand"><span class="mark">R</span><span>Robot<small>本地客户端</small></span></div><span class="edition">WINDOWS · 0.1.0</span></header>
    <div class="layout">
      <aside><span class="eyebrow">LOCAL AGENT</span><h1>连接云端，<br/>从这里开始。</h1><p>独立实例，本地运行。<br/>登录后进入你的工作空间。</p><div class="instance"><span class="dot"></span>当前文件夹 · 独立实例<code>{{ state?.instanceID || '正在初始化…' }}</code></div></aside>
      <section class="card" aria-live="polite">
        <div v-if="error" role="alert" class="notice error">{{ error }}</div>
        <div v-if="state?.notice" class="notice">{{ state.notice }}</div>
        <template v-if="!ready"><h2>正在初始化</h2><p>准备本地配置与运行环境。</p></template>
        <template v-else-if="!state?.configured"><span class="step">首次使用</span><h2>导入通信配置</h2><p>选择管理员提供的 <strong>bootstrap.cfg</strong> 文件，配置完成后即可登录。</p><div class="file-box"><span class="file-icon">CFG</span><strong>客户端通信配置</strong><span>仅接受有效的 Robot 配置文件</span></div><button class="primary full" :disabled="busy" @click="importConfig">{{ busy ? '正在导入…' : '选择 .cfg 配置文件' }}</button><small class="hint">如软件包已附带配置，首次启动会自动导入。</small></template>
        <template v-else-if="!state.loggedIn"><span class="step">身份验证</span><h2>代理商登录</h2><p>请输入你的账号与客户端编号，并选择本客户端的授权协议。</p><form @submit.prevent="login"><label>授权协议<span class="protocol-select" :class="{ disabled: busy }"><select v-model="form.auth_protocol" autocomplete="off" required :disabled="busy"><option disabled value="">请选择微信或 YYB</option><option value="wechat">微信协议</option><option value="yyb">YYB 协议</option></select></span></label><label>用户名<input v-model="form.username" autocomplete="off" maxlength="64" required :disabled="busy" placeholder="请输入代理商用户名"/></label><label>密码<div class="password"><input v-model="form.password" :type="reveal ? 'text' : 'password'" autocomplete="off" required :disabled="busy" placeholder="请输入密码"/><button type="button" class="reveal" :aria-label="reveal ? '隐藏密码' : '显示密码'" @click="reveal = !reveal">{{ reveal ? '隐藏' : '显示' }}</button></div></label><label>客户端编号<input v-model="form.client_number" autocomplete="off" maxlength="128" required :disabled="busy" placeholder="请输入全局唯一的编号"/></label><label class="checkbox"><input v-model="form.resetTaskIDs" type="checkbox" :disabled="busy"/>登录成功后重置本地任务编号</label><label class="checkbox"><input v-model="form.remember" type="checkbox" :disabled="busy"/>记住登录信息</label><button class="primary full" :disabled="busy">{{ busy ? '正在登录…' : '登录' }}</button></form><small class="hint">重置将清除本机现有任务及协议账号绑定，并从编号 1 重新开始。</small><small class="hint">每次打开软件均需手动登录。授权协议按客户端保存，所有任务共用。</small><div class="server"><span :title="state.serverURL">{{ state.serverURL }}</span><button class="text" :disabled="busy" @click="importConfig">重新导入配置</button></div></template>
      </section>
    </div><footer>本地配置与登录信息按实例独立保存<span>Robot / Local Agent</span></footer>
  </main>
  <dialog ref="resetDialog" class="reset-dialog" aria-labelledby="reset-title" @cancel.prevent="resetDialog?.close()"><header><span class="reset-mark">!</span><div><h2 id="reset-title">重置任务编号</h2><p>此操作会清除本机现有任务</p></div></header><section><p>登录成功后，将删除本地所有任务及其协议账号绑定，并从编号 1 重新开始。</p><p class="reset-warning">任务删除后无法恢复，协议账号本身不会退出。</p></section><footer><button type="button" @click="resetDialog?.close()">取消</button><button type="button" class="reset-confirm" @click="confirmReset">确认并继续登录</button></footer></dialog>
  <TaskWorkspace v-if="state?.loggedIn && state" :state="state" @logout="run(async () => { await backend().Logout() })" />
  <ProtocolProxyDialog v-if="proxyOpen" :protocol="form.auth_protocol" :initial="proxyInitial" :busy="busy" :error="proxyError" :restart="proxyRestart" @cancel="cancelProxy" @confirm="confirmProxy" />
</template>
<style scoped>
.reset-dialog{width:420px;max-width:calc(100vw - 28px);padding:0;border:1px solid var(--app-line);border-radius:10px;background:var(--app-panel);color:var(--app-ink);font:14px/1.55 'Segoe UI','Microsoft YaHei',sans-serif;box-shadow:0 18px 55px var(--app-shadow)}
.reset-dialog::backdrop{background:var(--app-backdrop);backdrop-filter:blur(2px)}
.reset-dialog header{display:flex;align-items:center;gap:12px;padding:20px 22px;border-bottom:1px solid var(--app-line)}
.reset-mark{display:grid;place-items:center;width:32px;height:32px;flex:none;border-radius:50%;background:var(--app-warning-bg);color:var(--app-warning);font-weight:700}
.reset-dialog h2{margin:0;font-size:17px;font-weight:600}.reset-dialog header p,.reset-dialog section p{margin:4px 0 0;color:var(--app-muted);font-size:12px}
.reset-dialog section{padding:20px 22px}.reset-dialog section p:first-child{margin:0;font-size:13px;line-height:1.7}.reset-dialog section .reset-warning{margin-top:12px;padding:10px 12px;border-left:3px solid var(--app-warning);background:var(--app-warning-bg);color:var(--app-warning)}
.reset-dialog footer{display:flex;justify-content:flex-end;gap:8px;padding:14px 22px;border-top:1px solid var(--app-line)}.reset-dialog button{padding:8px 12px;border:1px solid var(--app-line);border-radius:6px;background:var(--app-panel);color:var(--app-ink);font:inherit}.reset-dialog .reset-confirm{border-color:var(--app-danger);background:var(--app-danger);color:var(--app-on-danger)}
</style>
