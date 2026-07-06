<template>
  <div class="min-h-[68px]">
    <div ref="container"></div>
  </div>
</template>

<script setup>
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'

const TURNSTILE_SCRIPT_ID = 'cloudflare-turnstile-script'
const TURNSTILE_SCRIPT_SRC = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'

let turnstileScriptPromise = null

function loadTurnstileScript() {
  if (typeof window === 'undefined') {
    return Promise.resolve(null)
  }

  if (window.turnstile) {
    return Promise.resolve(window.turnstile)
  }

  if (turnstileScriptPromise) {
    return turnstileScriptPromise
  }

  turnstileScriptPromise = new Promise((resolve, reject) => {
    let script = document.getElementById(TURNSTILE_SCRIPT_ID)

    const handleLoad = () => {
      if (window.turnstile) {
        resolve(window.turnstile)
        return
      }
      turnstileScriptPromise = null
      reject(new Error('turnstile_unavailable'))
    }

    const handleError = () => {
      turnstileScriptPromise = null
      reject(new Error('turnstile_load_failed'))
    }

    if (!script) {
      script = document.createElement('script')
      script.id = TURNSTILE_SCRIPT_ID
      script.src = TURNSTILE_SCRIPT_SRC
      script.async = true
      script.defer = true
      document.head.appendChild(script)
    }

    script.addEventListener('load', handleLoad, { once: true })
    script.addEventListener('error', handleError, { once: true })
  })

  return turnstileScriptPromise
}

const props = defineProps({
  modelValue: {
    type: String,
    default: ''
  },
  siteKey: {
    type: String,
    default: ''
  },
  theme: {
    type: String,
    default: 'light'
  },
  action: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['update:modelValue', 'error', 'expired'])

const container = ref(null)
const widgetId = ref(null)

function clearToken() {
  emit('update:modelValue', '')
}

function reset() {
  clearToken()
  if (window.turnstile && widgetId.value !== null) {
    window.turnstile.reset(widgetId.value)
  }
}

async function renderWidget(siteKey) {
  if (!siteKey) {
    clearToken()
    return
  }

  try {
    await loadTurnstileScript()
    await nextTick()

    if (!container.value || !window.turnstile) {
      emit('error')
      return
    }

    if (widgetId.value !== null) {
      window.turnstile.remove(widgetId.value)
      widgetId.value = null
    }

    clearToken()
    const options = {
      sitekey: siteKey,
      theme: props.theme,
      callback(token) {
        emit('update:modelValue', token)
      },
      'expired-callback': () => {
        clearToken()
        emit('expired')
      },
      'error-callback': () => {
        clearToken()
        emit('error')
      }
    }
    if (props.action) {
      options.action = props.action
    }

    widgetId.value = window.turnstile.render(container.value, options)
  } catch {
    emit('error')
  }
}

watch(
  () => props.siteKey,
  (siteKey) => {
    renderWidget(siteKey)
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  clearToken()
  if (window.turnstile && widgetId.value !== null) {
    window.turnstile.remove(widgetId.value)
  }
})

defineExpose({
  reset
})
</script>
