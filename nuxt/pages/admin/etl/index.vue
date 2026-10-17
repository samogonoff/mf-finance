<template>
  <div class="page-etl">
    <header class="page-header">
      <div>
        <h1 class="page-title">Загрузки данных</h1>
        <p class="page-subtitle">
          <span>Реплика FinDWH → ClickHouse (<code>finance.findwh_*</code>) для аналитиков</span>
          <template v-if="report">
            <span class="sep">·</span>
            <span>проверено {{ fmtTime(report.checked_at) }}</span>
          </template>
          <span v-if="loading" class="sep">·</span>
          <span v-if="loading">обновляю…</span>
        </p>
      </div>
      <div class="page-actions">
        <span v-if="report" class="badge badge-dot" :class="ETL_SEVERITY_BADGE[report.overall]">
          {{ overallLabel }}
        </span>
        <label class="check">
          <input v-model="autoRefresh" type="checkbox" /> автообновление
        </label>
        <button class="btn btn-ghost" :disabled="loading" @click="reload">
          <Icon name="lucide:refresh-cw" /> Обновить
        </button>
      </div>
    </header>

    <div v-if="error" class="banner banner-neg">{{ error }}</div>

    <template v-if="report">
      <div v-if="report.ch_error" class="banner banner-neg">
        ClickHouse недоступен — журнал прогонов и объём реплики не прочитаны: {{ report.ch_error }}
      </div>

      <!-- Воркер и часы источника -->
      <section class="card">
        <div class="card-header">
          <div>
            <div class="card-title">Воркер go-api</div>
            <div class="card-subtitle">
              Проверяет раз в интервал, собрал ли DWH таблицу за сегодня, и грузит её один раз в сутки.
              Загрузки из CLI (<code>cmd/findebt-etl MODE=findwh</code>) видны только в журнале.
            </div>
          </div>
          <span class="badge badge-dot" :class="report.worker.enabled ? 'badge-pos' : 'badge-warn'">
            {{ report.worker.enabled ? "включён" : "выключен" }}
          </span>
        </div>
        <div class="card-body facts">
          <div v-if="!report.worker.enabled" class="fact">
            <div class="fact-label">Почему выключен</div>
            <div class="fact-value">{{ report.worker.disabled_reason || "—" }}</div>
          </div>
          <div class="fact">
            <div class="fact-label">Интервал</div>
            <div class="fact-value num">{{ report.worker.interval_sec ? dur(report.worker.interval_sec) : "—" }}</div>
          </div>
          <div class="fact">
            <div class="fact-label">Сейчас</div>
            <div class="fact-value">
              <template v-if="report.worker.running">
                грузит <b>{{ titleOf(report.worker.running) }}</b> с {{ fmtTime(report.worker.running_since) }}
              </template>
              <template v-else-if="report.worker.tick_started_at">
                проверяет готовность с {{ fmtTime(report.worker.tick_started_at) }}
              </template>
              <template v-else>простаивает</template>
            </div>
          </div>
          <div class="fact">
            <div class="fact-label">Последний тик закончен</div>
            <div class="fact-value num">{{ fmtDateTime(report.worker.last_tick_end_at) }}</div>
          </div>
          <div class="fact">
            <div class="fact-label">Следующий тик</div>
            <div class="fact-value num">{{ report.worker.tick_started_at ? "после текущего" : fmtDateTime(report.worker.next_tick_at) }}</div>
          </div>
          <div class="fact">
            <div class="fact-label">Часы MSSQL</div>
            <div class="fact-value">
              <span v-if="report.source.checked" class="num">{{ ruDate(report.source.today) }}, {{ report.source.hour }} ч</span>
              <span v-else class="txt-neg">нет связи</span>
            </div>
          </div>
        </div>
        <div v-if="report.worker.last_tick_error" class="card-footer txt-neg">
          Ошибка последнего тика: {{ report.worker.last_tick_error }}
        </div>
        <div v-else-if="!report.source.checked && report.source.error" class="card-footer">
          MSSQL: {{ report.source.error }}
        </div>
      </section>

      <!-- Таблицы -->
      <div class="tables-grid">
        <section v-for="t in report.tables" :key="t.table" class="card table-card" :class="`sev-${t.severity}`">
          <div class="card-header">
            <div>
              <div class="card-title">{{ t.title }}</div>
              <div class="card-subtitle"><code>{{ t.target }}</code></div>
            </div>
            <span class="badge badge-dot" :class="ETL_SEVERITY_BADGE[t.severity]">
              {{ ETL_STATE_LABELS[t.state] || t.state }}
            </span>
          </div>
          <div class="card-body">
            <p class="state-text">{{ t.state_text }}</p>
            <dl class="kv">
              <dt>Данные за</dt>
              <dd>
                <template v-if="t.loaded_for">
                  <span class="num">{{ ruDate(t.loaded_for) }}</span>
                  <span v-if="t.lag_days !== undefined" class="muted">
                    · {{ t.lag_days === 0 ? "сегодня" : t.lag_days === 1 ? "вчера" : `${t.lag_days} дн. назад` }}
                  </span>
                </template>
                <span v-else class="muted">успешных загрузок нет</span>
              </dd>

              <dt>Последний прогон</dt>
              <dd>
                <template v-if="t.last_run">
                  <span class="badge" :class="t.last_run.status === 'ok' ? 'badge-pos' : 'badge-neg'">
                    {{ t.last_run.status === "ok" ? "ok" : "ошибка" }}
                  </span>
                  <span class="num"> {{ fmtDateTime(t.last_run.started_at) }}</span>
                  <span class="muted"> · {{ dur(t.last_run.duration_sec) }} · {{ t.last_run.triggered_by }}</span>
                </template>
                <span v-else class="muted">—</span>
              </dd>

              <dt>Обычно длится</dt>
              <dd class="num">{{ t.typical_duration_sec ? dur(t.typical_duration_sec) : "—" }}</dd>

              <dt>Строк за прогон</dt>
              <dd>
                <span class="num">{{ t.last_ok ? num(t.last_ok.rows) : "—" }}</span>
                <span v-if="t.table === 'glmf' && t.last_ok" class="muted"> · только перегруженные дни</span>
              </dd>

              <dt>В реплике</dt>
              <dd>
                <template v-if="t.data.rows !== undefined">
                  <span class="num">{{ num(t.data.rows) }}</span> строк
                  <template v-if="t.data.from">
                    <span class="muted"> · </span><span class="num">{{ ruDate(t.data.from) }} – {{ ruDate(t.data.to) }}</span>
                  </template>
                  <template v-if="t.data.days">
                    <span class="muted"> · {{ num(t.data.days) }} дн., {{ t.data.companies }} компаний</span>
                  </template>
                </template>
                <span v-if="t.data.error" class="txt-neg">{{ t.data.error }}</span>
              </dd>

              <dt>{{ t.table === "glmf" ? "Последнее изменение" : "Залито" }}</dt>
              <dd class="num">{{ fmtDateTime(t.data.loaded_at) }}</dd>

              <dt>Источник готов</dt>
              <dd>
                <template v-if="t.source_ready !== undefined">
                  <span :class="t.source_ready ? 'txt-pos' : 'muted'">{{ t.source_ready ? "да" : "нет" }}</span>
                </template>
                <span v-else class="txt-warn" :title="t.source_ready_error">не проверить</span>
                <div class="hint">{{ t.ready_rule }}</div>
              </dd>

              <dt>Откуда</dt>
              <dd>
                <code>{{ t.source }}</code>
                <div class="hint">{{ t.method }}</div>
              </dd>
            </dl>
            <pre v-if="t.last_run?.status === 'error'" class="err-details">{{ t.last_run.details }}</pre>
            <div v-else-if="t.last_ok?.details" class="hint">Итог: <code>{{ t.last_ok.details }}</code></div>
          </div>
        </section>
      </div>

      <!-- Настройки -->
      <section class="card">
        <div class="card-header">
          <div>
            <div class="card-title">Настройки</div>
            <div class="card-subtitle">Действующие значения env go-api; меняются в конфиге и применяются после перезапуска</div>
          </div>
        </div>
        <table class="data-table">
          <thead>
            <tr><th>Переменная</th><th>Значение</th><th>Что делает</th></tr>
          </thead>
          <tbody>
            <tr v-for="s in report.settings" :key="s.env">
              <td><code>{{ s.env }}</code></td>
              <td class="num value-cell">
                {{ s.value }}
                <div v-if="s.error" class="txt-warn hint-err">{{ s.error }}</div>
              </td>
              <td class="muted">{{ s.hint }}</td>
            </tr>
          </tbody>
        </table>
      </section>

      <!-- Журнал -->
      <section class="card">
        <div class="card-header">
          <div>
            <div class="card-title">Журнал прогонов</div>
            <div class="card-subtitle">Последние {{ report.runs.length }} из <code>finance.findwh_etl_runs</code></div>
          </div>
        </div>
        <div class="table-wrap no-bd">
          <table class="data-table">
            <thead>
              <tr>
                <th>Начало</th>
                <th>Таблица</th>
                <th>Данные за</th>
                <th>Статус</th>
                <th class="col-num">Длительность</th>
                <th class="col-num">Строк</th>
                <th>Запуск</th>
                <th>Детали</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in report.runs" :key="r.table + r.started_at">
                <td class="num">{{ fmtDateTime(r.started_at) }}</td>
                <td>{{ titleOf(r.table) }}</td>
                <td class="num">{{ ruDate(r.source_date) }}</td>
                <td>
                  <span class="badge" :class="r.status === 'ok' ? 'badge-pos' : 'badge-neg'">
                    {{ r.status === "ok" ? "ok" : "ошибка" }}
                  </span>
                </td>
                <td class="num">{{ dur(r.duration_sec) }}</td>
                <td class="num">{{ num(r.rows) }}</td>
                <td>{{ r.triggered_by }}</td>
                <td class="details-cell" :title="r.details">{{ r.details }}</td>
              </tr>
              <tr v-if="!report.runs.length">
                <td colspan="8" class="cell-empty">Прогонов ещё не было</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from "vue";
