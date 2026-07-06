<template>
  <AuthLayout>
    <Card class="bg-card box">
      <CardContent class="space-y-5 p-6">
        <div class="space-y-1 text-center">
          <CardTitle class="text-2xl font-bold text-foreground">找回密码</CardTitle>
          <p class="text-sm text-muted-foreground">输入注册邮箱，我们会发送重置链接。</p>
        </div>

        <form class="space-y-3" @submit.prevent="submitAction">
          <div class="space-y-2">
            <Label for="customer-forgot-email" class="text-muted-foreground">邮箱</Label>
            <Input id="customer-forgot-email" v-model.trim="email" type="email" />
          </div>

          <div v-if="turnstileEnabled" class="space-y-2">
            <Label class="text-muted-foreground">{{ t('auth.turnstileLabel') }}</Label>
            <TurnstileField
              ref="turnstileRef"
              v-model="turnstileToken"
              :site-key="turnstileSiteKey"
              @error="handleTurnstileError"
            />
          </div>

          <Button
            class="w-full"
            :disabled="isLoading || (turnstileEnabled && !turnstileToken)"
            type="submit"
          >
            {{ isLoading ? '发送中...' : '发送重置链接' }}
          </Button>
        </form>

        <p v-if="successMessage" class="rounded bg-emerald-50 p-3 text-sm text-emerald-700">
          {{ successMessage }}
        </p>

        <Error
          v-if="errorMessage"
          :errorMessage="errorMessage"
          :border="true"
          class="w-full rounded bg-destructive/10 p-3 text-sm text-destructive"
        />

        <div class="text-center text-sm text-muted-foreground">
          <router-link to="/portal/login" class="hover:text-foreground">返回登录</router-link>
        </div>
      </CardContent>
    </Card>
  </AuthLayout>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { handleHTTPError } from '@shared-ui/utils/http.js'
import api from '@/api'
import TurnstileField from '@/components/TurnstileField.vue'
import { useAppSettingsStore } from '@/stores/appSettings'
import AuthLayout from '@/layouts/auth/AuthLayout.vue'
import { Button } from '@shared-ui/components/ui/button'
import { Error } from '@shared-ui/components/ui/error'
import { Card, CardContent, CardTitle } from '@shared-ui/components/ui/card'
import { Input } from '@shared-ui/components/ui/input'
import { Label } from '@shared-ui/components/ui/label'

const { t } = useI18n()
const appSettingsStore = useAppSettingsStore()
const email = ref('')
const isLoading = ref(false)
const errorMessage = ref('')
const successMessage = ref('')
const turnstileRef = ref(null)
const turnstileToken = ref('')
const turnstileEnabled = computed(() => !!appSettingsStore.public_config?.['app.turnstile_enabled'])
const turnstileSiteKey = computed(
  () => appSettingsStore.public_config?.['app.turnstile_site_key'] || ''
)

function handleTurnstileError() {
  turnstileToken.value = ''
  errorMessage.value = t('auth.turnstileLoadFailed')
}

async function submitAction() {
  isLoading.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    await api.customerForgotPassword({
      email: email.value,
      turnstile_token: turnstileToken.value
    })
    successMessage.value = '如果该邮箱已注册，我们已发送重置链接。'
    turnstileRef.value?.reset()
  } catch (error) {
    errorMessage.value = handleHTTPError(error).message
    turnstileRef.value?.reset()
  } finally {
    isLoading.value = false
  }
}
</script>
