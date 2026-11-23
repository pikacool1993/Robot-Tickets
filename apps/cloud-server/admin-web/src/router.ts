import { createRouter, createWebHistory } from 'vue-router'
import { token } from './api.ts'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/admin', redirect: '/admin/agents' },
    { path: '/admin/login', component: { render: () => null }, meta: { guestOnly: true } },
    { path: '/admin/agents/:agentId/terminals/:instance/tasks', name: 'terminal-tasks', component: { render: () => null }, meta: { requiresAuth: true } },
    { path: '/admin/agents/:agentId', component: { render: () => null }, meta: { requiresAuth: true } },
    { path: '/admin/agents', component: { render: () => null }, meta: { requiresAuth: true } },
    { path: '/admin/orders/viewers', component: { render: () => null }, meta: { requiresAuth: true } },
    { path: '/admin/orders', component: { render: () => null }, meta: { requiresAuth: true } },
    { path: '/:pathMatch(.*)*', redirect: '/admin/login' },
  ],
})

router.beforeEach((to) => {
  if (to.meta.requiresAuth && !token()) return '/admin/login'
  if (to.meta.guestOnly && token()) return '/admin/agents'
})
