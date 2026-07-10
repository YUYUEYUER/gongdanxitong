<template>
  <div class="secure-letter">
    <div
      v-if="hasRemoteResources"
      class="mb-2 rounded border border-border bg-muted/40 px-3 py-2 text-xs text-muted-foreground"
      role="status"
    >
      <span>Remote images are blocked.</span>
    </div>
    <div v-if="renderFailed" class="whitespace-pre-wrap break-words">{{ fallbackText }}</div>
    <Letter
      v-else
      :key="`remote-blocked-${renderVersion}`"
      :html="renderHtml"
      :text="text"
      :allowed-schemas="allowedSchemas"
      :allowed-css-properties="allowedCssProperties"
      :preserve-css-priority="preserveCssPriority"
      :rewrite-external-links="rewriteLink"
      :rewrite-external-resources="rewriteResource"
    />
  </div>
</template>

<script setup>
import { computed, onErrorCaptured, ref, watch } from 'vue'
import { Letter } from 'vue-letter'
import { sanitizeLinkUrl, sanitizeResourceUrl } from '@shared-ui/utils/url.js'
import { stripMalformedCssUrls } from '@shared-ui/utils/emailHtml.js'

const BLOCKED_RESOURCE = 'data:image/gif;base64,R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs='

const props = defineProps({
  html: { type: String, default: '' },
  text: { type: String, default: '' },
  allowedSchemas: { type: Array, default: () => ['cid', 'https', 'http', 'mailto'] },
  allowedCssProperties: { type: Array, default: undefined },
  preserveCssPriority: { type: Boolean, default: true }
})

const renderVersion = ref(0)
const renderFailed = ref(false)
const renderHtml = computed(() => stripMalformedCssUrls(props.html))
const fallbackText = computed(() => {
  if (props.text) return props.text
  if (typeof DOMParser === 'undefined') return renderHtml.value.replace(/<[^>]*>/g, ' ')
  return new DOMParser().parseFromString(renderHtml.value, 'text/html').body.textContent || ''
})

onErrorCaptured(() => {
  renderFailed.value = true
  return false
})

const isRemoteResource = (value) => {
  const safe = sanitizeResourceUrl(value)
  if (!safe || safe.toLowerCase().startsWith('cid:')) return false
  return new URL(safe).origin !== window.location.origin
}

const hasRemoteResources = computed(() => {
  if (!renderHtml.value || typeof DOMParser === 'undefined') return false
  const document = new DOMParser().parseFromString(renderHtml.value, 'text/html')
  if (Array.from(document.querySelectorAll('[src]')).some((node) => isRemoteResource(node.getAttribute('src')))) {
    return true
  }

  const css = [
    ...Array.from(document.querySelectorAll('[style]'), (node) => node.getAttribute('style') || ''),
    ...Array.from(document.querySelectorAll('style'), (node) => node.textContent || '')
  ].join('\n')
  const resources = Array.from(css.matchAll(/url\(\s*["']?([^"')]+)["']?\s*\)/gi), (match) => match[1])
  return resources.some(isRemoteResource)
})

watch(() => props.html, () => {
  renderFailed.value = false
  renderVersion.value += 1
})

const rewriteLink = (value) => sanitizeLinkUrl(value)

const rewriteResource = (value) => {
  const safe = sanitizeResourceUrl(value)
  if (!safe) return BLOCKED_RESOURCE
  if (safe.toLowerCase().startsWith('cid:')) return safe

  const parsed = new URL(safe)
  if (parsed.origin === window.location.origin) return safe
  return BLOCKED_RESOURCE
}
</script>
