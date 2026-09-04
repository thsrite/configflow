<template>
  <div class="profiles-page">
    <PageHeader title="配置空间" description="新建、克隆、导入导出与切换配置空间">
      <template #actions>
        <el-button @click="pickImportFile">
          <el-icon><Upload /></el-icon>
          导入到当前配置空间
        </el-button>
        <el-button type="primary" @click="openCreate">
          <el-icon><Plus /></el-icon>
          新建配置空间
        </el-button>
        <input ref="importInput" type="file" accept="application/json" hidden @change="importProfile" />
      </template>
    </PageHeader>

    <div v-loading="loading" class="profiles-grid">
      <div
        v-for="profile in profiles"
        :key="profile.id"
        class="profile-card"
        :class="{ 'is-active': profile.id === activeProfileId }"
      >
        <div class="card-header">
          <div class="card-title-group">
            <div class="profile-avatar">
              <el-icon><Collection /></el-icon>
            </div>
            <div class="card-title-text">
              <div class="card-title">{{ profile.name }}</div>
              <div class="card-id">{{ profile.id }}</div>
            </div>
          </div>
          <span v-if="profile.id === activeProfileId" class="active-pill">
            <el-icon><CircleCheck /></el-icon>
            使用中
          </span>
        </div>

        <p class="card-desc">{{ profile.description || '暂无说明' }}</p>

        <div class="card-actions">
          <el-button
            v-if="profile.id !== activeProfileId"
            class="card-btn primary"
            @click="activate(profile.id)"
          >
            <el-icon><CircleCheck /></el-icon>
            使用
          </el-button>
          <span v-else class="current-hint">当前正在使用</span>
          <div class="icon-actions">
            <el-button class="icon-btn" title="编辑" @click="openEdit(profile)">
              <el-icon><Edit /></el-icon>
            </el-button>
            <el-button class="icon-btn" title="克隆" @click="clone(profile)">
              <el-icon><CopyDocument /></el-icon>
            </el-button>
            <el-button class="icon-btn" title="导出" @click="exportProfile(profile.id)">
              <el-icon><Download /></el-icon>
            </el-button>
            <el-button
              v-if="profile.id !== 'default'"
              class="icon-btn danger"
              title="删除"
              @click="remove(profile)"
            >
              <el-icon><Delete /></el-icon>
            </el-button>
          </div>
        </div>
      </div>

      <div v-if="!loading && profiles.length === 0" class="empty-state">
        <el-icon><Collection /></el-icon>
        <p>还没有配置空间，点击右上角新建一个吧</p>
      </div>
    </div>

    <el-dialog
      v-model="dialogVisible"
      :title="editingId ? '编辑配置空间' : '新建配置空间'"
      width="460px"
      class="profile-dialog"
    >
      <el-form label-width="82px" @submit.prevent="submit">
        <el-form-item label="标识 ID">
          <el-input v-model="form.id" :disabled="Boolean(editingId)" maxlength="64" />
        </el-form-item>
        <el-form-item label="名称">
          <el-input v-model="form.name" maxlength="120" />
        </el-form-item>
        <el-form-item label="说明">
          <el-input v-model="form.description" type="textarea" :rows="3" maxlength="500" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import PageHeader from '@/components/shell/PageHeader.vue'
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CircleCheck, Collection, CopyDocument, Delete, Download, Edit, Plus, Upload } from '@element-plus/icons-vue'
import { profileApi } from '@/api'
import { useProfileStore, type Profile } from '@/stores/profile'

const profileStore = useProfileStore()
const { profiles, loading, activeProfileId, refreshProfiles, switchProfile } = profileStore
const dialogVisible = ref(false)
const saving = ref(false)
const editingId = ref('')
const importInput = ref<HTMLInputElement>()
const form = reactive({ id: '', name: '', description: '' })

const openCreate = () => {
  editingId.value = ''
  Object.assign(form, { id: '', name: '', description: '' })
  dialogVisible.value = true
}

