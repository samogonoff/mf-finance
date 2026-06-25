<template>
  <span v-if="manual" class="cell-comment" :title="title">
    <Icon name="lucide:pencil" class="cell-comment-icon" />
  </span>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    manual?: boolean;
    title?: string;
  }>(),
  { manual: false, title: "Ручная корректировка" }
);
</script>

<style scoped>
.cell-comment {
  display: inline-flex;
  align-items: center;
  color: var(--accent, #4338ca);
  margin-left: 4px;
}
.cell-comment-icon {
  width: 12px;
  height: 12px;
}
</style>
