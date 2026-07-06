<template>
  <div class="ticket-page relative min-h-screen overflow-x-hidden bg-slate-50/60">
    <div
      class="pointer-events-none absolute left-1/4 top-0 h-80 w-80 rounded-full bg-indigo-500/8 blur-3xl"
    ></div>
    <div
      class="pointer-events-none absolute bottom-10 right-1/4 h-80 w-80 rounded-full bg-emerald-500/8 blur-3xl"
    ></div>

    <main
      class="relative mx-auto flex min-h-screen w-full max-w-6xl flex-col px-4 py-8 sm:px-6 lg:px-8"
    >
      <header
        class="mb-8 flex flex-col gap-5 border-b border-slate-200/70 pb-6 sm:flex-row sm:items-start sm:justify-between"
      >
        <div class="space-y-2">
          <div
            class="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-[0.24em] text-indigo-600"
          >
            <span class="inline-block h-2 w-2 rounded-full bg-indigo-600"></span>
            <span>{{ siteName }}</span>
          </div>
          <h1 class="text-3xl font-bold tracking-tight text-slate-900 sm:text-4xl">
            {{ t('publicTicket.title') }}
          </h1>
          <p class="max-w-2xl text-sm leading-7 text-slate-500">
            {{ subtitleText }}
          </p>
        </div>

        <div class="flex flex-wrap items-center gap-2.5">
          <div v-for="badge in headerBadges" :key="badge" class="ticket-chip">
            {{ badge }}
          </div>
        </div>
      </header>

      <div class="ticket-layout flex-1">
        <section class="ticket-layout-main">
          <div class="ticket-panel p-6 sm:p-8">
            <div v-if="submittedTicket" class="space-y-8">
              <div class="flex items-start justify-between gap-4 border-b border-slate-100 pb-5">
                <div class="space-y-3">
                  <div
                    class="flex h-12 w-12 items-center justify-center rounded-2xl border border-emerald-200 bg-emerald-50 text-emerald-600"
                  >
                    <CheckCircle2 class="h-6 w-6" />
                  </div>
                  <div>
                    <p class="text-xs font-semibold uppercase tracking-[0.22em] text-emerald-700">
                      {{ t('publicTicket.successBadge') }}
                    </p>
                    <h2 class="mt-2 text-2xl font-bold tracking-tight text-slate-900">
                      {{ successTitle }}
                    </h2>
                    <p class="mt-2 max-w-xl text-sm leading-7 text-slate-500">
                      {{ successDescription }}
                    </p>
                  </div>
                </div>
              </div>

              <div class="grid gap-4 sm:grid-cols-[minmax(0,1fr)_auto]">
                <div class="rounded-2xl border border-slate-200 bg-slate-50 px-5 py-4">
                  <p class="text-[11px] font-semibold uppercase tracking-[0.18em] text-slate-400">
                    {{ t('publicTicket.referenceNumber') }}
                  </p>
                  <div class="mt-2 flex items-center gap-2 text-lg font-bold text-slate-900">
                    <Ticket class="h-4 w-4 text-indigo-600" />
                    <code class="font-mono">#{{ submittedTicket.reference_number }}</code>
                  </div>
                </div>

                <button type="button" class="ticket-button-secondary" @click="copyReference">
                  <Copy class="h-4 w-4" />
                  <span>{{
                    copiedReference ? t('globals.messages.copied') : t('globals.terms.copy')
                  }}</span>
                </button>
              </div>

              <div class="rounded-2xl border border-slate-100 bg-slate-50/80 p-5">
                <div class="flex items-start gap-3">
                  <div
                    class="flex h-8 w-8 shrink-0 items-center justify-center rounded-xl border border-indigo-100 bg-indigo-50 text-indigo-600"
                  >
                    <Shield class="h-4 w-4" />
                  </div>
                  <div>
                    <p class="text-sm font-semibold text-slate-900">
                      {{ t('globals.terms.security') }}
                    </p>
                    <p class="mt-1 text-sm leading-6 text-slate-500">
                      {{ t('publicTicket.footerNote') }}
                    </p>
                  </div>
                </div>
              </div>

              <div class="flex flex-col gap-3 sm:flex-row">
                <button
                  v-if="isAuthenticated"
                  type="button"
                  class="ticket-button-primary flex-1"
                  @click="openInSystem"
                >
                  <span>{{ t('publicTicket.openInSystem') }}</span>
                  <ArrowRight class="h-4 w-4" />
                </button>
                <button type="button" class="ticket-button-secondary flex-1" @click="resetForm">
                  {{ t('publicTicket.submitAnother') }}
                </button>
              </div>
            </div>

            <form v-else class="space-y-6" @submit.prevent="submitTicket">
              <div class="flex items-start justify-between gap-4 border-b border-slate-100 pb-5">
                <div>
                  <p class="text-xs font-semibold uppercase tracking-[0.22em] text-indigo-600">
                    {{ t('publicTicket.formBadge') }}
                  </p>
                  <h2 class="mt-2 text-2xl font-bold tracking-tight text-slate-900">
                    {{ t('publicTicket.formTitle') }}
                  </h2>
                  <p class="mt-2 max-w-xl text-sm leading-7 text-slate-500">
                    {{ t('publicTicket.formDescription') }}
                  </p>
                  <div
                    v-if="isAuthenticated"
                    class="mt-3 inline-flex items-center gap-2 rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-xs font-semibold text-emerald-700"
                  >
                    <CheckCircle2 class="h-3.5 w-3.5" />
                    <span>{{ t('publicTicket.loggedInBadge') }}</span>
                  </div>
                </div>

                <div
                  v-if="formEnabled && publicInboxes.length === 1"
                  class="hidden rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 text-right sm:block"
                >
                  <p class="text-[11px] font-semibold uppercase tracking-[0.16em] text-slate-400">
                    {{ t('publicTicket.inboxLabel') }}
                  </p>
                  <p class="mt-1 text-sm font-semibold text-slate-900">
                    {{ publicInboxes[0]?.name }}
                  </p>
                </div>
              </div>

              <div v-if="loginRequiredBlocked" class="ticket-alert ticket-alert-warning">
                <AlertCircle class="mt-0.5 h-4 w-4 shrink-0" />
                <div class="flex-1">
                  <p class="font-semibold">{{ t('publicTicket.loginRequiredTitle') }}</p>
                  <p class="mt-1 text-sm leading-6">
                    {{ t('publicTicket.loginRequiredDescription') }}
                  </p>
                  <button type="button" class="ticket-button-primary mt-4" @click="goToLogin">
                    <span>{{ t('publicTicket.loginRequiredAction') }}</span>
                    <ArrowRight class="h-4 w-4" />
                  </button>
                </div>
              </div>

              <div v-if="configLoaded && !formEnabled" class="ticket-alert ticket-alert-danger">
                <AlertCircle class="mt-0.5 h-4 w-4 shrink-0" />
                <div>
                  <p class="font-semibold">{{ t('publicTicket.unavailableTitle') }}</p>
                  <p class="mt-1 text-sm leading-6">{{ unavailableMessage }}</p>
                </div>
              </div>

              <div v-else-if="blockedLanguageDetected" class="ticket-alert ticket-alert-danger">
                <AlertCircle class="mt-0.5 h-4 w-4 shrink-0" />
                <div>
                  <p class="font-semibold">{{ t('publicTicket.profanityTitle') }}</p>
                  <p class="mt-1 text-sm leading-6">{{ t('publicTicket.profanityBlocked') }}</p>
                </div>
              </div>

              <div v-if="errorMessage" class="ticket-alert ticket-alert-danger">
                <AlertCircle class="mt-0.5 h-4 w-4 shrink-0" />
                <div>
                  <p class="font-semibold">{{ t('globals.terms.warning') }}</p>
                  <p class="mt-1 text-sm leading-6">{{ errorMessage }}</p>
                </div>
              </div>

              <div class="ticket-form-grid">
                <div>
                  <label class="ticket-label" for="public-ticket-name">{{
                    t('publicTicket.nameLabel')
                  }}</label>
                  <div class="relative">
                    <User class="ticket-field-icon" />
                    <input
                      id="public-ticket-name"
                      v-model.trim="form.name"
                      autocomplete="name"
                      class="ticket-field ticket-field-with-icon"
                      :placeholder="t('publicTicket.namePlaceholder')"
                      :disabled="inputDisabled"
                    />
                  </div>
                </div>

                <div v-if="!isAuthenticated">
                  <label class="ticket-label" for="public-ticket-email">{{
                    t('globals.terms.email')
                  }}</label>
                  <div class="relative">
                    <Mail class="ticket-field-icon" />
                    <input
                      id="public-ticket-email"
                      v-model.trim="form.email"
                      type="email"
                      autocomplete="email"
                      class="ticket-field ticket-field-with-icon"
                      :placeholder="t('publicTicket.emailPlaceholder')"
                      :disabled="inputDisabled"
                    />
                  </div>
                  <p class="ticket-help mt-2">{{ t('publicTicket.emailHint') }}</p>
                </div>

                <div v-else class="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-4">
                  <p class="ticket-label mb-1">{{ t('globals.terms.email') }}</p>
                  <div class="flex items-start gap-3">
                    <Mail class="mt-0.5 h-4 w-4 shrink-0 text-slate-400" />
                    <div>
                      <p class="text-sm font-semibold text-slate-900">{{ authenticatedEmail }}</p>
                      <p class="mt-1 text-sm leading-6 text-slate-500">
                        {{ t('publicTicket.loggedInDescription') }}
                      </p>
                    </div>
                  </div>
                </div>

                <div v-if="publicInboxes.length > 1" class="ticket-field-full">
                  <label class="ticket-label" for="public-ticket-inbox">{{
                    t('publicTicket.inboxLabel')
                  }}</label>
                  <select
                    id="public-ticket-inbox"
                    v-model="form.inbox_id"
                    class="ticket-field"
                    :disabled="inputDisabled"
                  >
                    <option disabled value="">{{ t('publicTicket.selectInbox') }}</option>
                    <option
                      v-for="inbox in publicInboxes"
                      :key="inbox.id"
                      :value="String(inbox.id)"
                    >
                      {{ inbox.name }}
                    </option>
                  </select>
                </div>

                <div v-if="requireOrderNumber" class="ticket-field-full">
                  <label class="ticket-label" for="public-ticket-order-number">{{
                    t('publicTicket.orderNumberLabel')
                  }}</label>
                  <div class="relative">
                    <Hash class="ticket-field-icon" />
                    <input
                      id="public-ticket-order-number"
                      v-model.trim="form.order_number"
                      autocomplete="off"
                      class="ticket-field ticket-field-with-icon"
                      :placeholder="t('publicTicket.orderNumberPlaceholder')"
                      :disabled="inputDisabled"
                    />
                  </div>
                  <p class="ticket-help mt-2">{{ t('publicTicket.orderNumberHint') }}</p>
                </div>

                <div class="ticket-field-full">
                  <label class="ticket-label" for="public-ticket-subject">{{
                    t('globals.terms.subject')
                  }}</label>
                  <div class="relative">
                    <FileText class="ticket-field-icon" />
                    <input
                      id="public-ticket-subject"
                      v-model.trim="form.subject"
                      class="ticket-field ticket-field-with-icon"
                      :placeholder="t('publicTicket.subjectPlaceholder')"
                      :disabled="inputDisabled"
                    />
                  </div>
                </div>

                <div class="ticket-field-full">
                  <label class="ticket-label" for="public-ticket-content">{{
                    t('globals.terms.message')
                  }}</label>
                  <textarea
                    id="public-ticket-content"
                    v-model="form.content"
                    class="ticket-field min-h-[210px] resize-y"
                    :placeholder="t('publicTicket.contentPlaceholder')"
                    :disabled="inputDisabled"
                  ></textarea>
                  <p class="ticket-help mt-2">{{ t('publicTicket.messageHint') }}</p>
                </div>
              </div>

              <div class="rounded-[22px] border border-slate-200 bg-slate-50 px-5 py-5">
                <template v-if="turnstileEnabled">
                  <div>
                    <p class="ticket-label mb-3">{{ t('auth.turnstileLabel') }}</p>
                    <TurnstileField
                      ref="turnstileRef"
                      v-model="turnstileToken"
                      :site-key="turnstileSiteKey"
                      @error="handleTurnstileError"
                    />
                  </div>
                </template>

                <template v-else>
                  <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                    <div>
                      <p class="ticket-label mb-1">{{ t('publicTicket.captchaLabel') }}</p>
                      <p class="text-[28px] font-bold tracking-tight text-slate-900">
                        {{ captcha.challenge || t('publicTicket.loadingCaptcha') }}
                      </p>
                    </div>

                    <button
                      type="button"
                      class="ticket-button-secondary"
                      :disabled="captchaLoading || loginRequiredBlocked || isSubmitting"
                      @click="loadCaptcha"
                    >
                      <RefreshCw class="h-4 w-4" />
                      <span>{{ t('publicTicket.refreshCaptcha') }}</span>
                    </button>
                  </div>

                  <div class="mt-5">
                    <label class="ticket-label" for="public-ticket-captcha-answer">
                      {{ t('publicTicket.captchaAnswerLabel') }}
                    </label>
                    <input
                      id="public-ticket-captcha-answer"
                      v-model.trim="form.captcha_answer"
                      class="ticket-field"
                      :placeholder="t('publicTicket.captchaPlaceholder')"
                      :disabled="inputDisabled"
                    />
                  </div>
                </template>
              </div>

              <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
                <p class="ticket-help max-w-xl">{{ t('publicTicket.footerNote') }}</p>
                <button type="submit" class="ticket-button-primary" :disabled="!canSubmit">
                  <span>{{
                    isSubmitting ? t('globals.messages.submitting') : t('globals.messages.submit')
                  }}</span>
                  <ArrowRight class="h-4 w-4" />
                </button>
              </div>
            </form>
          </div>
        </section>

        <aside class="ticket-layout-side flex flex-col gap-6">
          <div class="ticket-panel p-6">
            <p class="text-[11px] font-semibold uppercase tracking-[0.22em] text-slate-400">
              {{ t('publicTicket.formBadge') }}
            </p>
            <h3 class="mt-2 text-lg font-bold text-slate-900">{{ t('publicTicket.formTitle') }}</h3>

            <div class="mt-6 space-y-6">
              <div
                v-for="(item, index) in guidanceItems"
                :key="item.title"
                class="flex items-start gap-3.5"
              >
                <div
                  class="flex h-7 w-7 shrink-0 items-center justify-center rounded-xl border border-indigo-100 bg-indigo-50 text-xs font-bold text-indigo-600"
                >
                  {{ index + 1 }}
                </div>
                <div>
                  <p class="text-sm font-semibold text-slate-900">{{ item.title }}</p>
                  <p class="mt-1 text-sm leading-6 text-slate-500">{{ item.description }}</p>
                </div>
              </div>
            </div>
          </div>

          <div class="ticket-panel bg-gradient-to-br from-indigo-900/5 to-white p-5">
            <div class="flex items-start gap-3">
              <div
                class="flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl border border-indigo-100 bg-indigo-50 text-indigo-600"
              >
                <Shield class="h-4 w-4" />
              </div>
              <div>
                <p class="text-xs font-semibold uppercase tracking-[0.18em] text-slate-400">
                  {{ t('publicTicket.captchaLabel') }}
                </p>
                <h3 class="mt-1 text-base font-bold text-slate-900">
                  {{ t('globals.terms.security') }}
                </h3>
              </div>
            </div>

            <p class="mt-4 text-sm leading-7 text-slate-500">
              {{ t('publicTicket.footerNote') }}
            </p>

            <div
              class="mt-4 flex flex-wrap items-center gap-4 text-[11px] font-semibold uppercase tracking-[0.18em] text-slate-400"
            >
              <div class="flex items-center gap-1.5">
                <CheckCircle2 class="h-3.5 w-3.5 text-emerald-500" />
                <span>Captcha</span>
              </div>
              <div class="flex items-center gap-1.5">
                <CheckCircle2 class="h-3.5 w-3.5 text-emerald-500" />
                <span>Filter</span>
              </div>
              <div class="flex items-center gap-1.5">
                <CheckCircle2 class="h-3.5 w-3.5 text-emerald-500" />
                <span>Rate Limit</span>
              </div>
            </div>
          </div>
        </aside>
      </div>

      <footer class="mt-10 border-t border-slate-200/70 pt-5 text-center text-xs text-slate-400">
        <p>{{ siteName }}</p>
        <p class="mt-1">{{ t('publicTicket.footerNote') }}</p>
      </footer>
    </main>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppSettingsStore } from '@/stores/appSettings'
