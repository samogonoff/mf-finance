<template>
  <div class="kpi">
    <div class="kpi-row">
      <div class="kpi-label">{{ label }}</div>
      <span v-if="badge" class="badge" :class="badgeClass">{{ badge }}</span>
    </div>

    <div class="kpi-value">
      <span>{{ formattedValue }}</span>
      <span v-if="unit" class="kpi-unit">{{ unit }}</span>
    </div>

    <div class="kpi-meta">
      <span v-if="deltaPct !== undefined" class="kpi-delta" :class="deltaDir">
        <Icon
          v-if="deltaDir === 'pos'"
          name="lucide:trending-up"
          class="kpi-delta-icon"
        />
        <Icon
          v-else-if="deltaDir === 'neg'"
          name="lucide:trending-down"
          class="kpi-delta-icon"
        />
        {{ formatPct(deltaPct) }}
      </span>
      <span v-if="caption" class="kpi-caption">{{ caption }}</span>
    </div>

    <Sparkline v-if="spark && spark.length" :values="spark" :trend="deltaDir" class="kpi-spark" />
  </div>
</template>

<script setup lang="ts">
import { money, num as nfmt, pct as pctFmt } from "~/utils/format";

const props = defineProps<{
  label: string;
  value: number | string;
  unit?: string;
  format?: "money" | "number" | "raw";
  currency?: "RUB" | "USD" | "EUR" | "BYN";
  compact?: boolean;
  deltaPct?: number;
  caption?: string;
  spark?: number[];
  badge?: string;
  badgeKind?: "pos" | "neg" | "warn" | "info" | "default";
}>();

const formattedValue = computed(() => {
  if (props.format === "raw" || typeof props.value === "string") return props.value as string;
  if (props.format === "money") {
    return money(props.value as number, {
      currency: props.currency || "RUB",
      compact: props.compact
    });
  }
  return nfmt(props.value as number);
});

const deltaDir = computed<"pos" | "neg" | "zero">(() => {
  if (props.deltaPct === undefined) return "zero";
  if (props.deltaPct > 0) return "pos";
  if (props.deltaPct < 0) return "neg";
  return "zero";
});

const badgeClass = computed(() => {
  switch (props.badgeKind) {
    case "pos": return "badge-pos";
    case "neg": return "badge-neg";
    case "warn": return "badge-warn";
    case "info": return "badge-info";
    default: return "";
  }
});

const formatPct = (v: number) => pctFmt(v, 1);
</script>

<style scoped>
.kpi-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-3);
}
.kpi-caption {
  color: var(--text-muted);
  font-size: var(--fs-xs);
}
.kpi-delta-icon {
  width: 12px;
  height: 12px;
}
.kpi-spark {
  margin-top: var(--sp-3);
  height: 32px;
}
</style>