const openEdit = (profile: Profile) => {
  editingId.value = profile.id
  Object.assign(form, {
    id: profile.id,
    name: profile.name,
    description: profile.description || '',
  })
  dialogVisible.value = true
}

const submit = async () => {
  if (!form.name.trim() || (!editingId.value && !form.id.trim())) {
    ElMessage.warning('请填写标识 ID 和名称')
    return
  }
  saving.value = true
  try {
    if (editingId.value) {
      await profileApi.update(editingId.value, { name: form.name, description: form.description })
    } else {
      await profileApi.create({ id: form.id.trim(), name: form.name, description: form.description })
    }
    await refreshProfiles()
    dialogVisible.value = false
    ElMessage.success('配置空间已保存')
  } catch (error: any) {
    ElMessage.error(error.response?.data?.message || '配置空间 保存失败')
  } finally {
    saving.value = false
  }
}

const activate = async (profileId: string) => {
  try {
    await profileApi.activate(profileId)
    switchProfile(profileId)
    window.location.reload()
  } catch (error: any) {
    ElMessage.error(error.response?.data?.message || '切换 配置空间 失败')
  }
}

const clone = async (profile: Profile) => {
  try {
    const { value } = await ElMessageBox.prompt('输入新 标识 ID', `克隆 ${profile.name}`, {
      confirmButtonText: '克隆',
      cancelButtonText: '取消',
      inputValue: `${profile.id}-copy`,
      inputPattern: /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/,
      inputErrorMessage: 'ID 只能包含字母、数字、下划线和短横线',
    })
    await profileApi.clone(profile.id, { id: value, name: `${profile.name} 副本` })
    await refresh配置空间s()
    ElMessage.success('配置空间已克隆')
  } catch (error: any) {
    if (error !== 'cancel') ElMessage.error(error.response?.data?.message || '配置空间 克隆失败')
  }
}

const remove = async (profile: Profile) => {
  try {
    await ElMessageBox.confirm(`确定删除配置空间「${profile.name}」及其缓存和生成文件吗？`, '删除配置空间', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'warning',
    })
    await profileApi.delete(profile.id)
    await refreshProfiles()
    ElMessage.success('配置空间 已删除')
  } catch (error: any) {
    if (error !== 'cancel') ElMessage.error(error.response?.data?.message || '配置空间 删除失败')
  }
}

const exportProfile = async (profileId: string) => {
  try {
    const response = await profileApi.export(profileId)
    const url = URL.createObjectURL(new Blob([response.data], { type: 'application/json' }))
    const link = document.createElement('a')
    link.href = url
    link.download = `${profileId}.json`
    link.click()
    URL.revokeObjectURL(url)
  } catch (error: any) {
    ElMessage.error(error.response?.data?.message || '配置空间 导出失败')
  }
}

const pickImportFile = () => importInput.value?.click()

const importProfile = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  try {
    const data = JSON.parse(await file.text())
    await profileApi.import(activeProfileId.value, data)
    await refreshProfiles()
    ElMessage.success('配置已导入当前 配置空间')
  } catch (error: any) {
    ElMessage.error(error.response?.data?.message || '配置空间 导入失败')
  } finally {
    input.value = ''
  }
}

onMounted(() => {
  refreshProfiles().catch(() => ElMessage.error('加载配置空间失败'))
})
</script>

<style scoped>
.profiles-page {
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 16px;
  position: sticky;
  top: 0;
  z-index: 100;
  background: var(--cf-bg);
  margin: -28px -32px 28px -32px;
  padding: 28px 32px;
}

.title-block h2 {
  margin: 0;
  font-size: 26px;
  font-weight: 700;
  color: var(--cf-fg);
}

.title-block p {
  margin: 6px 0 0;
  font-size: 14px;
  color: var(--cf-fg-2);
}

.header-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  justify-content: flex-end;
}

.profiles-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 24px;
  min-height: 120px;
}

