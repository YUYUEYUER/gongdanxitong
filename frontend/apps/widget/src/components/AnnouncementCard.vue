<template>
  <a :href="safeUrl" target="_blank" rel="noopener noreferrer" class="block no-underline">
    <Card class="overflow-hidden hover:bg-accent transition-colors cursor-pointer rounded-md">
      <img
        :src="safeImageUrl"
        :alt="announcement.title"
        class="w-full h-auto"
      />
      <CardContent class="p-3 text-sm">
        <div class="font-bold">{{ announcement.title }}</div>
        <div v-if="announcement.description" class="text-muted-foreground mt-1 line-height-10">
          {{ announcement.description }}
        </div>
      </CardContent>
    </Card>
  </a>
</template>

<script setup>
import { computed } from 'vue'
import { Card, CardContent } from '@shared-ui/components/ui/card'
import { sanitizeHttpUrl } from '@shared-ui/utils/url.js'

const props = defineProps({
  announcement: {
    type: Object,
    required: true
  }
})

const safeUrl = computed(() => sanitizeHttpUrl(props.announcement.url) || '#')
const safeImageUrl = computed(() => sanitizeHttpUrl(props.announcement.image_url))
</script>
