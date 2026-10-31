<template><template v-for="(piece, index) in parts" :key="index"><span v-if="piece.secret && hidden" class="whitespace-nowrap font-sans text-muted-foreground" data-secret-hidden><Lock class="mr-0.5 inline size-3 align-[-1px]" aria-hidden="true" />{{ HIDDEN_TEXT }}</span><span v-else-if="piece.secret" data-secret>{{ piece.text }}</span><template v-else>{{ piece.text }}</template></template></template>

<script setup lang="ts">
// 「待推送的变更」里的一段显示文字：服务端标出的敏感片段在 hidden 时只显示锁图标和「已隐藏」，原文不进页面。
import { computed } from 'vue'
import { Lock } from '@lucide/vue'
import type { PushDiffText } from '@/api/agentPushDiff'
import { HIDDEN_TEXT, textPieces, type TextPiece } from '@/lib/agentPushDiff'

const props = defineProps<{ value?: PushDiffText | null; pieces?: TextPiece[]; hidden: boolean }>()
const parts = computed(() => props.pieces ?? textPieces(props.value))
</script>
