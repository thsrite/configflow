<template>
  <CommandDialog v-model:open="open">
    <CommandInput placeholder="跳转到页面，或输入操作…" />
    <CommandList>
      <CommandEmpty>没有匹配的页面</CommandEmpty>
      <CommandGroup v-for="group in groups" :key="group.scope" :heading="group.title || '总览'">
        <CommandItem
          v-for="item in visibleItems(group)"
          :key="item.path"
          :value="`${item.label} ${item.path}`"
          @select="go(item.path)"
        >
          <component :is="iconOf(item.icon)" class="size-4 text-muted-foreground" aria-hidden="true" />
          <span>{{ item.label }}</span>
          <CommandShortcut class="font-mono">{{ item.path }}</CommandShortcut>
        </CommandItem>
      </CommandGroup>
      <CommandSeparator />
      <CommandGroup heading="操作">
        <CommandItem value="切换主题 theme dark light" @select="onToggleTheme">
          <SunMoon class="size-4 text-muted-foreground" aria-hidden="true" />
          <span>切换深浅主题</span>
        </CommandItem>
      </CommandGroup>
    </CommandList>
  </CommandDialog>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { SunMoon } from '@lucide/vue'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut
} from '@/components/ui/command'
import { NAV_GROUPS, type NavGroup, type NavItem } from '@/navigation'
import { iconOf } from '@/lib/icons'
import { useThemeStore } from '@/stores/theme'

const props = defineProps<{ subscriptionAggregationEnabled: boolean }>()

const router = useRouter()
const { toggleTheme } = useThemeStore()
const open = ref(false)
const groups = NAV_GROUPS

const visibleItems = (group: NavGroup): NavItem[] =>
  group.items.filter(
    item => item.flag !== 'subscriptionAggregation' || props.subscriptionAggregationEnabled
  )

const go = (path: string): void => {
  open.value = false
  router.push(path)
}

const onToggleTheme = (): void => {
  open.value = false
  toggleTheme()
}

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
