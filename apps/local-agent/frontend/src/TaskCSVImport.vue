<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref } from 'vue'
import { backend, type TaskCSVPreview } from './bridge'
const emit = defineEmits<{ imported: [] }>()
const dialog = ref<HTMLDialogElement|null>(null)
const preview = ref<TaskCSVPreview|null>(null)
const selected = ref<number[]>([]), duplicates = ref<number[]>([])
const busy = ref(false), error = ref(''), notice = ref('')
const labels:Record<string,string>={ready:'可导入',linked:'已关联',duplicate:'疑似重复',error:'校验失败'}
const count = computed(()=>selected.value.length)
const readyRows = computed(()=>preview.value?.rows.filter(row=>row.status==='ready').map(row=>row.index)||[])
function apply(data:TaskCSVPreview) {
 preview.value=data
 selected.value=data.rows.filter(row=>row.status==='ready').map(row=>row.index)
 duplicates.value=[]
}
async function open() {
 error.value='';notice.value=''
 await nextTick()
 dialog.value?.showModal()
 busy.value=true
 try{apply(await backend().GetTaskCSVPreview())}catch(e){error.value=String(e)}finally{busy.value=false}
}
async function choose() {
 busy.value=true;error.value='';notice.value=''
 try{apply(await backend().OpenTaskCSV())}catch(e){error.value=String(e)}finally{busy.value=false}
}
function toggle(index:number,checked:boolean,duplicate:boolean) {
 selected.value=selected.value.filter(value=>value!==index)
 duplicates.value=duplicates.value.filter(value=>value!==index)
 if(checked){selected.value.push(index);if(duplicate)duplicates.value.push(index)}
}
async function refresh() {
 busy.value=true;error.value=''
 try{apply(await backend().GetTaskCSVPreview())}catch(e){error.value=String(e)}finally{busy.value=false}
}
async function create() {
 busy.value=true;error.value='';notice.value=''
 try {
  const total=selected.value.length
  apply(await backend().CommitTaskCSV(selected.value,duplicates.value))
  emit('imported')
  notice.value=`已创建 ${total} 个待授权任务，请导出回填 CSV。`
 }catch(e){error.value=String(e)}finally{busy.value=false}
}
async function exportCSV() {
 busy.value=true;error.value=''
 try{if(await backend().ExportTaskCSV())notice.value='回填 CSV 已保存。'}catch(e){error.value=String(e)}finally{busy.value=false}
}
function close(){if(!busy.value)dialog.value?.close()}
defineExpose({open})
onUnmounted(()=>dialog.value?.close())
</script>
<template>
 <dialog ref="dialog" class="csv-dialog" aria-labelledby="csv-title" @cancel.prevent="close">
  <header><div><h2 id="csv-title">导入客户预定</h2><p>每条记录 1 至 2 位观演人，创建后扫码授权。</p></div><button type="button" aria-label="关闭导入窗口" :disabled="busy" @click="close">×</button></header>
  <section class="csv-content">
   <div class="csv-toolbar">
    <div class="csv-toolbar-actions"><button type="button" :disabled="busy" @click="choose">选择 CSV</button><button type="button" :disabled="busy||!preview?.rows.length" @click="refresh">重新校验</button><label v-if="readyRows.length"><input type="checkbox" :checked="readyRows.every(index=>selected.includes(index))" :disabled="busy" @change="selected=($event.target as HTMLInputElement).checked?[...new Set([...selected,...readyRows])]:selected.filter(index=>!readyRows.includes(index))"/>选择可导入行</label></div>
    <span v-if="preview" class="csv-summary">共 {{ preview.rows.length }} 条，已选 {{ count }} 条，已创建 {{ preview.created }} 个</span>
   </div>
   <p v-if="error" class="csv-error" role="alert">{{ error }}</p><p v-if="notice" class="csv-notice" role="status">{{ notice }}</p>
   <div v-if="preview?.rows.length" class="csv-table-wrap"><table><colgroup><col class="csv-col-select"/><col class="csv-col-record"/><col class="csv-col-names"/><col class="csv-col-seats"/><col class="csv-col-cross"/><col class="csv-col-relax"/><col class="csv-col-result"/></colgroup><thead><tr><th>选择</th><th>记录</th><th>观演人</th><th>座位配置</th><th>跨区</th><th>价格放宽</th><th>结果</th></tr></thead>
    <tbody><tr v-for="row in preview.rows" :key="row.index"><td><label v-if="row.status==='ready'||row.status==='duplicate'"><input type="checkbox" :checked="selected.includes(row.index)" :disabled="busy" :aria-label="`第 ${row.line} 行${row.status==='duplicate'?'确认重复并创建':'导入'}`" @change="toggle(row.index,($event.target as HTMLInputElement).checked,row.status==='duplicate')"/><span v-if="row.status==='duplicate'">仍创建</span></label></td><td>{{ row.line }}</td><td>{{ row.names }}</td><td>{{ row.mode==='tier'?'票类':'区域' }} {{ row.seats||'—' }}</td><td>{{ row.allowCross?'开':'关' }}</td><td>{{ row.relaxSeats?`${row.relaxAfterSec}秒后`:'关' }}</td><td :data-status="row.status"><strong>{{ labels[row.status] }}</strong><span v-if="row.taskId"> #{{ row.taskId }}</span><small v-if="row.message">{{ row.message }}</small><small v-if="row.warning" class="csv-warning">{{ row.warning }}</small></td></tr></tbody>
   </table></div>
   <p v-else>请选择飞书导出的 CSV 文件。</p>
  </section>
  <footer><button type="button" :disabled="busy" @click="close">关闭</button><button type="button" :disabled="busy||!preview?.rows.length" @click="exportCSV">导出回填 CSV</button><button type="button" class="csv-primary" :disabled="busy||!count" @click="create">{{ busy?'处理中…':`创建 ${count} 个任务` }}</button></footer>
 </dialog>
