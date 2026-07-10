<script setup>
import { AvatarImage } from 'radix-vue'
import { computed } from 'vue'
import { sanitizeImageUrl } from '@shared-ui/utils/url.js'

const props = defineProps({
  src: { type: String, required: false, default: '' },
  asChild: { type: Boolean, required: false },
  as: { type: null, required: false }
})

const safeProps = computed(() => ({
  ...props,
  src: sanitizeImageUrl(props.src)
}))
</script>

<template>
  <AvatarImage v-bind="safeProps" class="h-full w-full object-cover" />
</template>
