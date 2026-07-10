<template>
  <div
    class="libredesk-widget-app text-foreground bg-background"
    :class="{ dark: widgetStore.config.dark_mode, mobile: widgetStore.isMobileFullScreen }"
    :style="customColorStyle"
    @click.once="initAudioContext"
    @touchstart.once="initAudioContext"
  >
    <div class="widget-container">
      <MainLayout />
    </div>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, watch, getCurrentInstance } from 'vue'
import { useWidgetStore } from './store/widget.js'
import { useChatStore } from '@widget/store/chat.js'
import { useUserStore } from './store/user.js'
import { initWidgetWS, closeWidgetWebSocket, sendPageVisit, skipInitialWsSync } from './websocket.js'
import api, { clearVisitorToken, setApiSessionToken, initVisitorToken, saveSession, registerStores } from '@widget/api/index.js'
import { useUnreadCount } from './composables/useUnreadCount.js'
import { initAudioContext } from '@shared-ui/composables/useNotificationSound.js'
import { hexToHSL, getContrastingHSL } from '@shared-ui/utils/color.js'
import MainLayout from '@widget/layouts/MainLayout.vue'
import { postToParent, startParentBridge, stopParentBridge } from './parentBridge.js'
import { createOrderedAuthHandler } from './authOperationQueue.js'

const widgetStore = useWidgetStore()
const chatStore = useChatStore()
const userStore = useUserStore()

// Register stores for the global 401 response interceptor.
registerStores({ userStore, chatStore, widgetStore })

// Initialize unread count tracking and sending to parent window.
useUnreadCount()

const widgetConfig = getCurrentInstance().appContext.config.globalProperties.$widgetConfig
if (widgetConfig) {
  widgetStore.updateConfig(widgetConfig)
}

const customColorStyle = computed(() => {
  const style = {}
  const colors = widgetStore.config.colors
  if (colors?.primary) {
    style['--primary'] = hexToHSL(colors.primary)
    style['--primary-foreground'] = getContrastingHSL(colors.primary)
  }
  return style
})

onMounted(() => {
  startParentBridge({
    domains: widgetConfig?.trusted_domains || [],
    expectedOrigin: new URLSearchParams(window.location.search).get('parent_origin') || '',
    onMessage: dispatchParentMessage
  })
})

const signalWidgetLoaded = () => {
  postToParent({ type: 'WIDGET_LOADED' })
}

const fetchInitialConversations = async () => {
  const success = await chatStore.fetchConversations()
  if (success && chatStore.hasConversations) {
    try {
      await chatStore.loadConversation(chatStore.getConversations[0].uuid)
    } catch { /* non-blocking */ }
  }
  if (widgetStore.config?.direct_to_conversation && success) {
    widgetStore.navigateToChat()
  }
}

const restoreSession = async () => {
  try {
    const meResp = await api.getAuthMe()
    const authData = meResp?.data?.data || {}
    const sessionToken = authData.session_token
    const user = authData.user || authData
    if (!sessionToken) return false

    userStore.setSessionToken(sessionToken)
    setApiSessionToken(sessionToken)
    userStore.setUserMeta(user)
    initVisitorToken(user?.is_visitor ? sessionToken : '')
    skipInitialWsSync()
    postToParent({ type: 'CLEAR_SESSION_TOKEN' })
    postToParent({ type: 'CLEAR_VISITOR_TOKEN' })
    return true
  } catch {
    setApiSessionToken('')
    return false
  }
}

// Messages have already passed source, origin, nonce and schema checks in parentBridge.
const handleParentMessage = async (data) => {
    if (data.type === 'WIDGET_CLOSED') {
      widgetStore.setOpen(false)
    } else if (data.type === 'WIDGET_OPENED') {
      widgetStore.setOpen(true)
    } else if (data.type === 'SET_MOBILE_STATE') {
      widgetStore.setMobileFullScreen(data.isMobile)
    } else if (data.type === 'WIDGET_EXPANDED') {
      widgetStore.setExpanded(data.isExpanded)
    } else if (data.type === 'SESSION_DATA') {
      try {
        await restoreSession()
        await fetchInitialConversations()
      } finally {
        signalWidgetLoaded()
      }
    } else if (data.type === 'SET_JWT_TOKEN') {
      if (data.jwt) {
        try {
          const resp = await api.exchangeJWTForSession(data.jwt)
          const { session_token, user } = resp.data.data
          saveSession(session_token, user, userStore)
          postToParent({ type: 'CLEAR_SESSION_TOKEN' })
          // Session exists, fetchInitialConversations will load data. Skip WS sync.
          skipInitialWsSync()
          chatStore.conversations = null
          await fetchInitialConversations()
        } catch {
          console.error('Failed to exchange JWT for session')
        } finally {
          signalWidgetLoaded()
        }
      }
    } else if (data.type === 'CLEAR_SESSION') {
      try {
        await api.logout()
        userStore.clearSessionToken()
        setApiSessionToken('')
        clearVisitorToken()
        chatStore.setCurrentConversation(null)
        chatStore.conversations = null
        postToParent({ type: 'SESSION_CLEARED' })
      } catch {
        console.error('Failed to revoke widget session')
        postToParent({ type: 'SESSION_CLEAR_FAILED' })
      }
    } else if (data.type === 'PAGE_VISIT') {
      sendPageVisit(data.url, data.title)
    }
}

const dispatchParentMessage = createOrderedAuthHandler(handleParentMessage)

onBeforeUnmount(stopParentBridge)

const initializeWebSocket = () => {
  const token = userStore.userSessionToken
  if (token) {
    const urlParams = new URLSearchParams(window.location.search)
    const inboxId = urlParams.get('inbox_id')
    if (inboxId) {
      initWidgetWS(token, inboxId)
    } else {
      console.error('Cannot initialize WebSocket: missing `inbox_id`')
    }
  } else {
    closeWidgetWebSocket()
  }
}

watch(
  () => userStore.userSessionToken,
  (newToken) => {
    if (newToken) {
      initializeWebSocket()
    } else {
      closeWidgetWebSocket()
    }
  }
)
</script>

<style scoped>
.libredesk-widget-app {
  width: 100vw;
  height: 100dvh;
  overflow: hidden;
}

.widget-container {
  width: 100%;
  height: 100%;
}

/* iOS Safari auto-zooms on focus when font-size < 16px. Force 16px on mobile to prevent it. */
.mobile :deep(input),
.mobile :deep(textarea),
.mobile :deep(select) {
  font-size: 16px;
}
</style>
