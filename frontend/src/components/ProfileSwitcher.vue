<template>
  <div class="flex min-w-0 items-center gap-1">
    <Select v-model="selectedProfileId" :disabled="loading" @update:model-value="handleChange">
      <SelectTrigger
        class="h-8 w-[190px] gap-2 border-border/60 bg-background/40 text-[13px] font-medium transition-colors hover:border-border-strong max-md:w-[132px]"
        aria-label="当前配置空间"
      >
        <Boxes class="size-4 shrink-0 text-primary-accent" />
        <SelectValue :class="cn('truncate', !currentLabel && 'text-muted-foreground')">
          {{ currentLabel || '选择配置空间' }}
        </SelectValue>
      </SelectTrigger>
      <SelectContent align="end" class="glass-strong min-w-[220px]">
        <SelectItem v-for="profile in profiles" :key="profile.id" :value="profile.id">
          <span class="flex flex-col leading-snug">
            <span class="text-[13px] font-medium">{{ profile.name }}</span>
            <span class="font-mono text-[11px] text-muted-foreground">{{ profile.id }}</span>
          </span>
        </SelectItem>
      </SelectContent>
    </Select>

    <Button
      variant="ghost"
      size="icon-sm"
      class="text-primary-accent"
      title="管理配置空间"
      aria-label="管理配置空间"
      @click="router.push('/profiles')"
    >
      <Settings class="size-4" />
    </Button>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Boxes, Settings } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { confirm } from '@/lib/feedback'
import { useProfileStore } from '@/stores/profile'

const router = useRouter()
const profileStore = useProfileStore()
const { profiles, loading, activeProfileId, refreshProfiles, switchProfile } = profileStore
const selectedProfileId = ref(activeProfileId.value)

/* 配置空间列表尚未加载（或接口不可用）时，回退显示当前生效的 id，
 * 避免顶栏错误地呈现为「未选择」。
 */
const currentLabel = computed(
  () =>
    profiles.value.find(p => p.id === selectedProfileId.value)?.name ||
    selectedProfileId.value ||
    ''
)

watch(activeProfileId, value => {
  selectedProfileId.value = value
})

const handleChange = async (profileId: unknown) => {
  const id = String(profileId)
  if (!id || id === activeProfileId.value) return
  const ok = await confirm('切换后当前页面将重新加载，未保存的编辑内容会丢失。继续吗？', {
    title: '切换配置空间',
    confirmText: '切换'
  })
  if (!ok) {
    // 取消切换：回退到当前生效的配置空间
    selectedProfileId.value = activeProfileId.value
    return
  }
  switchProfile(id)
  window.location.reload()
}

onMounted(() => {
  refreshProfiles().catch(() => undefined)
})
</script>
