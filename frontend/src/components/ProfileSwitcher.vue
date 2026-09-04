<template>
  <div class="profile-switcher">
    <el-icon class="profile-icon"><Collection /></el-icon>
    <el-select
      v-model="selectedProfileId"
      size="small"
      class="profile-select"
      :loading="loading"
      aria-label="当前配置 Profile"
      popper-class="profile-select-popper"
      @change="handleChange"
    >
      <el-option
        v-for="profile in profiles"
        :key="profile.id"
        :label="profile.name"
        :value="profile.id"
      >
        <div class="profile-option">
          <span class="profile-option-name">{{ profile.name }}</span>
          <span class="profile-option-id">{{ profile.id }}</span>
        </div>
      </el-option>
    </el-select>
    <el-button
      text
      class="profile-manager-button"
      title="管理配置 Profile"
      aria-label="管理配置 Profile"
      @click="router.push('/profiles')"
    >
      <el-icon><Setting /></el-icon>
    </el-button>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { ElMessageBox } from 'element-plus'
import { useRouter } from 'vue-router'
import { useProfileStore } from '@/stores/profile'

const router = useRouter()
const profileStore = useProfileStore()
const { profiles, loading, activeProfileId, refreshProfiles, switchProfile } = profileStore
const selectedProfileId = ref(activeProfileId.value)

watch(activeProfileId, value => {
  selectedProfileId.value = value
})

const handleChange = async (profileId: string) => {
  if (profileId === activeProfileId.value) return
  try {
    await ElMessageBox.confirm(
      '切换后当前页面将重新加载，未保存的编辑内容会丢失。继续吗？',
      '切换配置 Profile',
      { confirmButtonText: '切换', cancelButtonText: '取消', type: 'warning' }
    )
    switchProfile(profileId)
    window.location.reload()
  } catch {
    selectedProfileId.value = activeProfileId.value
  }
}

onMounted(() => {
  refreshProfiles().catch(() => undefined)
})
</script>

<style scoped>
.profile-switcher {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
  padding: 4px 6px 4px 10px;
  border-radius: 10px;
  background: rgba(107, 115, 255, 0.05);
  transition: background 0.3s ease;
}

.profile-switcher:hover {
  background: rgba(107, 115, 255, 0.1);
}

.profile-icon {
  color: var(--cf-primary);
  font-size: 16px;
  flex-shrink: 0;
}

.profile-select {
  width: 150px;
}

.profile-select :deep(.el-select__wrapper),
.profile-select :deep(.el-input__wrapper) {
  background: transparent;
  box-shadow: none !important;
  padding-left: 6px;
  padding-right: 6px;
  min-height: 28px;
}

.profile-select :deep(.el-select__placeholder),
.profile-select :deep(.el-input__inner) {
  color: var(--cf-primary);
  font-weight: 600;
  font-size: 13px;
}

.profile-manager-button {
  color: var(--cf-primary);
  padding: 4px;
  height: 28px;
  border-radius: 8px;
  transition: all 0.3s ease;
}

.profile-manager-button:hover {
  color: var(--cf-primary);
  background: rgba(107, 115, 255, 0.14);
}

@media (max-width: 768px) {
  .profile-switcher {
    padding: 4px;
    gap: 2px;
  }

  .profile-icon {
    display: none;
  }

  .profile-select {
    width: 112px;
  }
}
/* 移动端触控尺寸：28px 在触屏上过小 */
@media (max-width: 900px) {
  .profile-select :deep(.el-select__wrapper) {
    min-height: 32px;
  }

  .profile-manager-button {
    height: 32px;
    min-width: 32px;
  }
}
</style>

<style>
.profile-select-popper {
  border-radius: 14px !important;
  border: 1px solid rgba(107, 115, 255, 0.15) !important;
  box-shadow: 0 12px 32px rgba(65, 80, 180, 0.16) !important;
}

.profile-select-popper .el-select-dropdown__item {
  height: auto;
  padding: 8px 16px;
  border-radius: 10px;
  margin: 2px 6px;
}

.profile-select-popper .el-select-dropdown__item.is-selected {
  background: rgba(107, 115, 255, 0.1);
}

.profile-select-popper .profile-option {
  display: flex;
  flex-direction: column;
  line-height: 1.35;
}

.profile-select-popper .profile-option-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--cf-fg);
}

.profile-select-popper .el-select-dropdown__item.is-selected .profile-option-name {
  color: var(--cf-primary);
}

.profile-select-popper .profile-option-id {
  font-size: 11px;
  color: var(--cf-fg-2);
  font-family: 'SFMono-Regular', Menlo, Consolas, monospace;
}
</style>