</template>
<style scoped>
.csv-dialog {
 box-sizing:border-box;width:1120px;max-width:calc(100vw - 32px);max-height:88vh;
 padding:0;border:1px solid var(--app-line);border-radius:10px;
 background:var(--app-panel);color:var(--app-ink);font:13px/1.5 "Microsoft YaHei",sans-serif;
}
.csv-dialog::backdrop{background:var(--app-backdrop)}
header,footer{display:flex;align-items:center;gap:8px;padding:16px 20px;background:var(--app-panel)}
header{align-items:flex-start;justify-content:space-between;border-bottom:1px solid var(--app-line)}
header>button{display:grid;place-items:center;flex:none;width:32px;padding:0;font-size:20px}
h2{font-size:18px;line-height:32px;margin:0}
p{margin:8px 0;color:var(--app-muted)}
header p{margin:0}
.csv-content{padding:16px 20px;max-height:62vh;overflow:auto}
.csv-toolbar,.csv-toolbar-actions{display:flex;align-items:center;flex-wrap:wrap;gap:8px}
.csv-toolbar{justify-content:space-between;gap:12px;margin-bottom:16px}
.csv-toolbar-actions>label{min-height:32px;margin-left:8px}
.csv-summary{margin-left:auto;color:var(--app-muted);font-variant-numeric:tabular-nums}
button{box-sizing:border-box;min-height:32px;flex:none;font:inherit;line-height:20px;padding:5px 12px;border:1px solid var(--app-line);border-radius:5px;background:var(--app-panel);color:var(--app-ink);white-space:nowrap;cursor:pointer}
button:disabled{opacity:.5;cursor:default}
input[type=checkbox]{box-sizing:border-box;flex:none;width:14px;height:14px;margin:0;accent-color:var(--app-accent);vertical-align:middle}
label{display:inline-flex;align-items:center;gap:6px;white-space:nowrap;vertical-align:middle;line-height:20px}
.csv-table-wrap{overflow:auto}
table{width:100%;min-width:900px;table-layout:fixed;border-collapse:collapse}
.csv-col-select{width:9%}.csv-col-record{width:6%}.csv-col-names{width:14%}.csv-col-seats{width:24%}.csv-col-cross{width:6%}.csv-col-relax{width:10%}.csv-col-result{width:31%}
th,td{box-sizing:border-box;padding:12px 8px;border-bottom:1px solid var(--app-line);text-align:left;vertical-align:middle;overflow-wrap:anywhere}
th{color:var(--app-muted);font-weight:500;white-space:nowrap}
th:nth-child(1),td:nth-child(1),th:nth-child(2),td:nth-child(2),th:nth-child(5),td:nth-child(5),th:nth-child(6),td:nth-child(6){text-align:center}
td:nth-child(1)>label{justify-content:center}
td:nth-child(2){font-variant-numeric:tabular-nums}
small{display:block;margin-top:4px;line-height:1.5;overflow-wrap:anywhere}
.csv-error,[data-status=error]{color:var(--app-error)}
.csv-warning,[data-status=duplicate]{color:var(--app-warning)}
.csv-notice{color:var(--app-success)}
footer{justify-content:flex-end;border-top:1px solid var(--app-line)}
footer>button{min-width:72px}
footer>.csv-primary{min-width:132px}
.csv-primary{background:var(--app-accent);color:var(--app-on-accent);border-color:var(--app-accent)}
@media(max-width:640px){header,footer{padding:16px}.csv-content{padding:16px}footer{flex-wrap:wrap}.csv-summary{width:100%;margin-left:0}}
</style>
