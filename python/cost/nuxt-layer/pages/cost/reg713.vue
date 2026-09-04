<template>
  <div class="page-reg713">
    <header class="page-header">
      <div>
        <h1 class="page-title">Согласование по постановлению 713</h1>
        <p class="page-subtitle">
          Изделия, отмеченные «требуется согласование с исполкомом»: цены, аналоги, решения.
          Отметка ставится в карточке из главной таблицы (колонка «713»).
        </p>
      </div>
      <div class="page-actions">
        <button class="btn btn-ghost" @click="navigateTo('/cost')"><Icon name="lucide:arrow-left" /> К таблице</button>
        <button class="btn btn-ghost" :disabled="loading" @click="load"><Icon name="lucide:refresh-cw" /> Обновить</button>
        <button class="btn btn-primary" :disabled="!filtered.length" @click="exportExcel"><Icon name="lucide:download" /> Экспорт в Excel ({{ filtered.length }})</button>
      </div>
    </header>

    <section class="card filters">
      <input v-model="q" type="text" class="f-search" placeholder="Поиск: модель, артикул, наименование, аналог, № письма…" />
      <label class="f-item">Вид
        <select v-model="fKind">
          <option value="">Все</option>
          <option value="price_increase">Повышение цены</option>
          <option value="novelty">Новинка</option>
          <option value="none">Не указан</option>
        </select>
      </label>
      <label class="f-item">Решение исполкома
        <select v-model="fDecision">
          <option value="">Все</option>
          <option value="pending">Не рассмотрено</option>
          <option value="approved">Согласовано</option>
          <option value="rejected">Не согласовано</option>
        </select>
      </label>
      <label class="f-item">Аналог
        <select v-model="fAnalog">
          <option value="">Все</option>
          <option value="yes">Указан</option>
          <option value="no">Не указан</option>
        </select>
      </label>
      <label class="f-item">Отмечено с <input v-model="fFrom" type="date" /></label>
      <label class="f-item">по <input v-model="fTo" type="date" /></label>
      <label class="f-check"><input v-model="showAll" type="checkbox" @change="load" /> Показать и снятые с согласования</label>
      <a v-if="filtersActive" href="#" class="f-reset" @click.prevent="resetFilters">Сбросить фильтры</a>
    </section>

    <section class="card">
      <div class="summary">
        <template v-if="loading">Загрузка…</template>
        <template v-else>
          Показано <strong>{{ filtered.length }}</strong> из {{ items.length }}
          · не рассмотрено <strong>{{ count('pending') }}</strong>
          · согласовано <strong>{{ count('approved') }}</strong>
          · не согласовано <strong>{{ count('rejected') }}</strong>
        </template>
        <span v-if="error" class="err">{{ error }}</span>
      </div>
      <div class="table-wrap">
        <table class="data-table compact">
          <thead>
            <tr>
              <th v-for="c in COLUMNS" :key="c.key" :class="[c.num ? 'col-num' : '', { sorted: sortKey === c.key }]" @click="toggleSort(c.key)" :title="c.title || ''">
                {{ c.label }}<span v-if="sortKey === c.key" class="sort-arrow">{{ sortDir === 'asc' ? ' ▲' : ' ▼' }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!loading && !filtered.length"><td :colspan="COLUMNS.length" class="muted center">Нет карточек по заданным условиям</td></tr>
            <tr v-for="c in filtered" :key="c.id" :class="{ unmarked: !c.required }">
              <td class="date-col">{{ fmtDate(c.required_at || c.created_at) }}</td>
              <td class="mono">{{ c.model }}</td>
              <td class="mono">{{ c.articul }}</td>
              <td class="cell-ellipsis" :title="c.name || ''">{{ c.name || '—' }}</td>
              <td class="col-num num" :title="sourceTitle(c.price_source)">{{ fmt(c.retail) }}</td>
              <td class="col-num num" :title="sourceTitle(c.price_source)">{{ fmt(c.wholesale) }}</td>
              <td>{{ KIND_LABEL[c.kind] || '—' }}</td>
              <td class="mono">{{ c.analog_model || '—' }}</td>
              <td class="mono">{{ c.analog_articul || '—' }}</td>
              <td class="cell-ellipsis" :title="c.analog_name || ''">{{ c.analog_name || '—' }}</td>
              <td class="col-num num" :title="sourceTitle(c.analog_price_source)">{{ fmt(c.analog_retail) }}</td>
              <td class="col-num num" :title="sourceTitle(c.analog_price_source)">{{ fmt(c.analog_wholesale) }}</td>
              <td><span class="pill" :class="'pill-' + decisionKey(c)">{{ DECISION_LABEL[decisionKey(c)] }}</span></td>
              <td class="date-col">{{ fmtDate(c.decision_at) }}</td>
              <td class="cell-ellipsis" :title="c.decision_doc || ''">{{ c.decision_doc || '—' }}</td>
              <td class="cell-ellipsis" :title="c.comment || ''">{{ c.comment || '—' }}</td>
              <td class="cell-ellipsis muted" :title="(c.updated_by || '') + ' ' + fmtDateTime(c.updated_at)">{{ c.updated_by || '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'

// Отчёт по карточкам согласования 713 (пожелание № 8, вторая часть): все
// изделия, отмеченные «требуется согласование», их цены и аналоги, решения.
// Данные отдаёт GET /reg713/list уже с ценами «по правилам главной таблицы»;
// фильтры, сортировка и Excel — здесь, карточек сотни, не миллионы.

const config = useRuntimeConfig()
const apiBase = computed(() => (config.public.costOnly ? '' : ((config.public.apiBase as string) || '')))
const user = useState<any>('auth-user')
const fetchHeaders = computed(() => {
  const email = user.value?.email || ''
  return email ? { 'X-Cost-User': email } : {}
})

const items = ref<any[]>([])
const loading = ref(false)
const error = ref('')
const showAll = ref(false)

const q = ref('')
const fKind = ref('')
const fDecision = ref('')
const fAnalog = ref('')
const fFrom = ref('')
const fTo = ref('')

const KIND_LABEL: Record<string, string> = { price_increase: 'Повышение цены', novelty: 'Новинка' }
const DECISION_LABEL: Record<string, string> = { pending: 'Не рассмотрено', approved: 'Согласовано', rejected: 'Не согласовано' }
const SOURCE_LABEL: Record<string, string> = {
  calc: 'последняя калькуляция в CostHistory', dwh: 'утверждённая цена в DWH',
  gpartner: 'плановая цена из S_MODELI', manual: 'введено вручную',
}

const COLUMNS = [
  { key: 'required_at', label: 'Отмечено', title: 'Дата отметки «требуется согласование»' },
  { key: 'model', label: 'Модель' },
  { key: 'articul', label: 'Артикул' },
  { key: 'name', label: 'Наименование' },
  { key: 'retail', label: 'Розница', num: true, title: 'Розничная цена изделия по правилам главной таблицы' },
  { key: 'wholesale', label: 'Опт', num: true, title: 'Отпускная цена изделия по правилам главной таблицы' },
  { key: 'kind', label: 'Вид' },
  { key: 'analog_model', label: 'Аналог: модель' },
  { key: 'analog_articul', label: 'Аналог: артикул' },
  { key: 'analog_name', label: 'Аналог: наименование' },
  { key: 'analog_retail', label: 'Аналог: розница', num: true },
  { key: 'analog_wholesale', label: 'Аналог: опт', num: true },
  { key: 'decision', label: 'Решение исполкома' },
  { key: 'decision_at', label: 'Дата решения' },
  { key: 'decision_doc', label: '№ письма' },
  { key: 'comment', label: 'Комментарий' },
  { key: 'updated_by', label: 'Изменил' },
]

const sortKey = ref('required_at')
const sortDir = ref<'asc' | 'desc'>('desc')
function toggleSort(key: string) {
  if (sortKey.value === key) sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
  else { sortKey.value = key; sortDir.value = key === 'required_at' || key === 'decision_at' ? 'desc' : 'asc' }
}

function decisionKey(c: any): 'pending' | 'approved' | 'rejected' {
  return c.decision === 'approved' ? 'approved' : c.decision === 'rejected' ? 'rejected' : 'pending'
}

const filtersActive = computed(() => !!(q.value || fKind.value || fDecision.value || fAnalog.value || fFrom.value || fTo.value))
function resetFilters() { q.value = ''; fKind.value = ''; fDecision.value = ''; fAnalog.value = ''; fFrom.value = ''; fTo.value = '' }

const filtered = computed(() => {
  const needle = q.value.trim().toLowerCase()
  let rows = items.value.filter((c) => {
    if (needle) {
      const hay = [c.model, c.articul, c.name, c.analog_model, c.analog_articul, c.analog_name, c.decision_doc, c.comment, c.updated_by]
        .map((v) => String(v || '').toLowerCase()).join(' ')
      if (!hay.includes(needle)) return false
    }
    if (fKind.value === 'none' ? !!c.kind : (fKind.value && c.kind !== fKind.value)) return false
    if (fDecision.value && decisionKey(c) !== fDecision.value) return false
    if (fAnalog.value === 'yes' && !c.analog_articul) return false
    if (fAnalog.value === 'no' && c.analog_articul) return false
    const marked = (c.required_at || c.created_at || '').slice(0, 10)
    if (fFrom.value && marked && marked < fFrom.value) return false
    if (fTo.value && marked && marked > fTo.value) return false
    return true
  })
  const k = sortKey.value
  const dir = sortDir.value === 'asc' ? 1 : -1
  const col = COLUMNS.find((c) => c.key === k)
  rows = [...rows].sort((a, b) => {
    let va = k === 'decision' ? decisionKey(a) : a[k]
    let vb = k === 'decision' ? decisionKey(b) : b[k]
    if (va == null && vb == null) return 0
    if (va == null) return 1
    if (vb == null) return -1
    if (col?.num) return (Number(va) - Number(vb)) * dir
    return String(va).localeCompare(String(vb), 'ru') * dir
  })
  return rows
})

function count(key: 'pending' | 'approved' | 'rejected') {
  return filtered.value.filter((c) => decisionKey(c) === key).length
}

function fmt(v: any): string {
  if (v == null || v === '') return '—'
  const n = Number(v)
  return Number.isNaN(n) ? String(v) : n.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}
function fmtDate(v: any): string { return v ? new Date(v).toLocaleDateString('ru-RU') : '—' }
function fmtDateTime(v: any): string { return v ? new Date(v).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' }) : '' }
function sourceTitle(src: any): string { return src ? 'Источник: ' + (SOURCE_LABEL[src] || src) : '' }

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await $fetch<any>(`${apiBase.value}/api/cost/reg713/list?all=${showAll.value ? 1 : 0}`, { headers: fetchHeaders.value })
    items.value = res?.items || []
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || String(e)
  } finally {
    loading.value = false
  }
}

/** Excel — как в остальном разделе: HTML-таблица под application/vnd.ms-excel
 *  (настоящий .xlsx в разделе не собирается нигде). Выгружается то, что
 *  отфильтровано и отсортировано на экране. */
function exportExcel() {
  const esc = (s: any) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  const num = (v: any) => (v == null || v === '' ? '' : Number(v).toFixed(2).replace('.', ','))
  const head = COLUMNS.map((c) => `<th>${esc(c.label)}</th>`).join('')
  const body = filtered.value.map((c) => {
    const cells = [
      fmtDate(c.required_at || c.created_at), c.model, c.articul, c.name || '',
      num(c.retail), num(c.wholesale), KIND_LABEL[c.kind] || '',
      c.analog_model || '', c.analog_articul || '', c.analog_name || '',
      num(c.analog_retail), num(c.analog_wholesale),
      DECISION_LABEL[decisionKey(c)], fmtDate(c.decision_at), c.decision_doc || '', c.comment || '', c.updated_by || '',
    ]
    return '<tr>' + cells.map((v) => `<td>${esc(v)}</td>`).join('') + '</tr>'
  }).join('')
  const html = `<html><head><meta charset="utf-8"></head><body><table border="1"><tr>${head}</tr>${body}</table></body></html>`
  const blob = new Blob(['﻿' + html], { type: 'application/vnd.ms-excel' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `soglasovanie-713-${new Date().toISOString().slice(0, 10)}.xls`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

onMounted(load)
</script>

<style scoped>
.page-reg713 { display: flex; flex-direction: column; gap: var(--sp-4, 16px); }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; flex-wrap: wrap; }
.page-title { margin: 0; font-size: 24px; }
.page-subtitle { margin: 4px 0 0; color: var(--text-muted, #6b7280); max-width: 70ch; }
.page-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.card { background: var(--bg-surface, #fff); border: 1px solid var(--border, #e5e7eb); border-radius: 8px; padding: 12px 16px; }
.filters { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 12px; }
.f-search { flex: 1 1 320px; padding: 6px 10px; border: 1px solid var(--border-color, #d1d5db); border-radius: 4px; font-size: 13px; }
.f-item { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--text-muted, #6b7280); }
.f-item select, .f-item input { padding: 5px 8px; border: 1px solid var(--border-color, #d1d5db); border-radius: 4px; font-size: 13px; background: var(--bg-surface, #fff); color: inherit; }
.f-check { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; cursor: pointer; }
.f-reset { font-size: 12px; }
.summary { display: flex; flex-wrap: wrap; gap: 6px 12px; align-items: center; font-size: 13px; color: var(--text-muted, #6b7280); margin-bottom: 8px; }
.summary strong { color: inherit; }
.err { color: var(--neg, #dc2626); }
.table-wrap { overflow-x: auto; }
.data-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.data-table th { position: sticky; top: 0; background: var(--bg-surface-2, #f9fafb); text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--border, #e5e7eb); font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted, #6b7280); white-space: nowrap; cursor: pointer; user-select: none; }
.data-table th.sorted { color: var(--accent, #4338ca); }
.data-table td { padding: 6px 10px; border-bottom: 1px solid var(--border, #eef0f3); white-space: nowrap; vertical-align: top; }
.data-table tr.unmarked td { opacity: 0.55; }
.col-num { text-align: right; }
.num { font-variant-numeric: tabular-nums; font-family: 'JetBrains Mono', ui-monospace, monospace; }
.mono { font-family: 'JetBrains Mono', ui-monospace, monospace; font-variant-numeric: tabular-nums; }
.cell-ellipsis { max-width: 240px; overflow: hidden; text-overflow: ellipsis; }
.date-col { white-space: nowrap; }
.center { text-align: center; padding: 24px !important; }
.muted { color: var(--text-muted, #6b7280); }
.pill { display: inline-block; padding: 1px 8px; border-radius: 999px; font-size: 12px; white-space: nowrap; }
.pill-pending { background: color-mix(in srgb, #2563eb 12%, transparent); color: #1d4ed8; }
.pill-approved { background: color-mix(in srgb, var(--pos, #16a34a) 14%, transparent); color: var(--pos, #15803d); }
.pill-rejected { background: color-mix(in srgb, var(--neg, #dc2626) 12%, transparent); color: var(--neg, #b91c1c); }
.sort-arrow { font-size: 10px; }
</style>
