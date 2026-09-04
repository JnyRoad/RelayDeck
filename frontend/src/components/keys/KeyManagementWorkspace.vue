<template>
  <KeysView :adapter="adapter" :embedded="true" />
</template>

<script setup lang="ts">
import KeysView from '@/views/user/KeysView.vue'
import type { KeyManagementAdapter } from './keyManagementAdapter'

// adapter is target-bound by the modal host, so this component never invents or changes Key ownership.
defineProps<{
  adapter: KeyManagementAdapter
}>()
</script>
