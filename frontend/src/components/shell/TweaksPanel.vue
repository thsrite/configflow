<template>
  <Transition
    enter-active-class="transition duration-350 ease-(--ease-flow)"
    enter-from-class="opacity-0 translate-y-2.5"
    leave-active-class="transition duration-200"
    leave-to-class="opacity-0 translate-y-2"
  >
    <section
      v-if="open"
      class="fixed right-5 bottom-5 z-40 w-[280px] rounded-[18px] border border-border-strong bg-card/92 p-4 shadow-overlay backdrop-blur-xl max-[900px]:right-4 max-[900px]:bottom-[calc(env(safe-area-inset-bottom)+90px)] max-[900px]:left-4 max-[900px]:w-auto"
      role="dialog"
      aria-label="Tweaks"
    >
      <h2 class="font-display mb-3 flex items-center text-[18px]">
        Tweaks
        <Button variant="ghost" size="icon-sm" class="ml-auto size-7" aria-label="关闭" @click="emit('close')">
          <X class="size-4" />
        </Button>
      </h2>

      <div class="grid gap-3.5">
        <div class="grid gap-1.5">
          <span class="font-mono text-[11.5px] tracking-[0.06em] text-muted-foreground">主题</span>
          <Segmented
            block
            label="主题"
            :model-value="preference"
            :options="[{ value: 'system', label: '跟随系统' }, { value: 'dark', label: '炭黑' }, { value: 'light', label: '象牙' }]"
            @update:model-value="setTheme"
          />
        </div>

        <div class="grid gap-1.5">
          <span class="font-mono text-[11.5px] tracking-[0.06em] text-muted-foreground">强调色</span>
          <div class="flex gap-2" role="radiogroup" aria-label="强调色">
            <button
              v-for="swatch in SWATCHES"
              :key="swatch.value"
              type="button"
              role="radio"
              :aria-checked="prefs.accent === swatch.value"
              :aria-label="swatch.label"
              :title="swatch.label"
              class="size-[30px] cursor-pointer rounded-full transition-shadow duration-200 focus-visible:outline-none"
              :style="{
                background: swatch.color,
                boxShadow: `0 0 0 2px var(--card), 0 0 0 ${prefs.accent === swatch.value ? 4 : 3}px ${prefs.accent === swatch.value ? swatch.color : 'transparent'}`
              }"
              @click="prefs.accent = swatch.value"
            />
          </div>
        </div>

        <div class="grid gap-1.5">
          <label for="tweak-density" class="flex font-mono text-[11.5px] tracking-[0.06em] text-muted-foreground">
            流量密度
            <output class="ml-auto text-foreground/80">{{ prefs.density.toFixed(1) }}×</output>
          </label>
          <input
            id="tweak-density"
            v-model.number="prefs.density"
            type="range"
            min="0.2"
            max="3"
            step="0.1"
            class="w-full accent-(--primary)"
          />
        </div>

        <div class="grid gap-1.5">
          <span class="font-mono text-[11.5px] tracking-[0.06em] text-muted-foreground">动效</span>
          <Segmented
            block
            label="动效"
            :model-value="prefs.motion ? 'on' : 'off'"
            :options="[{ value: 'on', label: '流动' }, { value: 'off', label: '静止' }]"
            @update:model-value="v => (prefs.motion = v === 'on')"
          />
        </div>

        <div class="grid gap-1.5">
          <span class="font-mono text-[11.5px] tracking-[0.06em] text-muted-foreground">纸感纹理</span>
          <Segmented
            block
            label="纸感纹理"
            :model-value="prefs.texture ? 'on' : 'off'"
            :options="[{ value: 'on', label: '开启' }, { value: 'off', label: '关闭' }]"
            @update:model-value="v => (prefs.texture = v === 'on')"
          />
        </div>
      </div>
    </section>
  </Transition>
</template>

<script setup lang="ts">
import { X } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import Segmented from '@/components/common/Segmented.vue'
import { usePreferences, type AccentName } from '@/stores/preferences'
import { useThemeStore, type ThemePreference } from '@/stores/theme'

defineProps<{ open: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const { prefs } = usePreferences()
const { preference, setTheme: applyTheme } = useThemeStore()
const setTheme = (mode: string) => applyTheme(mode as ThemePreference)

/* 色板只是预览色块，真实取值在 theme.css 的 data-accent 变体里 */
const SWATCHES: Array<{ value: AccentName; label: string; color: string }> = [
  { value: 'clay', label: '陶土', color: '#d97757' },
  { value: 'kraft', label: '牛皮纸', color: '#d4a27f' },
  { value: 'sky', label: '天空', color: '#6a9bcc' }
]
</script>
