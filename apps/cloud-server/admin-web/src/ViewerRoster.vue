<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from './api.ts'
const route = useRoute(), router = useRouter()
const names = ref<string[]>([]), loading = ref(false), ready = ref(false), error = ref('')
const filters = Object.fromEntries(['agent_id', 'client_number', 'match_id'].map(key => [key, typeof route.query[key] === 'string' ? route.query[key] as string : '']))
function back() { void router.push({ path: '/admin/orders', query: route.query }) }
function onKey(event: KeyboardEvent) { if (event.key === 'Escape') back() }
async function load() {
  loading.value = true; error.value = ''
  try { names.value = (await api.viewerRoster(filters)).names || []; ready.value = true }
  catch (e) { error.value = e instanceof Error ? e.message : '观演人名单读取失败' }
  finally { loading.value = false }
}
onMounted(() => { document.addEventListener('keydown', onKey) })
onUnmounted(() => { document.removeEventListener('keydown', onKey); names.value = [] })
</script>

<template>
  <main class="viewer-roster">
    <h1>观演人名单</h1>
    <section v-if="!ready" class="roster-access">
      <p>汇总符合筛选条件的全部订单。查看名单会记入审计。</p>
      <p v-if="error" role="alert" class="error">{{ error }}</p>
      <div class="roster-access-actions"><button :disabled="loading" @click="load">{{ loading ? '正在汇总全部名单…' : error ? '重试' : '确认查看名单' }}</button><button class="secondary" :disabled="loading" @click="back">返回订单查询</button></div>
    </section>
    <ol v-else-if="names.length" class="roster-names"><li v-for="(name,index) in names" :key="index"><span class="roster-number">{{ index + 1 }}</span><span>{{ name }}</span></li></ol>
    <p v-else class="roster-empty">当前查询条件下暂无观演人名单</p>
  </main>
</template>

<style scoped>
.viewer-roster{box-sizing:border-box;width:100%;min-height:100vh;padding:20px 24px;background:#fff;color:#1d1d1f}.viewer-roster h1{font-size:20px;line-height:28px;margin:0 0 12px;font-weight:600}.roster-access{max-width:600px;font-size:14px;line-height:1.6}.roster-access-actions{display:flex;flex-wrap:wrap;gap:10px}.roster-names{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 24px;max-width:760px;list-style:none;margin:0;padding:0}.roster-names li{display:flex;align-items:baseline;gap:10px;min-height:24px;line-height:24px;font-size:14px;overflow-wrap:anywhere}.roster-number{flex:none;min-width:3ch;color:#6e6e73;font-size:12px;font-variant-numeric:tabular-nums}.roster-empty{font-size:14px;color:#6e6e73}@media(max-width:480px){.viewer-roster{padding:16px}.roster-names{grid-template-columns:1fr}}@media print{.viewer-roster{padding:0;min-height:0}}
</style>
