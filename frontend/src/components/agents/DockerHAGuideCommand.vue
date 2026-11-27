<script setup lang="ts">
import { Copy } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { copyText } from '@/lib/clipboard'
import { notify } from '@/lib/feedback'

const props = defineProps<{ title: string; command: string; note?: string; collapsed?: boolean; wrap?: boolean }>()
async function copy() {
  try { await copyText(props.command); notify.success('已复制，请在指定机器执行') }
  catch { notify.error('复制失败，请手动选择命令') }
}
</script>

<template>
  <section class="flex min-w-0 flex-col gap-2" :aria-label="title">
    <div class="flex min-w-0 items-center justify-between gap-2">
      <p class="m-0 min-w-0 text-[13px] font-medium [overflow-wrap:anywhere]">{{ title }}</p>
      <Button :variant="wrap ? 'outline' : 'ghost'" size="sm" class="min-h-11 shrink-0" :aria-label="wrap ? '复制命令' : `复制${title}`" @click="copy"><Copy class="size-3.5" />{{ wrap ? '复制命令' : '复制' }}</Button>
    </div>
    <p v-if="note" class="m-0 text-[12px] leading-relaxed text-muted-foreground">{{ note }}</p>
    <details v-if="collapsed" class="min-w-0"><summary class="min-h-11 cursor-pointer py-3 text-[13px]">查看完整命令</summary><pre class="m-0 max-h-72 min-w-0 overflow-auto rounded-md border border-border bg-muted p-3 text-[12px] leading-relaxed"><code>{{ command }}</code></pre></details>
    <pre v-else :class="wrap && 'whitespace-pre-wrap [overflow-wrap:anywhere]'" class="m-0 max-h-72 min-w-0 overflow-auto rounded-md border border-border bg-muted p-3 text-[12px] leading-relaxed"><code>{{ command }}</code></pre>
  </section>
</template>
