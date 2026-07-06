<template>
  <div class="min-h-screen bg-secondary">
    <main class="mx-auto flex min-h-screen w-full max-w-6xl flex-col px-4 py-8 sm:px-6 lg:px-8">
      <header
        class="mb-8 flex flex-col gap-4 border-b border-border pb-5 sm:flex-row sm:items-center sm:justify-between"
      >
        <div>
          <p class="text-xs font-semibold uppercase tracking-[0.24em] text-primary">客户工单中心</p>
          <h1 class="mt-2 text-3xl font-bold tracking-tight text-foreground">
            {{ title }}
          </h1>
          <p class="mt-2 text-sm leading-7 text-muted-foreground">
            {{ description }}
          </p>
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <div class="rounded-full border border-border bg-background px-4 py-2 text-sm">
            {{ customerName || customerEmail }}
          </div>
          <Button variant="outline" @click="router.push('/portal/tickets')">我的工单</Button>
          <Button variant="outline" @click="router.push('/portal/tickets/new')">新建工单</Button>
          <Button @click="logout">退出登录</Button>
        </div>
      </header>

      <RouterView />
    </main>
  </div>
</template>

<script setup>
import { computed, onMounted } from 'vue'
import { useRoute, useRouter, RouterView } from 'vue-router'
import { Button } from '@shared-ui/components/ui/button'
import { useCustomerPortalStore } from '@/stores/customerPortal'

const route = useRoute()
const router = useRouter()
const store = useCustomerPortalStore()

const title = computed(() => route.meta.customerTitle || '我的工单')
const description = computed(
  () => route.meta.customerDescription || '查看历史工单、提交新工单并继续回复。'
)
const customerName = computed(() => store.fullName)
const customerEmail = computed(() => store.customer?.email || '')

async function logout() {
  try {
    await store.logout()
  } finally {
    router.push('/portal/login')
  }
}

onMounted(async () => {
  const customer = await store.fetchCurrentCustomer()
  if (!customer) {
    router.push(`/portal/login?next=${encodeURIComponent(route.fullPath)}`)
  }
})
</script>
