<template>
  <AlertDialog :open="confirmState.open" @update:open="value => !value && settleConfirm('cancel')">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{ confirmState.title }}</AlertDialogTitle>
        <AlertDialogDescription class="whitespace-pre-line">
          {{ confirmState.description }}
        </AlertDialogDescription>
        <a
          v-if="confirmState.link"
          :href="confirmState.link.href"
          target="_blank"
          rel="noopener noreferrer"
          class="inline-flex min-h-11 items-center gap-1 self-center text-sm sm:self-start text-primary-accent underline-offset-4 hover:underline"
        >
          {{ confirmState.link.text }}<ExternalLink class="size-4" aria-hidden="true" />
        </a>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel @click="settleConfirm('cancel')">
          {{ confirmState.cancelText }}
        </AlertDialogCancel>
        <!-- 先在捕获阶段结算选择，避免 Reka 的自动关闭先将 Promise 结算为取消。
             第三个按钮只在调用方给了 altText 时出现。 -->
        <AlertDialogAction
          v-if="confirmState.altText"
          class="bg-secondary text-secondary-foreground hover:bg-secondary/80"
          @click.capture="settleConfirm('alt')"
        >
          {{ confirmState.altText }}
        </AlertDialogAction>
        <AlertDialogAction
          :class="confirmState.danger ? 'bg-destructive text-destructive-foreground hover:bg-destructive/90' : ''"
          @click.capture="settleConfirm('confirm')"
        >
          {{ confirmState.confirmText }}
        </AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>

<script setup lang="ts">
import { ExternalLink } from '@lucide/vue'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from '@/components/ui/alert-dialog'
import { confirmState, settleConfirm } from '@/lib/feedback'
</script>
