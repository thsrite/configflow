<template>
  <div class="flex min-w-0 flex-col gap-4 max-sm:gap-3">
    <p v-if="error" role="alert" class="m-0 break-words text-sm text-warning-accent">{{ error }}{{ snapshot ? '，以下是上次读取的数据。' : '' }}</p>
    <MihomoConnectionStats :connections="snapshot?.connections || []" :speeds="speeds" :notes="notes" :loading="!snapshot && !error"
      :agent-id="agentId" :active="active" tall />
  </div>
</template>

<script setup lang="ts">
import MihomoConnectionStats from './MihomoConnectionStats.vue'
import { useTabActive } from '@/components/ui/tabs'
import { useMihomoSnapshot } from '@/composables/useMihomoSnapshot'

/** Mihomo 面板「统计」标签：连接统计单独成页，只在打开这个标签时读取 */
const props = withDefaults(defineProps<{ agentId: string; reloadToken?: number; active?: boolean }>(), { active: true })
const active = useTabActive(() => props.active)
const { snapshot, error, notes, speeds } = useMihomoSnapshot(() => props.agentId, active, () => props.reloadToken)
</script>