import api from '@/api'
import TurnstileField from '@/components/TurnstileField.vue'
import { handleHTTPError } from '@shared-ui/utils/http.js'
import {
  AlertCircle,
  ArrowRight,
  CheckCircle2,
  Copy,
  FileText,
  Hash,
  Mail,
  RefreshCw,
  Shield,
  Ticket,
  User
} from 'lucide-vue-next'
import { findBlockedLanguage } from '@/utils/profanity'

const { t } = useI18n()
const appSettingsStore = useAppSettingsStore()

const configLoaded = ref(false)
const captchaLoading = ref(false)
const isSubmitting = ref(false)
const errorMessage = ref('')
const copiedReference = ref(false)
const currentUser = ref(null)
const requireLogin = ref(true)
const requireOrderNumber = ref(false)
const publicInboxes = ref([])
const submittedTicket = ref(null)
const turnstileRef = ref(null)
const turnstileToken = ref('')

const captcha = reactive({
  captcha_token: '',
  challenge: ''
})

const form = reactive({
  inbox_id: '',
  name: '',
  email: '',
  order_number: '',
  subject: '',
  content: '',
  captcha_answer: ''
})

const siteName = computed(() => appSettingsStore.public_config?.['app.site_name'] || 'lya')
const turnstileEnabled = computed(() => !!appSettingsStore.public_config?.['app.turnstile_enabled'])
const turnstileSiteKey = computed(
  () => appSettingsStore.public_config?.['app.turnstile_site_key'] || ''
)
const isAuthenticated = computed(() => !!currentUser.value?.id)
const authenticatedName = computed(() => {
  if (!currentUser.value) return ''
  const fullName = [currentUser.value.first_name, currentUser.value.last_name]
    .filter(Boolean)
    .join(' ')
    .trim()
  return fullName || currentUser.value.email || ''
})
const authenticatedEmail = computed(() => currentUser.value?.email || '')
const loginRequiredBlocked = computed(() => requireLogin.value && !isAuthenticated.value)
const formEnabled = computed(() => publicInboxes.value.length > 0)
const inputDisabled = computed(
  () => !formEnabled.value || loginRequiredBlocked.value || isSubmitting.value
)
const blockedLanguageDetected = computed(
  () => !!findBlockedLanguage(form.name, form.subject, form.content)
)
const subtitleText = computed(() =>
  isAuthenticated.value ? t('publicTicket.loggedInSubtitle') : t('publicTicket.subtitle')
)
const guidanceItems = computed(() => [
  {
    title: t('publicTicket.stepSubmitTitle'),
    description: t('publicTicket.stepSubmitDescription')
  },
  {
    title: isAuthenticated.value
      ? t('publicTicket.stepInAppTitle')
      : t('publicTicket.stepReplyTitle'),
    description: isAuthenticated.value
      ? t('publicTicket.stepInAppDescription')
      : t('publicTicket.stepReplyDescription')
  },
  {
    title: t('publicTicket.civilityTitle'),
    description: t('publicTicket.civilityDescription')
  }
])
const headerBadges = computed(() => [
  t('publicTicket.civilityTitle'),
  isAuthenticated.value ? t('publicTicket.loggedInBadge') : t('publicTicket.stepReplyTitle')
])
const successTitle = computed(() =>
  isAuthenticated.value
    ? t('publicTicket.successAuthenticatedTitle')
    : t('publicTicket.successTitle')
)
const successDescription = computed(() =>
  isAuthenticated.value
    ? t('publicTicket.successAuthenticatedDescription')
    : t('publicTicket.successDescription')
)
const unavailableMessage = computed(() => t('publicTicket.noInboxConfigured'))
const canSubmit = computed(() => {
  if (
    !formEnabled.value ||
    loginRequiredBlocked.value ||
    (!turnstileEnabled.value && captchaLoading.value) ||
    isSubmitting.value
  )
    return false
  if (!turnstileEnabled.value && (!captcha.challenge || !captcha.captcha_token)) return false
  if (!form.name || !form.subject || !form.content) {
    return false
  }
  if (turnstileEnabled.value && !turnstileToken.value) return false
  if (!turnstileEnabled.value && !form.captcha_answer) return false
  if (requireOrderNumber.value && !form.order_number) return false
  if (!isAuthenticated.value && !form.email) return false
  if (blockedLanguageDetected.value) return false
  if (publicInboxes.value.length > 1 && !form.inbox_id) return false
  return true
})

