<script setup lang="ts">
import type { LinuxGatewaySpec } from '@/api/linuxGateway'
import FormField from '@/components/common/FormField.vue'
import { gatewayCopy } from '@/lib/gatewayCopy'
import { Input } from '@/components/ui/input'
defineProps<{ modelValue: LinuxGatewaySpec; disabled?: boolean; hideAddress?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: LinuxGatewaySpec] }>()
</script>

<template>
  <div class="grid min-w-0 gap-3 sm:grid-cols-2">
    <FormField v-if="!hideAddress" label="Linux 的固定 IPv4" html-for="onboarding-address" v-bind="gatewayCopy.address"><Input id="onboarding-address" :model-value="modelValue.address" :disabled="disabled" class="min-h-11 font-mono" placeholder="192.0.2.2" @update:model-value="emit('update:modelValue', { ...modelValue, address: String($event) })" /></FormField>
    <FormField label="主路由 IPv4" html-for="onboarding-upstream" v-bind="gatewayCopy.upstream"><Input id="onboarding-upstream" :model-value="modelValue.upstream_gateway" :disabled="disabled" class="min-h-11 font-mono" placeholder="192.0.2.1" @update:model-value="emit('update:modelValue', { ...modelValue, upstream_gateway: String($event) })" /></FormField>
    <FormField label="LAN 网卡名" html-for="onboarding-interface" v-bind="gatewayCopy.interface"><Input id="onboarding-interface" :model-value="modelValue.interface" :disabled="disabled" class="min-h-11 font-mono" placeholder="例如 ens18、br0" @update:model-value="emit('update:modelValue', { ...modelValue, interface: String($event) })" /></FormField>
    <FormField label="LAN 网段" html-for="onboarding-cidr" v-bind="gatewayCopy.cidr"><Input id="onboarding-cidr" :model-value="modelValue.lan_cidr" :disabled="disabled" class="min-h-11 font-mono" placeholder="192.0.2.0/24" @update:model-value="emit('update:modelValue', { ...modelValue, lan_cidr: String($event) })" /></FormField>
  </div>
</template>
