<template>
  <RouterView />
</template>

<script setup>
import { onMounted, onUnmounted } from 'vue'
import { RouterView } from 'vue-router'
import { EMITTER_EVENTS } from './constants/emitterEvents.js'
import { useEmitter } from './composables/useEmitter'
import { toast as sooner } from 'vue-sonner'

const emitter = useEmitter()
const OUTER_ROUTE_SCROLL_CLASS = 'outer-route-scroll'

const toastHandler = (message) => {
  if (!message.description) return
  if (message.variant === 'destructive') {
    sooner.error(message.description)
  } else {
    sooner.success(message.description)
  }
}

onMounted(() => {
  document.documentElement.classList.add(OUTER_ROUTE_SCROLL_CLASS)
  document.body.classList.add(OUTER_ROUTE_SCROLL_CLASS)
  emitter.on(EMITTER_EVENTS.SHOW_TOAST, toastHandler)
})

onUnmounted(() => {
  document.documentElement.classList.remove(OUTER_ROUTE_SCROLL_CLASS)
  document.body.classList.remove(OUTER_ROUTE_SCROLL_CLASS)
  emitter.off(EMITTER_EVENTS.SHOW_TOAST, toastHandler)
})
</script>
