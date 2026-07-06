<template>
  <div class="rounded-3xl border border-border bg-background p-6 shadow-sm">
    <div class="space-y-1">
      <p class="text-xs font-semibold uppercase tracking-[0.2em] text-primary">客户工单</p>
      <h2 class="text-2xl font-bold tracking-tight text-foreground">提交新工单</h2>
      <p class="text-sm leading-7 text-muted-foreground">
        你的工单会默认进入客户门户专用队列，后续直接在系统内继续跟进。
      </p>
    </div>

    <form class="mt-6 space-y-5" @submit.prevent="submitAction">
      <div class="rounded-2xl border border-border bg-secondary/40 px-4 py-4">
        <p class="text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground">
          受理通道
        </p>
        <p class="mt-2 text-base font-semibold text-foreground">
          {{ inboxName || '客户门户' }}
        </p>
        <p class="mt-1 text-sm text-muted-foreground">系统会自动把你的工单路由到这个专用入口。</p>
      </div>

      <div class="space-y-2">
        <Label for="customer-ticket-subject">主题</Label>
        <Input id="customer-ticket-subject" v-model.trim="form.subject" />
      </div>

      <div class="space-y-2">
        <Label for="customer-ticket-message">问题描述</Label>
        <Textarea id="customer-ticket-message" v-model="form.content" class="min-h-[220px]" />
      </div>

      <div class="rounded-2xl border border-dashed border-border bg-secondary/40 p-4">
        <div class="flex flex-wrap items-center gap-3">
          <input ref="fileInput" type="file" multiple class="hidden" @change="handleFileChange" />
          <Button type="button" variant="outline" @click="triggerFileSelect">
            <Paperclip class="mr-2 h-4 w-4" />
            添加附件
          </Button>
          <span class="text-xs text-muted-foreground">可附上截图、日志或其它说明文件。</span>
        </div>

        <div v-if="pendingAttachments.length" class="mt-4 space-y-3">
          <div
            v-for="attachment in pendingAttachments"
            :key="attachment.id"
            class="overflow-hidden rounded-xl border border-border bg-background"
          >
            <div
              v-if="attachment.previewUrl"
              class="grid grid-cols-[96px_minmax(0,1fr)_auto] items-center gap-3 p-3"
            >
              <img
                :src="attachment.previewUrl"
                :alt="attachment.name"
                class="h-24 w-24 rounded-lg object-cover"
              />
              <div>
                <p class="text-sm font-medium text-foreground">{{ attachment.name }}</p>
                <p class="text-xs text-muted-foreground">{{ formatFileSize(attachment.size) }}</p>
              </div>
              <button
                type="button"
                class="rounded-full p-1 text-muted-foreground transition hover:bg-secondary hover:text-foreground"
                @click="removeAttachment(attachment.id)"
              >
                <X class="h-4 w-4" />
              </button>
            </div>
            <div v-else class="flex items-center justify-between px-3 py-2">
              <div class="flex items-center gap-3">
                <Paperclip class="h-4 w-4 text-muted-foreground" />
                <div>
                  <p class="text-sm font-medium text-foreground">{{ attachment.name }}</p>
                  <p class="text-xs text-muted-foreground">{{ formatFileSize(attachment.size) }}</p>
                </div>
              </div>
              <button
                type="button"
                class="rounded-full p-1 text-muted-foreground transition hover:bg-secondary hover:text-foreground"
                @click="removeAttachment(attachment.id)"
              >
                <X class="h-4 w-4" />
              </button>
            </div>
          </div>
        </div>
      </div>

      <p v-if="errorMessage" class="rounded bg-destructive/10 p-3 text-sm text-destructive">
        {{ errorMessage }}
      </p>

      <div class="flex justify-end">
        <Button :disabled="isSubmitting || isUploading || !inboxId" type="submit">
          {{ isSubmitting ? '提交中...' : isUploading ? '上传中...' : '提交工单' }}
        </Button>
      </div>
    </form>
  </div>
</template>

<script setup>
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { handleHTTPError } from '@shared-ui/utils/http.js'
import { Button } from '@shared-ui/components/ui/button'
import { Input } from '@shared-ui/components/ui/input'
import { Label } from '@shared-ui/components/ui/label'
import { Textarea } from '@shared-ui/components/ui/textarea'
import { Paperclip, X } from 'lucide-vue-next'
import api from '@/api'

const router = useRouter()
const fileInput = ref(null)
const inboxId = ref(0)
const inboxName = ref('')
const pendingAttachments = ref([])
const errorMessage = ref('')
const isSubmitting = ref(false)
const isUploading = ref(false)
const form = reactive({
  subject: '',
  content: ''
})

function formatFileSize(value) {
  const size = Number(value || 0)
  if (!size) return '0 B'
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / (1024 * 1024)).toFixed(1)} MB`
}

function triggerFileSelect() {
  fileInput.value?.click()
}

function removeAttachment(id) {
  const item = pendingAttachments.value.find((entry) => entry.id === id)
  if (item?.previewUrl) {
    URL.revokeObjectURL(item.previewUrl)
  }
  pendingAttachments.value = pendingAttachments.value.filter((entry) => entry.id !== id)
}

function isImageFile(file) {
  return file.type.startsWith('image/') || /\.(png|jpe?g|gif|webp|bmp|svg)$/i.test(file.name)
}

function handleFileChange(event) {
  const files = Array.from(event.target.files || [])
  const additions = files.map((file) => ({
    id: `${file.name}-${file.size}-${file.lastModified}-${Math.random().toString(36).slice(2, 8)}`,
    file,
    name: file.name,
    size: file.size,
    previewUrl: isImageFile(file) ? URL.createObjectURL(file) : ''
  }))
  pendingAttachments.value = [...pendingAttachments.value, ...additions]
  if (event.target) {
    event.target.value = ''
  }
}

function clearAttachmentPreviews() {
  pendingAttachments.value.forEach((attachment) => {
    if (attachment.previewUrl) {
      URL.revokeObjectURL(attachment.previewUrl)
    }
  })
}

async function uploadPendingAttachments() {
  if (!pendingAttachments.value.length) return []

  const uploadedIds = []
  for (const attachment of pendingAttachments.value) {
    const formData = new FormData()
    formData.append('files', attachment.file)
    const response = await api.customerUploadMedia(formData)
    if (response?.data?.data?.id) {
      uploadedIds.push(response.data.data.id)
    }
  }
  return uploadedIds
}

async function loadConfig() {
  const response = await api.customerTicketConfig()
  const data = response?.data?.data || {}
  inboxId.value = data.default_inbox_id || 0
  inboxName.value = data.inbox_name || '客户门户'
}

async function submitAction() {
  isSubmitting.value = true
  errorMessage.value = ''
  try {
    isUploading.value = pendingAttachments.value.length > 0
    const attachmentIds = await uploadPendingAttachments()
    const response = await api.customerCreateTicket({
      inbox_id: inboxId.value,
      subject: form.subject,
      content: form.content,
      attachments: attachmentIds
    })
    const ticket = response?.data?.data
    clearAttachmentPreviews()
    pendingAttachments.value = []
    router.push(`/portal/tickets/${ticket.conversation_uuid}`)
  } catch (error) {
    errorMessage.value = handleHTTPError(error).message
  } finally {
    isUploading.value = false
    isSubmitting.value = false
  }
}

onMounted(loadConfig)
onBeforeUnmount(clearAttachmentPreviews)
</script>
