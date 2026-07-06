<template>
  <div class="grid gap-6 xl:grid-cols-[minmax(0,1.45fr)_23rem]">
    <section class="rounded-3xl border border-border bg-background p-6 shadow-sm">
      <div class="flex items-start justify-between gap-4 border-b border-border pb-5">
        <div class="space-y-2">
          <p class="text-xs font-semibold uppercase tracking-[0.2em] text-muted-foreground">
            {{ conversation?.reference_number || 'Ticket' }}
          </p>
          <h2 class="text-2xl font-bold tracking-tight text-foreground">
            {{ conversation?.subject || '工单详情' }}
          </h2>
          <div class="flex flex-wrap gap-2">
            <span
              class="rounded-full border px-3 py-1 text-xs font-medium"
              :class="customerTicketStatusClass(conversation?.status)"
            >
              {{ customerTicketStatusLabel(conversation?.status) }}
            </span>
            <span class="rounded-full border border-border px-3 py-1 text-xs font-medium">
              {{ conversation?.inbox_name || '-' }}
            </span>
          </div>
        </div>

        <Button variant="outline" @click="router.push('/portal/tickets')">返回列表</Button>
      </div>

      <div v-if="isLoading" class="py-8 text-sm text-muted-foreground">正在加载工单...</div>

      <div v-else class="mt-6 space-y-5">
        <article
          v-for="message in messages"
          :key="message.uuid"
          :class="[
            'rounded-[1.7rem] border px-5 py-4 shadow-sm transition',
            message.sender_type === 'contact'
              ? 'ml-10 border-primary/20 bg-primary/5'
              : 'mr-10 border-border bg-secondary/50'
          ]"
        >
          <div class="flex items-start justify-between gap-4">
            <div class="flex items-start gap-3">
              <div
                :class="[
                  'flex h-10 w-10 shrink-0 items-center justify-center rounded-2xl text-sm font-bold',
                  message.sender_type === 'contact'
                    ? 'bg-primary text-primary-foreground'
                    : 'bg-slate-900 text-white'
                ]"
              >
                {{ message.sender_type === 'contact' ? '我' : '客' }}
              </div>

              <div>
                <div class="flex items-center gap-2">
                  <p class="text-sm font-semibold text-foreground">
                    {{ message.sender_type === 'contact' ? '我的回复' : '客服回复' }}
                  </p>
                  <span
                    class="rounded-full px-2 py-0.5 text-[11px] font-medium"
                    :class="
                      message.sender_type === 'contact'
                        ? 'bg-primary/10 text-primary'
                        : 'bg-slate-900/10 text-slate-700'
                    "
                  >
                    {{ message.sender_type === 'contact' ? '客户' : '客服' }}
                  </span>
                </div>
                <p class="mt-1 text-xs text-muted-foreground">
                  {{ formatDate(message.created_at) }}
                </p>
              </div>
            </div>
          </div>

          <div class="mt-4 whitespace-pre-wrap text-sm leading-7 text-foreground">
            {{ message.content }}
          </div>

          <div v-if="message.attachments?.length" class="mt-4 flex flex-wrap gap-3">
            <button
              v-for="attachment in imageAttachments(message)"
              :key="`${message.uuid}-${attachment.uuid}-img`"
              type="button"
              class="group overflow-hidden rounded-2xl border border-border bg-background"
              @click="openImagePreview(message, attachment)"
            >
              <img
                :src="attachment.url"
                :alt="attachment.name"
                class="h-24 w-24 object-cover transition duration-200 group-hover:scale-[1.03]"
              />
            </button>

            <a
              v-for="attachment in fileAttachments(message)"
              :key="`${message.uuid}-${attachment.uuid}-file`"
              :href="attachment.url"
              target="_blank"
              rel="noreferrer"
              class="inline-flex items-center gap-2 rounded-full border border-border bg-background px-3 py-2 text-xs font-medium text-foreground transition hover:border-primary/35"
            >
              <Paperclip class="h-3.5 w-3.5 text-muted-foreground" />
              <span>{{ attachment.name }}</span>
              <span class="text-muted-foreground">{{ formatFileSize(attachment.size) }}</span>
            </a>
          </div>
        </article>
      </div>
    </section>

    <aside class="space-y-6">
      <div class="rounded-3xl border border-border bg-background p-5 shadow-sm">
        <div class="flex items-center gap-3">
          <div
            class="flex h-10 w-10 items-center justify-center rounded-2xl bg-primary/10 text-primary"
          >
            <ListTodo class="h-5 w-5" />
          </div>
          <div>
            <p class="text-sm font-semibold text-foreground">状态时间线</p>
            <p class="text-xs text-muted-foreground">查看工单从提交到处理的关键节点。</p>
          </div>
        </div>

        <div class="mt-5 space-y-4">
          <div v-for="item in timelineItems" :key="item.key" class="flex items-start gap-3">
            <div
              class="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full border"
              :class="
                item.active
                  ? 'border-primary/20 bg-primary/10 text-primary'
                  : 'border-border bg-secondary text-muted-foreground'
              "
            >
              <component :is="item.icon" class="h-4 w-4" />
            </div>
            <div>
              <p class="text-sm font-semibold text-foreground">{{ item.title }}</p>
              <p class="mt-1 text-xs leading-6 text-muted-foreground">{{ item.description }}</p>
              <p v-if="item.time" class="mt-1 text-xs font-medium text-foreground">
                {{ formatDate(item.time) }}
              </p>
            </div>
          </div>
        </div>
      </div>

      <div class="rounded-3xl border border-border bg-background p-5 shadow-sm">
        <div class="flex items-center gap-3">
          <div
            class="flex h-10 w-10 items-center justify-center rounded-2xl bg-slate-900 text-white"
          >
            <MessageSquareText class="h-5 w-5" />
          </div>
          <div>
            <p class="text-sm font-semibold text-foreground">继续回复</p>
            <p class="text-xs text-muted-foreground">补充更多上下文、截图或文件。</p>
          </div>
        </div>

        <textarea
          v-model="reply"
          class="mt-4 min-h-[180px] w-full rounded-xl border border-input bg-background px-3 py-3 text-sm"
          placeholder="补充更多信息..."
        ></textarea>

        <div class="mt-4 rounded-2xl border border-dashed border-border bg-secondary/40 p-4">
          <div class="flex flex-wrap items-center gap-3">
            <input ref="fileInput" type="file" multiple class="hidden" @change="handleFileChange" />
            <Button type="button" variant="outline" @click="triggerFileSelect">
              <Paperclip class="mr-2 h-4 w-4" />
              添加附件
            </Button>
            <span class="text-xs text-muted-foreground">支持多文件，上传后会附在本次回复里。</span>
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
                    <p class="text-xs text-muted-foreground">
                      {{ formatFileSize(attachment.size) }}
                    </p>
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

        <p v-if="errorMessage" class="mt-4 rounded bg-destructive/10 p-3 text-sm text-destructive">
          {{ errorMessage }}
        </p>

        <Button
          class="mt-4 w-full"
          :disabled="isReplying || isUploading || (!reply.trim() && !pendingAttachments.length)"
          @click="replyAction"
        >
          {{ isReplying ? '发送中...' : isUploading ? '上传中...' : '发送回复' }}
        </Button>
      </div>
    </aside>

    <ImageLightbox v-model="lightboxOpen" :images="lightboxImages" :start-index="lightboxIndex" />
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { handleHTTPError } from '@shared-ui/utils/http.js'
import { Button } from '@shared-ui/components/ui/button'
import {
  CheckCircle2,
  CircleDashed,
  Clock3,
  ListTodo,
  MessageSquareText,
  Paperclip,
  SendHorizonal,
  ShieldCheck,
  X
} from 'lucide-vue-next'
import api from '@/api'
import ImageLightbox from '@/components/ImageLightbox.vue'
import {
  customerTicketStatusClass,
  customerTicketStatusLabel
} from '@/utils/customer-ticket-status'

