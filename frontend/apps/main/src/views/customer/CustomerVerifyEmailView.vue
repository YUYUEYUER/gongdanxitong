<template>
  <AuthLayout>
    <Card class="bg-card box">
      <CardContent class="space-y-5 p-6">
        <div class="space-y-1 text-center">
          <CardTitle class="text-2xl font-bold text-foreground">验证邮箱</CardTitle>
          <p v-if="verified" class="text-sm text-emerald-700" role="status">
            账号已启用，正在进入工单中心...
          </p>
          <p v-else class="text-sm text-muted-foreground">设置密码以完成邮箱验证和账号启用。</p>
        </div>

        <form v-if="token && !verified" class="space-y-3" @submit.prevent="verifyAction">
          <div class="space-y-2">
            <Label for="customer-verify-password" class="text-muted-foreground">设置密码</Label>
            <Input
              id="customer-verify-password"
              v-model="password"
              type="password"
              autocomplete="new-password"
              required
            />
            <p class="text-xs text-muted-foreground">
              10 至 72 个字符，需包含大写字母、小写字母、数字和特殊字符。
            </p>
          </div>

          <div class="space-y-2">
            <Label for="customer-verify-confirm" class="text-muted-foreground">确认密码</Label>
            <Input
              id="customer-verify-confirm"
              v-model="confirmPassword"
              type="password"
              autocomplete="new-password"
              required
            />
          </div>

          <Button class="w-full" type="submit" :disabled="isLoading">
            {{ isLoading ? '正在启用...' : '验证并启用账号' }}
          </Button>
        </form>

        <Error
          v-if="errorMessage"
          :errorMessage="errorMessage"
          :border="true"
          class="w-full rounded bg-destructive/10 p-3 text-left text-sm text-destructive"
        />

        <div v-if="!token || errorMessage" class="flex justify-center gap-4 text-sm text-muted-foreground">
          <router-link to="/portal/register" class="hover:text-foreground">重新注册</router-link>
          <router-link to="/portal/login" class="hover:text-foreground">返回登录</router-link>
        </div>
      </CardContent>
    </Card>
  </AuthLayout>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { handleHTTPError } from '@shared-ui/utils/http.js'
import { consumeSensitiveFragmentToken } from '@shared-ui/utils/url.js'
import api from '@/api'
import { useCustomerPortalStore } from '@/stores/customerPortal'
import AuthLayout from '@/layouts/auth/AuthLayout.vue'
import { Button } from '@shared-ui/components/ui/button'
import { Error } from '@shared-ui/components/ui/error'
import { Card, CardContent, CardTitle } from '@shared-ui/components/ui/card'
import { Input } from '@shared-ui/components/ui/input'
import { Label } from '@shared-ui/components/ui/label'

const route = useRoute()
const router = useRouter()
const store = useCustomerPortalStore()
const token = ref('')
const password = ref('')
const confirmPassword = ref('')
const isLoading = ref(false)
const verified = ref(false)
const errorMessage = ref('')

onMounted(() => {
  const candidate = consumeSensitiveFragmentToken(route.query.token)
  if (!candidate) {
    errorMessage.value = '验证链接无效或不完整。'
    return
  }
  token.value = candidate
})

async function verifyAction() {
  if (isLoading.value) return
  errorMessage.value = ''
  if (password.value !== confirmPassword.value) {
    errorMessage.value = '两次输入的密码不一致。'
    return
  }

  isLoading.value = true
  try {
    const response = await api.customerVerifyEmail({ token: token.value, password: password.value })
    store.setCustomer(response?.data?.data || null)
    token.value = ''
    password.value = ''
    confirmPassword.value = ''
    verified.value = true
    await router.replace('/portal/tickets')
  } catch (error) {
    errorMessage.value = handleHTTPError(error).message
  } finally {
    isLoading.value = false
  }
}
</script>
