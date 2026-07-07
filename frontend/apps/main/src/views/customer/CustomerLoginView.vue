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

          <div v-if="turnstileEnabled" class="space-y-2">
            <Label class="text-muted-foreground">{{ t('auth.turnstileLabel') }}</Label>
            <TurnstileField
              ref="turnstileRef"
              v-model="turnstileToken"
              :site-key="turnstileSiteKey"
              action="customer_login"
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
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
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

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const appSettingsStore = useAppSettingsStore()
const store = useCustomerPortalStore()
const form = ref({ email: '', password: '' })
const turnstileRef = ref(null)
const turnstileToken = ref('')
const turnstileVerified = ref(false)
const turnstileError = ref('')
const isLoading = ref(false)
const errorMessage = ref('')
const turnstileEnabled = computed(() => !!appSettingsStore.public_config?.['app.turnstile_enabled'])
const turnstileSiteKey = computed(
  () => appSettingsStore.public_config?.['app.turnstile_site_key'] || ''
)

onMounted(() => {
  if (!Object.keys(appSettingsStore.public_config || {}).length) {
    appSettingsStore.fetchPublicConfig()
  }
})

watch(turnstileToken, (token) => {
  turnstileVerified.value = !!token
  if (token) {
    turnstileError.value = ''
  }
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

async function loginAction() {
  if (isLoading.value) return

  errorMessage.value = ''
  if (turnstileEnabled.value && !turnstileVerified.value) {
    turnstileError.value = t('auth.turnstileRequired')
    return
  }

  isLoading.value = true
  try {
    const response = await api.customerLogin({
      ...form.value,
      'cf-turnstile-response': turnstileToken.value
    })
    store.setCustomer(response?.data?.data || null)
    router.push(String(route.query.next || '/portal/tickets'))
  } catch (error) {
    errorMessage.value = handleHTTPError(error).message
    turnstileVerified.value = false
    turnstileRef.value?.reset()
  } finally {
    isLoading.value = false
  }
}
</script>
