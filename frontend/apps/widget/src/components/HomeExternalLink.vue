<template>
  <a :href="safeUrl" target="_blank" rel="noopener noreferrer" class="block no-underline">
    <Card class="hover:bg-accent transition-colors cursor-pointer rounded-md">
      <CardContent class="p-4">
        <div class="flex justify-between items-center">
          <span class="text-sm text-primary font-medium">
            {{ link.text }}
          </span>
          <ExternalLink size="18" class="text-muted-foreground" />
        </div>
      </CardContent>
    </Card>
  </a>
</template>

<script setup>
import { computed } from 'vue'
import { Card, CardContent } from '@shared-ui/components/ui/card'
import { ExternalLink } from 'lucide-vue-next'
import { sanitizeHttpUrl } from '@shared-ui/utils/url.js'

const props = defineProps({
  link: {
    type: Object,
    required: true
  }
})

const safeUrl = computed(() => sanitizeHttpUrl(props.link.url) || '#')
</script>
