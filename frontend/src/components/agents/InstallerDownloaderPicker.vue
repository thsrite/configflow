<script setup lang="ts">
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { INSTALLER_DOWNLOADERS, type InstallerDownloader } from '@/lib/coreInstallCommand'

// Shared by every short Shell install command: native Alpine has only BusyBox wget.
// Pass aria-label; it falls through to the radio group.
const model = defineModel<InstallerDownloader>({ required: true })

const chipClass = (active: boolean): string =>
  [
    'flex min-h-11 cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-[13px] transition-colors',
    active
      ? 'border-primary-accent bg-primary-soft text-primary-accent dark:border-primary-accent/40 dark:bg-primary-soft/50 dark:text-foreground'
      : 'border-input bg-card text-muted-foreground hover:border-border-strong dark:border-border/50 dark:bg-background/40',
  ].join(' ')
</script>

<template>
  <RadioGroup v-model="model" class="flex gap-2">
    <label v-for="downloader in INSTALLER_DOWNLOADERS" :key="downloader" :class="chipClass(model === downloader)">
      <RadioGroupItem :value="downloader" />{{ downloader }}
    </label>
  </RadioGroup>
</template>
