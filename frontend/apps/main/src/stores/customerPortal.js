import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import api from '@/api'

export const useCustomerPortalStore = defineStore('customerPortal', () => {
  const customer = ref(null)
  const isLoading = ref(false)

  const isAuthenticated = computed(() => !!customer.value?.id)
  const fullName = computed(() => {
    if (!customer.value) return ''
    return [customer.value.first_name, customer.value.last_name].filter(Boolean).join(' ').trim()
  })

  async function fetchCurrentCustomer() {
    isLoading.value = true
    try {
      const response = await api.getCurrentCustomer()
      customer.value = response?.data?.data || null
      return customer.value
    } catch {
      customer.value = null
      return null
    } finally {
      isLoading.value = false
    }
  }

  async function logout() {
    await api.customerLogout()
    customer.value = null
  }

  function setCustomer(value) {
    customer.value = value
  }

  return {
    customer,
    isLoading,
    isAuthenticated,
    fullName,
    fetchCurrentCustomer,
    logout,
    setCustomer
  }
})
