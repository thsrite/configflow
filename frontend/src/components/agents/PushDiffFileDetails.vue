<template>
  <div class="flex flex-col gap-2.5 rounded-md border border-border bg-background/50 p-2.5" :data-file-details="details.kind">
    <section v-for="group in details.groups" :key="group.change" :data-group="group.change">
      <p class="m-0 text-[12px] text-muted-foreground">
        {{ CHANGE_LABELS[group.change] }} <span class="num">{{ group.total }}</span> {{ unit.unit }}{{ unit.noun }}
      </p>
      <ul v-if="group.entries.length" class="m-0 mt-1 flex list-none flex-col gap-1 p-0 font-mono text-[12px] leading-relaxed">
        <li v-for="(entry, index) in group.entries" :key="index" class="min-w-0 break-all">
          <span :class="ENTRY_TONES[entry.change]">{{ ENTRY_SIGNS[entry.change] }} <PushDiffText :value="entry.text" :hidden="hidden" /></span>
          <span v-if="entry.nth" class="font-sans text-muted-foreground">{{ nthText(entry.nth) }}</span>
          <span v-if="entry.info" class="text-muted-foreground"> · <PushDiffText :value="entry.info" :hidden="hidden" /></span>
          <ul v-if="entry.fields?.length" class="m-0 mt-0.5 flex list-none flex-col gap-0.5 border-l border-border py-0.5 pl-3 text-muted-foreground">
            <li v-for="(field, fieldIndex) in entry.fields" :key="fieldIndex"><PushDiffText :pieces="fieldPieces(field)" :hidden="hidden" /></li>
            <li v-if="entry.fields_omitted">另有 <span class="num">{{ entry.fields_omitted }}</span> 处变化</li>
          </ul>
        </li>
      </ul>
      <p v-if="group.omitted" class="m-0 mt-1 text-[12px] text-muted-foreground">
        还有 <span class="num">{{ group.omitted }}</span> {{ unit.unit }}未列出。
      </p>
    </section>
    <p v-for="note in notes" :key="note" class="m-0 text-[12px] leading-relaxed text-muted-foreground">{{ note }}</p>
  </div>
</template>

<script setup lang="ts">
// 「待推送的变更」表 1 里订阅、规则集展开的明细：按新增、删除、修改分组；敏感片段随开关隐藏。
import { computed } from 'vue'
import type { PushDiffChange, PushDiffFileDetails } from '@/api/agentPushDiff'
import { CHANGE_LABELS, detailNotes, detailUnit, fieldPieces, nthText } from '@/lib/agentPushDiff'
import PushDiffText from '@/components/agents/PushDiffText.vue'

const props = defineProps<{ details: PushDiffFileDetails; hidden: boolean }>()

const ENTRY_TONES: Record<PushDiffChange, string> = {
  added: 'text-success-accent', removed: 'text-destructive-accent', modified: 'text-warning-accent',
}
const ENTRY_SIGNS: Record<PushDiffChange, string> = { added: '+', removed: '−', modified: '~' }

const unit = computed(() => detailUnit(props.details.kind))
const notes = computed(() => detailNotes(props.details))
</script>
