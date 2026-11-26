<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { backend, type OrderAlarmStatus } from './bridge'
import TaskToast from './TaskToast.vue'
defineProps<{ settings?: boolean }>()
const state=ref<OrderAlarmStatus|null>(null), error=ref(''), busy=ref(false)
let alive=true, timer:ReturnType<typeof setTimeout>|undefined
async function poll(){try{const data=await backend().GetOrderAlarm();if(alive)state.value=data}catch(e){if(alive)error.value=String(e)}finally{if(alive)timer=setTimeout(poll,700)}}
async function act(action:()=>Promise<void>){if(busy.value)return;busy.value=true;error.value='';try{await action();state.value=await backend().GetOrderAlarm()}catch(e){error.value=String(e)}finally{busy.value=false}}
onMounted(poll)
onUnmounted(()=>{alive=false;if(timer)clearTimeout(timer)})
</script>
<template>
 <section v-if="settings" class="order-alarm-settings">
  <h3>新订单铃声</h3>
  <div class="alarm-settings-row"><label><input type="checkbox" :checked="state?.enabled||false" :disabled="busy||!state" @change="act(()=>backend().SaveOrderAlarm(($event.target as HTMLInputElement).checked))"/>开启铃声</label><div><button type="button" :disabled="busy" @click="act(()=>backend().PreviewOrderAlarm())">试听铃声</button><button type="button" :disabled="busy||!(state?.active||state?.count||state?.error)" @click="act(()=>backend().StopOrderAlarm())">停止铃声</button></div></div>
  <p>新订单循环提醒，需手动停止。开关保存在本机；试听不改变开关。</p>
  <p v-if="state?.error||error" class="alarm-error" role="alert">{{ state?.error||error }}</p>
 </section>
 <template v-else>
   <TaskToast :message="state?.active||state?.count||state?.error ? (state.error || (state.preview?'正在试听订单铃声':`新增 ${state.count} 笔订单，请及时通知客户支付`)) : ''" pending :warning="!!state?.error">
    <template #actions><button class="alarm-stop" type="button" :disabled="busy" @click="act(()=>backend().StopOrderAlarm())">停止铃声</button><span v-if="error" role="alert">{{ error }}</span></template>
   </TaskToast>
 </template>
</template>
<style scoped>
.order-alarm-settings{margin-top:22px;padding-top:18px;border-top:1px solid var(--app-line);font-size:12px}.order-alarm-settings h3{margin:0 0 12px;font-size:14px}.alarm-settings-row,.alarm-settings-row label,.alarm-settings-row>div{display:flex;align-items:center;gap:8px}.alarm-settings-row{justify-content:space-between;flex-wrap:wrap}.alarm-settings-row label{margin:0}.alarm-settings-row input{width:14px;height:14px;accent-color:var(--app-accent)}.order-alarm-settings p{color:var(--app-muted);line-height:1.6;margin:8px 0}.order-alarm-settings .alarm-error{color:var(--app-warning)}.alarm-settings-row button,.alarm-stop{font:inherit;padding:5px 9px;border:1px solid var(--app-line);border-radius:5px;background:var(--app-panel);color:var(--app-ink);cursor:pointer}.alarm-stop:disabled{opacity:.5}
</style>
