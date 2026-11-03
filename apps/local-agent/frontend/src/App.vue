<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { backend, type State, type Form } from './bridge'
const state = ref<State | null>(null)
const form = reactive<Form>({ username: '', password: '', client_number: '', remember: false })
const error = ref(''); const busy = ref(false); const ready = ref(false); const reveal = ref(false)
let timer: ReturnType<typeof setInterval> | undefined
let polling = false
function message(e: unknown) { return e instanceof Error ? e.message : String(e) }
async function remembered() { Object.assign(form, { username: '', password: '', client_number: '', remember: false }, await backend().GetRememberedForm()); reveal.value = false }
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
function login() { return run(async () => { await backend().Login({ ...form }); form.password = ''; reveal.value = false }) }
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
        <template v-else-if="!state.loggedIn"><span class="step">身份验证</span><h2>代理商登录</h2><p>请输入你的账号与客户端编号。</p><form @submit.prevent="login"><label>用户名<input v-model="form.username" autocomplete="off" maxlength="64" required :disabled="busy" placeholder="请输入代理商用户名"/></label><label>密码<div class="password"><input v-model="form.password" :type="reveal ? 'text' : 'password'" autocomplete="off" required :disabled="busy" placeholder="请输入密码"/><button type="button" class="reveal" :aria-label="reveal ? '隐藏密码' : '显示密码'" @click="reveal = !reveal">{{ reveal ? '隐藏' : '显示' }}</button></div></label><label>客户端编号<input v-model="form.client_number" autocomplete="off" maxlength="128" required :disabled="busy" placeholder="请输入全局唯一的编号"/></label><label class="checkbox"><input v-model="form.remember" type="checkbox" :disabled="busy"/>记住登录信息</label><button class="primary full" :disabled="busy">{{ busy ? '正在登录…' : '登录' }}</button></form><small class="hint">每次打开软件均需手动登录。</small><div class="server"><span :title="state.serverURL">{{ state.serverURL }}</span><button class="text" :disabled="busy" @click="importConfig">重新导入配置</button></div></template>
      </section>
    </div><footer>本地配置与登录信息按实例独立保存<span>Robot / Local Agent</span></footer>
  </main>
  <main v-else class="workspace" aria-label="工作空间"></main>
</template>
