<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { backend, type State, type Form } from './bridge'
import TaskWorkspace from './TaskWorkspace.vue'
import ClientShell from './ClientShell.vue'
import ProtocolProxyDialog from './ProtocolProxyDialog.vue'
import { confirmDialog } from './dialogKeyboard'
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
  if (!form.auth_protocol) { error.value = '请选择微信或 YYB 协议'; return }
  if (form.resetTaskIDs) { resetDialog.value?.showModal(); return }
  await openProxyDialog()
}
async function openProxyDialog() {
  if (busy.value || proxyOpen.value) return
  busy.value = true; error.value = ''; proxyError.value = ''; proxyRestart.value = false
  try { proxyInitial.value = await backend().GetProtocolProxy(form.auth_protocol); proxyOpen.value = true }
  catch (e) { error.value = message(e) } finally { busy.value = false }
}
function confirmReset() { if (busy.value) return; resetDialog.value?.close(); void openProxyDialog() }
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
  <main v-if="!state?.loggedIn" class="login-page">
      <section class="card login-card" aria-label="客户端登录">
        <div class="brand"><span class="mark">R</span><span>Robot<small>本地客户端</small></span></div>
        <div v-if="error" role="alert" class="notice error">{{ error }}</div>
        <div v-if="state?.notice" class="notice">{{ state.notice }}</div>
        <template v-if="!ready"><h2>正在初始化</h2></template>
        <template v-else-if="!state?.configured"><h2>导入通信配置</h2><p>请选择管理员提供的 <strong>bootstrap.cfg</strong>。</p><button class="primary full" :disabled="busy" @click="importConfig">{{ busy ? '正在导入…' : '选择 .cfg 配置文件' }}</button></template>
        <template v-else-if="!state.loggedIn">
          <h2>代理商登录</h2>
          <form @submit.prevent="login">
            <div class="login-protocols" role="group" aria-label="授权协议">
              <button v-for="option in [{ value: 'wechat', label: '微信协议' }, { value: 'yyb', label: 'YYB 协议' }]" :key="option.value" type="button" :aria-pressed="form.auth_protocol === option.value" :disabled="busy" @click="form.auth_protocol = option.value as 'wechat' | 'yyb'">{{ option.label }}</button>
            </div>
            <label>用户名<input v-model="form.username" autocomplete="off" maxlength="64" required :disabled="busy"/></label>
            <label>密码<div class="password"><input v-model="form.password" :type="reveal ? 'text' : 'password'" autocomplete="off" required :disabled="busy"/><button type="button" class="reveal" :aria-label="reveal ? '隐藏密码' : '显示密码'" @click="reveal = !reveal">{{ reveal ? '隐藏' : '显示' }}</button></div></label>
            <label>客户端编号<input v-model="form.client_number" autocomplete="off" maxlength="128" required :disabled="busy" placeholder="例如 A01"/></label>
            <div class="login-options"><label class="checkbox"><input v-model="form.remember" type="checkbox" :disabled="busy"/>记住登录信息</label><label class="checkbox"><input v-model="form.resetTaskIDs" type="checkbox" :disabled="busy"/>重置任务编号</label></div>
            <p v-if="form.resetTaskIDs" class="login-reset-note">登录成功后将清除本机任务及其账号绑定，编号从 1 开始；协议账号本身不退出。</p>
            <button class="primary full" :disabled="busy">{{ busy ? '正在登录…' : '登录客户端' }}</button>
          </form>
          <div class="server"><span :title="state.serverURL">已导入通信配置</span><button class="text" :disabled="busy" @click="importConfig">重新导入配置</button></div>
        </template>
      </section>
  </main>
  <dialog ref="resetDialog" class="reset-dialog" aria-labelledby="reset-title" @keydown="confirmDialog($event, confirmReset)" @cancel.prevent="resetDialog?.close()"><header><span class="reset-mark">!</span><div><h2 id="reset-title">重置任务编号</h2></div></header><section><p>登录成功后，将删除本地所有任务及其协议账号绑定，并从编号 1 重新开始。</p><p class="reset-warning">任务删除后无法恢复，协议账号本身不会退出。</p></section><footer><button type="button" @click="resetDialog?.close()">取消</button><button type="button" class="reset-confirm" @click="confirmReset">确认并继续登录</button></footer></dialog>
  <ClientShell v-if="state?.loggedIn && state" :state="state" :busy="busy" @logout="run(async () => { await backend().Logout() })" v-slot="{ page }"><TaskWorkspace :state="state" :page="page" /></ClientShell>
  <ProtocolProxyDialog v-if="proxyOpen" :protocol="form.auth_protocol" :initial="proxyInitial" :busy="busy" :error="proxyError" :restart="proxyRestart" @cancel="cancelProxy" @confirm="confirmProxy" />
