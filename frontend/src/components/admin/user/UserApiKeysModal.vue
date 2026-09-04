<template>
  <BaseDialog :show="show" :title="t('admin.users.userApiKeys')" width="full" @close="emit('close')">
    <div v-if="user" class="space-y-4">
      <div class="flex items-center gap-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30">
          <span class="text-lg font-medium text-primary-700 dark:text-primary-300">{{ user.email.charAt(0).toUpperCase() }}</span>
        </div>
        <div>
          <p class="font-medium text-gray-900 dark:text-white">{{ user.email }}</p>
          <p class="text-sm text-gray-500 dark:text-dark-400">{{ user.username }}</p>
        </div>
      </div>

      <KeyManagementWorkspace
        v-if="adapter"
        :key="user.id"
        :adapter="adapter"
      />
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getBatchApiKeysUsage } from '@/api/admin/dashboard'
import {
  createUserApiKey,
  deleteUserApiKey,
  getUserApiKeyAvailableGroups,
  getUserApiKeyGroupRates,
  listUserApiKeys,
  updateUserApiKey,
} from '@/api/admin/users'
import BaseDialog from '@/components/common/BaseDialog.vue'
import KeyManagementWorkspace from '@/components/keys/KeyManagementWorkspace.vue'
import type { KeyManagementAdapter } from '@/components/keys/keyManagementAdapter'
import type { AdminUser } from '@/types'

const props = defineProps<{
  show: boolean
  user: AdminUser | null
}>()

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()

// adapter is rebuilt per user ID, so every workspace request remains target-user scoped.
const adapter = computed<KeyManagementAdapter | null>(() => {
  const userID = props.user?.id
  if (!userID) return null

  return {
    list: (page, pageSize, filters, options) => listUserApiKeys(userID, page, pageSize, filters, options),
    create: (payload) => createUserApiKey(userID, payload),
    update: (keyID, updates) => updateUserApiKey(userID, keyID, updates),
    delete: (keyID) => deleteUserApiKey(userID, keyID),
    getAvailableGroups: () => getUserApiKeyAvailableGroups(userID),
    getUserGroupRates: () => getUserApiKeyGroupRates(userID),
    getUsageStats: (apiKeyIDs, options) => getBatchApiKeysUsage(apiKeyIDs, userID, options),
  }
})
</script>
