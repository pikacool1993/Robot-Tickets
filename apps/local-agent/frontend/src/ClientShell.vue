<script setup lang="ts">
import { computed, ref } from 'vue'
import type { State } from './bridge'
const props = defineProps<{ state: State; busy?: boolean }>()
const connectionLabel = computed(() => props.state.connection === '已连接' ? '云端已连接' : props.state.connection === '连接中断' ? '云端连接中断' : props.state.connection)
defineEmits<{ logout: [] }>()
// Register new client features here as their pages become available.
const navigation = [{ id: 'tasks', label: '任务', description: '账号配置与任务管理' }, { id: 'orders', label: '订单', description: '订单汇总与售后操作' }] as const
const activePage = ref<'tasks' | 'orders'>('tasks')
const collapsed = ref(false)
const currentPage = computed(() => navigation.find(item => item.id === activePage.value) || navigation[0])
</script>

<template>
  <div class="client-shell" :class="{ 'sidebar-collapsed': collapsed }">
    <aside class="client-sidebar" aria-label="客户端侧边栏">
      <div class="client-brand" :aria-label="`客户端编号 ${state.clientNumber}`"><div class="client-identity"><small class="client-number-label">客户端编号</small><span class="client-connection" role="status" :title="connectionLabel" :class="{ disconnected: state.connection !== '已连接' }"><span aria-hidden="true"></span>{{ connectionLabel }}</span></div><strong class="client-number" :title="state.clientNumber">{{ state.clientNumber }}</strong></div>
      <div class="client-nav-label sidebar-copy">工作空间 / {{ currentPage.label }}</div>
      <nav class="client-nav" aria-label="客户端功能">
        <button v-for="item in navigation" :key="item.id" type="button" :class="{ 'is-active': activePage === item.id }" :aria-current="activePage === item.id ? 'page' : undefined" :aria-label="item.label" :title="item.description" @click="activePage = item.id">
          <svg v-if="item.id === 'tasks'" viewBox="0 0 20 20" fill="none" aria-hidden="true"><rect x="3" y="3" width="14" height="14" rx="2.5" stroke="currentColor" stroke-width="1.5"/><path d="M6 7h8M6 10h8M6 13h5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/><circle cx="5" cy="7" r=".65" fill="currentColor"/><circle cx="5" cy="10" r=".65" fill="currentColor"/><circle cx="5" cy="13" r=".65" fill="currentColor"/></svg>
          <svg v-else viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="M5 2.75h7l3 3v11.5H5z" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/><path d="M12 2.75v3h3M7.5 9h5M7.5 12h5" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/><path d="M3 5.5v12a.75.75 0 0 0 .75.75H13" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/></svg>
          <span class="sidebar-copy">{{ item.label }}</span><span class="client-nav-indicator" aria-hidden="true"></span>
        </button>
      </nav>
      <div class="client-sidebar-bottom"><div id="client-sidebar-protocol" class="sidebar-protocol"></div><button class="client-logout" type="button" aria-label="退出登录" title="退出登录" :disabled="busy" @click="$emit('logout')"><span aria-hidden="true">↪</span><span class="sidebar-copy">退出登录</span></button><button class="client-collapse" type="button" :aria-expanded="!collapsed" aria-controls="client-content" :aria-label="collapsed ? '展开侧边栏' : '收起侧边栏'" @click="collapsed = !collapsed"><span aria-hidden="true">{{ collapsed ? '»' : '«' }}</span><span class="sidebar-copy">收起侧边栏</span></button></div>
    </aside>
    <div id="client-content" class="client-content">

      <slot :page="activePage" />
    </div>
  </div>
</template>

