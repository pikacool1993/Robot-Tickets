<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from './api.ts'

interface OrderViewer { name: string; phone: string }
interface CloudOrder { id: number; agent_name: string; client_number: string; task_id: number; order_id: string; match_id: string; match_name: string; seats: { tier: string; row: number; seat: number }[]; created_at: string; payment_deadline: string | null; simulated: boolean; viewers: OrderViewer[] }
const route = useRoute(), router = useRouter()
const queryString = (key: string) => typeof route.query[key] === 'string' ? route.query[key] as string : ''
const initialFilters = () => ({ agent_id: queryString('agent_id'), client_number: queryString('client_number'), match_id: queryString('match_id') })
const agents = ref<{ id: string; name: string }[]>([])
const orders = ref<CloudOrder[]>([])
const total = ref(0)
const page = ref(Math.max(1, Math.min(100000, Number(queryString('page')) || 1)))
const pageSize = ref([50, 100, 200].includes(Number(queryString('limit'))) ? Number(queryString('limit')) : 50)
const filters = ref(initialFilters())
const appliedFilters = ref(initialFilters())
const loading = ref(false), error = ref('')
const visiblePhones = ref<Record<number, OrderViewer[]>>({})
const revealing = ref<Record<number, boolean>>({})
let resultRevision = 0
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))
const query = () => ({ ...appliedFilters.value, page: String(page.value), limit: String(pageSize.value) })
function stamp(value: string | null) {
  return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—'
}
async function search(apply = false) {
  if (loading.value) return
  if (apply) { appliedFilters.value = { ...filters.value }; page.value = 1 }
  loading.value = true; error.value = ''
  resultRevision++
  visiblePhones.value = {}
  revealing.value = {}
  try {
    const data = await api.adminOrders({ ...appliedFilters.value, page: page.value, limit: pageSize.value })
    orders.value = data.items || []; total.value = data.total || 0
    await router.replace({ path: '/admin/orders', query: query() })
  } catch (e) { error.value = e instanceof Error ? e.message : '订单查询失败' }
  finally { loading.value = false }
}
function clear() { filters.value = { agent_id: '', client_number: '', match_id: '' }; void search(true) }
function resize() { page.value = 1; void search() }
function openRoster() { void router.push({ path: '/admin/orders/viewers', query: query() }) }
async function togglePhones(order: CloudOrder) {
  if (visiblePhones.value[order.id]) {
    delete visiblePhones.value[order.id]
    return
  }
  const revision = resultRevision
  revealing.value[order.id] = true
  error.value = ''
  try {
    const result = await api.revealAdminOrderViewers(order.id)
    if (revision === resultRevision) visiblePhones.value[order.id] = result.viewers
  } catch (e) {
    if (revision === resultRevision) error.value = e instanceof Error ? e.message : '电话号码读取失败'
  } finally {
    if (revision === resultRevision) delete revealing.value[order.id]
  }
}
onMounted(() => {
  void api.agents().then(data => { agents.value = Array.isArray(data) ? data : data.items || [] }).catch(() => { error.value = '代理商选项加载失败，请刷新重试' })
  void search()
})
</script>

<template>
  <section class="panel admin-orders">
    <form class="order-filters" @submit.prevent="search(true)">
      <label>代理商<select v-model="filters.agent_id"><option value="">全部代理商</option><option v-for="agent in agents" :key="agent.id" :value="agent.id">{{ agent.name }}</option></select></label>
      <label>终端编号<input v-model.trim="filters.client_number" placeholder="如 A01" /></label>
      <label>场次（场次 ID）<input v-model.trim="filters.match_id" placeholder="输入场次 ID" /></label>
      <div class="order-filter-actions"><button type="submit" :disabled="loading">查询</button><button type="button" class="secondary" :disabled="loading" @click="clear">清除</button></div>
    </form>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <div class="order-results-heading"><span>共 <strong>{{ total }}</strong> 条订单</span><div class="order-result-actions"><button class="secondary" :disabled="loading || !total" @click="openRoster">观演人名单</button><button class="secondary" :disabled="loading" @click="search()">{{ loading ? '查询中…' : '刷新' }}</button></div></div>
    <div v-if="loading && !orders.length" class="empty">正在查询订单…</div><div v-else-if="!orders.length" class="empty">没有符合条件的订单</div>
    <div v-else class="order-table-wrap" :aria-busy="loading"><table class="orders-table"><thead><tr><th>订单信息</th><th>代理商 / 终端</th><th>场次</th><th>票务信息</th><th>观演人</th><th>联系方式</th><th>出票时间</th><th>支付截止时间</th></tr></thead><tbody>
      <tr v-for="order in orders" :key="order.id">
        <td data-label="订单信息"><strong class="order-number">{{ order.order_id }}</strong><span v-if="order.simulated" class="simulation-badge">模拟订单</span><small>任务 #{{ order.task_id }}</small></td>
        <td data-label="代理商 / 终端"><strong>{{ order.agent_name }}</strong><small>{{ order.client_number }}</small></td>
        <td data-label="场次"><strong class="match-name" :title="order.match_name">{{ order.match_name }}</strong><small>ID：{{ order.match_id }}</small></td>
        <td data-label="票务信息">
          <div v-if="order.seats?.length">
            <div v-for="(seat, index) in order.seats" :key="index" class="order-line" :title="`${seat.tier} ${seat.row}排${seat.seat}座`">{{ seat.tier }} {{ seat.row }}排{{ seat.seat }}座</div>
          </div>
          <span v-else>座位未获取</span>
        </td>
        <td data-label="观演人">
          <div v-for="(viewer, index) in order.viewers" :key="index" class="order-line" :title="viewer.name">{{ viewer.name }}</div>
          <span v-if="!order.viewers?.length">—</span>
        </td>
        <td data-label="联系方式">
          <div v-for="(viewer, index) in order.viewers" :key="index" class="order-line">
            <button
              v-if="viewer.phone"
              type="button"
              class="phone-toggle"
              :disabled="loading || revealing[order.id]"
              :aria-pressed="!!visiblePhones[order.id]"
              :title="visiblePhones[order.id] ? '隐藏完整电话' : '查看完整电话'"
              @click="togglePhones(order)"
            >{{ visiblePhones[order.id]?.[index]?.phone || viewer.phone }}</button>
            <span v-else>{{ viewer.phone || '—' }}</span>
          </div>
          <span v-if="!order.viewers?.length">—</span>
        </td>
        <td data-label="出票时间" class="order-time">{{ stamp(order.created_at) }}</td>
        <td data-label="支付截止时间" class="order-time">{{ stamp(order.payment_deadline) }}</td>
      </tr>
    </tbody></table></div>
    <div class="orders-pagination"><label>每页 <select v-model.number="pageSize" :disabled="loading" @change="resize"><option :value="50">50</option><option :value="100">100</option><option :value="200">200</option></select> 条</label><button class="secondary" :disabled="loading || page <= 1" @click="page--;search()">上一页</button><span> {{ page }} / {{ pageCount }} </span><button class="secondary" :disabled="loading || page >= pageCount" @click="page++;search()">下一页</button></div>
  </section>
