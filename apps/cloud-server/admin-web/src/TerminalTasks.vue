<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { type AgentDevice, type AgentTaskSummary } from './api.ts'
import { taskStatuses, taskLabels, taskStatus } from './taskStatus.ts'
import { terminalView } from './terminalTaskNavigation.ts'
const props = defineProps<{ agentId: string; devices: AgentDevice[]; lastRefreshed: string; refreshing: boolean; loading: boolean }>()
const emit = defineEmits<{ refresh: [] }>()
function summary(device: AgentDevice): AgentTaskSummary | null {
  if (device.task_summary) return device.task_summary
  if (!Array.isArray(device.tasks)) return null
  const result: AgentTaskSummary = { total: device.tasks.length, online: 0, expired: 0, pending_authorization: 0, protocol_offline: 0, maintenance_paused: 0, unknown: 0 }
  for (const task of device.tasks) result[taskStatus(task.authorization_status)]++
  return result
}
function target(device: AgentDevice, status = '') { return { name: 'terminal-tasks', params: { agentId: props.agentId, instance: device.client_instance_id }, query: status ? { status, page: '1' } : terminalView(props.agentId,device.client_instance_id)?.query || {} } }
function formatTime(value: string) { return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—' }
</script>
<template>
  <section class="terminal-summary">
    <div class="device-heading"><div><h3>已登录终端</h3><small v-if="lastRefreshed" class="terminal-refreshed">刷新于 {{ lastRefreshed }}</small></div><button class="secondary" :disabled="loading || refreshing" @click="emit('refresh')">{{ refreshing ? '刷新中…' : '刷新' }}</button></div>
    <div v-if="!devices.length" class="empty">暂无已登录终端</div>
    <table v-else class="devices responsive-table"><thead><tr><th>终端</th><th>登录会话</th><th>任务概况</th><th>操作</th></tr></thead><tbody>
      <tr v-for="device in devices" :key="device.id">
        <td data-label="终端"><div><strong>{{ device.client_number }}</strong><code>{{ device.client_instance_id }}</code></div></td>
        <td data-label="登录会话"><div><span class="session-state"><i aria-hidden="true"></i>有效</span><small>到期 {{ formatTime(device.expires_at) }}</small></div></td>
        <td data-label="任务概况"><div v-if="summary(device)" class="terminal-counts"><template v-for="status in taskStatuses" :key="status"><RouterLink v-if="summary(device)?.[status]" :to="target(device,status)" :class="['task-status-chip',status]">{{ taskLabels[status] }} {{ summary(device)?.[status] }}</RouterLink></template><small>共 {{ summary(device)?.total }} 个任务</small></div><small v-else>待终端上报</small></td>
        <td data-label="操作"><RouterLink class="task-entry" :to="target(device)">查看任务 →</RouterLink></td>
      </tr>
    </tbody></table>
  </section>
</template>
<style scoped>
.terminal-summary{min-width:0}
.terminal-summary .device-heading { margin: 0 0 12px; }
.terminal-refreshed { display: block; margin-top: 4px; color: var(--text-secondary); }
.devices td{vertical-align:top;white-space:normal}.devices code{display:block;margin-top:4px;overflow-wrap:anywhere;color:var(--text-secondary);font-size:11px}.devices small{display:block;color:var(--text-secondary);font-size:11px;margin-top:5px}.session-state{display:inline-flex;align-items:center;gap:5px;color:#248a3d;font-size:12px}.session-state i{width:7px;height:7px;border-radius:50%;background:currentColor}.terminal-counts{display:flex;flex-wrap:wrap;gap:6px}.terminal-counts small{width:100%}.task-entry{display:inline-block;color:var(--blue);text-decoration:none;white-space:nowrap;padding:4px 0}.task-entry:hover{text-decoration:underline}
</style>
