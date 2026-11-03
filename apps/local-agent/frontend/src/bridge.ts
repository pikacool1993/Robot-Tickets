export interface State { configured: boolean; serverURL: string; instanceID: string; loggedIn: boolean; agentName: string; clientNumber: string; expiresAt: string; connection: string; notice: string }
export interface Form { username: string; password: string; client_number: string; remember: boolean }
interface Backend { GetState(): Promise<State>; GetRememberedForm(): Promise<Form>; Login(form: Form): Promise<void>; Logout(): Promise<void>; ImportConfig(): Promise<void> }
declare global { interface Window { go?: { main: { App: Backend } } } }
export function backend(): Backend {
  if (!window.go?.main?.App) throw new Error('请通过 Robot 桌面程序打开此界面')
  return window.go.main.App
}
