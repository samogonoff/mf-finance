<template>
  <div class="page-route">
    <header class="page-header">
      <div>
        <h1 class="page-title">Маршрут процесса — ответственные</h1>
        <p class="page-subtitle">Назначение ответственных и согласующих по этапам (по схеме ТЗ)</p>
      </div>
    </header>

    <p v-if="!canEdit" class="banner banner-warn">Просмотр. Редактирование — у роли «Администратор процессов».</p>
    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="saved" class="banner banner-pos">Сохранено.</p>

    <div v-for="tr in tracks" :key="tr.key" class="track-block">
      <h2 class="track-title">{{ tr.label }}</h2>
      <div v-for="s in byTrack(tr.key)" :key="s.stage_code" class="route-row card">
        <div class="rr-stage">
          <span class="rr-code">Этап {{ s.stage_code }}</span>
          <span class="rr-name">{{ s.name }}</span>
          <span class="rr-due">{{ s.prev25 ? "25-е (М−1)" : s.due_rd ? s.due_rd + "-й р.д." : "—" }}</span>
        </div>
        <input
          v-model="s.responsible"
          class="select rr-input"
          :disabled="!canEdit"
          placeholder="ответственные / согласующие"
        />
        <button v-if="canEdit" class="btn btn-sm btn-primary" @click="save(s)">
          <Icon name="lucide:save" /> Сохранить
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { usePlans, type StageRoute } from "~/composables/usePlans";

definePageMeta({ middleware: "scope-guard" });

const { route: loadRoute, setRoute } = usePlans();
const { hasRole, isAdmin } = useScope();
const canEdit = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));

const list = ref<StageRoute[]>([]);
const error = ref("");
const saved = ref(false);

const tracks = [
  { key: "sales", label: "Поток продаж" },
  { key: "production", label: "Поток производства" },
  { key: "final", label: "Финал — ЮЛ" }
];
const byTrack = (k: string) => list.value.filter((s) => s.track === k);

const save = async (s: StageRoute) => {
  error.value = "";
  saved.value = false;
  try {
    await setRoute(s.stage_code, s.responsible);
    saved.value = true;
    setTimeout(() => (saved.value = false), 2000);
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка сохранения (нужна роль ROLE_PLANS_ADMIN)";
  }
};

onMounted(async () => {
  try {
    list.value = await loadRoute();
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка загрузки";
  }
});
</script>

<style scoped>
.banner {
  padding: var(--sp-4) var(--sp-5);
  border-radius: var(--rd-4, 6px);
  margin-bottom: var(--sp-5);
  font-size: var(--fs-sm);
}
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
.banner-warn { background: var(--warn-soft); color: var(--warn); }
.banner-pos { background: var(--pos-soft); color: var(--pos-strong); }
.track-block {
  margin-bottom: var(--sp-6);
}
.track-title {
  font-size: var(--fs-sm);
  color: var(--text-secondary);
  margin-bottom: var(--sp-3);
}
.route-row {
  display: grid;
  grid-template-columns: 260px 1fr auto;
  align-items: center;
  gap: var(--sp-5);
  padding: var(--sp-4) var(--sp-5);
  margin-bottom: var(--sp-3);
}
.rr-stage {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.rr-code {
  font-family: var(--font-mono);
  font-weight: var(--fw-semibold);
  font-size: var(--fs-sm);
}
.rr-name {
  font-size: var(--fs-sm);
  color: var(--text-strong);
}
.rr-due {
  font-size: var(--fs-2xs);
  color: var(--text-muted);
}
.rr-input {
  width: 100%;
}
</style>
