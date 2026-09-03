<template>
  <div v-if="active" class="cf-reorder-bar" role="region" aria-label="排序模式">
    <div class="cf-reorder-bar__text">
      <div class="cf-reorder-bar__title">正在调整顺序</div>
      <div class="cf-reorder-bar__hint">{{ hint }}</div>
    </div>
    <div class="cf-reorder-bar__actions">
      <el-button size="small" :disabled="saving" @click="$emit('cancel')">取消</el-button>
      <el-button size="small" type="primary" :loading="saving" @click="$emit('save')">
        保存顺序
      </el-button>
    </div>
  </div>
  <!-- 排序状态变化对屏幕阅读器播报 -->
  <div class="cf-sr-only" role="status" aria-live="polite">{{ announcement }}</div>
</template>

<script setup lang="ts">
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

<style scoped>
.cf-reorder-bar {
  position: sticky;
  top: calc(var(--cf-topbar-h) + env(safe-area-inset-top));
  z-index: 20;
  display: flex;
  align-items: center;
  gap: var(--cf-sp-3);
  padding: 9px 10px 9px 13px;
  margin-bottom: var(--cf-sp-3);
  border-radius: var(--cf-r-lg);
  background: var(--cf-primary-soft);
  border: 1px solid color-mix(in srgb, var(--cf-primary) 34%, transparent);
}

.cf-reorder-bar__text {
  min-width: 0;
}

.cf-reorder-bar__title {
  font-size: 13px;
  font-weight: 650;
  color: var(--cf-fg);
}

.cf-reorder-bar__hint {
  font-size: 11.5px;
  color: var(--cf-fg-2);
  margin-top: 1px;
}

.cf-reorder-bar__actions {
  margin-left: auto;
  display: flex;
  gap: var(--cf-sp-2);
  flex: 0 0 auto;
}

.cf-sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
}

@media (max-width: 640px) {
  .cf-reorder-bar__hint {
    display: none;
  }
}
</style>
