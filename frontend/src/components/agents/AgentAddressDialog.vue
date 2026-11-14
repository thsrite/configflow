<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Loader2 } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import FormField from '@/components/common/FormField.vue'
import InfoNote from '@/components/common/InfoNote.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import { notify } from '@/lib/feedback'
import { agentApi, type AgentAddressInfo } from '@/api'
import type { Agent } from '@/types'

const props = defineProps<{ agent: Agent }>()
const emit = defineEmits<{ close: []; changed: [] }>()

const info = ref<AgentAddressInfo | null>(null)
const loadError = ref('')
const host = ref(props.agent.host)
const saving = ref(false)
const saveError = ref('')

const locked = computed(() => info.value?.mode === 'env' || info.value?.mode === 'ha')
const valid = computed(() => /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/.test(host.value.trim()))
// Pinning the address the record already has is still a change on a device that pins nothing or something else.
const unchanged = computed(() => host.value.trim() === info.value?.host
  && (info.value?.mode !== 'agent' || info.value.configured === host.value.trim()))
const scope = computed(() => {
  if (!info.value || locked.value) return ''
  if (info.value.mode === 'agent') return '保存后同时写入面板记录和设备上的 Agent 配置文件。'
  if (info.value.mode === 'record') return info.value.message
  return ''
})

onMounted(async () => {
  try {
    const { data } = await agentApi.getAddress(props.agent.id)
    info.value = data.data
    host.value = data.data.host
  } catch (error: any) {
    loadError.value = error.response?.data?.message || error.message || '读取地址信息失败'
  }
})

async function save() {
  if (!valid.value || saving.value) return
  saving.value = true
  saveError.value = ''
  try {
    const { data } = await agentApi.setAddress(props.agent.id, host.value.trim())
    notify.success('Agent 地址已修改', data.message)
    emit('changed')
    emit('close')
  } catch (error: any) {
    saveError.value = error.response?.data?.message || error.message || '修改失败，请稍后重试'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <Dialog :open="true" @update:open="value => { if (!value && !saving) emit('close') }">
    <DialogContent class="max-w-[480px]">
      <DialogHeader>
        <DialogTitle>修改地址 · {{ agent.name }}</DialogTitle>
        <DialogDescription>面板用这个地址联系设备上的 Agent（端口 {{ agent.port }}）。保存前会先核实能用新地址连上它。</DialogDescription>
      </DialogHeader>

      <LoadingRows v-if="!info && !loadError" :rows="2" />
      <p v-else-if="loadError" role="alert" class="m-0 text-[13px] text-destructive">{{ loadError }}</p>
      <div v-else-if="info" class="flex flex-col gap-4 text-[13px] leading-relaxed">
        <InfoNote v-if="locked || info.mode === 'unknown'" data-address-note>{{ info.message }}</InfoNote>
        <template v-if="!locked">
          <FormField label="设备地址" html-for="agent-address-host" :hint="scope || undefined">
            <Input
              id="agent-address-host" v-model="host" class="min-h-11 bg-background/50 font-mono" inputmode="decimal"
              autocomplete="off" spellcheck="false" placeholder="192.168.1.10" :aria-invalid="!valid" @keydown.enter="save"
            />
          </FormField>
          <div v-if="info.addresses.length > 1" class="flex flex-col gap-1.5">
            <span class="text-[12px] text-muted-foreground">这台设备现有的地址，点一下填入：</span>
            <div class="flex flex-wrap gap-2">
              <Button
                v-for="address in info.addresses" :key="address" type="button" size="sm" class="font-mono"
                :variant="address === host.trim() ? 'default' : 'outline'" :aria-pressed="address === host.trim()" @click="host = address"
              >{{ address }}</Button>
            </div>
          </div>
          <p v-if="info.mode === 'agent' && info.configured && info.configured !== info.host" class="m-0 text-[12px] text-muted-foreground">
            设备配置文件里现在写的是 {{ info.configured }}。
          </p>
          <p v-if="!valid" role="alert" class="m-0 text-destructive">请输入 IPv4 地址，例如 192.168.1.10。</p>
          <p v-if="saveError" role="alert" class="m-0 text-destructive" data-address-error>{{ saveError }}</p>
        </template>
      </div>

      <DialogFooter class="flex-row items-center justify-end gap-2">
        <Button variant="outline" class="min-h-11" :disabled="saving" @click="emit('close')">{{ locked ? '关闭' : '取消' }}</Button>
        <Button v-if="info && !locked" class="min-h-11" :disabled="!valid || unchanged || saving" @click="save">
          <Loader2 v-if="saving" class="size-3.5 animate-spin" />
          {{ saving ? '正在核实…' : '核实并保存' }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
