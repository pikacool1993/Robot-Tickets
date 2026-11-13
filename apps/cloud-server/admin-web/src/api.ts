const config = window.__ROBOT_CONFIG__ ?? {}
const API = config.apiBase || '/api/admin/v1'
const tokenKey = 'cloud_admin_token'

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
  agent: (id: string) => request(`/agents/${id}`), updateAgent: (id: string, data: any) => request(`/agents/${id}`, 'PATCH', data), viewCredential: (id: string) => request(`/agents/${id}/credential/view`, 'POST', {}),
  resetCredential: (id: string) => request(`/agents/${id}/credential/reset`, 'POST', {}),
  setStatus: (id: string, status: string) => request(`/agents/${id}/status`, 'PATCH', { status }),
  setLimit: (id: string, max_devices: number) => request(`/agents/${id}/device-limit`, 'PATCH', { max_devices }),
  deleteAgent: (id: string) => request(`/agents/${id}`, 'DELETE', {}), audit: (id: string) => request(`/agents/${id}/audit-events`)
}