const handleTurnstileError = () => {
  turnstileToken.value = ''
  errorMessage.value = t('auth.turnstileLoadFailed')
}

const applyAuthenticatedDefaults = () => {
  if (!isAuthenticated.value) return
  if (!form.name) {
    form.name = authenticatedName.value
  }
  form.email = authenticatedEmail.value
}

const loadCurrentUser = async () => {
  try {
    const response = await api.getCurrentCustomer()
    currentUser.value = response?.data?.data || null
    applyAuthenticatedDefaults()
  } catch {
    currentUser.value = null
  }
}

const loadConfig = async () => {
  configLoaded.value = false
  try {
    const response = await api.getPublicTicketConfig()
    const data = response?.data?.data || {}
    requireLogin.value = data.require_login !== undefined ? !!data.require_login : true
    requireOrderNumber.value = !!data.require_order_number
    publicInboxes.value = data.inboxes || []
    form.inbox_id = data.default_inbox_id ? String(data.default_inbox_id) : ''
  } catch (error) {
    publicInboxes.value = []
    errorMessage.value = handleHTTPError(error).message
  } finally {
    configLoaded.value = true
  }
}

const loadCaptcha = async () => {
  captchaLoading.value = true
  try {
    const response = await api.getPublicTicketCaptcha()
    const data = response?.data?.data || {}
    captcha.captcha_token = data.captcha_token || ''
    captcha.challenge = data.challenge || ''
    form.captcha_answer = ''
  } catch (error) {
    errorMessage.value = handleHTTPError(error).message
  } finally {
    captchaLoading.value = false
  }
}

