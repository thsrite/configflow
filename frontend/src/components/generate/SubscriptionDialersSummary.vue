<script setup lang="ts">
import { computed } from 'vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import type { SubscriptionDialersReport } from '@/types'

const props = defineProps<{ report: SubscriptionDialersReport }>()

const HELP = '有些机场订阅里的节点要先经过机场自己的中转分组（dialer-proxy）再连出口。'
  + 'ConfigFlow 只导入节点，所以会按订阅里记录的结构自动补上这些分组，名称是「订阅名/原分组名」，在 Mihomo 面板中隐藏；'
  + '每次刷新订阅都会重新读取，机场改名后重新推送即可。机场的分组没法补上时（成员不在订阅里或是动态分组），这些节点本次跳过，其他节点照常发布。'
  + 'Surge、Loon 不支持这种中转，这些节点在它们的订阅里同样跳过。'
const hiddenSkipped = computed(() => Math.max(0, props.report.skipped_count - props.report.skipped.length))
const reason = (item: SubscriptionDialersReport['skipped'][number]) =>
  item.reason === 'refresh'
    ? '订阅缓存还没有记录机场的分组，在「订阅来源」刷新这个订阅后会自动补上，刷新前发布会报错'
    : '机场的这个分组没法补上，本次跳过这个节点'
</script>

<template>
  <div class="min-w-0 border-b border-border px-5 py-2 text-xs max-sm:px-3" role="group" aria-label="订阅链式节点" data-subscription-dialers>
    <div class="flex min-h-11 flex-wrap items-center gap-x-2 gap-y-1">
      <span class="font-semibold text-foreground">订阅链式节点</span>
      <span class="text-muted-foreground" data-subscription-dialers-counts>
        自动补上中转分组 <span class="cf-num text-foreground">{{ report.groups.length }}</span> 个<template v-if="report.skipped_count">，未能补上的节点
          <span class="cf-num text-warning-accent">{{ report.skipped_count }}</span> 个</template>
      </span>
      <FieldHelp label="订阅链式节点" :text="HELP" />
    </div>
    <ul v-if="report.skipped.length" class="m-0 list-none space-y-0.5 p-0 pb-1 text-warning-accent">
      <li v-for="item in report.skipped" :key="`${item.subscription}/${item.node}`" class="break-words">
        {{ item.subscription }}「{{ item.node }}」：找不到中转「{{ item.target }}」，{{ reason(item) }}
      </li>
      <li v-if="hiddenSkipped" class="text-muted-foreground">另有 {{ hiddenSkipped }} 个，见服务端日志</li>
    </ul>
  </div>
</template>
