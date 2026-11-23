export const taskStatuses = ['online', 'expired', 'pending_authorization', 'protocol_offline', 'maintenance_paused', 'unknown'] as const
export type TaskStatus = typeof taskStatuses[number]
export const taskLabels: Record<TaskStatus, string> = { online: '在线', expired: '授权失效', pending_authorization: '待授权', protocol_offline: '协议离线', maintenance_paused: '维护暂停', unknown: '状态未知' }
export function taskStatus(value: string): TaskStatus { return taskStatuses.includes(value as TaskStatus) ? value as TaskStatus : 'unknown' }