</template>
<style scoped>
.login-page{max-width:none;min-height:100vh;padding:28px 16px;display:grid;place-items:center}
.login-page .login-card{width:360px;max-width:100%;margin:0;padding:22px;border-radius:12px}
.login-page .brand{gap:9px;font-size:15px;margin-bottom:22px;letter-spacing:.03em}
.login-page .mark{width:30px;height:30px;border-radius:7px;font-size:17px}
.login-page .brand small{font-size:11px;margin-top:0;letter-spacing:normal}
.login-page h2{font-size:20px;margin:0 0 5px;letter-spacing:normal}
.login-page .login-subtitle{font-size:12px;margin:0 0 18px}
.login-page form{display:grid;gap:12px}
.login-page label{margin:0;font-size:12px}
.login-page input:not([type=checkbox]){margin-top:5px;padding:9px 10px;border-radius:6px;font-size:13px;background:var(--app-bg)}
.login-page .password input{padding-right:47px}
.login-page .reveal{top:0;bottom:0;right:8px;padding:0;font-size:11px}
.login-protocols{display:flex;gap:4px;background:var(--app-bg);border:1px solid var(--app-line);border-radius:7px;padding:3px}
.login-protocols button{flex:1;padding:6px 10px;border:0;border-radius:5px;background:transparent;color:var(--app-muted);font:inherit;font-size:12px}
.login-protocols button[aria-pressed=true]{background:var(--app-soft);color:var(--app-accent)}
.login-options{display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:8px;margin:1px 0}
.login-page .checkbox{margin:0;gap:6px;font-size:11px}
.login-page .checkbox input{margin:0;width:13px;height:13px}
.login-page .primary{padding:10px;border-radius:6px;font-size:13px;font-weight:600}
.login-page .login-reset-note{font-size:11px;color:var(--app-warning);background:var(--app-warning-bg);padding:8px 10px;border-radius:5px;margin:0}
.login-page .server{margin-top:16px;padding-top:13px;font-size:11px}
.login-page .notice{margin-bottom:12px}
@media(pointer:coarse){.login-page input:not([type=checkbox]){font-size:16px}.login-protocols button,.login-page .checkbox{min-height:40px}}
.reset-dialog{width:420px;max-width:calc(100vw - 28px);padding:0;border:1px solid var(--app-line);border-radius:10px;background:var(--app-panel);color:var(--app-ink);font:14px/1.55 'Segoe UI','Microsoft YaHei',sans-serif;box-shadow:0 18px 55px var(--app-shadow)}
.reset-dialog::backdrop{background:var(--app-backdrop);backdrop-filter:blur(2px)}
.reset-dialog header{display:flex;align-items:center;gap:12px;padding:20px 22px;border-bottom:1px solid var(--app-line)}
.reset-mark{display:grid;place-items:center;width:32px;height:32px;flex:none;border-radius:50%;background:var(--app-warning-bg);color:var(--app-warning);font-weight:700}
.reset-dialog h2{margin:0;font-size:17px;font-weight:600}.reset-dialog header p,.reset-dialog section p{margin:4px 0 0;color:var(--app-muted);font-size:12px}
.reset-dialog section{padding:20px 22px}.reset-dialog section p:first-child{margin:0;font-size:13px;line-height:1.7}.reset-dialog section .reset-warning{margin-top:12px;padding:10px 12px;border-left:3px solid var(--app-warning);background:var(--app-warning-bg);color:var(--app-warning)}
.reset-dialog footer{display:flex;justify-content:flex-end;gap:8px;padding:14px 22px;border-top:1px solid var(--app-line)}.reset-dialog button{padding:8px 12px;border:1px solid var(--app-line);border-radius:6px;background:var(--app-panel);color:var(--app-ink);font:inherit}.reset-dialog .reset-confirm{border-color:var(--app-danger);background:var(--app-danger);color:var(--app-on-danger)}
</style>