const resetForm = async () => {
  submittedTicket.value = null
  copiedReference.value = false
  errorMessage.value = ''
  turnstileToken.value = ''
  form.name = isAuthenticated.value ? authenticatedName.value : ''
  form.email = isAuthenticated.value ? authenticatedEmail.value : ''
  form.order_number = ''
  form.subject = ''
  form.content = ''
  form.captcha_answer = ''
  if (publicInboxes.value.length === 1) {
    form.inbox_id = String(publicInboxes.value[0].id)
  }
  if (turnstileEnabled.value) {
    turnstileRef.value?.reset()
    return
  }
  await loadCaptcha()
}

const openInSystem = () => {
  window.location.href = '/portal/tickets'
}

const goToLogin = () => {
  window.location.href = '/portal/login?next=/portal/tickets/new'
}

const copyReference = async () => {
  const reference = submittedTicket.value?.reference_number
  if (!reference || !navigator?.clipboard) return
  try {
    await navigator.clipboard.writeText(reference)
    copiedReference.value = true
    setTimeout(() => {
      copiedReference.value = false
    }, 1800)
  } catch {
    copiedReference.value = false
  }
}

const submitTicket = async () => {
  if (!canSubmit.value) return

  if (blockedLanguageDetected.value) {
    errorMessage.value = t('publicTicket.profanityBlocked')
    return
  }

  isSubmitting.value = true
  errorMessage.value = ''

  try {
    const response = await api.submitPublicTicket({
      inbox_id: form.inbox_id ? Number(form.inbox_id) : 0,
      name: form.name,
      email: isAuthenticated.value ? authenticatedEmail.value : form.email,
      order_number: form.order_number,
      subject: form.subject,
      content: form.content,
      turnstile_token: turnstileToken.value,
      captcha_token: captcha.captcha_token,
      captcha_answer: form.captcha_answer
    })

    submittedTicket.value = response?.data?.data || null
  } catch (error) {
    const handled = handleHTTPError(error)
    errorMessage.value =
      handled.status_code === 429 ? t('publicTicket.rateLimited') : handled.message
    if (turnstileEnabled.value) {
      turnstileRef.value?.reset()
    } else {
      await loadCaptcha()
    }
  } finally {
    isSubmitting.value = false
  }
}