import {
  useEtlStatus, ETL_SEVERITY_BADGE, ETL_STATE_LABELS, type EtlStatusReport
} from "~/composables/useEtlStatus";
import { num } from "~/utils/format";

definePageMeta({ middleware: "scope-guard" });

const api = useEtlStatus();
const report = ref<EtlStatusReport | null>(null);
const loading = ref(false);
const error = ref<string | null>(null);
const autoRefresh = ref(true);
let timer: ReturnType<typeof setInterval> | null = null;

const reload = async () => {
  loading.value = true;
  try {
    report.value = await api.findwh();
    error.value = null;
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || "Не удалось получить статус";
  } finally {
    loading.value = false;
  }
};

const overallLabel = computed(() => ({
  ok: "всё в порядке", info: "в работе", warn: "есть предупреждения", error: "есть ошибки"
} as Record<string, string>)[report.value?.overall ?? "ok"]);

const titleOf = (table: string) =>
  report.value?.tables.find((t) => t.table === table)?.title ?? table;

const ruDate = (iso?: string) => {
  if (!iso) return "—";
  const [y, m, d] = iso.split("-");
  return `${d}.${m}.${y}`;
};
const fmtDateTime = (iso?: string) =>
  iso ? new Date(iso).toLocaleString("ru-RU", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" }) : "—";
const fmtTime = (iso?: string) =>
  iso ? new Date(iso).toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit", second: "2-digit" }) : "—";
const dur = (sec: number) => {
  if (sec < 60) return `${sec} с`;
  if (sec < 3600) return `${Math.floor(sec / 60)} мин`;
  return `${Math.floor(sec / 3600)} ч ${String(Math.floor((sec % 3600) / 60)).padStart(2, "0")} мин`;
};

// Раз в 30 с — пока вкладка видна: статус меняется медленно, а GLMF грузится часами.
const tick = () => {
  if (autoRefresh.value && document.visibilityState === "visible" && !loading.value) reload();
};
watch(autoRefresh, (on) => { if (on && !loading.value) reload(); });

onMounted(() => {
  reload();
  timer = setInterval(tick, 30_000);
});
onBeforeUnmount(() => { if (timer) clearInterval(timer); });
</script>

<style scoped>
.page-etl { display: flex; flex-direction: column; gap: var(--sp-7); }
.page-etl .page-header { margin-bottom: 0; }
.page-actions { display: flex; align-items: center; gap: var(--sp-5); }
.sep { margin: 0 var(--sp-3); color: var(--text-muted); }
.hint-err { font-family: var(--font-sans); font-size: var(--fs-xs); white-space: normal; }
.check { display: inline-flex; align-items: center; gap: var(--sp-3); font-size: var(--fs-sm); color: var(--text-secondary); }

.banner { padding: var(--sp-4) var(--sp-5); border-radius: var(--rd-3); font-size: var(--fs-sm); }
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }

.facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: var(--sp-6); }
.fact-label {
  font-size: var(--fs-xs); font-weight: var(--fw-semibold); color: var(--text-muted);
  text-transform: uppercase; letter-spacing: 0.06em; margin-bottom: var(--sp-2);
}
.fact-value { font-size: var(--fs-base); color: var(--text-strong); }

.tables-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(340px, 1fr)); gap: var(--sp-6); }
.table-card { border-top-width: 3px; }
.table-card.sev-ok { border-top-color: var(--pos); }
.table-card.sev-info { border-top-color: var(--accent); }
.table-card.sev-warn { border-top-color: var(--warn); }
.table-card.sev-error { border-top-color: var(--neg); }

.state-text { margin: 0 0 var(--sp-6); color: var(--text-primary); font-size: var(--fs-base); line-height: 1.45; }

.kv { display: grid; grid-template-columns: 132px 1fr; gap: var(--sp-4) var(--sp-5); margin: 0; font-size: var(--fs-sm); }
.kv dt { color: var(--text-muted); }
.kv dd { margin: 0; color: var(--text-strong); min-width: 0; overflow-wrap: anywhere; }

.hint { font-size: var(--fs-xs); color: var(--text-muted); margin-top: var(--sp-2); }
.muted { color: var(--text-muted); }
.txt-pos { color: var(--pos); }
.txt-neg { color: var(--neg); }
.txt-warn { color: var(--warn); }

code { font-family: var(--font-mono); font-size: 0.92em; }
.num { font-family: var(--font-mono); font-variant-numeric: tabular-nums; }

.err-details {
  margin: var(--sp-6) 0 0; padding: var(--sp-4) var(--sp-5);
  background: var(--neg-soft); color: var(--neg-strong); border-radius: var(--rd-3);
  font-family: var(--font-mono); font-size: var(--fs-xs);
  white-space: pre-wrap; overflow-wrap: anywhere; max-height: 160px; overflow: auto;
}

.value-cell { white-space: nowrap; }
.details-cell {
  max-width: 420px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  font-family: var(--font-mono); font-size: var(--fs-xs); color: var(--text-secondary);
}
.cell-empty { text-align: center; color: var(--text-muted); padding: var(--sp-7); }

@media (max-width: 640px) {
  .kv { grid-template-columns: 1fr; gap: var(--sp-2); }
  .kv dt { margin-top: var(--sp-4); }
}
</style>
