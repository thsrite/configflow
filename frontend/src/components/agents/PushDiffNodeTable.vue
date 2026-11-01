<template>
  <table class="cf-table cf-mobile-records push-diff-nodes" :aria-labelledby="labelledby" data-node-table>
    <colgroup>
      <col class="w-[24%]"><col class="w-[18%]"><col class="w-[29%]"><col class="w-[29%]">
    </colgroup>
    <thead>
      <tr><th scope="col">节点</th><th scope="col">变更项</th><th scope="col">变更前</th><th scope="col">变更后</th></tr>
    </thead>
    <tbody v-for="(node, index) in nodes" :key="index" :data-node="node.entry.change">
      <tr v-for="(row, rowIndex) in node.rows" :key="rowIndex">
        <!-- 节点名跨这个节点的所有行；scope=rowgroup 让读屏把它当作这一组行的标题 -->
        <th v-if="!rowIndex" scope="rowgroup" :rowspan="node.rows.length" data-node-name>
          <PushDiffText :value="node.entry.text" :hidden="hidden" /><span v-if="node.entry.nth" class="font-normal text-muted-foreground">{{ nthText(node.entry.nth) }}</span>
        </th>
        <td v-if="row.kind === 'note'" colspan="3" class="text-muted-foreground" data-node-note>{{ row.text }}</td>
        <template v-else>
          <td data-node-key>
            <span v-if="row.kind === 'change'" :class="['chip max-md:mr-1.5', CHANGE_CHIPS[row.change]]">{{ CHANGE_LABELS[row.change] }}</span>
            <template v-else><span class="font-mono"><PushDiffText :value="row.key" :hidden="hidden" breaks /></span><span class="hidden max-md:inline" aria-hidden="true">：</span></template>
          </td>
          <td :class="cellClass(row, row.before)" data-node-before><PushDiffText :pieces="row.before.pieces" :hidden="hidden" breaks /></td>
          <td :class="cellClass(row, row.after)" data-node-after><span v-if="row.kind === 'field'" class="hidden max-md:inline" aria-hidden="true"> → </span><PushDiffText :pieces="row.after.pieces" :hidden="hidden" breaks /></td>
        </template>
      </tr>
    </tbody>
  </table>
</template>

<script setup lang="ts">
// 订阅明细里的一组节点（新增、删除或修改）：节点 | 变更项 | 变更前 | 变更后。修改的节点每个字段一行、节点名跨行；
// 新增、删除的节点一行，另一侧是类型 · 服务器:端口。手机上按记录排列：节点名作标题，每行「变更项：变更前 → 变更后」。
// 字段路径和值只在「.」「-」「/」之后断行，没有这些位置的长串（如密码）才在格内任意处折行。
import { computed } from 'vue'
import type { PushDiffListEntry } from '@/api/agentPushDiff'
import { CHANGE_CHIPS, CHANGE_LABELS, nodeRows, nthText, type NodeRow, type NodeValue } from '@/lib/agentPushDiff'
import PushDiffText from '@/components/agents/PushDiffText.vue'

const props = defineProps<{
  entries: PushDiffListEntry[]
  hidden: boolean
  /** 分组标题的 id，作为表格的名称 */
  labelledby?: string
}>()

const nodes = computed(() => props.entries.map(entry => ({ entry, rows: nodeRows(entry) })))

/** 「—」用次要文字；新增节点的变更前、删除节点的变更后在手机上不占位置 */
function cellClass(row: NodeRow, value: NodeValue) {
  if (!value.empty) return 'font-mono'
  return ['text-muted-foreground', row.kind === 'change' && 'push-diff-empty']
}
</script>