</template>

<style scoped>
.admin-orders{min-width:0}.order-filters{display:grid;grid-template-columns:repeat(3,minmax(0,1fr)) auto;gap:12px;margin-bottom:18px}.order-filters label{display:grid;gap:6px;color:var(--text-secondary);font-size:12px}.order-filters input,.order-filters select,.orders-pagination select{min-width:0;min-height:38px;border:1px solid #d2d2d7;border-radius:8px;padding:0 10px;background:#fff;color:#1d1d1f;font:inherit;font-size:13px}.order-filter-actions{display:flex;align-items:end;gap:8px}.order-filter-actions button{min-height:38px}.order-results-heading{display:flex;align-items:center;justify-content:space-between;gap:12px;font-size:13px}
.order-results-heading { flex-wrap: wrap; margin-bottom: 12px; }
.order-result-actions { display: flex; flex-wrap: wrap; gap: 8px; }
 .order-table-wrap { overflow: auto; }
.orders-table {
  width: 100%;
  table-layout: fixed;
  font-size: 12px;
  line-height: 1.5;
}
.orders-table th:nth-child(1) { width: 15%; }
.orders-table th:nth-child(2) { width: 12%; }
.orders-table th:nth-child(3) { width: 13%; }
.orders-table th:nth-child(4) { width: 17%; }
.orders-table th:nth-child(5) { width: 9%; }
.orders-table th:nth-child(6) { width: 10%; }
.orders-table th:nth-child(n+7) { width: 12%; }
.orders-table th, .orders-table td { padding: 7px 8px; }
.orders-table td { vertical-align: top; white-space: normal; overflow-wrap: anywhere; }
.orders-table strong { font-weight: 600; }
.orders-table small { display: block; color: var(--text-secondary); font-size: 11px; }
.match-name, .order-line { display: block; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.order-line { height: 18px; }
.phone-toggle {
  min-height: 0;
  padding: 0;
  background: transparent;
  color: var(--blue);
  font: inherit;
  font-variant-numeric: tabular-nums;
}
.phone-toggle:hover:not(:disabled) { background: transparent; text-decoration: underline; }
.orders-table .order-time { font-variant-numeric: tabular-nums; }
.orders-pagination{display:flex;align-items:center;justify-content:flex-end;flex-wrap:wrap;gap:8px;margin-top:16px;font-size:12px;color:var(--text-secondary)}.orders-pagination select{min-height:32px}
.simulation-badge{display:inline-block;margin-left:7px;padding:2px 6px;border-radius:4px;background:#fff2d7;color:#8a5400;font-size:10px;font-weight:500;vertical-align:1px}
@media(max-width:1100px) and (min-width:721px){.order-filters{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:720px){.order-filters{grid-template-columns:1fr}.order-filter-actions button{flex:1}.orders-table,.orders-table tbody{display:block;width:100%;min-width:0}.orders-table thead{display:none}.orders-table tr{display:block;border:1px solid var(--line);border-radius:10px;padding:8px 12px;margin-bottom:12px}.orders-table td{display:block;position:relative;min-height:26px;border:0;padding:4px 0 4px 96px}.orders-table td:before{content:attr(data-label);position:absolute;left:0;top:4px;width:92px;color:var(--text-secondary)}.orders-table td small{margin-top:0}.order-table-wrap{overflow:visible}}
</style>
