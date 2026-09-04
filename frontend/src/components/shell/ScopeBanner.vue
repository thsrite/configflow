<template>
  <div class="cf-scope" :class="`is-${scope}`" role="note">
    <el-icon class="cf-scope__icon"><component :is="icon" /></el-icon>
    <div class="cf-scope__body">
      <b class="cf-scope__title">{{ title }}</b>
      <span v-if="description" class="cf-scope__desc">{{ description }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  /**
   * resource = 订阅/节点/规则集等资源（按配置空间隔离）
   * profile  = 策略与生成配置（按配置空间隔离）
   * system   = 真正跨配置空间共有的数据，目前只有 Agent 与配置空间本身
   */
  scope: 'resource' | 'profile' | 'system'
  profileName?: string
  description?: string
}>()

const icon = computed(() =>
  props.scope === 'system' ? 'Monitor' : props.scope === 'resource' ? 'Files' : 'Postcard'
)

const title = computed(() => {
  if (props.scope === 'system') return '系统级 · 所有配置空间共有'
  if (props.scope === 'resource') return `资源 · 当前配置「${props.profileName || '未选择'}」`
  return `当前配置 · ${props.profileName || '未选择'}`
})
</script>

<style scoped>
.cf-scope {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  padding: 11px 13px;
  border-radius: var(--cf-r-lg);
  border: 1px solid var(--cf-bd);
  font-size: 12.5px;
  line-height: 1.35;
  margin-bottom: var(--cf-sp-4);
}

.cf-scope.is-resource {
  background: var(--cf-shared-soft);
  border-color: color-mix(in srgb, var(--cf-shared) 28%, transparent);
}

.cf-scope.is-profile {
  background: var(--cf-profile-soft);
  border-color: color-mix(in srgb, var(--cf-profile) 30%, transparent);
}

.cf-scope.is-system {
  background: var(--cf-success-soft);
  border-color: color-mix(in srgb, var(--cf-success) 28%, transparent);
}
.cf-scope.is-system .cf-scope__icon {
  color: var(--cf-success);
}

.cf-scope__icon {
  font-size: 14px;
  margin-top: 1px;
  flex: 0 0 auto;
}
.cf-scope.is-resource .cf-scope__icon {
  color: var(--cf-shared);
}
.cf-scope.is-profile .cf-scope__icon {
  color: var(--cf-profile);
}

.cf-scope__title {
  font-weight: 650;
  color: var(--cf-fg);
}

.cf-scope__desc {
  display: block;
  margin-top: 1px;
  color: var(--cf-fg-2);
}
</style>
