<template>
  <div class="page-approvals">
    <header class="page-header">
      <h1 class="page-title">Согласование цен</h1>
      <p class="page-subtitle">Утверждение изменений, предложенных бренд-менеджерами</p>
    </header>

    <!-- Action bar -->
    <div class="approval-actions">
      <div class="approval-info">
        <template v-if="loading">
          <span>Загрузка…</span>
        </template>
        <template v-else>
          <span>Всего ожидает согласования: <strong>{{ pendingChanges.length }}</strong></span>
        </template>
      </div>
      <div class="approval-buttons">
        <button class="btn btn-primary" :disabled="!selectedIds.length || applying" @click="applySelected">
          <Icon name="lucide:check-circle" />
          {{ applying ? 'Установка…' : `Установить цены (${selectedIds.length})` }}
        </button>
        <button class="btn btn-ghost" :disabled="!pendingChanges.length || clearing" @click="confirmClear">
          <Icon name="lucide:trash-2" /> Очистить таблицу
        </button>
        <button class="btn btn-ghost" @click="loadData">
          <Icon name="lucide:refresh-cw" /> Обновить
        </button>
      </div>
    </div>

    <!-- Table -->
    <section class="card">
      <div class="table-wrap">
        <table class="data-table compact">
          <thead>
            <tr>
              <th><input type="checkbox" :checked="allSelected" @change="toggleSelectAll" /></th>
              <th>Модель</th>
              <th>Артикул</th>
              <th>Наименование модели</th>
              <th>Признак калькуляции</th>
              <th>PLAN_ID</th>
              <th>Уровень цен</th>
              <th class="col-num">Розн., руб</th>
              <th class="col-num">Опт., руб</th>
              <th>Автор</th>
              <th>Дата</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="11" class="muted" style="text-align:center;padding:24px">Загрузка…</td>
            </tr>
            <tr v-else-if="!pendingChanges.length">
              <td colspan="11" class="muted" style="text-align:center;padding:24px">Нет ожидающих согласования изменений</td>
            </tr>
            <tr v-for="pc in pendingChanges" :key="pc.id">
              <td><input type="checkbox" :value="pc.id" v-model="selectedIds" /></td>
              <td>{{ pc['Модель'] || '—' }}</td>
              <td>{{ pc['Артикул'] || '—' }}</td>
              <td>{{ pc['Наименование модели'] || '—' }}</td>
              <td>{{ pc['Признак калькуляции'] || '—' }}</td>
              <td>{{ pc['PLAN_ID'] || '—' }}</td>
              <td>{{ pc['Уровень цен'] || '—' }}</td>
              <td class="col-num num">{{ fmt(pc['Розничная цена по уровню, руб.']) }}</td>
              <td class="col-num num">{{ fmt(pc['Отпускная цена по уровню, руб']) }}</td>
              <td>{{ pc['username'] }}</td>
              <td>{{ formatDate(pc['created_at']) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- Error banner -->
    <div v-if="error" class="approval-error">
      <span>{{ error }}</span>
      <button class="approval-error-x" @click="error = ''">×</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'

const config = useRuntimeConfig()
const apiBase = computed(() =>
  config.public.costOnly ? '' : ((config.public.apiBase as string) || '')
)

const pendingChanges = ref<any[]>([])
const selectedIds = ref<number[]>([])
const loading = ref(false)
const applying = ref(false)
const clearing = ref(false)
const error = ref('')

const allSelected = computed(() =>
  pendingChanges.value.length > 0 && selectedIds.value.length === pendingChanges.value.length
)

function toggleSelectAll(e: Event) {
  const checked = (e.target as HTMLInputElement).checked
  if (checked) {
    selectedIds.value = pendingChanges.value.map(p => p.id)
  } else {
    selectedIds.value = []
  }
}

async function loadData() {
  loading.value = true
  error.value = ''
  try {
    const res = await $fetch<{ data: any[] }>(`${apiBase.value}/api/cost/pending-changes`)
    pendingChanges.value = res.data || []
    selectedIds.value = []
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || String(e)
  } finally {
    loading.value = false
  }
}

async function applySelected() {
  if (!selectedIds.value.length) return
  applying.value = true
  error.value = ''
  try {
    await $fetch(`${apiBase.value}/api/cost/pending-changes/apply`, {
      method: 'POST',
      body: { ids: selectedIds.value },
    })
    await loadData()
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || String(e)
  } finally {
    applying.value = false
  }
}

function confirmClear() {
  if (!confirm('Очистить таблицу согласования? Все необработанные изменения будут удалены.')) return
  clearAll()
}

async function clearAll() {
  clearing.value = true
  error.value = ''
  try {
    await $fetch(`${apiBase.value}/api/cost/pending-changes/clear`, { method: 'POST' })
    await loadData()
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || String(e)
  } finally {
    clearing.value = false
  }
}

function fmt(v: any): string {
  if (v === null || v === undefined || v === '') return '—'
  const n = Number(v)
  if (isNaN(n)) return String(v)
  return n.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function formatDate(v: any): string {
  if (!v) return '—'
  const s = String(v)
  return s.includes('T') ? s.split('T')[0] : s
}

onMounted(loadData)
</script>

<style scoped>
.page-approvals { padding-top: var(--sp-2); }
.page-subtitle { color: var(--text-muted); font-size: var(--fs-sm); margin-top: 4px; }

.approval-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-4);
  padding: var(--sp-3) var(--sp-5);
  background: var(--bg-surface-2, var(--bg-surface));
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  margin-bottom: var(--sp-5);
  flex-wrap: wrap;
}
.approval-info { font-size: var(--fs-sm); }
.approval-info strong { color: var(--text-strong); }
.approval-buttons { display: inline-flex; gap: var(--sp-3); align-items: center; }

.table-wrap { overflow-x: auto; }
.data-table th, .data-table td { padding: var(--sp-2) var(--sp-3); font-size: var(--fs-sm); }
.data-table th input[type="checkbox"], .data-table td input[type="checkbox"] { width: 16px; height: 16px; cursor: pointer; }

.approval-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--sp-3) var(--sp-4);
  margin-top: var(--sp-4);
  border-radius: var(--rd-3);
  background: color-mix(in srgb, var(--neg) 8%, var(--bg-surface));
  border: 1px solid color-mix(in srgb, var(--neg) 30%, transparent);
  color: var(--text-strong);
  font-size: var(--fs-sm);
}
.approval-error-x {
  border: none;
  background: transparent;
  font-size: 18px;
  cursor: pointer;
  color: var(--text-muted);
  padding: 0 4px;
}
.approval-error-x:hover { color: var(--text-strong); }
</style>