onMounted(async () => {
  await Promise.all([loadCurrentUser(), loadConfig()])
  if (!turnstileEnabled.value) {
    await loadCaptcha()
  }
})
</script>

<style scoped>
.ticket-page {
  font-family: 'Plus Jakarta Sans', 'Inter', 'PingFang SC', 'Microsoft YaHei UI', 'Microsoft YaHei',
    sans-serif;
}

.ticket-panel {
  border: 1px solid rgba(226, 232, 240, 0.9);
  border-radius: 24px;
  background: rgba(255, 255, 255, 0.94);
  box-shadow: 0 10px 30px rgba(15, 23, 42, 0.06);
  backdrop-filter: blur(8px);
}

.ticket-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 2rem;
}

.ticket-form-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 1.5rem;
}

.ticket-chip {
  display: inline-flex;
  align-items: center;
  border-radius: 999px;
  border: 1px solid rgba(226, 232, 240, 0.95);
  background: rgba(255, 255, 255, 0.88);
  padding: 0.55rem 0.9rem;
  font-size: 0.73rem;
  font-weight: 600;
  color: rgb(71, 85, 105);
}

.ticket-label {
  margin-bottom: 0.45rem;
  display: block;
  font-size: 0.74rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  color: rgb(100, 116, 139);
}

