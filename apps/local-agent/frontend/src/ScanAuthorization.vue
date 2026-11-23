<script setup lang="ts">
import { computed, ref, watch } from 'vue'

const props = defineProps<{
  taskId: number; clientNumber: string; protocol: string; image: string; status: string; message: string
  remaining: number; busy: boolean; loggedIn: boolean
}>()
defineEmits<{ refresh: [] }>()
const copying = ref(false), feedback = ref(''), copyError = ref(false)
const instructionColors = { normal: '#526173', important: '#995416' }
const identifier = computed(() => `${props.clientNumber}-${props.taskId}`)
const terminal = computed(() => ['confirmed', 'failed', 'cancelled', 'expired', 'timeout'].includes(props.status))
const canCopy = computed(() => !!props.image && !props.busy && !terminal.value && props.remaining > 0)
const countdown = computed(() => `${String(Math.floor(props.remaining / 60)).padStart(2, '0')}:${String(props.remaining % 60).padStart(2, '0')}`)
const labels: Record<string, string> = { creating: '正在获取二维码', pending: '等待扫码', scanned: '已扫码，等待手机确认', authorized: '正在确认账号', confirmed: '协议账号已登录', expired: '二维码已过期，请重新获取', timeout: '扫码已超时，请重新获取', cancelled: '扫码已取消', failed: '授权失败，请重试', verification: '需要额外验证，请在微信中完成验证' }
const statusText = computed(() => props.busy ? '正在获取二维码…' : labels[props.status] || '等待获取二维码')
const instructions = computed(() => [
  '请用微信摄像头扫码，长按或截图识别无效。',
  props.protocol === 'yyb' ? '如提示登录地异常，点击「继续登录」。' : '扫码后可能提示「车载设备登录」，请在手机上确认登录。',
  '登录后请勿在手机上打开「蓉城小程序」，以免账号下线影响后续任务；如误开，请及时告知。',
])
watch(() => [props.image, identifier.value], () => { feedback.value = ''; copyError.value = false })

// Compose an opaque PNG rather than a screenshot: UI controls and status never enter the clipboard.
async function makeImage(image: string, number: string, lines: string[]): Promise<Blob> {
  await document.fonts.ready
  const qr = new Image()
  qr.src = image
  await qr.decode()
  const canvas = document.createElement('canvas'), qrSize = 320, padding = 16, width = qrSize + padding * 2
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('无法生成二维码图片')
  const font = '10px "Microsoft YaHei", "Segoe UI", sans-serif'
  ctx.font = font
  const wrapped: { text: string; important: boolean }[] = []
  lines.forEach((line, index) => {
    const important = index === lines.length - 1
    let row = `${index + 1}. `
    for (const char of line) {
      if (ctx.measureText(row + char).width > qrSize) { wrapped.push({ text: row, important }); row = '   ' }
      row += char
    }
    wrapped.push({ text: row, important })
  })
  const height = padding + qrSize + 26 + (lines.length ? 28 + wrapped.length * 15 + 8 : 0)
  // Render at twice the display resolution to keep the small text clear when shared.
  canvas.width = width * 2; canvas.height = height * 2
  ctx.scale(2, 2)
  ctx.fillStyle = '#ffffff'; ctx.fillRect(0, 0, width, height)
  ctx.imageSmoothingEnabled = false
  ctx.drawImage(qr, (width - qrSize) / 2, padding, qrSize, qrSize)
  ctx.fillStyle = '#182331'; ctx.textAlign = 'center'; ctx.font = '500 13px "Microsoft YaHei", sans-serif'
  ctx.fillText(number, width / 2, padding + qrSize + 17, width - padding * 2)
  if (lines.length) {
    const top = padding + qrSize + 26
    ctx.strokeStyle = '#dce2e9'; ctx.beginPath(); ctx.moveTo(padding, top); ctx.lineTo(width - padding, top); ctx.stroke()
    ctx.textAlign = 'left'; ctx.font = '600 10px "Microsoft YaHei", sans-serif'; ctx.fillText('扫码须知', padding, top + 17)
    ctx.font = font
    wrapped.forEach((line, index) => {
      ctx.fillStyle = line.important ? instructionColors.important : instructionColors.normal
      ctx.fillText(line.text, padding, top + 28 + index * 15)
    })
  }
  return new Promise((resolve, reject) => canvas.toBlob(blob => blob ? resolve(blob) : reject(new Error('无法生成 PNG 图片')), 'image/png'))
}
async function copy(withInstructions: boolean) {
  if (!canCopy.value || copying.value) return
  copying.value = true; feedback.value = ''; copyError.value = false
  try {
    if (!navigator.clipboard?.write || typeof ClipboardItem === 'undefined') throw new Error('当前运行环境不支持复制图片，请使用桌面客户端')
    // Start the clipboard operation within the click gesture; composition can finish asynchronously.
    const png = makeImage(props.image, identifier.value, withInstructions ? [...instructions.value] : [])
    await navigator.clipboard.write([new ClipboardItem({ 'image/png': png })])
    feedback.value = withInstructions ? '已复制二维码、编号和说明' : '已复制二维码和编号'
  } catch (e) { copyError.value = true; feedback.value = `复制失败：${e instanceof Error ? e.message : String(e)}` }
  finally { copying.value = false }
}
</script>

