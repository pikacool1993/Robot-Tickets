<script setup lang="ts">
import { ref, watch } from 'vue'

const props = defineProps<{ message: string; warning?: boolean; pending?: boolean }>()
const visible = ref(false)

watch(() => [props.message, props.pending] as const, ([message, pending], _, onCleanup) => {
  visible.value = !!message
  if (!message || pending) return
  const timer = setTimeout(() => { visible.value = false }, props.warning ? 8000 : 5000)
  onCleanup(() => clearTimeout(timer))
}, { immediate: true })
</script>

<template>
  <div v-if="visible" class="task-toast" :class="{ warning }">
    <span class="task-toast-icon" aria-hidden="true">{{ warning ? '!' : pending ? '…' : 'i' }}</span>
    <div class="task-toast-message" :role="warning ? 'alert' : 'status'">
      <div>{{ message }}</div>
      <slot />
      <div v-if="$slots.actions" class="task-toast-actions"><slot name="actions" /></div>
    </div>
    <button v-if="!pending" type="button" class="task-toast-close" aria-label="关闭提示" @click="visible = false">×</button>
  </div>
</template>

<style>
.task-toast-stack {
  position: fixed;
  z-index: 40;
  top: 12px;
  right: 12px;
  width: min(344px, calc(100vw - 24px));
  max-height: min(50vh, calc(100vh - 24px));
  display: flex;
  flex-direction: column;
  gap: 8px;
  pointer-events: none;
}
.task-toast-stack:has(.task-toast) { overflow-y: auto }
</style>

<style scoped>
.task-toast {
  --toast-accent: var(--app-accent);
  pointer-events: auto;
  display: grid;
  grid-template-columns: 22px minmax(0, 1fr) 24px;
  align-items: start;
  gap: 9px;
  flex: none;
  padding: 10px 9px 11px 12px;
  border: 1px solid var(--app-line);
  border-top: 4px solid var(--toast-accent);
  border-radius: 8px;
  background: var(--app-panel);
  color: var(--app-ink);
  box-shadow: 0 7px 22px var(--app-backdrop);
  font-size: 13px;
  line-height: 1.55;
  overflow-wrap: anywhere;
}
.task-toast.warning {
  --toast-accent: var(--app-warning);
  border-color: var(--app-warning-line);
  border-top-color: var(--toast-accent);
  background: var(--app-warning-bg);
}
.task-toast-icon {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border: 1px solid currentColor;
  border-radius: 50%;
  color: var(--toast-accent);
  font-weight: 700;
}
.task-toast-message > div:first-child {
  font-weight: 600;
}
.task-toast-close {
  min-height: 24px;
  padding: 0;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: inherit;
  font-size: 19px;
  line-height: 1;
}
.task-toast-close:hover { background: var(--app-soft) }
.task-toast-actions { margin-top: 4px }
</style>
