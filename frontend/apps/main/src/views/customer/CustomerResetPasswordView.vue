<template>
  <AuthLayout>
    <Card class="bg-card box">
      <CardContent class="space-y-5 p-6">
        <div class="space-y-1 text-center">
          <CardTitle class="text-2xl font-bold text-foreground">重置密码</CardTitle>
          <p class="text-sm text-muted-foreground">设置一个新的客户门户密码。</p>
        </div>

        <form class="space-y-3" @submit.prevent="submitAction">
          <div class="space-y-2">
            <Label for="customer-reset-password" class="text-muted-foreground">新密码</Label>
            <Input id="customer-reset-password" v-model="password" type="password" />
          </div>

          <div class="space-y-2">
            <Label for="customer-reset-confirm" class="text-muted-foreground">确认密码</Label>
            <Input id="customer-reset-confirm" v-model="confirmPassword" type="password" />
          </div>

          <Button class="w-full" :disabled="isLoading" type="submit">
            {{ isLoading ? '设置中...' : '设置新密码' }}
          </Button>
        </form>

        <Error
          v-if="errorMessage"
          :errorMessage="errorMessage"
          :border="true"
          class="w-full rounded bg-destructive/10 p-3 text-sm text-destructive"
        />
      </CardContent>
    </Card>
  </AuthLayout>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { handleHTTPError } from '@shared-ui/utils/http.js'
import api from '@/api'
import AuthLayout from '@/layouts/auth/AuthLayout.vue'
import { Button } from '@shared-ui/components/ui/button'
import { Error } from '@shared-ui/components/ui/error'
import { Card, CardContent, CardTitle } from '@shared-ui/components/ui/card'
import { Input } from '@shared-ui/components/ui/input'
import { Label } from '@shared-ui/components/ui/label'

const route = useRoute()
const router = useRouter()
const token = ref('')
const password = ref('')
const confirmPassword = ref('')
const isLoading = ref(false)
const errorMessage = ref('')

onMounted(() => {
  token.value = String(route.query.token || '')
  if (!token.value) {
    router.push('/portal/login')
  }
})

async function submitAction() {
  if (password.value !== confirmPassword.value) {
    errorMessage.value = '两次输入的密码不一致。'
    return
  }

  isLoading.value = true
  errorMessage.value = ''
  try {
    await api.customerResetPassword({
      token: token.value,
      password: password.value
    })
    router.push('/portal/login')
  } catch (error) {
    errorMessage.value = handleHTTPError(error).message
  } finally {
    isLoading.value = false
  }
}
</script>
