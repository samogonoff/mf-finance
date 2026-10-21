<!-- Согласующие списка (этап 3 / 4) и кто уже согласовал: ✓ — согласовал, ○ — ждём; «вы» — текущий пользователь. -->
<template>
  <span class="chips">
    <span v-if="scope.no_approvers" class="chip chip-none">не назначены</span>
    <span
      v-for="a in scope.approvers"
      :key="a.user_id"
      class="chip"
      :class="{ 'chip-ok': a.approved }"
      :title="a.approved ? (a.comment ? `Согласовано: ${a.comment}` : 'Согласовано') : 'Ещё не согласовал(а)'"
    >
      {{ a.approved ? "✓" : "○" }} {{ a.user_name || `#${a.user_id}` }}<template v-if="a.mine"> (вы)</template>
    </span>
  </span>
</template>

<script setup lang="ts">
import type { LeScope } from "~/composables/useLeSvod";

defineProps<{ scope: LeScope }>();
</script>

<style scoped>
.chips { display: inline-flex; flex-wrap: wrap; gap: var(--sp-1); }
.chip { padding: 2px var(--sp-2); border-radius: var(--rd-4, 6px); background: var(--bg-surface-3); color: var(--text-secondary); font-size: var(--fs-sm); white-space: nowrap; }
.chip-ok { background: var(--pos-soft); color: var(--pos-strong); }
.chip-none { background: var(--neg-soft); color: var(--neg-strong); }
</style>
