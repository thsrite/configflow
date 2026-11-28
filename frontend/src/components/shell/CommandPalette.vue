<template>
  <CommandPaletteDialog v-if="hasOpened" v-model:open="open" :subscription-aggregation-enabled="subscriptionAggregationEnabled" />
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { NAV_GROUPS, navigationPath } from '@/navigation'
import { recordVisit } from '@/lib/frequentPages'

const props = defineProps<{ subscriptionAggregationEnabled: boolean }>()
const CommandPaletteDialog = defineAsyncComponent(() => import('./CommandPaletteDialog.vue'))
const open = ref(false)
const hasOpened = computed<boolean>(previous => open.value || !!previous)
const removeVisitHook = useRouter().afterEach(to => {
  const path = navigationPath(to.path)
  if (NAV_GROUPS.some(group => group.items.some(item => item.path === path &&
    (item.flag !== 'subscriptionAggregation' || props.subscriptionAggregationEnabled)))) recordVisit(path)
})
onUnmounted(removeVisitHook)

const onKeydown = (event: KeyboardEvent): void => {
  if (event.key.toLowerCase() === 'k' && (event.metaKey || event.ctrlKey)) {
    event.preventDefault()
    open.value = !open.value
  }
}

onMounted(() => document.addEventListener('keydown', onKeydown))
onUnmounted(() => document.removeEventListener('keydown', onKeydown))

defineExpose({ show: () => (open.value = true) })
</script>
