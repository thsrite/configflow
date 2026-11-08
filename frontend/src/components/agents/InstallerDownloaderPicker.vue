<script setup lang="ts">
import RadioChips from '@/components/common/RadioChips.vue'
import { INSTALLER_DOWNLOADERS, type InstallerDownloader } from '@/lib/coreInstallCommand'

// Shared by every short Shell install command: native Alpine has only BusyBox wget.
// Pass aria-label; it falls through to the radio group.
const model = defineModel<InstallerDownloader>({ required: true })
const options = INSTALLER_DOWNLOADERS.map(value => ({ value, label: value }))
</script>

<template>
  <RadioChips v-model="model" :options="options" class="flex gap-2" />
</template>
