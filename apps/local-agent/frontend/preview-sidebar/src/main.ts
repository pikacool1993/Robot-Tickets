import { createApp } from 'vue'
import App from '../../src/App.vue'
import '../../src/style.css'
// Standalone preview: all actions use in-memory demo data.
const state = { configured:true, serverURL:'', instanceID:'preview', loggedIn:true, agentName:'预览客户端', clientNumber:'A01', expiresAt:'', connection:'已连接', notice:'', authProtocol:'yyb' }
const protocol = {kind:'yyb',state:'running',address:'127.0.0.1:8059',proxy:'直连',retries:0,message:'',affected:[1,2,3]}
const catalog = {stadium:'示例体育场 · 座位目录',tiers:['A','B','C','D','E'].map((tier,i)=>({tier,price:1280-i*200,areas:[{code:`${i+1}01`,level:'首层看台',specialArea:'',isSaleable:true,restriction:''}]}))}
const tasks:any[] = [
 {id:1,wechatNickname:'小林',note:'优先 A 类，两张连座',protocolStatus:'alive',businessStatus:'ready',peopleUsable:true,peopleSelected:['p1','p2'],spectatorNames:['林先生','陈女士'],selected:['A','B'],seatValid:true},
 {id:2,wechatNickname:'橙子',note:'尽量靠近舞台',protocolStatus:'alive',businessStatus:'ready',peopleUsable:true,peopleSelected:['p1'],spectatorNames:['周女士'],selected:['C','D'],seatValid:true},
 {id:3,wechatNickname:'阿远',note:'',protocolStatus:'expired',businessStatus:'ready',peopleUsable:false,peopleSelected:[],spectatorNames:[],selected:['A','B','C'],seatValid:true},
 {id:4,wechatNickname:'',note:'备用账号',peopleSelected:[],spectatorNames:[],selected:['A','B','C','D','E'],seatValid:true}
].map(t=>({...t,mode:'tier',authProtocol:'yyb',fingerprint:'demo',people:[{id:'p1',name:t.spectatorNames[0]||'示例观演人',card:'************1234',phone:'138****0000',self:true}],latest:{at:new Date().toISOString(),text:t.id===3?'授权已失效，请重新扫码':t.id===4?'任务已创建，等待绑定微信账号':'账号与任务配置已就绪',level:t.id===3?'warning':'info'}}))
const inventory:any = {match:{id:'DEMO-01',name:'示例比赛 · 主队 VS 客队',match_at:'2026-11-20T19:30:00+08:00',sale_at:'2026-11-13T12:00:00+08:00',enabled:true,revision:1},settings:{queryMinMS:300,queryMaxMS:800,orderMinMS:4500,orderMaxMS:7500},clockReady:true,clockError:'',offsetMS:12,now:new Date().toISOString(),runs:{}}
window.go = {main:{App:{
 GetState:async()=>({...state}),GetRememberedForm:async()=>({}),GetTasks:async()=>({catalog,configError:'',tasks:structuredClone(tasks)}),GetProtocolStatus:async()=>({...protocol}),GetInventory:async()=>({...inventory,now:new Date().toISOString()}),
 ControlProtocol:async(action:string)=>{protocol.state=action==='stop'?'stopped':'running'},
 CreateTask:async()=>{const t={id:Math.max(...tasks.map(t=>t.id))+1,mode:'tier',selected:['A','B','C','D','E'],seatValid:true,fingerprint:'demo',latest:null};tasks.push(t);return structuredClone(t)},
 SaveTaskNote:async(id:number,note:string)=>{tasks.find(t=>t.id===id).note=note},SaveTaskSeats:async(id:number,mode:string,selected:string[])=>Object.assign(tasks.find(t=>t.id===id),{mode,selected}),DeleteTask:async(id:number)=>{tasks.splice(tasks.findIndex(t=>t.id===id),1)},
 SaveInventorySettings:async(settings:any)=>{inventory.settings=settings},SyncInventoryClock:async()=>{inventory.offsetMS=0},StartInventory:async(id:number)=>{inventory.runs[id]={status:'monitoring',message:'演示：库存监控中',target:''}},StopInventory:async(id:number)=>{delete inventory.runs[id]},
 Logout:async()=>{window.alert('这是布局预览，退出登录入口已移至侧边栏底部。')},
 RefreshTaskPeople:async()=>{},SaveTaskCross:async()=>{},CopyTaskLogs:async()=>{},SaveTaskAddress:async(id:number,address:any)=>{tasks.find(t=>t.id===id).shippingAddress=address},
 SaveTaskPeople:async(id:number,ids:string[])=>{const t=tasks.find(t=>t.id===id);t.peopleSelected=ids;t.spectatorNames=t.people.filter((p:any)=>ids.includes(p.id)).map((p:any)=>p.name)},GetTaskPersonDetails:async()=>({id:'p1',name:'示例观演人',card:'************1234',phone:'138****0000',self:true}),
 BeginAuthorization:async(id:number)=>({id:'demo',taskID:id,protocol:'yyb',image:'',status:'failed',message:'预览版本不连接真实授权服务',expiresAt:''}),GetAuthorization:async(id:number)=>({id:'demo',taskID:id,protocol:'yyb',image:'',status:'failed',message:'预览版本不连接真实授权服务',expiresAt:''})
} as any}}
createApp(App).mount('#app')