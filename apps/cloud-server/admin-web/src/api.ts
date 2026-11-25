const config = window.__ROBOT_CONFIG__ ?? {}
const API = config.apiBase || '/api/admin/v1'
const tokenKey = 'cloud_admin_token'

export type AgentTaskStatus = 'online' | 'expired' | 'pending_authorization' | 'protocol_offline' | 'maintenance_paused' | 'unknown'
export interface AgentTask { task_id: string | number; authorization_status: AgentTaskStatus; updated_at?: string }
export interface AgentTaskSummary { total: number; online: number; expired: number; pending_authorization: number; protocol_offline: number; maintenance_paused: number; unknown: number }
export interface AgentDevice { id: string; client_number: string; client_instance_id: string; login_at: string; expires_at: string; login_ip?: string; task_summary?: (AgentTaskSummary & { tasks_synced_at: string }) | null; tasks?: AgentTask[] | null }
export interface AgentDetail { id: string; name: string; username: string; status: string; max_devices: number; active_devices: number; devices: AgentDevice[]; last_login_at?: string | null; last_login_ip?: string | null; created_at: string }

export function token() { return sessionStorage.getItem(tokenKey) }
export function clearToken() { sessionStorage.removeItem(tokenKey) }
async function exportClientConfig() {
  const t = token()
  const response = await fetch(`${API}/client-config/export`, {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...(t ? { Authorization: `Bearer ${t}` } : {}) }, body: '{}'
  })
  if (!response.ok) {
    const payload = await response.json().catch(() => null)
    const error = new Error(payload?.message || '配置导出失败'); (error as any).status = response.status; throw error
  }
  if (!(response.headers.get('Content-Type') || '').startsWith('application/octet-stream')) throw new Error('配置文件响应格式错误')
  const blob = await response.blob()
  const name = 'bootstrap.cfg'
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a'); link.href = url; link.download = name
  document.body.appendChild(link); link.click(); link.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}
async function request(path: string, method = 'GET', body?: unknown) {
  const headers: Record<string, string> = {}; const t = token(); if (t) headers.Authorization = `Bearer ${t}`
  if (body !== undefined) { headers['Content-Type'] = 'application/json'; body = body }
  const response = await fetch(`${API}${path}`, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) })
  let payload: any = null; if (response.status !== 204) payload = await response.json()
  if (!response.ok) { const error = new Error(payload?.message || '请求失败'); (error as any).status = response.status; throw error }
  return payload?.data
}
export const api = {
  agentFeishuConfig: (id: string) => request(`/agents/${id}/feishu-config`),
  saveAgentFeishuConfig: (id: string, data: unknown) => request(`/agents/${id}/feishu-config`, 'PUT', data),
  exportClientConfig,
 currentMatch: () => request("/current-match"),
 saveCurrentMatch: (data: unknown) => request("/current-match", "PUT", data),
  businessTemplates: () => request('/business-device-templates'),
  saveBusinessTemplate: (device: string, data: unknown) => request(`/business-device-templates/${device}`, 'PUT', data),
  agentBusinessConfig: (id: string) => request(`/agents/${id}/business-config`),
  saveAgentBusinessConfig: (id: string, data: unknown) => request(`/agents/${id}/business-config`, 'PUT', data),
  login: async (username: string, password: string) => { const data = await request('/auth/login', 'POST', { username, password }); sessionStorage.setItem(tokenKey, data.access_token); return data.admin },
  me: () => request('/auth/me'), logout: () => request('/auth/logout', 'POST', {}),
  agents: () => request('/agents'), createAgent: (data: any) => request('/agents', 'POST', data),
  agent: (id: string): Promise<AgentDetail> => request(`/agents/${id}`), terminalTasks: (agentId: string, instanceId: string, filters: Record<string, string | number> = {}) => request(`/agents/${encodeURIComponent(agentId)}/terminals/${encodeURIComponent(instanceId)}/tasks?${new URLSearchParams(Object.entries(filters).map(([key,value]) => [key,String(value)]))}`), updateAgent: (id: string, data: any) => request(`/agents/${id}`, 'PATCH', data), viewCredential: (id: string) => request(`/agents/${id}/credential/view`, 'POST', {}),
  agentOrders: (id: string) => request(`/agents/${id}/orders`),
  confirmReveal: (orderId: string) => Promise.resolve(window.confirm(`订单 ${orderId} 的观演人姓名和电话将显示完整，并写入管理员审计记录。是否继续？`)),
  revealOrderViewers: (id: string, orderId: number) => request(`/agents/${id}/orders/${orderId}/reveal-viewers`, 'POST', { confirm: true }),
  adminOrders: (filters: Record<string, string | number>) => { const query = new URLSearchParams(Object.entries(filters).filter(([, value]) => String(value) !== '').map(([key, value]) => [key, String(value)])); return request(`/orders?${query}`) },
  revealAdminOrderViewers: (orderId: number) => request(`/orders/${orderId}/reveal-viewers`, 'POST', { confirm: true }),
  viewerRoster: (filters: Record<string, string>) => { const query = new URLSearchParams(Object.entries(filters).filter(([, value]) => value !== '')); return request(`/orders/viewer-roster?${query}`, 'POST') },
  resetCredential: (id: string) => request(`/agents/${id}/credential/reset`, 'POST', {}),
  setStatus: (id: string, status: string) => request(`/agents/${id}/status`, 'PATCH', { status }),
  setLimit: (id: string, max_devices: number) => request(`/agents/${id}/device-limit`, 'PATCH', { max_devices }),
  deleteAgent: (id: string) => request(`/agents/${id}`, 'DELETE', {}), audit: (id: string) => request(`/agents/${id}/audit-events`)
}
