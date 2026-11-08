<script setup lang="ts">
import { computed } from 'vue'
import AgentStatusRow from '@/components/agents/AgentStatusRow.vue'
import MihomoIPv6StepRow from './MihomoIPv6StepRow.vue'
import { buildMihomoIPv6Summary } from '@/lib/mihomoIPv6Summary'
import type { IPv6Verification, MihomoIPv6Readiness } from '@/types/mihomoIPv6'

// checklist: rows of the IPv6 step list (status pill beside the name) instead of status rows.
const props = defineProps<{ readiness?: MihomoIPv6Readiness; verification?: IPv6Verification; preferVerificationSnapshot?: boolean; checklist?: boolean }>()
const groups = computed(() => buildMihomoIPv6Summary({ readiness: props.readiness, verification: props.verification, preferVerificationSnapshot: props.preferVerificationSnapshot }))
</script>

<template>
  <div class="min-w-0 divide-y" :class="checklist ? 'divide-border/60' : 'divide-border/70'" data-ipv6-check-summary>
    <div v-for="group in groups" :key="group.id" class="min-w-0" :data-ipv6-check-group="group.id">
      <component :is="checklist ? MihomoIPv6StepRow : AgentStatusRow" :label="group.label" :status="group.status" :tone="group.tone">
        <template v-if="group.issues.length || (group.note && group.tone !== 'success')" #notice>
          <p v-for="issue in group.issues" :key="issue" class="m-0 [overflow-wrap:anywhere]" :role="group.tone === 'danger' ? 'alert' : undefined">{{ issue }}</p>
          <p v-if="!group.issues.length && group.note" class="m-0 text-muted-foreground [overflow-wrap:anywhere]">{{ group.note }}</p>
        </template>
      </component>
    </div>
  </div>
</template>