.ticket-field {
  width: 100%;
  border-radius: 16px;
  border: 1px solid rgba(203, 213, 225, 0.95);
  background: rgba(248, 250, 252, 0.95);
  padding: 0.92rem 1rem;
  font-size: 0.95rem;
  color: rgb(15, 23, 42);
  transition:
    border-color 0.2s ease,
    box-shadow 0.2s ease,
    background-color 0.2s ease;
}

.ticket-field::placeholder {
  color: rgb(148, 163, 184);
}

.ticket-field:focus {
  outline: none;
  border-color: rgb(99, 102, 241);
  background: white;
  box-shadow: 0 0 0 4px rgba(99, 102, 241, 0.12);
}

.ticket-field:disabled {
  cursor: not-allowed;
  opacity: 0.7;
}

.ticket-field-with-icon {
  padding-left: 2.8rem;
}

.ticket-field-icon {
  pointer-events: none;
  position: absolute;
  left: 1rem;
  top: 50%;
  height: 1rem;
  width: 1rem;
  transform: translateY(-50%);
  color: rgb(148, 163, 184);
}

.ticket-help {
  font-size: 0.8rem;
  line-height: 1.65;
  color: rgb(100, 116, 139);
}

.ticket-button-primary,
.ticket-button-secondary {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  border-radius: 14px;
  padding: 0.9rem 1.2rem;
  font-size: 0.9rem;
  font-weight: 700;
  transition:
    transform 0.15s ease,
    background-color 0.2s ease,
    border-color 0.2s ease,
    color 0.2s ease,
    opacity 0.2s ease;
}