const route = useRoute()
const router = useRouter()
const fileInput = ref(null)
const conversation = ref(null)
const messages = ref([])
const reply = ref('')
const errorMessage = ref('')
const isLoading = ref(false)
const isReplying = ref(false)
const isUploading = ref(false)
const pendingAttachments = ref([])
const lightboxOpen = ref(false)
const lightboxImages = ref([])
const lightboxIndex = ref(0)

const timelineItems = computed(() => {
  const ticket = conversation.value
  if (!ticket) return []

  const status = String(ticket.status || '').toLowerCase()
  const isResolved = status.includes('resolved') || status.includes('closed')

  return [
    {
      key: 'created',
      title: '工单已提交',
      description: '你的问题已经进入系统，等待处理。',
      time: ticket.created_at,
      active: true,
      icon: SendHorizonal
    },
    {
      key: 'first_reply',
      title: '客服开始跟进',
      description: ticket.first_reply_at
        ? '客服已经开始处理这张工单。'
        : '客服尚未发送第一条回复。',
      time: ticket.first_reply_at || ticket.last_reply_at,
      active: !!(ticket.first_reply_at || ticket.last_reply_at),
      icon: MessageSquareText
    },
    {
      key: 'resolved',
      title: isResolved ? '工单已解决' : customerTicketStatusLabel(ticket.status),
      description: isResolved ? '当前工单已经进入解决或关闭状态。' : '当前工单仍在处理中。',
      time: ticket.resolved_at || ticket.updated_at,
      active: !!(ticket.resolved_at || isResolved),
      icon: isResolved ? CheckCircle2 : Clock3
    },
    {
      key: 'updated',
      title: '最新动态',
      description: '最近一次状态或消息更新时间。',
      time: ticket.updated_at,
      active: true,
      icon: ticket.updated_at ? ShieldCheck : CircleDashed
    }
  ]
})

