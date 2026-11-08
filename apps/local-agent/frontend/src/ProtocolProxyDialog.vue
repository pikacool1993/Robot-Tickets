<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
const props = defineProps<{ protocol: string; initial: string; busy: boolean; error: string; restart: boolean }>()
const emit = defineEmits<{ cancel: []; confirm: [proxy: string, confirmed: boolean] }>()
const dialog = ref<HTMLDialogElement | null>(null), proxy = ref(props.initial), reveal = ref(false)
onMounted(() => dialog.value?.showModal())
onUnmounted(() => { proxy.value = ''; dialog.value?.close() })
</script>
<template>
<dialog ref="dialog" class="proxy-dialog" aria-labelledby="proxy-title" @cancel.prevent="!busy && emit('cancel')">
  <form @submit.prevent="emit('confirm', proxy.trim(), restart)">
    <header><div><h2 id="proxy-title">{{ protocol === 'yyb' ? 'YYB' : '微信' }}协议代理设置</h2><p>登录前确认 · 留空使用直连</p></div><button type="button" :disabled="busy" aria-label="关闭" @click="emit('cancel')">×</button></header>
    <section>
      <label>代理地址（选填）<input v-model="proxy" :type="reveal ? 'text' : 'password'" autocomplete="off" :disabled="busy" placeholder="http://user:password@host:port"/></label>
      <div class="proxy-row"><button type="button" :disabled="busy" @click="reveal = !reveal">{{ reveal ? '隐藏地址' : '显示地址' }}</button><button type="button" :disabled="busy" @click="proxy = ''">清空，使用直连</button></div>
      <p>支持 HTTP 代理（可带用户名密码）和无认证 SOCKS5。配置在当前客户端加密保存，两种协议分别配置。</p>
      <p v-if="restart" class="proxy-warning">当前协议正在运行，代理已改变。继续将重启协议，取消相关请求并暂停关联任务，账号凭据保留。</p>
      <p v-if="error" role="alert" class="proxy-warning">{{ error }}</p>
    </section>
    <footer><button type="button" :disabled="busy" @click="emit('cancel')">取消</button><button class="proxy-primary" :disabled="busy">{{ busy ? '正在启动协议并登录…' : restart ? '确认重启并登录' : '确认并登录' }}</button></footer>
  </form>
</dialog>
</template>
<style scoped>
.proxy-dialog{width:470px;max-width:calc(100vw - 24px);max-height:90vh;padding:0;background:var(--app-panel);color:var(--app-ink);border:1px solid var(--app-line);border-radius:12px;font:14px/1.5 'Segoe UI','Microsoft YaHei',sans-serif}.proxy-dialog::backdrop{background:var(--app-backdrop)}header,footer{display:flex;align-items:center;justify-content:space-between;padding:18px 20px;gap:12px}header{border-bottom:1px solid var(--app-line)}footer{border-top:1px solid var(--app-line);justify-content:flex-end}h2{font-size:18px;margin:0}p{color:var(--app-muted);font-size:12px;margin:8px 0}section{padding:20px}label{display:grid;gap:8px}input{box-sizing:border-box;width:100%;padding:11px;background:var(--app-bg);color:var(--app-ink);border:1px solid var(--app-line);border-radius:6px;font:inherit}button{padding:8px 12px;border:1px solid var(--app-line);border-radius:6px;background:var(--app-panel);color:var(--app-ink);font:inherit}button:disabled{opacity:.6}.proxy-primary{background:var(--app-accent);color:var(--app-on-accent)}.proxy-row{display:flex;justify-content:space-between;gap:8px;margin:12px 0}.proxy-warning{background:var(--app-warning-bg);color:var(--app-warning);padding:12px;border-radius:6px}
</style>
