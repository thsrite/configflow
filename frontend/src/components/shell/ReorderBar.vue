<template>
  <div
    v-if="active"
    class="sticky top-[calc(var(--cf-topbar-h)+env(safe-area-inset-top))] z-20 mb-3 flex items-center gap-3 rounded-lg border border-primary-accent/35 bg-primary-soft py-2.5 pr-2.5 pl-3.5"
    role="region"
    aria-label="排序模式"
  >
    <div class="min-w-0">
      <div class="text-[13px] font-semibold text-foreground">正在调整顺序</div>
      <div class="mt-px text-[11.5px] text-muted-foreground max-[640px]:hidden">{{ hint }}</div>
    </div>
    <div class="ml-auto flex shrink-0 gap-2">
      <Button variant="outline" size="sm" :disabled="saving" @click="$emit('cancel')">取消</Button>
      <Button size="sm" :disabled="saving" @click="$emit('save')">
        <Loader2 v-if="saving" class="size-3.5 animate-spin" />
        保存顺序
      </Button>
    </div>
  </div>
  <!-- 排序状态变化对屏幕阅读器播报 -->
  <div class="cf-sr" role="status" aria-live="polite">{{ announcement }}</div>
</template>

<script setup lang="ts">
import { Loader2 } from '@lucide/vue'
import { Button } from '@/components/ui/button'

withDefaults(
  defineProps<{
    active: boolean
    saving?: boolean
    announcement?: string
    hint?: string
  }>(),
  {
    saving: false,
    announcement: '',
    hint: '拖动手柄调整顺序，移动端长按手柄；键盘可用空格抓取、方向键移动'
  }
)

defineEmits<{ (e: 'cancel'): void; (e: 'save'): void }>()
</script>
