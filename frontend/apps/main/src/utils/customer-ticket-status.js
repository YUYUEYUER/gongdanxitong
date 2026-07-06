function normalizeStatus(status) {
  return String(status || '')
    .trim()
    .toLowerCase()
}

export function customerTicketStatusLabel(status) {
  switch (normalizeStatus(status)) {
    case 'open':
      return '待处理'
    case 'replied':
      return '已回复'
    case 'in_progress':
      return '处理中'
    case 'resolved':
      return '已解决'
    case 'closed':
      return '已关闭'
    case 'snoozed':
      return '已暂挂'
    default:
      return status || '未知状态'
  }
}

export function customerTicketStatusClass(status) {
  switch (normalizeStatus(status)) {
    case 'open':
      return 'border-amber-200 bg-amber-50 text-amber-700'
    case 'replied':
      return 'border-sky-200 bg-sky-50 text-sky-700'
    case 'in_progress':
      return 'border-violet-200 bg-violet-50 text-violet-700'
    case 'resolved':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700'
    case 'closed':
      return 'border-slate-200 bg-slate-100 text-slate-700'
    case 'snoozed':
      return 'border-orange-200 bg-orange-50 text-orange-700'
    default:
      return 'border-border bg-secondary text-foreground'
  }
}