.profile-card {
  background: var(--cf-s1);
  border-radius: 24px;
  padding: 24px;
  box-shadow: 0 8px 24px rgba(65, 80, 180, 0.08);
  border: 1px solid rgba(107, 115, 255, 0.1);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.profile-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 22px 48px rgba(91, 112, 255, 0.2);
  border-color: rgba(107, 115, 255, 0.25);
}

.profile-card.is-active {
  border-color: rgba(107, 115, 255, 0.45);
  box-shadow: 0 14px 36px rgba(91, 112, 255, 0.18);
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.card-title-group {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 1;
  min-width: 0;
}

.profile-avatar {
  width: 42px;
  height: 42px;
  flex-shrink: 0;
  border-radius: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--cf-s2);
  color: var(--cf-primary);
  font-size: 20px;
}

.card-title-text {
  min-width: 0;
}

.card-title {
  font-size: 17px;
  font-weight: 700;
  color: var(--cf-fg);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-id {
  margin-top: 2px;
  font-size: 12px;
  color: var(--cf-fg-2);
  font-family: 'SFMono-Regular', Menlo, Consolas, monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.active-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  flex-shrink: 0;
  padding: 6px 12px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  background: rgba(107, 115, 255, 0.12);
  color: var(--cf-primary);
  border: 1px solid rgba(107, 115, 255, 0.18);
}

.card-desc {
  margin: 0;
  font-size: 13px;
  line-height: 1.6;
  color: var(--cf-fg-2);
  min-height: 42px;
}

.card-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-top: 16px;
  border-top: 1px dashed rgba(107, 115, 255, 0.16);
  margin-top: auto;
}

.card-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 34px;
  padding: 0 16px;
  margin: 0;
  border-radius: 12px;
  font-size: 13px;
  font-weight: 600;
  border: none;
  transition: all 0.2s ease;
}

.card-btn.primary {
  background: var(--cf-primary-fill);
  color: var(--cf-primary-fg);
}

.card-btn.primary:hover {
  color: var(--cf-s1);
  box-shadow: 0 8px 18px rgba(87, 104, 255, 0.28);
}

.current-hint {
  font-size: 13px;
  font-weight: 600;
  color: var(--cf-fg-2);
}

.icon-actions {
  display: flex;
  gap: 8px;
  margin-left: auto;
}

.icon-btn {
  width: 34px;
  height: 34px;
  padding: 0;
  margin: 0;
  border-radius: 12px;
  border: 1px solid rgba(107, 115, 255, 0.18);
  background: rgba(107, 115, 255, 0.06);
  color: var(--cf-primary);
  font-size: 15px;
  transition: all 0.2s ease;
}

.icon-btn:hover {
  background: rgba(107, 115, 255, 0.14);
  border-color: rgba(107, 115, 255, 0.32);
  color: var(--cf-primary);
  transform: translateY(-1px);
}

.icon-btn.danger {
  border-color: rgba(245, 108, 108, 0.22);
  background: rgba(245, 108, 108, 0.08);
  color: var(--cf-danger);
}

.icon-btn.danger:hover {
  background: rgba(245, 108, 108, 0.16);
  border-color: rgba(245, 108, 108, 0.35);
  color: var(--cf-danger);
}

.empty-state {
  grid-column: 1 / -1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 60px 20px;
  border-radius: 24px;
  background: var(--cf-s1);
  border: 1px dashed rgba(107, 115, 255, 0.24);
  color: var(--cf-fg-2);
}

.empty-state .el-icon {
  font-size: 40px;
  color: rgba(107, 115, 255, 0.4);
}

.empty-state p {
  margin: 0;
  font-size: 14px;
}

.profile-dialog :deep(.el-dialog) {
  border-radius: 20px;
}

@media (max-width: 720px) {
  .profiles-page {
    padding: 20px 16px 32px;
  }

  .page-header {
    margin: -20px -16px 20px -16px;
    padding: 20px 16px;
  }

  .header-actions {
    width: 100%;
    justify-content: flex-start;
  }

  .profiles-grid {
    grid-template-columns: 1fr;
    gap: 16px;
  }
}
</style>