<template>
  <section class="scan-auth" aria-label="扫码授权凭条">
    <div class="scan-paper">
      <div class="scan-qr">
        <img v-if="image" :src="image" :alt="`${protocol === 'yyb' ? 'YYB' : '微信'}协议授权二维码`"/>
        <div v-else class="scan-placeholder" role="status">{{ statusText }}</div>
        <div v-if="image && terminal" class="scan-overlay">{{ statusText }}</div>
      </div>
      <div class="scan-number"><strong>{{ identifier }}</strong></div>
      <div class="scan-instructions"><h4>扫码须知</h4><ol><li v-for="(line, index) in instructions" :key="line" :class="{ important: index === instructions.length - 1 }">{{ line }}</li></ol></div>
    </div>
    <div class="scan-status" role="status" aria-live="polite"><span :class="{ attention: terminal && status !== 'confirmed' }">{{ statusText }}</span><time v-if="image && !terminal && !busy">{{ countdown }}</time><button :disabled="busy" @click="$emit('refresh')">重新获取二维码</button></div>
    <div class="scan-actions"><button title="复制二维码和客户端任务编号" :disabled="!canCopy || copying" @click="copy(false)">复制二维码</button><button class="scan-copy-full" title="复制二维码、客户端任务编号和扫码说明" :disabled="!canCopy || copying" @click="copy(true)">复制含说明</button></div>
    <div class="scan-notices" role="status" aria-live="polite" aria-atomic="true">
      <p class="scan-message">{{ message && message !== statusText ? message : '' }}</p>
      <p class="scan-feedback" :class="{ 'copy-error': copyError }">{{ feedback }}</p>
    </div>
    <div class="scan-account"><span>{{ loggedIn ? '协议账号已登录' : '协议账号尚未确认' }}</span><span>业务授权尚未完成</span></div>
    <p class="scan-note">关闭后继续等待，最长 5 分钟。重新打开会重新取码。</p>
  </section>
</template>

<style scoped>
.scan-auth{font:14px/1.6 'Segoe UI','Microsoft YaHei',sans-serif}
.scan-auth .scan-paper{background:#fff;color:#182331;border-radius:6px;padding:6px 8px}
.scan-qr{position:relative;width:320px;max-width:100%;aspect-ratio:1;margin:3px auto 0;background:white}
.scan-qr img{display:block;width:100%;height:100%;object-fit:contain}
.scan-placeholder,.scan-overlay{display:flex;align-items:center;justify-content:center;text-align:center;height:100%;padding:20px;font-size:13px;color:#526173}
.scan-overlay{position:absolute;inset:0;background:#fffffff0;color:#8c4917;font-weight:600}
.scan-number{text-align:center;margin:1px auto 0;font-size:13px;line-height:18px}
.scan-number strong{font-size:13px;font-weight:500;letter-spacing:.5px;overflow-wrap:anywhere}
.scan-instructions{box-sizing:border-box;width:320px;max-width:100%;margin:4px auto 0;padding-top:4px;border-top:1px dashed #ccd4de}
.scan-instructions h4{margin:0 0 2px;font-size:10px;font-weight:600}
.scan-instructions ol{margin:0;padding-left:14px;color:v-bind('instructionColors.normal');font-size:10px;line-height:1.45}
.scan-instructions li{padding-left:1px;margin-bottom:1px;overflow-wrap:anywhere}
.scan-instructions .important{color:v-bind('instructionColors.important')}
.scan-status{display:flex;align-items:center;justify-content:center;flex-wrap:wrap;gap:6px;font-size:11px;margin-top:4px;color:var(--app-success)}
.scan-status .attention{color:var(--app-warning)}
.scan-status time{font-variant-numeric:tabular-nums;color:var(--app-ink)}
.scan-auth .scan-status button{padding:0;background:transparent;border:0;color:var(--app-accent);font-size:11px}
.scan-notices{box-sizing:border-box;margin-top:3px}
.scan-auth .scan-notices>p{box-sizing:border-box;min-height:16px;margin:0;font-size:11px;line-height:16px;overflow:visible;overflow-wrap:anywhere;text-align:center}
.scan-actions{display:flex;justify-content:center;flex-wrap:wrap;gap:6px;margin-top:4px}
.scan-auth .scan-actions button{flex:none;padding:4px 8px;font-size:11px;background:var(--app-soft)}
.scan-auth .scan-actions .scan-copy-full{background:var(--app-accent);border-color:var(--app-accent);color:var(--app-on-accent)}
.scan-auth .scan-actions .scan-copy-full:hover:not(:disabled){background:var(--app-accent-hover)}
.scan-auth .scan-feedback{color:var(--app-success)}
.scan-auth .copy-error{color:var(--app-warning)}
.scan-account{display:flex;justify-content:space-between;gap:6px;flex-wrap:wrap;font-size:11px;color:var(--app-muted);border-top:1px solid var(--app-line);margin-top:4px;padding-top:4px}
.scan-auth .scan-note{font-size:10px;line-height:1.45;color:var(--app-muted);margin:3px 0 0}
</style>
