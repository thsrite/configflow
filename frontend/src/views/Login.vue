<template>
  <div class="relative flex min-h-screen min-h-dvh items-center justify-center overflow-hidden bg-background px-5">
    <div class="tech-backdrop" aria-hidden="true" />

    <Motion
      v-bind="riseIn"
      class="hairline edge-light relative z-10 w-full max-w-[400px] overflow-hidden rounded-2xl border border-border/35 bg-card/70 p-8 backdrop-blur-2xl max-[480px]:p-6"
      style="box-shadow: var(--shadow-overlay)"
    >
      <div class="flex flex-col items-center gap-3 pb-7 text-center">
        <span class="relative flex size-14 items-center justify-center">
          <span
            class="absolute inset-0 rounded-2xl bg-linear-to-br from-primary/60 to-accent-2-fill/45 blur-lg"
            aria-hidden="true"
          />
          <img src="/icon.png" alt="" class="relative size-12 rounded-[14px]" />
        </span>
        <div>
          <h1 class="m-0 text-[26px] leading-tight font-semibold tracking-[-0.03em] text-foreground">
            ConfigFlow
          </h1>
          <p class="mt-1.5 mb-0 text-[13px] text-muted-foreground">代理配置管理系统</p>
        </div>
      </div>

      <form class="flex flex-col gap-4" @submit.prevent="handleLogin">
        <div class="flex flex-col gap-1.5">
          <Label for="login-username" class="text-[12.5px] text-muted-foreground">用户名</Label>
          <div class="relative">
            <User
              class="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
              aria-hidden="true"
            />
            <Input
              id="login-username"
              v-model="loginForm.username"
              autocomplete="username"
              placeholder="请输入用户名"
              class="h-11 bg-background/50 pl-9 text-[14px]"
              :disabled="loading"
              :aria-invalid="Boolean(errors.username)"
            />
          </div>
          <p v-if="errors.username" class="m-0 text-[12px] text-destructive-accent">
            {{ errors.username }}
          </p>
        </div>

        <div class="flex flex-col gap-1.5">
          <Label for="login-password" class="text-[12.5px] text-muted-foreground">密码</Label>
          <div class="relative">
            <Lock
              class="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
              aria-hidden="true"
            />
            <Input
              id="login-password"
              v-model="loginForm.password"
              type="password"
              autocomplete="current-password"
              placeholder="请输入密码"
              class="h-11 bg-background/50 pl-9 text-[14px]"
              :disabled="loading"
              :aria-invalid="Boolean(errors.password)"
            />
          </div>
          <p v-if="errors.password" class="m-0 text-[12px] text-destructive-accent">
            {{ errors.password }}
          </p>
        </div>

        <Button type="submit" class="mt-2 h-11 w-full text-[14px] shadow-glow" :disabled="loading">
          <Loader2 v-if="loading" class="size-4 animate-spin" aria-hidden="true" />
          {{ loading ? '登录中…' : '登录' }}
        </Button>
      </form>
    </Motion>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { Motion } from 'motion-v'
import { Loader2, Lock, User } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { notify } from '@/lib/feedback'
import { riseIn } from '@/lib/motion'

const router = useRouter()
const loading = ref(false)

const loginForm = reactive({ username: '', password: '' })
const errors = reactive({ username: '', password: '' })

/* 表单校验就地实现：只有两个必填项，引入表单库不划算 */
const validate = (): boolean => {
  errors.username = loginForm.username.trim() ? '' : '请输入用户名'
  errors.password = loginForm.password ? '' : '请输入密码'
  return !errors.username && !errors.password
}

const handleLogin = async () => {
  if (!validate()) return

  loading.value = true
  try {
    const response = await axios.post('/api/auth/login', {
      username: loginForm.username,
      password: loginForm.password
    })

    if (response.data.success) {
      localStorage.setItem('token', response.data.token)
      localStorage.setItem('username', response.data.username)
      notify.success('登录成功')
      router.push('/')
    } else {
      notify.error(response.data.message || '登录失败')
    }
  } catch (error: any) {
    console.error('登录失败:', error)
    if (error.response?.data?.message) {
      notify.error(error.response.data.message)
    } else if (error.response?.status === 401) {
      notify.error('用户名或密码错误')
    } else {
      notify.error('登录失败，请稍后重试')
    }
  } finally {
    loading.value = false
  }
}
</script>
