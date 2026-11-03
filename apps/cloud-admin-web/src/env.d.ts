/// <reference types="vite/client" />
declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}
interface Window {
  __ROBOT_CONFIG__?: { apiBase?: string; adminTransportKey?: string; adminKeyId?: string }
}
