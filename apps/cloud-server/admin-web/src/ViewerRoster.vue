<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from './api.ts'
const route = useRoute(), router = useRouter()
const groups = ref<{ names: string[] }[]>([])
const loading = ref(false), ready = ref(false), error = ref('')
const pageSizes = [100, 200, 400]
const pageSize = ref(100)
const page = ref(1)
const pageCount = computed(() => Math.max(1, Math.ceil(groups.value.length / pageSize.value)))
const offset = computed(() => (page.value - 1) * pageSize.value)
const visibleGroups = computed(() => groups.value.slice(offset.value, offset.value + pageSize.value))
const filters = Object.fromEntries(['agent_id', 'client_number', 'match_id'].map(key => [key, typeof route.query[key] === 'string' ? route.query[key] as string : '']))
function back() { void router.push({ path: '/admin/orders', query: route.query }) }
function onKey(event: KeyboardEvent) { if (event.key === 'Escape') back() }
async function load() {
  if (loading.value) return
  loading.value = true; error.value = ''
  try {
    groups.value = (await api.viewerRoster(filters)).groups || []
    page.value = Math.min(page.value, pageCount.value)
    ready.value = true
  }
  catch (e) { error.value = e instanceof Error ? e.message : '观演人名单读取失败' }
  finally { loading.value = false }
}
function goToPage(value: number) {
  page.value = value
  window.scrollTo(0, 0)
}
onMounted(() => {
  document.addEventListener('keydown', onKey)
  void load()
})
onUnmounted(() => { document.removeEventListener('keydown', onKey); groups.value = [] })
</script>

<template>
  <main class="viewer-roster">
    <div class="roster-heading">
      <h1>观演人名单</h1>
      <button type="button" class="secondary roster-refresh" :disabled="loading" @click="load">
        {{ loading ? '刷新中…' : '刷新' }}
      </button>
    </div>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
    <ol v-if="visibleGroups.length" class="roster-names" :start="offset + 1" :aria-busy="loading">
      <li v-for="(group, index) in visibleGroups" :key="offset + index">
        <span class="roster-number">{{ offset + index + 1 }}</span>
        <span class="roster-group-names">{{ group.names.join('、') || '—' }}</span>
      </li>
    </ol>
    <p v-else-if="ready && !error" class="roster-empty">当前查询条件下暂无观演人名单</p>
    <div v-if="groups.length" class="roster-pagination" role="group" aria-label="观演人名单分页">
      <span class="roster-total">共 {{ groups.length }} 项</span>
      <label>每页 <select v-model.number="pageSize" :disabled="loading" @change="goToPage(1)">
        <option v-for="size in pageSizes" :key="size" :value="size">{{ size }}</option>
      </select> 项</label>
      <button type="button" class="secondary" :disabled="loading || page === 1" @click="goToPage(page - 1)">上一页</button>
      <span>{{ page }} / {{ pageCount }}</span>
      <button type="button" class="secondary" :disabled="loading || page >= pageCount" @click="goToPage(page + 1)">下一页</button>
    </div>
  </main>
</template>

<style scoped>
.viewer-roster{box-sizing:border-box;width:100%;min-height:100vh;padding:20px 24px;background:#fff;color:#1d1d1f}.viewer-roster h1{font-size:20px;line-height:28px;margin:0 0 12px;font-weight:600}.roster-access{max-width:600px;font-size:14px;line-height:1.6}.roster-access-actions{display:flex;flex-wrap:wrap;gap:10px}.roster-names{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 24px;max-width:760px;list-style:none;margin:0;padding:0}.roster-names li{display:flex;align-items:baseline;gap:10px;min-height:24px;line-height:24px;font-size:14px;overflow-wrap:anywhere}.roster-number{flex:none;min-width:3ch;color:#6e6e73;font-size:12px;font-variant-numeric:tabular-nums}.roster-empty{font-size:14px;color:#6e6e73}@media(max-width:480px){.viewer-roster{padding:16px}.roster-names{grid-template-columns:1fr}}@media print{.viewer-roster{padding:0;min-height:0}}
.roster-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  max-width: 760px;
  margin-bottom: 12px;
}
.roster-heading h1 { margin: 0; }
.roster-refresh { min-width: 86px; }
.roster-group-names { min-width: 0; }
.roster-pagination {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 8px;
  max-width: 760px;
  margin-top: 16px;
  font-size: 13px;
}
.roster-total { margin-right: auto; }
.roster-pagination select {
  min-height: 38px;
  padding: 0 8px;
  border: 1px solid #d2d2d7;
  border-radius: 6px;
  background: #fff;
  font: inherit;
}
@media print {
  .roster-refresh, .roster-pagination { display: none; }
}
</style>
