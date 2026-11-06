export interface State { configured: boolean; serverURL: string; instanceID: string; loggedIn: boolean; agentName: string; clientNumber: string; expiresAt: string; connection: string; notice: string; authProtocol: 'wechat' | 'yyb' | '' }
export interface Form { username: string; password: string; client_number: string; remember: boolean; auth_protocol: 'wechat' | 'yyb' | '' }
export interface SeatArea { code: string; level: string; specialArea: string; isSaleable: boolean; restriction: string }
export interface SeatTier { tier: string; price: number; areas: SeatArea[] }
export interface Task { id: number; wechatNickname?: string; spectatorNames?: string[]; mode: 'tier' | 'area'; selected: string[]; seatValid: boolean; fingerprint: string; latest: { at: string; text: string } | null }
export interface TaskView { catalog: { stadium: string; tiers: SeatTier[] }; configError: string; tasks: Task[] }
interface Backend { GetState(): Promise<State>; GetRememberedForm(): Promise<Form>; Login(form: Form): Promise<void>; Logout(): Promise<void>; ImportConfig(): Promise<void>; GetTasks(): Promise<TaskView>; CreateTask(): Promise<Task>; SaveTaskSeats(id: number, mode: string, selected: string[]): Promise<void>; DeleteTask(id: number): Promise<void> }
declare global { interface Window { go?: { main: { App: Backend } } } }
export function backend(): Backend {
  if (!window.go?.main?.App) throw new Error('请通过 Robot 桌面程序打开此界面')
  return window.go.main.App
}
