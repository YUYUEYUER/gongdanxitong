<template>
  <AuthLayout>
    <Card class="bg-card box">
      <CardContent class="space-y-5 p-6">
        <div class="space-y-1 text-center">
          <CardTitle class="text-2xl font-bold text-foreground">客户登录</CardTitle>
          <p class="text-sm text-muted-foreground">登录后查看我的工单并继续回复。</p>
        </div>

        <form class="space-y-3" @submit.prevent="loginAction">
          <div class="space-y-2">
            <Label for="customer-email" class="text-muted-foreground">邮箱</Label>
            <Input id="customer-email" v-model.trim="form.email" type="email" />
          </div>

          <div class="space-y-2">
            <Label for="customer-password" class="text-muted-foreground">密码</Label>
            <Input id="customer-password" v-model="form.password" type="password" />
          </div>

          <Button class="w-full" :disabled="isLoading" type="submit">
            {{ isLoading ? '登录中...' : '登录' }}
          </Button>
        </form>

        <Error
          v-if="errorMessage"
          :errorMessage="errorMessage"
          :border="true"
          class="w-full rounded bg-destructive/10 p-3 text-sm text-destructive"
        />

        <div class="flex items-center justify-between text-sm text-muted-foreground">
          <router-link to="/portal/register" class="hover:text-foreground">注册账号</router-link>
          <router-link to="/portal/forgot-password" class="hover:text-foreground"
            >忘记密码</router-link
          >
        </div>
      </CardContent>
    </Card>
  </AuthLayout>
</template>

<script setup>
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { handleHTTPError } from '@shared-ui/utils/http.js'
import api from '@/api'
import { useCustomerPortalStore } from '@/stores/customerPortal'
import AuthLayout from '@/layouts/auth/AuthLayout.vue'
import { Button } from '@shared-ui/components/ui/button'
import { Error } from '@shared-ui/components/ui/error'
import { Card, CardContent, CardTitle } from '@shared-ui/components/ui/card'
import { Input } from '@shared-ui/components/ui/input'
import { Label } from '@shared-ui/components/ui/label'

const router = useRouter()
const route = useRoute()
const store = useCustomerPortalStore()
const form = ref({ email: '', password: '' })
const isLoading = ref(false)
const errorMessage = ref('')

async function loginAction() {
  isLoading.value = true
  errorMessage.value = ''
  try {
    const response = await api.customerLogin(form.value)
    store.setCustomer(response?.data?.data || null)
    router.push(String(route.query.next || '/portal/tickets'))
  } catch (error) {
    errorMessage.value = handleHTTPError(error).message
  } finally {
    isLoading.value = false
  }
}
</script>
