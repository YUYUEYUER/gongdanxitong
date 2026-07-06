<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <div class="text-sm text-muted-foreground">查看你提交的全部工单。</div>
      <Button @click="router.push('/portal/tickets/new')">新建工单</Button>
    </div>

    <div
      v-if="isLoading"
      class="rounded-2xl border border-border bg-background p-6 text-sm text-muted-foreground"
    >
      正在加载工单...
    </div>

    <div
      v-else-if="tickets.length === 0"
      class="rounded-2xl border border-border bg-background p-6 text-sm text-muted-foreground"
    >
      你还没有提交过工单。
    </div>

    <div v-else class="grid gap-4">
      <button
        v-for="ticket in tickets"
        :key="ticket.uuid"
        type="button"
        class="rounded-2xl border border-border bg-background p-5 text-left transition hover:border-primary/40"
        @click="router.push(`/portal/tickets/${ticket.uuid}`)"
      >
        <div class="flex items-start justify-between gap-4">
          <div>
            <p class="text-xs font-semibold uppercase tracking-[0.2em] text-muted-foreground">
              {{ ticket.reference_number }}
            </p>
            <h2 class="mt-2 text-lg font-semibold text-foreground">
              {{ ticket.subject || '未命名工单' }}
            </h2>
            <p class="mt-2 text-sm leading-6 text-muted-foreground">
              {{ ticket.last_message || '暂无消息摘要' }}
            </p>
          </div>
          <div
            class="rounded-full border px-3 py-1 text-xs font-medium"
            :class="customerTicketStatusClass(ticket.status)"
          >
            {{ customerTicketStatusLabel(ticket.status) }}
          </div>
        </div>

        <div class="mt-4 flex flex-wrap gap-4 text-xs text-muted-foreground">
          <span>渠道：{{ ticket.inbox_name }}</span>
          <span>更新时间：{{ formatDate(ticket.updated_at) }}</span>
        </div>
      </button>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Button } from '@shared-ui/components/ui/button'
import api from '@/api'
import {
  customerTicketStatusClass,
  customerTicketStatusLabel
} from '@/utils/customer-ticket-status'

const router = useRouter()
const tickets = ref([])
const isLoading = ref(false)

function formatDate(value) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

async function loadTickets() {
  isLoading.value = true
  try {
    const response = await api.customerListTickets({ page: 1, page_size: 50 })
    tickets.value = response?.data?.data?.results || []
  } finally {
    isLoading.value = false
  }
}

onMounted(loadTickets)
</script>
