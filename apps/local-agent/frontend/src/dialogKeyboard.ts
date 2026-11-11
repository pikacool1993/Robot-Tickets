// Handle Enter once at the dialog boundary, including inputs inside forms.
export function confirmDialog(event: KeyboardEvent, confirm: () => unknown) {
  if (event.key !== 'Enter' || event.isComposing || event.keyCode === 229 || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return
  const target = event.target as HTMLElement | null
  if (target?.closest('textarea, select, [contenteditable="true"]')) return
  event.preventDefault()
  event.stopPropagation()
  if (!event.repeat) confirm()
}
