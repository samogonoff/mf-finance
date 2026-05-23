<template>
  <div class="page-bug-sources">
    <header class="page-header">
      <div>
        <h1 class="page-title">Источники проблем</h1>
        <p class="page-subtitle">
          Заполняются при выставлении статуса «Решён» —
          <NuxtLink to="/admin/bugtracker" class="link-muted">← к репортам</NuxtLink>
        </p>
      </div>
    </header>

    <div class="add-row">
      <input v-model="newName" class="input" placeholder="Название причины…" @keyup.enter="add" />
      <button class="btn btn-primary" :disabled="!newName.trim()" @click="add">
        <Icon name="lucide:plus" /> Добавить
      </button>
    </div>

    <div v-if="error" class="error-banner">{{ error }}</div>

    <div class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th class="col-w-16">ID</th>
            <th>Название</th>
            <th>Активна</th>
            <th class="col-num">Создана</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in sources" :key="s.id">
            <td class="num">{{ s.id }}</td>
            <td>
              <input v-model="s.name" class="input input-inline"
                     @blur="rename(s)" @keyup.enter="rename(s)" />
            </td>
            <td>
              <label class="check">
                <input type="checkbox" :checked="s.is_active" @change="toggleActive(s, ($event.target as HTMLInputElement).checked)" />
              </label>
            </td>
            <td class="num">{{ formatDate(s.created_at) }}</td>
          </tr>
          <tr v-if="!sources.length">
            <td colspan="4" class="cell-empty">Источников нет</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from "vue";
import { useBugTrackerAdmin, type BugSource } from "~/composables/useBugTrackerAdmin";

definePageMeta({ middleware: "scope-guard" });

const api = useBugTrackerAdmin();
const sources = ref<BugSource[]>([]);
const newName = ref("");
const error = ref<string | null>(null);

const reload = async () => {
  try {
    sources.value = await api.sources();
    error.value = null;
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || "Ошибка";
  }
};

const add = async () => {
  const n = newName.value.trim();
  if (!n) return;
  try {
    const s = await api.sourceCreate(n);
    sources.value.unshift(s);
    newName.value = "";
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || "Не удалось добавить";
  }
};

const rename = async (s: BugSource) => {
  try {
    await api.sourcePatch(s.id, { name: s.name });
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || "Не удалось переименовать";
  }
};

const toggleActive = async (s: BugSource, v: boolean) => {
  try {
    await api.sourcePatch(s.id, { is_active: v });
    s.is_active = v;
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || "Ошибка";
  }
};

const formatDate = (iso: string) =>
  new Date(iso).toLocaleDateString("ru-RU");

onMounted(reload);
</script>

<style scoped>
.add-row { display: flex; gap: var(--sp-3); margin-bottom: var(--sp-4); }
.add-row .input { max-width: 320px; }

.input, .input-inline {
  padding: 6px 10px;
  border: 1px solid var(--border); border-radius: var(--rd-3);
  background: var(--bg-base); color: var(--text-strong); font: inherit;
}
.input-inline { width: 100%; border-color: transparent; }
.input-inline:focus, .input-inline:hover { border-color: var(--border); }

.check { display: inline-flex; align-items: center; cursor: pointer; }
.cell-empty { text-align: center; color: var(--text-muted); padding: var(--sp-6); }
.link-muted { color: var(--text-muted); }
.error-banner {
  background: var(--neg-soft); color: var(--neg-strong);
  padding: var(--sp-3) var(--sp-4); border-radius: var(--rd-3);
  margin-bottom: var(--sp-4); font-size: var(--fs-sm);
}
</style>
