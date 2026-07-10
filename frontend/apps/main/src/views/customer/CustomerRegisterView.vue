<template>
  <AuthLayout>
    <Card class="bg-card box">
      <CardContent class="space-y-5 p-6">
        <div class="space-y-1 text-center">
          <CardTitle class="text-2xl font-bold text-foreground">客户注册</CardTitle>
          <p class="text-sm text-muted-foreground">填写资料并设置密码，完成验证后即可进入工单中心。</p>
        </div>

        <form class="space-y-3" @submit.prevent="registerAction">
          <div class="grid gap-3 sm:grid-cols-2">
            <div class="space-y-2">
              <Label for="customer-first-name" class="text-muted-foreground">称呼</Label>
              <Input
                id="customer-first-name"
                v-model.trim="form.first_name"
                autocomplete="given-name"
                required
              />
            </div>
            <div class="space-y-2">
              <Label for="customer-last-name" class="text-muted-foreground">姓氏</Label>
              <Input id="customer-last-name" v-model.trim="form.last_name" autocomplete="family-name" />
            </div>
          </div>

          <div class="space-y-2">
            <Label for="customer-register-email" class="text-muted-foreground">邮箱</Label>
            <Input
              id="customer-register-email"
              v-model.trim="form.email"
              type="email"
              autocomplete="email"
              required
            />
          </div>

          <div class="space-y-2">
            <Label for="customer-register-password" class="text-muted-foreground">设置密码</Label>
            <Input
              id="customer-register-password"
              v-model="form.password"
              type="password"
              autocomplete="new-password"
              required
            />
            <p class="text-xs text-muted-foreground">
              10 至 72 个字符，需包含大写字母、小写字母、数字和特殊字符。
            </p>
          </div>

          <div class="space-y-2">
            <Label for="customer-register-confirm" class="text-muted-foreground">确认密码</Label>
            <Input
              id="customer-register-confirm"
              v-model="form.confirm_password"
              type="password"
              autocomplete="new-password"
              required
            />
          </div>

          <div v-if="turnstileEnabled" class="space-y-2">
            <Label class="text-muted-foreground">{{ t('auth.turnstileLabel') }}</Label>
            <TurnstileField
              ref="turnstileRef"
              v-model="turnstileToken"
              :site-key="turnstileSiteKey"
              action="customer_register"
              @error="handleTurnstileError"
              @expired="handleTurnstileExpired"
            />
            <p v-if="turnstileError" class="text-xs text-destructive">{{ turnstileError }}</p>
          </div>

          <Button
            class="w-full"
            :disabled="isLoading || (turnstileEnabled && !turnstileVerified)"
            type="submit"
          >
            {{ isLoading ? '注册中...' : '注册' }}
          </Button>
        </form>

        <Error
          v-if="submitError"
          :errorMessage="submitError"
          :border="true"
          class="w-full rounded bg-destructive/10 p-3 text-sm text-destructive"
        />

        <div class="text-center text-sm text-muted-foreground">
          <router-link to="/portal/login" class="hover:text-foreground">已有账号？去登录</router-link>
        </div>
      </CardContent>
    </Card>
  </AuthLayout>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { handleHTTPError } from '@shared-ui/utils/http.js'
import api from '@/api'
import TurnstileField from '@/components/TurnstileField.vue'
import { useAppSettingsStore } from '@/stores/appSettings'
import { useCustomerPortalStore } from '@/stores/customerPortal'
import AuthLayout from '@/layouts/auth/AuthLayout.vue'
import { Button } from '@shared-ui/components/ui/button'
import { Error } from '@shared-ui/components/ui/error'
import { Card, CardContent, CardTitle } from '@shared-ui/components/ui/card'
import { Input } from '@shared-ui/components/ui/input'
import { Label } from '@shared-ui/components/ui/label'

const { t } = useI18n()
const router = useRouter()
const appSettingsStore = useAppSettingsStore()
const store = useCustomerPortalStore()
const turnstileRef = ref(null)
const turnstileToken = ref('')
const turnstileVerified = ref(false)
const turnstileError = ref('')
const form = ref({ first_name: '', last_name: '', email: '', password: '', confirm_password: '' })
const isLoading = ref(false)
const submitError = ref('')
const turnstileEnabled = computed(() => !!appSettingsStore.public_config?.['app.turnstile_enabled'])
const turnstileSiteKey = computed(
  () => appSettingsStore.public_config?.['app.turnstile_site_key'] || ''
)

watch(turnstileToken, (token) => {
  turnstileVerified.value = !!token
  if (token) turnstileError.value = ''
})

function handleTurnstileError() {
  turnstileToken.value = ''
  turnstileVerified.value = false
  turnstileError.value = t('auth.turnstileLoadFailed')
}

function handleTurnstileExpired() {
  turnstileToken.value = ''
  turnstileVerified.value = false
  turnstileError.value = t('auth.turnstileExpired')
}

async function registerAction() {
  if (isLoading.value) return
  submitError.value = ''
  if (form.value.password !== form.value.confirm_password) {
    submitError.value = '两次输入的密码不一致。'
    return
  }
  if (turnstileEnabled.value && !turnstileVerified.value) {
    turnstileError.value = t('auth.turnstileRequired')
    return
  }

  isLoading.value = true
  try {
    const response = await api.customerRegister({
      ...form.value,
      'cf-turnstile-response': turnstileToken.value
    })
    store.setCustomer(response?.data?.data || null)
    form.value.password = ''
    form.value.confirm_password = ''
    await router.replace('/portal/tickets')
  } catch (error) {
    submitError.value = handleHTTPError(error).message
    turnstileVerified.value = false
    turnstileRef.value?.reset()
  } finally {
    isLoading.value = false
  }
}
</script>
