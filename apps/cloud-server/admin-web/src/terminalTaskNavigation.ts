// Session-only navigation state: no credentials or task data are persisted.
const views = new Map<string, { query: Record<string, string>; scroll: number }>()
const key = (agentId: string, instance: string) => JSON.stringify([agentId, instance])
export function rememberTerminalView(agentId: string, instance: string, query: Record<string, string>, scroll: number) { views.set(key(agentId, instance), { query: { ...query }, scroll }) }
export function terminalView(agentId: string, instance: string) { return views.get(key(agentId, instance)) }
