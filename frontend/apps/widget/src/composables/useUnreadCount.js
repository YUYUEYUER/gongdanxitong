import { computed, watch } from 'vue'
import { useChatStore } from '@widget/store/chat.js'
import { postToParent } from '@widget/parentBridge.js'

export function useUnreadCount() {
  const chatStore = useChatStore()

  // Calculate total unread messages across all conversations.
  const totalUnreadCount = computed(() => {
    const conversations = chatStore.getConversations
    if (!conversations || conversations.length === 0) return 0
    
    return conversations.reduce((total, conversation) => {
      return total + (conversation.unread_message_count || 0)
    }, 0)
  })

  // Send unread count to parent widget.
  const sendUnreadCountToWidget = (count) => {
    try {
      postToParent({
        type: 'UPDATE_UNREAD_COUNT',
        count: count
      })
    } catch {
      console.error('Failed to send unread count to widget')
    }
  }

  // Watch for changes in unread count and notify the widget.
  watch(totalUnreadCount, (newCount) => {
    sendUnreadCountToWidget(newCount)
  }, { immediate: true })

  return {
    totalUnreadCount,
    sendUnreadCountToWidget
  }
}