<style scoped>
.client-shell{display:grid;grid-template-columns:260px minmax(0,1fr);min-height:100dvh;background:var(--app-bg);color:var(--app-ink)}
.client-sidebar{z-index:20;position:sticky;top:0;height:100dvh;overflow:auto;display:flex;flex-direction:column;background:var(--app-sidebar);border-right:1px solid var(--app-line);padding:28px 18px 14px;min-width:0}
.client-brand{padding:0 8px 26px;border-bottom:1px solid var(--app-line)}.client-identity{display:flex;align-items:center;justify-content:space-between;gap:8px}.client-number-label{font-size:11px;color:var(--app-muted)}.client-number{display:block;margin-top:10px;font-size:36px;font-weight:600;line-height:1.2;color:var(--app-accent);overflow-wrap:anywhere;letter-spacing:.02em}
.client-connection{white-space:nowrap;display:flex;align-items:center;gap:5px;font-size:11px;color:var(--app-success)}.client-connection>span{width:5px;height:5px;border-radius:50%;background:currentColor}.client-connection.disconnected{color:var(--app-warning)}
.client-nav-label{font-size:11px;color:var(--app-muted);padding:24px 8px 12px}.client-nav{display:grid;gap:5px}.client-nav button{position:relative;display:flex;align-items:center;gap:10px;min-height:44px;width:100%;padding:10px 12px;border:1px solid transparent;border-radius:7px;background:transparent;color:var(--app-muted);font:inherit;text-align:left;transition:background .15s,border-color .15s,color .15s}.client-nav button:hover{background:var(--app-soft);color:var(--app-ink)}.client-nav button.is-active{border-color:#efb07355;background:var(--app-selected);color:var(--app-accent);font-weight:600}.client-nav button.is-active::before{content:'';position:absolute;left:-1px;top:8px;bottom:8px;width:3px;border-radius:0 3px 3px 0;background:var(--app-accent)}.client-nav svg{width:19px;height:19px;flex-shrink:0}.client-nav-indicator{margin-left:auto;width:5px;height:5px;border-radius:50%;background:currentColor;opacity:0}.client-nav button.is-active .client-nav-indicator{opacity:1}
.sidebar-protocol{margin-top:0;padding-top:20px;border-top:1px solid var(--app-line)}.client-sidebar-bottom{margin-top:auto;padding-top:30px}.client-logout,.client-collapse{display:flex;align-items:center;gap:10px;width:100%;min-height:38px;border:0;border-radius:6px;background:transparent;color:var(--app-muted);font:inherit;font-size:12px;padding:8px 10px}.client-logout{border-top:0;border-radius:0;padding-top:16px;margin-bottom:6px}.client-logout:hover,.client-collapse:hover{color:var(--app-accent);background:var(--app-soft)}.client-collapse>span:first-child{font-size:20px}.client-content{min-width:0;container-type:inline-size;container-name:client-workspace}
.sidebar-collapsed{grid-template-columns:76px minmax(0,1fr)}.sidebar-collapsed .sidebar-copy,.sidebar-collapsed .client-number-label,.sidebar-collapsed .client-connection,.sidebar-collapsed .client-logout{display:none}.sidebar-collapsed .client-sidebar{padding-left:12px;padding-right:12px}.sidebar-collapsed .client-brand{padding:0 0 20px}.sidebar-collapsed .client-number{font-size:16px;text-align:center}.sidebar-collapsed .client-nav{margin-top:20px}.sidebar-collapsed .client-nav button{justify-content:center;padding:10px 0}.sidebar-collapsed .client-nav-indicator{display:none}.sidebar-collapsed .client-nav button.is-active::before{left:0;top:9px;bottom:9px}.sidebar-collapsed .client-logout,.sidebar-collapsed .client-collapse{justify-content:center}
@media(max-width:760px){.client-shell{grid-template-columns:220px minmax(0,1fr)}.client-sidebar{padding:20px 12px 12px}.client-shell.sidebar-collapsed{grid-template-columns:76px minmax(0,1fr)}}
</style>

<style scoped>
@media(pointer:coarse){.client-nav button,.client-logout,.client-collapse{min-height:44px}}
</style>

<style scoped>
.sidebar-collapsed .sidebar-protocol :deep(.sidebar-service){padding:0 0 16px;border-bottom:0;gap:0}
.sidebar-collapsed .sidebar-protocol :deep(.sidebar-service>div:not(.sidebar-service-mini)),
.sidebar-collapsed .sidebar-protocol :deep(.sidebar-service>p){display:none}
.sidebar-collapsed .sidebar-protocol :deep(.sidebar-service-mini){display:flex;flex-direction:column;align-items:center;gap:6px;width:100%;margin:0}
.sidebar-collapsed .sidebar-protocol :deep(.sidebar-service-mini strong){font-size:11px;line-height:1.4}
.sidebar-collapsed .sidebar-protocol :deep(.sidebar-service-mini span){display:flex;align-items:center;gap:4px;margin:0;padding:0;border:0;background:none;font-size:10px;line-height:1.4;color:var(--app-muted);white-space:nowrap}
</style>