.ticket-button-primary {
  border: 1px solid rgb(79, 70, 229);
  background: rgb(79, 70, 229);
  color: white;
  box-shadow: 0 10px 24px rgba(79, 70, 229, 0.18);
}

.ticket-button-primary:hover:not(:disabled) {
  transform: translateY(-1px);
  background: rgb(67, 56, 202);
}

.ticket-button-secondary {
  border: 1px solid rgba(203, 213, 225, 0.95);
  background: rgba(255, 255, 255, 0.96);
  color: rgb(51, 65, 85);
}

.ticket-button-secondary:hover:not(:disabled) {
  transform: translateY(-1px);
  background: rgb(248, 250, 252);
}

.ticket-button-primary:disabled,
.ticket-button-secondary:disabled {
  cursor: not-allowed;
  opacity: 0.55;
  transform: none;
}

.ticket-alert {
  display: flex;
  gap: 0.75rem;
  border-radius: 18px;
  border: 1px solid;
  padding: 1rem 1.05rem;
  font-size: 0.9rem;
}

.ticket-alert-danger {
  border-color: rgba(248, 113, 113, 0.28);
  background: rgba(254, 242, 242, 0.96);
  color: rgb(153, 27, 27);
}

.ticket-alert-warning {
  border-color: rgba(251, 191, 36, 0.32);
  background: rgba(255, 251, 235, 0.98);
  color: rgb(146, 64, 14);
}

@media (min-width: 768px) {
  .ticket-form-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .ticket-field-full {
    grid-column: 1 / -1;
  }
}

@media (min-width: 1100px) {
  .ticket-layout {
    grid-template-columns: minmax(0, 1.65fr) minmax(320px, 0.95fr);
    align-items: start;
  }

  .ticket-layout-main,
  .ticket-layout-side {
    min-width: 0;
  }
}
</style>
