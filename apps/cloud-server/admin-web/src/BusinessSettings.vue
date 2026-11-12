<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref } from 'vue'
import { api } from './api.ts'
const props = defineProps<{ agentId?: string; agentName?: string }>()
const emit = defineEmits<{ close: []; unauthorized: [] }>()
const labels: Record<string, string> = { windows: 'Windows', android: 'Android', ios: 'iOS' }
const templates = ref<any[]>([])
const device = ref('windows')
const draft = ref('')
const revision = ref(0)
const loading = ref(true)
const saving = ref(false)
const loaded = ref(false)
const error = ref('')
const message = ref('')
const configStatus = ref('')
const panel = ref<HTMLElement | null>(null)
const previousFocus = document.activeElement as HTMLElement | null
let alive = true
function fail(e: any) { if (!alive) return; if (e.status === 401) emit('unauthorized'); else error.value = e.message || '请求失败' }
function chooseTemplate() {
  const item = templates.value.find(t => t.device_type === device.value)
  revision.value = item?.revision || 0
  draft.value = item ? JSON.stringify(item.template, null, 2) : ''
  error.value = ''; message.value = ''
}
async function load() {
  loading.value = true; loaded.value = false; error.value = ''; message.value = ''
  try {
    const data = props.agentId ? await api.agentBusinessConfig(props.agentId) : await api.businessTemplates()
    if (!alive) return
    if (props.agentId) {
      device.value = data.device_type || 'windows'; revision.value = data.config_revision || 0
      configStatus.value = data.ready ? `当前：${labels[data.device_type]} · 模板版本 ${data.template_revision}` : data.message
    } else { templates.value = data; chooseTemplate() }
    loaded.value = true
  } catch (e) { fail(e) } finally { if (alive) loading.value = false }
}
async function save() {
  if (saving.value || loading.value || !loaded.value) return
  error.value = ''; message.value = ''
  let template: unknown
  if (!props.agentId) {
    try { template = JSON.parse(draft.value) } catch { error.value = '模板 JSON 格式错误，请检查后重试'; return }
  }
  saving.value = true
  try {
    const data = props.agentId
      ? await api.saveAgentBusinessConfig(props.agentId, { device_type: device.value, revision: revision.value })
      : await api.saveBusinessTemplate(device.value, { template, revision: revision.value })
    if (!alive) return
    revision.value = data.revision
    if (!props.agentId) {
      const item = templates.value.find(t => t.device_type === device.value)
      if (item) { item.template = template; item.revision = data.revision }
    } else configStatus.value = `已配置：${labels[device.value]}`
    message.value = '已保存，下次客户端登录时生效。'
  } catch (e) { fail(e) } finally { if (alive) saving.value = false }
}
function keydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && !saving.value) { event.preventDefault(); emit('close'); return }
  if (event.key !== 'Tab') return
  const elements = panel.value?.querySelectorAll<HTMLElement>('button:not(:disabled), select:not(:disabled), textarea:not(:disabled)')
  if (!elements?.length) return
  const first = elements[0]; const last = elements[elements.length - 1]
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
}
onMounted(async () => { await nextTick(); panel.value?.focus(); await load() })
onUnmounted(() => { alive = false; previousFocus?.focus() })
</script>

<template>
  <div class="overlay" @keydown="keydown">
    <section ref="panel" class="modal business-modal" role="dialog" aria-modal="true" aria-labelledby="business-title" tabindex="-1">
      <button class="close" aria-label="关闭业务设备设置" :disabled="saving" @click="emit('close')">×</button>
      <h2 id="business-title">{{ agentId ? `业务登录设备 · ${agentName}` : '业务设备模板' }}</h2>
      <p>{{ agentId ? '选择此代理商所有客户端使用的业务登录设备。' : '三个设备模板全局共用，修改会影响选择该设备的所有代理商。' }}修改仅在下次客户端登录时生效。</p>
      <p v-if="configStatus" class="config-status">{{ configStatus }}</p>
      <p v-if="loading" role="status">正在加载…</p>
      <form v-else @submit.prevent="save">
        <label>设备类型<select v-model="device" :disabled="saving" @change="!agentId && chooseTemplate()"><option v-for="(label, key) in labels" :key="key" :value="key">{{ label }}</option></select></label>
        <template v-if="!agentId">
          <p class="hint">模板包含 user_agent、referer、e、s、v。code 与 s.deviceId 由客户端生成；iOS 不包含 e.sessionId。切换设备会重新载入已保存模板。</p>
          <label>参数模板（JSON）<textarea v-model="draft" :disabled="saving" spellcheck="false" rows="18" maxlength="65536" required /></label>
          <small>当前模板版本：{{ revision }}</small>
        </template>
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <p v-if="message" class="success" role="status">{{ message }}</p>
        <div class="modal-actions"><button type="button" class="secondary" :disabled="saving" @click="load">重新加载</button><button :disabled="saving || loading || !loaded">{{ saving ? '保存中…' : '保存配置' }}</button></div>
      </form>
    </section>
  </div>
</template>

<style scoped>
.business-modal { width: min(880px, 100%); }
select, textarea { display: block; width: 100%; margin-top: 8px; padding: 10px; border: 1px solid #cbd5e1; border-radius: 8px; background: white; color: #172033; }
textarea { resize: vertical; font: 13px/1.5 Consolas, monospace; min-height: 180px; }
.hint, .config-status { font-size: 13px; line-height: 1.6; color: #53647a; }
.success { color: #187844; }
</style>
