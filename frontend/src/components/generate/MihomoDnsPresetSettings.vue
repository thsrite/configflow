<script setup lang="ts">
import { computed, ref } from 'vue'
import { ChevronDown } from '@lucide/vue'
import RadioChips from '@/components/common/RadioChips.vue'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { DNS_LIST_COPY, dnsPresetHelp, followPolicyHelp, respectRulesHelp, type BaseFieldDef } from '@/lib/mihomoBaseCopy'
import { DNS_POLICY_KEY, readDnsPreset, type BaseIssue, type BasePath, type DnsPreset, type MihomoBaseView } from '@/lib/mihomoBaseSettings'
import MihomoBaseCell from './MihomoBaseCell.vue'
import MihomoBaseField from './MihomoBaseField.vue'
import { CELL_POPOVER, CELL_TRIGGER } from './mihomoBaseCell'

const props = defineProps<{ view: MihomoBaseView; issues: (field: string) => BaseIssue[]; disabled?: boolean }>()
const emit = defineEmits<{ change: [path: BasePath, value: unknown, kind: 'list' | 'scalar']; preset: [preset: Exclude<DnsPreset, 'custom'>]; draft: [field: string, error: string] }>()
const labels: Record<DnsPreset, string> = { domestic: '国内 DoH', split: '国内外分开解析', custom: '自定义' }
// 自定义 is never picked here: editing any upstream cell beside it makes the setup custom.
const options = (Object.keys(labels) as DnsPreset[]).map(value => ({ value, label: labels[value], disabled: value === 'custom' }))
const descriptions: Record<DnsPreset, string> = {
  domestic: '与初始模板一致：全部使用国内 DoH，respect-rules 开启。',
  split: '国内和私有域名用国内 DoH，其他域名经代理用 Google、Cloudflare DoH；依赖 GeoSite 数据和可用节点。',
  custom: '当前写法与两种预设都不同。直接修改旁边各项上游即可，选一种预设会整体替换它们。',
}
const state = computed(() => readDnsPreset(props.view))
const menuOpen = ref(false)
function choose(value: string) {
  menuOpen.value = false
  if (value !== 'custom' && value !== state.value.preset) emit('preset', value as Exclude<DnsPreset, 'custom'>)
}
const listKeys = computed(() => ['default-nameserver', 'nameserver', 'proxy-server-nameserver', 'direct-nameserver',
  ...(props.view.value(['dns', 'fallback']) !== undefined ? ['fallback'] : [])])
const listField = (key: string): BaseFieldDef => ({ path: ['dns', key], label: DNS_LIST_COPY[key].label, help: DNS_LIST_COPY[key].help, type: 'list', placeholder: 'https://223.5.5.5/dns-query' })
const dnsIssues = computed(() => ['nameserver-policy', 'proxy-server-nameserver-policy'].flatMap(key => props.issues(`dns.${key}`)))
const respectField: BaseFieldDef = { path: ['dns', 'respect-rules'], label: 'DNS 查询遵循规则（respect-rules）', type: 'bool', coreDefault: false, help: respectRulesHelp }
const followField: BaseFieldDef = { path: ['dns', 'direct-nameserver-follow-policy'], label: '直连 DNS 遵循策略（direct-nameserver-follow-policy）', type: 'bool', coreDefault: false, help: followPolicyHelp }
const policyEntries = computed(() => {
  const policy = props.view.value(['dns', 'nameserver-policy'])
  return policy && typeof policy === 'object' && !Array.isArray(policy) ? Object.keys(policy) : []
})
const userPolicies = computed(() => policyEntries.value.filter(key => key !== DNS_POLICY_KEY))
</script>

<template>
  <div class="contents" data-base-dns>
    <MihomoBaseCell label="DNS 方案" :help="dnsPresetHelp" :status="state.editable ? '' : state.reason" warn data-base-field="dns.preset">
      <Popover v-model:open="menuOpen">
        <PopoverTrigger as-child>
          <button type="button" :class="CELL_TRIGGER" :disabled="disabled || !state.editable" :aria-label="`DNS 方案：${state.preset ? labels[state.preset] : '无法读取'}`" data-base-dns-preset>
            <span class="min-w-0 flex-1 truncate">{{ state.preset ? labels[state.preset] : '无法读取' }}</span>
            <ChevronDown class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" :class="CELL_POPOVER">
          <p class="m-0 text-[13px] font-medium">DNS 方案</p>
          <RadioChips :model-value="state.preset ?? ''" :options="options" aria-label="DNS 方案" @update:model-value="choose" />
          <p v-if="state.preset" class="m-0 break-words text-xs leading-relaxed text-muted-foreground" data-base-dns-description>{{ descriptions[state.preset] }}</p>
        </PopoverContent>
      </Popover>
    </MihomoBaseCell>
    <MihomoBaseField v-for="key in listKeys" :key="key" :field="listField(key)" :state="view.field(['dns', key], 'list')" :issues="issues(`dns.${key}`)"
      :disabled="disabled || !state.editable" @change="emit('change', ['dns', key], $event, 'list')" />
    <MihomoBaseField :field="respectField" :state="view.field(['dns', 'respect-rules'])" :issues="issues('dns.respect-rules')" :disabled="disabled || !state.editable"
      @change="emit('change', ['dns', 'respect-rules'], $event, 'scalar')" />
    <MihomoBaseField :field="followField" :state="view.field(['dns', 'direct-nameserver-follow-policy'])" :issues="issues('dns.direct-nameserver-follow-policy')"
      :disabled="disabled || !state.editable" @change="emit('change', ['dns', 'direct-nameserver-follow-policy'], $event, 'scalar')" />
    <p class="col-span-full m-0 break-words px-2 pt-1 text-[11.5px] leading-relaxed text-muted-foreground" data-base-dns-policy>
      <template v-if="userPolicies.length">nameserver-policy 中你自己的 {{ userPolicies.length }} 条（{{ userPolicies.slice(0, 3).join('、') }}{{ userPolicies.length > 3 ? ' 等' : '' }}）切换方案时保留并优先匹配，请在下方 YAML 中编辑。</template>
      <template v-else>需要为个别域名指定 DNS 时，请在下方 YAML 的 nameserver-policy 中添加。</template>
    </p>
    <p v-for="item in dnsIssues" :key="item.message" class="col-span-full m-0 break-words px-2 text-[11.5px]" :class="item.blocking ? 'text-destructive-accent' : 'text-warning-accent'"
      :role="item.blocking ? 'alert' : undefined" data-base-issue>
      {{ item.blocking ? item.message : `已保存的配置：${item.message}` }}
    </p>
  </div>
</template>
