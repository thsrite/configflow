<template>
  <div class="activity-timeline">
    <h3 class="timeline-title">🕐 最近活动</h3>

    <div class="timeline-items" v-if="activities.length > 0">
      <div
        v-for="(activity, index) in activities"
        :key="index"
        class="timeline-item"
      >
        <div class="timeline-dot" :style="{ background: activity.color }">
          <span class="activity-icon">{{ activity.icon }}</span>
        </div>
        <div class="timeline-content">
          <div class="activity-header">
            <span class="activity-action">{{ activity.action }}</span>
            <span class="activity-time">{{ activity.timeDisplay }}</span>
          </div>
          <div class="activity-target">{{ activity.target }}</div>
        </div>
      </div>
    </div>

    <div class="empty-state" v-else>
      <el-empty description="暂无活动记录" />
    </div>
  </div>
</template>

<script setup lang="ts">
interface Activity {
  time: string
  timeDisplay: string
  action: string
  target: string
  icon: string
  color: string
}

interface Props {
  activities: Activity[]
}

defineProps<Props>()
</script>

<style scoped>
.activity-timeline {
  width: 100%;
  height: 100%;
}

.timeline-title {
  font-size: 18px;
  font-weight: 600;
  color: var(--cf-s1);
  margin: 0 0 24px 0;
}

.timeline-items {
  display: flex;
  flex-direction: column;
  gap: 4px;
  max-height: 400px;
  overflow-y: auto;
  padding-right: 8px;
}

/* 滚动条样式 */
.timeline-items::-webkit-scrollbar {
  width: 4px;
}

.timeline-items::-webkit-scrollbar-track {
  background: rgba(255, 255, 255, 0.05);
  border-radius: 2px;
}

.timeline-items::-webkit-scrollbar-thumb {
  background: rgba(255, 255, 255, 0.2);
  border-radius: 2px;
}

.timeline-items::-webkit-scrollbar-thumb:hover {
  background: rgba(255, 255, 255, 0.3);
}

.timeline-item {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  padding: 12px;
  border-radius: 12px;
  transition: all 0.3s ease;
}

.timeline-item:hover {
  background: rgba(255, 255, 255, 0.05);
}

.timeline-dot {
  flex-shrink: 0;
  width: 36px;
  height: 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: rgba(107, 115, 255, 0.2);
  transition: all 0.3s ease;
}

.timeline-item:hover .timeline-dot {
  transform: scale(1.1);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
}

.activity-icon {
  font-size: 18px;
  filter: drop-shadow(0 2px 4px rgba(0, 0, 0, 0.3));
}

.timeline-content {
  flex: 1;
  min-width: 0;
}

.activity-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 4px;
}

.activity-action {
  font-size: 14px;
  font-weight: 600;
  color: var(--cf-s1);
}

.activity-time {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.5);
  flex-shrink: 0;
}

.activity-target {
  font-size: 13px;
  color: rgba(255, 255, 255, 0.7);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.empty-state {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
}

/* 响应式 */
@media (max-width: 768px) {
  .timeline-items {
    max-height: 300px;
  }

  .timeline-dot {
    width: 32px;
    height: 32px;
  }

  .activity-icon {
    font-size: 16px;
  }

  .activity-action {
    font-size: 13px;
  }

  .activity-time {
    font-size: 11px;
  }

  .activity-target {
    font-size: 12px;
  }
}
</style>
