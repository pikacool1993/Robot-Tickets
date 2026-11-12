export interface State { configured: boolean; serverURL: string; instanceID: string; loggedIn: boolean; agentName: string; clientNumber: string; expiresAt: string; connection: string; notice: string; authProtocol: 'wechat' | 'yyb' | '' }
export interface Form { username: string; password: string; client_number: string; remember: boolean; auth_protocol: 'wechat' | 'yyb' | ''; proxy?: string; confirmRestart?: boolean; resetTaskIDs?: boolean }
export interface SeatArea { code: string; level: string; specialArea: string; isSaleable: boolean; restriction: string }
export interface SeatTier { tier: string; price: number; areas: SeatArea[] }
export interface TaskLog { at: string; text: string; level?: string }
export interface AddressForm { province: string; city: string; district: string; address: string; name: string; phone: string; region: string[] }
export interface ShippingAddress { form: AddressForm }
export interface Person { id: string; name: string; card: string; phone: string; self: boolean }
export interface Task { businessStatus?: string; businessMessage?: string; people?: Person[]; peopleSelected?: string[]; peopleUsable?: boolean; note?: string; shippingAddress?: ShippingAddress; logs?: TaskLog[]; id: number; protocolAvatar?: string; wechatNickname?: string; protocolStatus?: string; authProtocol?: string; paused?: boolean; spectatorNames?: string[]; mode: 'tier' | 'area'; selected: string[]; seatValid: boolean; fingerprint: string; latest: TaskLog | null }
export interface ProtocolStatus { kind: string; state: string; address: string; proxy: string; retries: number; message: string; affected: number[] }
export interface AuthAttempt { id: string; taskID: number; protocol: string; image: string; status: string; message: string; expiresAt: string }
export interface TaskView { catalog: { stadium: string; tiers: SeatTier[] }; configError: string; tasks: Task[] }
interface Backend { RefreshTaskPeople(id: number, login: boolean): Promise<void>; SaveTaskPeople(id: number, ids: string[]): Promise<void>; SaveTaskNote(id: number, note: string): Promise<void>; SaveTaskAddress(id: number, address: ShippingAddress | null): Promise<void>; CopyTaskLogs(id: number): Promise<void>; GetState(): Promise<State>; GetRememberedForm(): Promise<Form>; Login(form: Form): Promise<void>; Logout(): Promise<void>; ImportConfig(): Promise<void>; GetTasks(): Promise<TaskView>; CreateTask(): Promise<Task>; SaveTaskSeats(id: number, mode: string, selected: string[]): Promise<void>; DeleteTask(id: number): Promise<void>; GetProtocolProxy(kind: string): Promise<string>; GetProtocolStatus(): Promise<ProtocolStatus>; ControlProtocol(action: string,confirmed: boolean): Promise<void>; BeginAuthorization(id: number): Promise<AuthAttempt>; GetAuthorization(id: number): Promise<AuthAttempt> }
declare global { interface Window { go?: { main: { App: Backend } } } }
export function backend(): Backend {
  if (!window.go?.main?.App) throw new Error('请通过 Robot 桌面程序打开此界面')
  return window.go.main.App
}
