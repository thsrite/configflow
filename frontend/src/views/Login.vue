<template>
  <div class="login-container">
    <el-card class="login-card">
      <template #header>
        <div class="login-header">
          <h2>Config Flow</h2>
          <p>配置管理系统</p>
        </div>
      </template>

      <el-form
        ref="loginFormRef"
        :model="loginForm"
        :rules="rules"
        label-width="0"
        @submit.prevent="handleLogin"
      >
        <el-form-item prop="username">
          <el-input
            v-model="loginForm.username"
            placeholder="用户名"
            size="large"
            prefix-icon="User"
            :disabled="loading"
          />
        </el-form-item>

        <el-form-item prop="password">
          <el-input
            v-model="loginForm.password"
            type="password"
            placeholder="密码"
            size="large"
            prefix-icon="Lock"
            :disabled="loading"
            @keyup.enter="handleLogin"
          />
        </el-form-item>

        <el-form-item>
          <el-button
            type="primary"
            size="large"
            style="width: 100%"
            :loading="loading"
            @click="handleLogin"
          >
            {{ loading ? '登录中...' : '登录' }}
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import axios from 'axios'

const router = useRouter()
const loginFormRef = ref()
const loading = ref(false)

const loginForm = reactive({
  username: '',
  password: ''
})

const rules = {
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' }
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' }
  ]
}

const handleLogin = async () => {
  try {
    // 验证表单
    await loginFormRef.value?.validate()

    loading.value = true

    // 发送登录请求
    const response = await axios.post('/api/auth/login', {
      username: loginForm.username,
      password: loginForm.password
    })

    if (response.data.success) {
      // 保存 token 到 localStorage
      localStorage.setItem('token', response.data.token)
      localStorage.setItem('username', response.data.username)

      ElMessage.success('登录成功')

      // 跳转到首页
      router.push('/')
    } else {
      ElMessage.error(response.data.message || '登录失败')
    }
  } catch (error: any) {
    console.error('登录失败:', error)
    if (error.response?.data?.message) {
      ElMessage.error(error.response.data.message)
    } else if (error.response?.status === 401) {
      ElMessage.error('用户名或密码错误')
    } else {
      ElMessage.error('登录失败，请稍后重试')
    }
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-container {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 100vh;
  background: var(--cf-s2);
  padding: 20px;
  position: relative;
  overflow: hidden;
}

.login-container::before {
  content: '';
  position: absolute;
  top: -50%;
  right: -50%;
  width: 100%;
  height: 100%;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.1) 0%, transparent 70%);
  animation: float 20s ease-in-out infinite;
}

.login-container::after {
  content: '';
  position: absolute;
  bottom: -50%;
  left: -50%;
  width: 100%;
  height: 100%;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.08) 0%, transparent 70%);
  animation: float 25s ease-in-out infinite reverse;
}

@keyframes float {
  0%, 100% {
    transform: translate(0, 0);
  }
  50% {
    transform: translate(-20px, 20px);
  }
}

.login-card {
  width: 100%;
  max-width: 420px;
  border-radius: 24px;
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.24), 0 0 0 1px rgba(255, 255, 255, 0.1) inset;
  backdrop-filter: blur(20px);
  background: rgba(255, 255, 255, 0.98);
  position: relative;
  z-index: 1;
  overflow: hidden;
}

.login-card::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 4px;
  background: var(--cf-s2);
}

.login-header {
  text-align: center;
  padding: 12px 0;
}

.login-header h2 {
  margin: 0 0 12px 0;
  font-size: 32px;
  color: var(--cf-fg);
  font-weight: 700;
  letter-spacing: -0.5px;
}

.login-header p {
  margin: 0;
  color: var(--cf-fg-2);
  font-size: 15px;
  font-weight: 500;
}

:deep(.el-card__header) {
  padding: 32px 32px 20px;
  border-bottom: none;
  background: transparent;
}

:deep(.el-card__body) {
  padding: 12px 32px 36px;
}

:deep(.el-form-item) {
  margin-bottom: 24px;
}

:deep(.el-input__wrapper) {
  padding: 14px 16px;
  background: var(--cf-s2);
  border: 2px solid transparent;
  border-radius: 14px;
  box-shadow: none;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

:deep(.el-input__wrapper:hover) {
  background: var(--cf-s3);
  border-color: rgba(139, 143, 255, 0.25);
}

:deep(.el-input__wrapper.is-focus) {
  background: var(--cf-s1);
  border-color: var(--cf-primary-hover);
  box-shadow: 0 0 0 4px rgba(139, 143, 255, 0.15);
}

:deep(.el-input__inner) {
  font-size: 15px;
  font-weight: 500;
  color: var(--cf-fg);
}

:deep(.el-input__inner::placeholder) {
  color: var(--cf-fg-3);
  font-weight: 400;
}

:deep(.el-input__prefix) {
  font-size: 18px;
  color: var(--cf-fg-2);
}

:deep(.el-input__wrapper.is-focus .el-input__prefix) {
  color: var(--cf-primary-hover);
}

:deep(.el-button--primary) {
  padding: 16px 24px;
  height: auto;
  background: var(--cf-s2);
  border: none;
  border-radius: 14px;
  font-weight: 600;
  font-size: 16px;
  letter-spacing: 0.3px;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  box-shadow: 0 8px 24px rgba(139, 143, 255, 0.4);
  position: relative;
  overflow: hidden;
}

:deep(.el-button--primary::before) {
  content: '';
  position: absolute;
  top: 0;
  left: -100%;
  width: 100%;
  height: 100%;
  background: var(--cf-s2);
  transition: left 0.5s;
}

:deep(.el-button--primary:hover) {
  transform: translateY(-3px);
  box-shadow: 0 12px 32px rgba(139, 143, 255, 0.5);
}

:deep(.el-button--primary:hover::before) {
  left: 100%;
}

:deep(.el-button--primary:active) {
  transform: translateY(-1px);
  box-shadow: 0 6px 20px rgba(139, 143, 255, 0.45);
}

:deep(.el-button--primary.is-loading) {
  opacity: 0.8;
}

/* 移动端适配 */
@media (max-width: 480px) {
  .login-card {
    max-width: 100%;
    border-radius: 20px;
    box-shadow: 0 20px 48px rgba(0, 0, 0, 0.2);
  }

  :deep(.el-card__header) {
    padding: 28px 24px 16px;
  }

  :deep(.el-card__body) {
    padding: 12px 24px 32px;
  }

  .login-header h2 {
    font-size: 28px;
  }

  .login-header p {
    font-size: 14px;
  }

  :deep(.el-input__wrapper) {
    padding: 12px 14px;
  }

  :deep(.el-button--primary) {
    padding: 14px 20px;
    font-size: 15px;
  }
}
</style>
