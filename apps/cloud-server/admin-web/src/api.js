const config = window.__ROBOT_CONFIG__ ?? {};
const API = config.apiBase || '/api/admin/v1';
const tokenKey = 'cloud_admin_token';
export function token() { return sessionStorage.getItem(tokenKey); }
export function clearToken() { sessionStorage.removeItem(tokenKey); }
async function request(path, method = 'GET', body) {
    const headers = {};
    const t = token();
    if (t)
        headers.Authorization = `Bearer ${t}`;
    if (body !== undefined) {
        headers['Content-Type'] = 'application/json';
        body = body;
    }
    const response = await fetch(`${API}${path}`, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
    let payload = null;
    if (response.status !== 204)
        payload = await response.json();
    if (!response.ok) {
        const error = new Error(payload?.message || '请求失败');
        error.status = response.status;
        throw error;
    }
    return payload?.data;
}
export const api = {
    login: async (username, password) => { const data = await request('/auth/login', 'POST', { username, password }); sessionStorage.setItem(tokenKey, data.access_token); return data.admin; },
    me: () => request('/auth/me'), logout: () => request('/auth/logout', 'POST', {}),
    agents: () => request('/agents'), createAgent: (data) => request('/agents', 'POST', data),
    agent: (id) => request(`/agents/${id}`), updateAgent: (id, data) => request(`/agents/${id}`, 'PATCH', data), viewCredential: (id) => request(`/agents/${id}/credential/view`, 'POST', {}),
    resetCredential: (id) => request(`/agents/${id}/credential/reset`, 'POST', {}),
    setStatus: (id, status) => request(`/agents/${id}/status`, 'PATCH', { status }),
    setLimit: (id, max_devices) => request(`/agents/${id}/device-limit`, 'PATCH', { max_devices }),
    deleteAgent: (id) => request(`/agents/${id}`, 'DELETE', {}), audit: (id) => request(`/agents/${id}/audit-events`)
};
