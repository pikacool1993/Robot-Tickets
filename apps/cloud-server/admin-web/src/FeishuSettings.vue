<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from './api.ts'
const props = defineProps<{ agentId: string }>()
const emit = defineEmits<{ unauthorized: [] }>()
const draft = ref({ enabled:true, order_created:true, auth_expired:true, order_blocked:true, revision:0, webhook:'', clear_webhook:false })
const loaded = ref(false), busy = ref(false), error = ref(''), message = ref(''), masked = ref('')
function fail(e: any) { if(e.status===401) emit('unauthorized'); else error.value=e.message||'请求失败' }
async function load() {
 busy.value=true;error.value='';loaded.value=false
 try { const data=await api.agentFeishuConfig(props.agentId);draft.value={...data.config,webhook:'',clear_webhook:false};masked.value=data.masked_webhook||'';loaded.value=true }
 catch(e){fail(e)} finally{busy.value=false}
}
async function save() {
 busy.value=true;error.value='';message.value=''
 try {await api.saveAgentFeishuConfig(props.agentId,draft.value);await load();message.value='已保存，所属客户端将在下次同步时生效。'}
 catch(e){fail(e)} finally{busy.value=false}
}
onMounted(load)
</script>
<template>
 <section class="feishu-settings"><h3>飞书机器人</h3>
  <form @submit.prevent="save"><fieldset :disabled="busy||!loaded">
   <label class="check"><input v-model="draft.enabled" type="checkbox"/>启用通知</label>
   <label>机器人 Webhook<input v-model.trim="draft.webhook" type="password" autocomplete="new-password" :placeholder="masked||'尚未配置'" :disabled="draft.clear_webhook" maxlength="512"/></label>
   <p>留空保留现有地址。消息由所属客户端直接发送，可在客户端设置中测试。</p>
   <label v-if="masked" class="check"><input v-model="draft.clear_webhook" type="checkbox"/>清除机器人地址</label>
   <div class="types"><label class="check"><input v-model="draft.order_created" type="checkbox"/>下单成功</label><label class="check"><input v-model="draft.auth_expired" type="checkbox"/>授权失效</label><label class="check"><input v-model="draft.order_blocked" type="checkbox"/>下单受阻</label></div>
  </fieldset>
  <p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="message" class="success" role="status">{{ message }}</p>
  <div class="actions"><button class="secondary" type="button" :disabled="busy" @click="load">重新加载</button><button :disabled="busy||!loaded">{{ busy?'处理中…':'保存配置' }}</button></div>
  </form>
 </section>
</template>
<style scoped>
.feishu-settings{margin-top:24px;padding-top:20px;border-top:1px solid var(--line)}fieldset{border:0;margin:0;padding:0;min-width:0}.check{display:flex;align-items:center;gap:8px}.check input{width:auto;margin:0}.types,.actions{display:flex;flex-wrap:wrap;gap:12px}.actions{justify-content:flex-end;margin-top:16px}p{font-size:12px;line-height:1.6}h3{margin-bottom:16px}
</style>
