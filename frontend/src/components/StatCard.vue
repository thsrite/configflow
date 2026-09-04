<template>
  <component
    :is="route ? 'button' : 'div'"
    class="cf-stat"
    :class="{ 'is-link': !!route }"
    :type="route ? 'button' : undefined"
    @click="handleClick"
  >
    <span class="cf-stat__edge" :class="`is-${scope}`" aria-hidden="true"></span>
    <span class="cf-stat__label">{{ label }}</span>
    <span class="cf-stat__value cf-num">{{ animatedValue }}</span>
    <span class="cf-stat__hint">
      <template v-if="change !== undefined">
        <em :class="changeType">{{ changeText }}</em>
        <template v-if="hint"> · {{ hint }}</template>
      </template>
      <template v-else>{{ hint || ' ' }}</template>
    </span>
  </component>
</template>

<script setup lang="ts">
import { computed, ref, watch, onMounted } from 'vue'
import { useRouter } from 'vue-router'

interface Props {
  label: string
  value: number
  change?: number
  /** 作用域色标：资源 / 当前配置 / 系统 */
  scope?: 'resource' | 'profile' | 'system'
  /** 数值下方的一行上下文，例如「7 正常 · 1 异常」 */
  hint?: string
  route?: string
}

const props = withDefaults(defineProps<Props>(), {
  change: undefined,
  scope: 'system',
  hint: undefined,
  route: undefined
})

const router = useRouter()

const handleClick = () => {
  if (props.route) router.push(props.route)
}

/* 数字滚动：尊重 prefers-reduced-motion，关闭动效时直接落终值 */
const animatedValue = ref(props.value)

const animateValue = () => {
  const reduced =
    typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
  if (reduced) {
    animatedValue.value = props.value
    return
  }

  const duration = 700
  const start = animatedValue.value
  const end = props.value
  const startTime = performance.now()

  const step = (now: number) => {
    const progress = Math.min((now - startTime) / duration, 1)
    const eased = 1 - Math.pow(1 - progress, 3)
    animatedValue.value = Math.round(start + (end - start) * eased)
    if (progress < 1) requestAnimationFrame(step)
  }

  requestAnimationFrame(step)
}

onMounted(animateValue)
watch(() => props.value, animateValue)

const changeType = computed(() => {
  if (props.change === undefined) return ''
  return props.change > 0 ? 'is-up' : props.change < 0 ? 'is-down' : 'is-flat'
})

const changeText = computed(() => {
  if (props.change === undefined) return ''
  if (props.change === 0) return '本周无变化'
  return `${props.change > 0 ? '+' : ''}${props.change} 本周`
})
</script>

<style scoped>
.cf-stat {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 14px 16px 13px;
  background: var(--cf-s1);
  border: 1px solid var(--cf-bd);
  border-radius: var(--cf-r-xl);
  box-shadow: var(--cf-shadow);
  overflow: hidden;
  text-align: left;
  font-family: inherit;
  color: inherit;
  width: 100%;
  transition: border-color var(--cf-dur) var(--cf-ease),
    transform var(--cf-dur) var(--cf-ease);
}

.cf-stat.is-link {
  cursor: pointer;
}

.cf-stat.is-link:hover {
  border-color: var(--cf-bd-strong);
  transform: translateY(-1px);
}

/* 3px 左色标标示资源作用域，配合分组标题使用，不作为唯一信息载体 */
.cf-stat__edge {
  position: absolute;
  inset: 0 auto 0 0;
  width: 3px;
}
.cf-stat__edge.is-resource {
  background: var(--cf-shared);
}
.cf-stat__edge.is-profile {
  background: var(--cf-profile);
}
.cf-stat__edge.is-system {
  background: var(--cf-success);
}

.cf-stat__label {
  font-size: 11.5px;
  font-weight: 550;
  color: var(--cf-fg-2);
}

.cf-stat__value {
  font-size: 29px;
  font-weight: 700;
  letter-spacing: -0.03em;
  line-height: 1.05;
  color: var(--cf-fg);
}

.cf-stat__hint {
  font-size: 11.5px;
  color: var(--cf-fg-3);
}

.cf-stat__hint em {
  font-style: normal;
  font-weight: 600;
}
.cf-stat__hint em.is-up {
  color: var(--cf-success);
}
.cf-stat__hint em.is-down {
  color: var(--cf-danger);
}
.cf-stat__hint em.is-flat {
  color: var(--cf-fg-2);
}
</style>
