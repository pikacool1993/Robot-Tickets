<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { backend, type IPPoolView, type IPEntry } from './bridge'
const view=ref<IPPoolView|null>(null), kind=ref<'N1'|'N5'>('N1'), raw=ref(''), notices=ref<string[]>([]), error=ref(''), busy=ref(false), testing=ref<string[]>([])
const editID=ref(''), edit=ref({ip:'',port:0,user:'',password:'',expires:''})
async function load(){view.value=await backend().GetIPPools()}
async function run(fn:()=>Promise<unknown>){if(busy.value)return;busy.value=true;error.value='';try{await fn();await load()}catch(e){error.value=String(e)}finally{busy.value=false}}
function failures(id:string){return Math.max(0,...Object.values(view.value?.bindings||{}).map(b=>b[kind.value]?.exit===id?b[kind.value].failures:0))}
async function save(){const p=view.value!.pools[kind.value];await run(()=>backend().SaveIPPool(kind.value,p.direct,p.includeDirect,p.threshold,p.cooldownSec))}
function openEdit(e:IPEntry){editID.value=e.id;edit.value={ip:e.ip,port:e.port,user:e.user,password:'',expires:e.expires}}
async function test(ids:string[]){const k=kind.value;testing.value=[...ids];let cursor=0;await Promise.all(Array.from({length:Math.min(3,ids.length)},async()=>{while(cursor<ids.length){const id=ids[cursor++];try{await backend().TestIPProxy(k,id)}catch(e){error.value=String(e)}finally{testing.value=testing.value.filter(x=>x!==id)}}}));await load()}
let timer:ReturnType<typeof setInterval>|undefined
onMounted(()=>{void load().catch(e=>error.value=String(e));timer=setInterval(()=>{if(!busy.value&&!testing.value.length&&!editID.value&&!document.activeElement?.matches('input,textarea'))void load().catch(e=>error.value=String(e))},2000)})
onUnmounted(()=>clearInterval(timer))
</script>
<template>
<section class="ip-page"><header><h1>IP 池</h1><div class="ip-tabs"><button v-for="k in (['N1','N5'] as const)" :key="k" :aria-pressed="kind===k" :disabled="busy||testing.length>0" @click="kind=k;editID='';notices=[]">{{ k }}</button></div></header>
<p v-if="error" role="alert">{{ error }}</p>
<template v-if="view"><div class="ip-settings">
<label><input type="checkbox" v-model="view.pools[kind].direct" :disabled="busy" @change="save"/>全部任务使用本机直连</label>
<label><input type="checkbox" v-model="view.pools[kind].includeDirect" :disabled="busy" @change="save"/>本机直连参与分配</label>
<label>连续{{ kind==='N1'?'网络失败':'超时' }}<input type="number" v-model.number="view.pools[kind].threshold" min="1" max="100" :disabled="busy" @change="save"/>次切换</label>
<label>冷却<input type="number" v-model.number="view.pools[kind].cooldownSec" min="1" max="86400" :disabled="busy" @change="save"/>秒</label>
</div>
<div class="ip-import"><textarea v-model="raw" rows="3" aria-label="批量录入HTTP代理" placeholder="IP|端口|用户名|密码|到期日期（一行一条）"></textarea><button :disabled="busy||!raw.trim()" @click="run(async()=>{notices=await backend().ImportIPPool(kind,raw);raw=''})">导入</button></div>
<p v-for="n in notices" :key="n">{{ n }}</p>
<form v-if="editID" class="ip-edit" @submit.prevent="run(async()=>{await backend().UpdateIPPoolEntry(kind,editID,'edit',[edit.ip,edit.port,edit.user,edit.password,edit.expires].join('|'));editID=''})">
<label>IP<input v-model="edit.ip" required/></label><label>端口<input type="number" v-model.number="edit.port" required/></label><label>用户名<input v-model="edit.user"/></label><label>密码<input type="password" v-model="edit.password" placeholder="留空保留原密码" autocomplete="new-password"/></label><label>到期日期<input type="date" v-model="edit.expires" required/></label><button :disabled="busy">保存</button><button type="button" @click="editID=''">取消</button></form>
<div class="ip-list-head"><span>{{ view.pools[kind].entries.length }} 个代理</span><button :disabled="!!testing.length||!view.pools[kind].entries.length" @click="test(view.pools[kind].entries.map(e=>e.id))">{{ testing.length?'测试中…':'测试全部' }}</button></div>
<div class="ip-table-wrap"><table><thead><tr><th>代理</th><th>用户名</th><th>到期日期</th><th>状态</th><th>绑定任务</th><th title="绑定该代理的任务中最高连续失败次数">连续失败</th><th>连通测试</th><th>操作</th></tr></thead><tbody><tr v-for="e in view.pools[kind].entries" :key="e.id"><td>{{ e.id }}</td><td>{{ e.user||'—' }}</td><td>{{ e.expires }}</td><td>{{ e.status }}</td><td>{{ e.bound }}</td><td>{{ failures(e.id) }}</td><td><template v-if="testing.includes(e.id)">测试中…</template><template v-else>{{ e.testResult||'未测试' }}<small v-if="e.testedAt">{{ e.testMS }} ms · {{ new Date(e.testedAt).toLocaleString() }}</small></template></td><td class="ip-actions"><button :disabled="testing.includes(e.id)" @click="test([e.id])">测试</button><button :disabled="busy" @click="openEdit(e)">编辑</button><button :disabled="busy" @click="run(()=>backend().UpdateIPPoolEntry(kind,e.id,'toggle',''))">{{ e.enabled?'停用':'启用' }}</button><button :disabled="busy" @click="run(()=>backend().UpdateIPPoolEntry(kind,e.id,'delete',''))">删除</button></td></tr></tbody></table></div>
</template></section>
</template>
<style scoped>
.ip-page{padding:24px;color:var(--app-ink);font-size:13px}.ip-page h1{font-size:22px;margin:0}.ip-page header,.ip-tabs,.ip-list-head,.ip-settings label{display:flex;align-items:center;gap:12px}.ip-page header{justify-content:space-between;margin-bottom:24px}.ip-page button,.ip-page input,.ip-page textarea{font:inherit;color:var(--app-ink);background:var(--app-panel);border:1px solid var(--app-line);border-radius:6px;padding:6px 10px}.ip-page button{cursor:pointer;white-space:nowrap}.ip-page button:disabled{opacity:.5;cursor:default}.ip-page button[aria-pressed=true]{color:var(--app-accent);border-color:var(--app-accent)}.ip-settings{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px;padding:16px;background:var(--app-panel);border:1px solid var(--app-line);border-radius:8px}.ip-settings input[type=number]{width:72px;height:30px;box-sizing:border-box}.ip-import{display:flex;align-items:flex-end;gap:12px;margin:24px 0}.ip-import textarea{flex:1;min-width:0;resize:vertical}.ip-list-head{justify-content:space-between;margin:16px 0}.ip-table-wrap{overflow:auto}.ip-page table{width:100%;border-collapse:collapse;text-align:left;white-space:nowrap}.ip-page th,.ip-page td{padding:12px 8px;border-bottom:1px solid var(--app-line)}.ip-page small{display:block;color:var(--app-muted);font-size:11px;margin-top:4px}.ip-actions button{margin-right:6px}.ip-edit{display:flex;flex-wrap:wrap;align-items:flex-end;gap:12px;padding:16px;background:var(--app-soft)}.ip-edit label{display:grid;gap:6px}.ip-edit input{width:130px}.ip-page p{color:var(--app-warning)}@media(max-width:700px){.ip-settings{grid-template-columns:1fr}.ip-page{padding:16px}}
</style>

