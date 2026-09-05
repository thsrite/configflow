<template>
  <Dialog :open="promptState.open" @update:open="value => !value && settlePrompt(null)">
    <DialogContent class="glass-strong hairline max-w-[420px] border-border/50">
      <DialogHeader>
        <DialogTitle>{{ promptState.title }}</DialogTitle>
        <DialogDescription v-if="promptState.description">
          {{ promptState.description }}
        </DialogDescription>
      </DialogHeader>

      <div class="flex flex-col gap-1.5">
        <Input
          v-model="promptState.value"
          :placeholder="promptState.placeholder"
          class="bg-background/50"
          :aria-invalid="Boolean(promptState.error)"
          autofocus
          @keyup.enter="settlePrompt(promptState.value)"
        />
        <p v-if="promptState.error" class="m-0 text-[12px] text-destructive-accent">
          {{ promptState.error }}
        </p>
      </div>

      <DialogFooter>
        <Button variant="outline" @click="settlePrompt(null)">{{ promptState.cancelText }}</Button>
        <Button @click="settlePrompt(promptState.value)">{{ promptState.confirmText }}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { promptState, settlePrompt } from '@/lib/feedback'
</script>
