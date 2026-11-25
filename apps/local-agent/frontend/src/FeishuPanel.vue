<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { backend, type FeishuStatus } from './bridge'
const status = ref<FeishuStatus|null>(null), error = ref(''), message = ref(''), busy = ref(false)
let alive = true, timer: ReturnType<typeof setTimeout>|undefined
async function poll() {
 try {const data=await backend().GetFeishuStatus();if(alive){status.value=data;error.value=''}}
 catch(e){if(alive)error.value=e instanceof Error?e.message:String(e)}
 finally{if(alive)timer=setTimeout(poll,2000)}
}
async function test() {
 busy.value=true;error.value='';message.value=''
 try{await backend().TestFeishu();message.value='测试消息已加入队列，请查看发送结果。'}
 catch(e){error.value=e instanceof Error?e.message:String(e)}finally{busy.value=false}
}
function time(value?:string){return value&&!value.startsWith('0001-')?new Date(value).toLocaleString('zh-CN',{hour12:false}):'—'}
onMounted(poll)
onUnmounted(()=>{alive=false;if(timer)clearTimeout(timer)})
</script>
<template>
 <section class="feishu-panel"><h3>飞书机器人</h3>
  <template v-if="status">
   <div class="feishu-row"><span>{{ status.enabled?'通知已开启':'通知已关闭' }} · {{ status.configured?'已配置':'未配置' }}</span><button type="button" :disabled="busy||!status.enabled||!status.configured" @click="test">发送测试</button></div>
   <p v-if="status.maskedWebhook">{{ status.maskedWebhook }}</p>
   <div class="feishu-events"><span>下单成功：{{ status.orderCreated?'开':'关' }}</span><span>授权失效：{{ status.authExpired?'开':'关' }}</span><span>下单受阻：{{ status.orderBlocked?'开':'关' }}</span></div>
   <p>配置由服务端统一管理。最近同步：{{ time(status.syncedAt) }}</p>
   <p v-if="status.syncError" class="feishu-warning" role="status">{{ status.syncError }}</p>
   <p>最近结果：{{ status.lastResult||'暂无发送记录' }}<template v-if="status.lastResult"> · {{ time(status.lastAt) }}</template></p>
   <p v-if="status.lastError" class="feishu-warning">{{ status.lastError }}</p>
   <p v-if="status.pending">待处理 {{ status.pending }} 条<template v-if="time(status.retryAt)!=='—'"> · 下次尝试：{{ time(status.retryAt) }}</template></p>
  </template>
  <p v-if="message" role="status">{{ message }}</p><p v-if="error" class="feishu-warning" role="alert">{{ error }}</p>
 </section>
</template>
<style scoped>
.feishu-panel{margin-top:22px;padding-top:18px;border-top:1px solid var(--app-line);font-size:12px}.feishu-panel h3{margin:0 0 12px;font-size:14px}.feishu-row{display:flex;align-items:center;justify-content:space-between;gap:12px}.feishu-row button{font:inherit;padding:5px 9px;border:1px solid var(--app-line);border-radius:5px;background:var(--app-panel);color:var(--app-ink);white-space:nowrap}.feishu-row button:disabled{opacity:.5}.feishu-panel p{margin:7px 0;color:var(--app-muted);overflow-wrap:anywhere;line-height:1.6}.feishu-events{display:flex;flex-wrap:wrap;gap:10px;margin-top:10px}.feishu-panel .feishu-warning{color:var(--app-warning)}
</style>
