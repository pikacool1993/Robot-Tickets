<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { api } from './api.ts'
const emit = defineEmits<{ close: []; unauthorized: [] }>()
const draft = ref({ id: '', name: '', match_at: '', sale_at: '', enabled: false, revision: 0 })
const loading = ref(false), loaded = ref(false), message = ref(''), error = ref('')
const panel = ref<HTMLElement | null>(null)
const previousFocus = document.activeElement as HTMLElement | null
let alive = true
function local(value: string) {
  if (!value || value.startsWith('0001-')) return ''
  const parts = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' }).format(new Date(value))
  return parts.replace(' ', 'T')
}
function fail(e: any) { if (!alive) return; if (e.status === 401) emit('unauthorized'); else error.value = e.message || '请求失败' }
async function load() {
  loading.value = true; loaded.value = false; error.value = ''; message.value = ''
  try { const data = await api.currentMatch(); if (alive) { draft.value = { ...data, match_at: local(data.match_at), sale_at: local(data.sale_at) }; loaded.value = true } }
  catch (e) { fail(e) } finally { if (alive) loading.value = false }
}
function defaultSale() {
  if (!draft.value.match_at) return
  const date = new Date(`${draft.value.match_at.slice(0, 10)}T14:00:00+08:00`)
  date.setTime(date.getTime() - 2 * 86400000)
  draft.value.sale_at = local(date.toISOString())
}
async function save() {
  if (loading.value || !loaded.value) return
  loading.value = true; error.value = ''; message.value = ''
  try {
    const data = await api.saveCurrentMatch({ ...draft.value, match_at: new Date(`${draft.value.match_at}+08:00`).toISOString(), sale_at: new Date(`${draft.value.sale_at}+08:00`).toISOString() })
    if (alive) { draft.value.revision = data.revision; message.value = '已保存。所有代理商重新登录客户端后获取新配置。' }
  } catch (e) { fail(e) } finally { if (alive) loading.value = false }
}
function keydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && !loading.value) { e.preventDefault(); emit('close') }
  if (e.key !== 'Tab') return
  const items = panel.value?.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled)')
  if (!items?.length) return
  const first = items[0], last = items[items.length - 1]
  if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus() }
  else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus() }
}
onMounted(() => { panel.value?.focus(); void load() })
onUnmounted(() => { alive = false; previousFocus?.focus() })
</script>
<template>
  <div class="overlay" @keydown="keydown"><section ref="panel" class="modal match-modal" role="dialog" aria-modal="true" aria-labelledby="match-title" tabindex="-1">
    <button class="close" aria-label="关闭当前比赛设置" :disabled="loading" @click="emit('close')">×</button>
    <h2 id="match-title">配置当前比赛</h2><p>所有代理商共用此配置。</p>
    <p v-if="loading && !loaded" role="status">正在读取…</p>
    <form @submit.prevent="save">
      <fieldset :disabled="loading || !loaded">
        <label>场次 ID<input v-model.trim="draft.id" inputmode="numeric" pattern="[0-9]+" maxlength="20" required /></label>
        <label>比赛名称<input v-model.trim="draft.name" maxlength="200" required /></label>
        <label>比赛时间（北京时间）<input v-model="draft.match_at" type="datetime-local" step="1" required /></label>
        <button type="button" class="secondary" @click="defaultSale">开售时间设为赛前两天 14:00</button>
        <label>开售时间（北京时间）<input v-model="draft.sale_at" type="datetime-local" step="1" required /></label>
        <label class="enable"><input v-model="draft.enabled" type="checkbox" />启用当前比赛</label>
      </fieldset>
      <p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="message" class="success" role="status">{{ message }}</p>
      <div class="modal-actions"><button type="button" class="secondary" :disabled="loading" @click="load">重新加载</button><button :disabled="loading || !loaded">保存配置</button></div>
    </form>
  </section></div>
</template>
<style scoped>
.match-modal{width:min(560px,100%)}fieldset{border:0;padding:0;margin:0;min-width:0}.enable{display:flex;align-items:center;gap:8px}.enable input{width:auto;margin:0}p{line-height:1.6}
</style>