function formatDate(value) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

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

function isImageAttachment(attachment) {
  return (
    (attachment?.content_type || '').startsWith('image/') ||
    /\.(png|jpe?g|gif|webp|bmp|svg)$/i.test(attachment?.name || '')
  )
}

function imageAttachments(message) {
  return (message.attachments || []).filter(isImageAttachment)
}

function fileAttachments(message) {
  return (message.attachments || []).filter((attachment) => !isImageAttachment(attachment))
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

function openImagePreview(message, attachment) {
  const images = imageAttachments(message).map((item) => ({
    url: item.url,
    name: item.name
  }))
  lightboxImages.value = images
  lightboxIndex.value = images.findIndex((item) => item.url === attachment.url)
  lightboxOpen.value = true
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

async function loadTicket() {
  isLoading.value = true
  try {
    const response = await api.customerGetTicket(route.params.uuid)
    conversation.value = response?.data?.data?.conversation || null
    messages.value = response?.data?.data?.messages || []
  } finally {
    isLoading.value = false
  }
}

async function replyAction() {
  isReplying.value = true
  errorMessage.value = ''
  try {
    isUploading.value = pendingAttachments.value.length > 0
    const attachmentIds = await uploadPendingAttachments()
    await api.customerReplyTicket(route.params.uuid, {
      message: reply.value,
      attachments: attachmentIds
    })
    reply.value = ''
    clearAttachmentPreviews()
    pendingAttachments.value = []
    await loadTicket()
  } catch (error) {
    errorMessage.value = handleHTTPError(error).message
  } finally {
    isUploading.value = false
    isReplying.value = false
  }
}

onMounted(loadTicket)
onBeforeUnmount(clearAttachmentPreviews)
</script>
