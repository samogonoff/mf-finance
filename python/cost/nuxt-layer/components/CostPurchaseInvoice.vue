<template>
  <div class="modal-overlay" @click.self="$emit('close')">
    <div class="modal pinv-modal" @click.stop>
      <div class="modal-header">
        <span>Приход закупной продукции · расчёт ПФКСС</span>
        <span class="modal-subtitle" v-if="view === 'edit'">
          {{ form.id ? `инвойс № ${form.number || '—'} · ${form.status === 'applied' ? 'применён' : 'черновик'}` : 'новый приход' }}
        </span>
        <button class="modal-close" @click="$emit('close')">✕</button>
      </div>

      <!-- ── Список инвойсов ─────────────────────────────────────────────── -->
      <div v-if="view === 'list'" class="pinv-body">
        <div class="pinv-toolbar">
          <button class="btn btn-primary" type="button" :disabled="!canEdit" @click="newInvoice()">
            <Icon name="lucide:plus" /> Новый приход
          </button>
          <button class="btn btn-ghost" type="button" :disabled="listLoading" @click="loadList">
            <Icon name="lucide:refresh-cw" /> Обновить
          </button>
          <span class="muted pinv-hint">Черновик можно править и применить; применённый инвойс создал калькуляции ПФКСС и больше не меняется.</span>
        </div>
        <div v-if="listError" class="pinv-error">{{ listError }}</div>
        <div class="pinv-table-wrap">
          <table class="pinv-table">
            <thead>
              <tr><th>№ инвойса</th><th>Дата прихода</th><th>Поставщик</th><th>Валюта</th><th class="col-num">Строк</th><th class="col-num">Σ, BYN</th><th>Статус</th><th>Изменил</th><th></th></tr>
            </thead>
            <tbody>
              <tr v-if="!listLoading && !invoices.length"><td colspan="9" class="muted center">Приходов ещё нет</td></tr>
              <tr v-for="inv in invoices" :key="inv.id">
                <td class="mono">{{ inv.number }}</td>
                <td>{{ fmtDate(inv.arrival_date || inv.invoice_date) }}</td>
                <td class="ellipsis" :title="inv.supplier || ''">{{ inv.supplier || '—' }}</td>
                <td class="mono">{{ inv.currency }}</td>
                <td class="col-num num">{{ inv.line_count }}</td>
                <td class="col-num num">{{ fmt(inv.totals?.sum_byn) }}</td>
                <td><span class="pill" :class="inv.status === 'applied' ? 'pill-ok' : 'pill-draft'">{{ inv.status === 'applied' ? 'применён' : 'черновик' }}</span></td>
                <td class="muted ellipsis">{{ inv.updated_by || inv.created_by }}</td>
                <td><button class="btn btn-ghost btn-sm" type="button" @click="openInvoice(inv.id)">{{ inv.status === 'applied' ? 'Смотреть' : 'Открыть' }}</button></td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- ── Редактор инвойса ────────────────────────────────────────────── -->
      <div v-else class="pinv-body">
        <div class="pinv-toolbar">
          <button class="btn btn-ghost btn-sm" type="button" @click="view = 'list'; error = ''"><Icon name="lucide:arrow-left" /> К списку</button>
          <span class="muted pinv-hint" v-if="form.status === 'applied'">Инвойс применён {{ fmtDateTime(form.applied_at) }} ({{ form.applied_by }}) — только просмотр.</span>
        </div>

        <fieldset class="pinv-block" :disabled="readonly">
          <legend>Инвойс, валюта прихода и курсы НБ РБ</legend>
          <div class="pinv-grid">
            <label>№ инвойса<input v-model="form.number" type="text" maxlength="80" /></label>
            <label>Дата инвойса<input v-model="form.invoice_date" type="date" /></label>
            <label title="По этой дате берутся курсы НБ РБ">Дата прихода<input v-model="form.arrival_date" type="date" @change="onArrivalDate" /></label>
            <label>Поставщик<input v-model="form.supplier" type="text" maxlength="200" /></label>
            <label>Контракт<input v-model="form.contract" type="text" maxlength="200" /></label>
            <label>Валюта прихода
              <select v-model="form.currency" @change="onCurrencyChange">
                <option v-for="c in CURRENCIES" :key="c.code" :value="c.code">{{ c.code }} — {{ c.name }}</option>
              </select>
            </label>
            <label :title="`Сколько бел. рублей в 1 ${form.currency}`">Курс {{ form.currency }} → BYN<input v-model="form.cur_rate" type="number" step="0.00000001" min="0" :disabled="form.currency === 'BYN'" /></label>
            <label title="Им итоговая себестоимость пересчитывается обратно в доллары">Курс USD → BYN<input v-model="form.usd_rate" type="number" step="0.00000001" min="0" /></label>
            <label>Дата курсов<input v-model="form.rate_date" type="date" @change="loadRates(true)" /></label>
          </div>
          <div class="pinv-rates">
            <button class="btn btn-ghost btn-sm" type="button" :disabled="ratesLoading || readonly" @click="loadRates(true)">
              <Icon name="lucide:banknote" /> {{ ratesLoading ? 'Курсы…' : 'Подтянуть курсы НБ РБ' }}
            </button>
            <span v-if="rates" class="muted pinv-hint">
              НБ РБ на {{ fmtDate(ratesDate) }}:
              <span v-for="c in CURRENCIES.filter((c) => c.code !== 'BYN')" :key="c.code" class="pinv-rate">
                <span class="mono">{{ c.code }}</span> {{ rates[c.code] ?? '—' }}
              </span>
            </span>
            <span v-if="ratesStale" class="pinv-warn">⚠ на эту дату курсов нет, взяты ближайшие предыдущие</span>
          </div>
          <div class="muted pinv-hint">Все затраты и цена поставщика приводятся к бел. рублю по курсам НБ РБ на дату прихода. Итоговая себестоимость пересчитывается в доллары по курсу USD той же даты. Курсы можно поправить руками — тогда источник считается ручным.</div>
        </fieldset>

        <fieldset class="pinv-block" :disabled="readonly">
          <legend>Накладные расходы на партию — у каждой статьи своя валюта и курс</legend>
          <div class="pinv-table-wrap">
            <table class="pinv-table pinv-oh">
              <thead>
                <tr>
                  <th>Статья</th><th>Код ТН ВЭД</th><th class="col-num">Сумма</th><th>Валюта</th>
                  <th class="col-num">Курс → BYN</th><th class="col-num">Сумма, BYN</th><th v-if="!readonly"></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(o, i) in form.overheads" :key="i" :class="{ 'oh-duty': o.kind === 'duty' }">
                  <td>
                    <select v-model="o.kind" class="cell" :disabled="o.kind === 'duty'">
                      <option v-for="k in OVERHEAD_KINDS" :key="k.key" :value="k.key">{{ k.label }}</option>
                    </select>
                  </td>
                  <td>
                    <input v-if="o.kind === 'duty'" v-model="o.hs_code" class="cell mono" type="text" readonly />
                    <span v-else class="muted">—</span>
                  </td>
                  <td><input v-model="o.amount" class="cell num" type="number" step="0.01" min="0" /></td>
                  <td>
                    <select v-model="o.currency" class="cell" @change="onOverheadCurrency(o)">
                      <option v-for="c in CURRENCIES" :key="c.code" :value="c.code">{{ c.code }}</option>
                    </select>
                  </td>
                  <td><input v-model="o.rate" class="cell num" type="number" step="0.00000001" min="0" :disabled="o.currency === 'BYN'" /></td>
                  <td class="col-num num strong">{{ fmt(ohByn(o)) }}</td>
                  <td v-if="!readonly">
                    <button v-if="o.kind !== 'duty'" class="btn-x" type="button" title="Убрать статью" @click="form.overheads.splice(i, 1)">✕</button>
                  </td>
                </tr>
                <tr v-if="!form.overheads.length"><td :colspan="readonly ? 6 : 7" class="muted center">Добавьте статьи расходов. Пошлина появится сама, когда в строках будут коды ТН ВЭД.</td></tr>
              </tbody>
              <tfoot v-if="form.overheads.length">
                <tr><td colspan="5" class="col-num muted">Итого накладных, BYN</td><td class="col-num num strong">{{ fmt(overheadTotalByn) }}</td><td v-if="!readonly"></td></tr>
              </tfoot>
            </table>
          </div>
          <div class="pinv-add" v-if="!readonly">
            <button v-for="k in OVERHEAD_KINDS.filter((k) => k.key !== 'duty')" :key="k.key"
                    class="btn btn-ghost btn-sm" type="button" @click="addOverhead(k.key)">+ {{ k.label.toLowerCase() }}</button>
            <span class="muted pinv-hint">Одну статью можно добавить дважды — например транспорт частью в рос. рублях, частью в долларах.</span>
          </div>
          <div class="muted pinv-hint">В хранении: Логистика = транспорт + СВХ, Таможня = пошлина + таможенный сбор, Сертификация как есть. Распределение — пропорционально стоимости строк в бел. рублях, пошлина — внутри своего кода ТН ВЭД.</div>
        </fieldset>

        <fieldset class="pinv-block" :disabled="readonly">
          <legend>Строки прихода</legend>
          <div class="pinv-add" v-if="!readonly">
            <input v-model="searchPlan" type="text" class="pinv-search pinv-search--sm" placeholder="№ плана" />
            <input v-model="searchModel" type="text" class="pinv-search pinv-search--sm" placeholder="Модель" />
            <input v-model="searchArticul" type="text" class="pinv-search pinv-search--sm" placeholder="Артикул" />
            <span class="muted pinv-hint">{{ searchActive ? `найдено ${searchHits.length}` : 'введите план, модель или артикул' }}</span>
            <button class="btn btn-ghost btn-sm" type="button" :disabled="!visiblePurchaseRows.length" @click="addRows(visiblePurchaseRows)" :title="'Добавить все закупные КПСС из текущей выборки таблицы (' + visiblePurchaseRows.length + ')'">
              + все из выборки ({{ visiblePurchaseRows.length }})
            </button>
            <button class="btn btn-ghost btn-sm" type="button" @click="addEmptyLine">+ пустая строка</button>
            <button class="btn btn-ghost btn-sm" type="button" :disabled="!form.lines.length || hsLoading" @click="fetchHsCodes(true)" title="Заполнить коды ТН ВЭД из справочника S_MODELI по модели и артикулу (перезаписывает введённые)">
              {{ hsLoading ? 'Коды…' : '⟳ коды ТН ВЭД' }}
            </button>
          </div>
          <div v-if="!readonly && searchActive" class="pinv-hits-wrap">
            <div v-if="!searchHits.length" class="muted pinv-hint">Ничего не найдено среди загруженных закупных КПСС. Проверьте фильтры и период в таблице.</div>
            <template v-else>
              <table class="pinv-table pinv-hits-table">
                <thead>
                  <tr>
                    <th><input type="checkbox" :checked="allHitsChecked" @change="toggleAllHits(($event.target as HTMLInputElement).checked)" title="Выбрать все найденные" /></th>
                    <th>План</th><th>Модель</th><th>Артикул</th><th>Наименование</th><th>Цвет</th><th>Задание</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="r in searchHits" :key="rowKey(r)" @click="toggleHit(rowKey(r))">
                    <td><input type="checkbox" :checked="hitChecked.has(rowKey(r))" @click.stop @change="toggleHit(rowKey(r))" /></td>
                    <td class="mono">{{ r['PLAN_ID'] || '—' }}</td>
                    <td class="mono">{{ r['Модель'] }}</td>
                    <td class="mono">{{ r['Артикул'] }}</td>
                    <td class="ellipsis" :title="r['Наименование модели'] || ''">{{ r['Наименование модели'] || '—' }}</td>
                    <td class="ellipsis">{{ r['color'] || '—' }}</td>
                    <td class="mono">{{ r['Номер задания производства'] || '—' }}</td>
                  </tr>
                </tbody>
              </table>
              <div class="pinv-add">
                <button class="btn btn-primary btn-sm" type="button" :disabled="!hitChecked.size" @click="addChecked">Добавить выбранные ({{ hitChecked.size }})</button>
                <button class="btn btn-ghost btn-sm" type="button" @click="addRows(searchHits); clearSearch()">Добавить все найденные ({{ searchHits.length }})</button>
                <button class="btn btn-ghost btn-sm" type="button" @click="clearSearch">Очистить поиск</button>
              </div>
            </template>
          </div>
          <div class="pinv-table-wrap">
            <table class="pinv-table pinv-lines">
              <thead>
                <tr>
                  <th>#</th><th>Модель</th><th>Артикул</th><th>Наименование</th><th>Цвет</th><th>План</th><th>Задание</th>
                  <th>Код ТН ВЭД</th><th class="col-num">Кол-во</th><th class="col-num">Цена, {{ form.currency }}</th>
                  <th class="col-num">Цена, BYN</th><th class="col-num">Логистика</th><th class="col-num">Таможня</th><th class="col-num">Сертиф.</th>
                  <th class="col-num">Итого, BYN</th><th class="col-num">Итого, $</th><th v-if="!readonly"></th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!form.lines.length"><td :colspan="readonly ? 16 : 17" class="muted center">Добавьте строки: из таблицы по поиску, все из выборки или пустую</td></tr>
                <tr v-for="(ln, i) in form.lines" :key="i">
                  <td class="muted">{{ i + 1 }}</td>
                  <td><input v-model="ln.model" class="cell mono" type="text" /></td>
                  <td><input v-model="ln.articul" class="cell mono" type="text" /></td>
                  <td><input v-model="ln.name" class="cell wide" type="text" /></td>
                  <td><input v-model="ln.color" class="cell" type="text" /></td>
                  <td><input v-model="ln.plan_id" class="cell mono narrow" type="text" /></td>
                  <td><input v-model="ln.task_number" class="cell mono" type="text" /></td>
                  <td><input v-model="ln.hs_code" class="cell mono" type="text" placeholder="ТН ВЭД" title="Код ТН ВЭД: подтягивается из S_MODELI, можно поправить" /></td>
                  <td><input v-model="ln.qty" class="cell num narrow" type="number" step="1" min="0" /></td>
                  <td><input v-model="ln.unit_price_cur" class="cell num narrow" type="number" step="0.0001" min="0" /></td>
                  <td class="col-num num">{{ fmt(calcLine(i)?.price_byn) }}</td>
                  <td class="col-num num">{{ fmt(calcLine(i)?.logistics_byn) }}</td>
                  <td class="col-num num" :title="calcLine(i) ? `пошлина ${fmt(calcLine(i).duty_byn)} + сбор ${fmt(calcLine(i).customs_fee_byn)}` : ''">{{ fmt(calcLine(i)?.customs_byn) }}</td>
                  <td class="col-num num">{{ fmt(calcLine(i)?.cert_byn) }}</td>
                  <td class="col-num num strong">{{ fmt(calcLine(i)?.total_byn) }}</td>
                  <td class="col-num num strong">{{ fmt(calcLine(i)?.total_usd) }}</td>
                  <td v-if="!readonly"><button class="btn-x" type="button" title="Убрать строку" @click="form.lines.splice(i, 1)">✕</button></td>
                </tr>
              </tbody>
            </table>
          </div>
        </fieldset>

        <div v-if="preview" class="pinv-totals">
          <div><span class="muted">Стоимость строк:</span> <b>{{ fmt(preview.totals.sum_byn) }} BYN</b>
            <template v-if="preview.totals.sum_usd"> ({{ fmt(preview.totals.sum_usd) }} $)</template> · {{ fmt(preview.totals.qty) }} шт</div>
          <div><span class="muted">Накладные, BYN:</span>
            транспорт {{ fmt(preview.totals.overhead_byn.transport) }} ·
            СВХ {{ fmt(preview.totals.overhead_byn.svh) }} ·
            пошлина {{ fmt(preview.totals.overhead_byn.duty) }} ·
            сбор {{ fmt(preview.totals.overhead_byn.customs_fee) }} ·
            сертификация {{ fmt(preview.totals.overhead_byn.cert) }}
            <b> = {{ fmt(preview.totals.overhead_total_byn) }}</b>
          </div>
          <div><span class="muted">Доли пошлины:</span>
            <span v-for="(v, k) in preview.totals.duty_share_pct" :key="k" class="pinv-share"><span class="mono">{{ k }}</span> {{ v }} %</span>
          </div>
          <div><span class="muted">Итого себестоимость партии:</span> <b>{{ fmt(preview.totals.total_byn) }} BYN</b>
            <template v-if="preview.totals.total_usd"> = {{ fmt(preview.totals.total_usd) }} $</template></div>
          <div v-for="w in preview.totals.warnings" :key="w" class="pinv-warn">⚠ {{ w }}</div>
        </div>
        <div v-if="error" class="pinv-error">{{ error }}</div>

        <div class="pinv-actions">
          <button class="btn btn-ghost" type="button" :disabled="busy || readonly" @click="runPreview">
            <Icon name="lucide:calculator" /> Распределить
          </button>
          <button class="btn btn-ghost" type="button" :disabled="busy || readonly" @click="save">
            <Icon name="lucide:save" /> Сохранить черновик
          </button>
          <button class="btn btn-primary" type="button" :disabled="busy || readonly" @click="applyInvoice">
            <Icon name="lucide:check-circle" /> {{ busy ? 'Выполняю…' : 'Применить: создать ПФКСС' }}
          </button>
          <button v-if="form.id && !readonly" class="btn btn-ghost" type="button" :disabled="busy" @click="removeDraft">
            <Icon name="lucide:trash-2" /> Удалить черновик
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted } from 'vue'

// Модалка прихода закупной готовой продукции (пожелание № 7, этап 2).
//
// Валютная логика — решение заказчика 04.09.2026: у прихода своя валюта, у
// КАЖДОЙ статьи накладных своя, все курсы берутся к БЕЛ. РУБЛЮ из НБ РБ на
// дату прихода, распределение считается в рублях, а итог пересчитывается
// обратно в доллары по курсу USD той же даты. Считает сервер
// (POST /purchase/invoice/preview), здесь только ввод и показ.

const props = defineProps<{
  apiBase: string
  headers: Record<string, string>
  allRows: any[]       // все загруженные строки таблицы — для поиска
  visibleRows: any[]   // отфильтрованные — для «добавить все из выборки»
  canEdit: boolean
  preselected?: any[]  // строки, с которых открыли модалку
}>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'applied', inv: any): void }>()

const CURRENCIES = [
  { code: 'BYN', name: 'бел. рубль' },
  { code: 'USD', name: 'доллар США' },
  { code: 'EUR', name: 'евро' },
  { code: 'RUB', name: 'рос. рубль' },
  { code: 'KZT', name: 'тенге' },
  { code: 'UZS', name: 'узб. сум' },
  { code: 'CNY', name: 'юань' },
]
const OVERHEAD_KINDS = [
  { key: 'transport', label: 'Транспорт' },
  { key: 'svh', label: 'СВХ' },
  { key: 'customs_fee', label: 'Таможенный сбор' },
  { key: 'cert', label: 'Сертификация' },
  { key: 'duty', label: 'Таможенная пошлина' },
]

const view = ref<'list' | 'edit'>('list')
const invoices = ref<any[]>([])
const listLoading = ref(false)
const listError = ref('')
const busy = ref(false)
const error = ref('')
const preview = ref<any>(null)
const rates = ref<Record<string, number> | null>(null)
const ratesDate = ref('')
const ratesStale = ref(false)
const ratesLoading = ref(false)

const emptyForm = () => ({
  id: null as number | null, status: 'draft', number: '', invoice_date: '', arrival_date: '', contract: '',
  supplier: '', comment: '', currency: 'USD', cur_rate: '' as number | string, usd_rate: '' as number | string,
  rate_date: '', rates_source: 'nbrb',
  overheads: [] as any[], lines: [] as any[], applied_at: null as string | null, applied_by: null as string | null,
})
const form = reactive<any>(emptyForm())
const readonly = computed(() => !props.canEdit || form.status === 'applied')

const num = (v: any) => (v === '' || v == null ? null : Number(v))
const ohByn = (o: any) => {
  const a = num(o.amount), r = o.currency === 'BYN' ? 1 : num(o.rate)
  return a == null || r == null ? null : a * r
}
const overheadTotalByn = computed(() => form.overheads.reduce((s: number, o: any) => s + (ohByn(o) || 0), 0))

// ── Пошлина по кодам ТН ВЭД: строки статей держим в соответствии со строками
// прихода. Появился новый код — добавляем статью, код исчез и сумма нулевая —
// убираем; заполненную не трогаем, чтобы не потерять введённое.
const normHs = (v: any) => String(v || '').replace(/\s+/g, '')
const hsGroups = computed(() => {
  const m = new Map<string, { key: string; label: string; count: number }>()
  for (const ln of form.lines) {
    const key = normHs(ln.hs_code)
    const g = m.get(key) || { key, label: String(ln.hs_code || '').trim() || 'без кода', count: 0 }
    g.count += 1
    m.set(key, g)
  }
  return [...m.values()]
})
function syncDutyRows() {
  if (readonly.value) return
  const groups = hsGroups.value
  const wanted = new Set(groups.map((g) => g.key))
  for (const g of groups) {
    if (!form.overheads.some((o: any) => o.kind === 'duty' && normHs(o.hs_code) === g.key)) {
      form.overheads.push({ kind: 'duty', hs_code: g.label === 'без кода' ? '' : g.label, amount: '', currency: defaultOverheadCurrency(), rate: rateFor(defaultOverheadCurrency()) })
    }
  }
  for (let i = form.overheads.length - 1; i >= 0; i--) {
    const o = form.overheads[i]
    if (o.kind === 'duty' && !wanted.has(normHs(o.hs_code)) && !num(o.amount)) form.overheads.splice(i, 1)
  }
}
watch(hsGroups, syncDutyRows, { deep: true })

function defaultOverheadCurrency() { return 'BYN' }
function rateFor(code: string): number | string {
  if (code === 'BYN') return 1
  return rates.value?.[code] ?? ''
}
function addOverhead(kind: string) {
  const cur = defaultOverheadCurrency()
  form.overheads.push({ kind, hs_code: '', amount: '', currency: cur, rate: rateFor(cur) })
  preview.value = null
}
function onOverheadCurrency(o: any) {
  o.rate = rateFor(o.currency)
  preview.value = null
}
function onCurrencyChange() {
  form.cur_rate = form.currency === 'BYN' ? 1 : rateFor(form.currency)
  preview.value = null
}
function onArrivalDate() {
  if (!form.rate_date || form.rates_source === 'nbrb') form.rate_date = form.arrival_date
  loadRates(true)
}

/** Курсы НБ РБ на дату курсов: заполняем курс прихода, курс доллара и курсы
 *  всех статей по их валютам. Пустые/нулевые курсы не оставляем. */
async function loadRates(apply = false) {
  const d = form.rate_date || form.arrival_date || form.invoice_date
  if (!d) { error.value = 'Укажите дату прихода — по ней берутся курсы НБ РБ.'; return }
  ratesLoading.value = true
  error.value = ''
  try {
    const res = await $fetch<any>(`${props.apiBase}/api/cost/purchase/rates?date=${encodeURIComponent(d)}`, { headers: props.headers })
    rates.value = res?.rates || null
    ratesDate.value = Object.values(res?.as_of || {})[0] as string || d
    ratesStale.value = !!ratesDate.value && ratesDate.value !== d
    if (apply && rates.value) {
      form.rate_date = d
      form.rates_source = 'nbrb'
      form.cur_rate = form.currency === 'BYN' ? 1 : (rates.value[form.currency] ?? form.cur_rate)
      form.usd_rate = rates.value['USD'] ?? form.usd_rate
      for (const o of form.overheads) o.rate = rateFor(o.currency)
      preview.value = null
    }
  } catch (e: any) {
    error.value = 'Курсы НБ РБ не получены: ' + (e?.data?.detail || e?.message || String(e))
  } finally { ratesLoading.value = false }
}

// ── Поиск закупных КПСС: три поля «И» ───────────────────────────────────────
const isPurchaseKpss = (r: any) => r.purchase_source && String(r['Признак калькуляции'] || '').trim() === 'КПСС'
const rowKey = (r: any) => [r['Модель'], r['Артикул'], r['PLAN_ID'], r['Номер задания производства']].map((v) => String(v || '').trim()).join('|')
const visiblePurchaseRows = computed(() => props.visibleRows.filter(isPurchaseKpss))
const searchPlan = ref('')
const searchModel = ref('')
const searchArticul = ref('')
const searchActive = computed(() => !!(searchPlan.value.trim() || searchModel.value.trim() || searchArticul.value.trim()))
const lineKeys = computed(() => new Set(form.lines.map((l: any) => [l.model, l.articul, l.plan_id, l.task_number].map((v: any) => String(v || '').trim()).join('|'))))
const searchHits = computed(() => {
  if (!searchActive.value) return []
  const p = searchPlan.value.trim().toLowerCase()
  const m = searchModel.value.trim().toLowerCase()
  const a = searchArticul.value.trim().toLowerCase()
  const has = (v: any, q: string) => !q || String(v || '').toLowerCase().includes(q)
  return props.allRows.filter(isPurchaseKpss)
    .filter((r) => has(r['PLAN_ID'], p) && has(r['Модель'], m) && has(r['Артикул'], a))
    .filter((r) => !lineKeys.value.has(rowKey(r)))
    .slice(0, 300)
})
const hitChecked = ref<Set<string>>(new Set())
const allHitsChecked = computed(() => searchHits.value.length > 0 && searchHits.value.every((r) => hitChecked.value.has(rowKey(r))))
function toggleHit(key: string) {
  const s = new Set(hitChecked.value)
  if (s.has(key)) s.delete(key); else s.add(key)
  hitChecked.value = s
}
function toggleAllHits(on: boolean) { hitChecked.value = on ? new Set(searchHits.value.map(rowKey)) : new Set() }
function addChecked() {
  addRows(searchHits.value.filter((r) => hitChecked.value.has(rowKey(r))))
  hitChecked.value = new Set()
}
function clearSearch() {
  searchPlan.value = ''; searchModel.value = ''; searchArticul.value = ''
  hitChecked.value = new Set()
}

// ── Коды ТН ВЭД из справочника S_MODELI ─────────────────────────────────────
const hsLoading = ref(false)
async function fetchHsCodes(overwrite = false) {
  const targets = form.lines.filter((l: any) => l.model && l.articul && (overwrite || !String(l.hs_code || '').trim()))
  if (!targets.length) return
  hsLoading.value = true
  try {
    const res = await $fetch<any>(`${props.apiBase}/api/cost/purchase/hs-codes`, {
      method: 'POST', headers: props.headers,
      body: { pairs: targets.map((l: any) => ({ model: l.model, articul: l.articul })) },
    })
    const codes = res?.codes || {}
    for (const l of targets) {
      const c = codes[`${String(l.model).trim()}|${String(l.articul).trim()}`]
      if (c) l.hs_code = c
    }
  } catch (e: any) {
    error.value = 'Коды ТН ВЭД не подтянулись: ' + (e?.data?.detail || e?.message || String(e))
  } finally { hsLoading.value = false }
}

function lineFromRow(r: any) {
  return {
    model: String(r['Модель'] || '').trim(), articul: String(r['Артикул'] || '').trim(),
    plan_id: String(r['PLAN_ID'] || '').trim(), task_number: String(r['Номер задания производства'] || '').trim(),
    name: r['Наименование модели'] || '', color: r['color'] || '', hs_code: '',
    // Количество прихода — из инвойса, не из плана.
    qty: '', unit_price_cur: '',
  }
}
function addRows(rows: any[]) {
  const have = new Set(form.lines.map((l: any) => [l.model, l.articul, l.plan_id, l.task_number].join('|')))
  let added = 0
  for (const r of rows) {
    const ln = lineFromRow(r)
    const k = [ln.model, ln.articul, ln.plan_id, ln.task_number].join('|')
    if (have.has(k)) continue
    have.add(k)
    form.lines.push(ln)
    added += 1
  }
  preview.value = null
  if (added) fetchHsCodes(false)
}
function addEmptyLine() {
  form.lines.push({ model: '', articul: '', plan_id: '', task_number: '', name: '', color: '', hs_code: '', qty: '', unit_price_cur: '' })
}

function calcLine(i: number) { return preview.value?.lines?.[i] ?? null }

function payload() {
  return {
    id: form.id, number: form.number, invoice_date: form.invoice_date || null,
    arrival_date: form.arrival_date || null, rate_date: form.rate_date || form.arrival_date || null,
    contract: form.contract, supplier: form.supplier, comment: form.comment,
    currency: form.currency, cur_rate: num(form.cur_rate), usd_rate: num(form.usd_rate),
    rates_source: form.rates_source,
    overheads: form.overheads
      .filter((o: any) => num(o.amount))
      .map((o: any) => ({ kind: o.kind, hs_code: o.hs_code || '', amount: num(o.amount), currency: o.currency, rate: num(o.rate) })),
    lines: form.lines.map((l: any) => ({ ...l, qty: num(l.qty), unit_price_cur: num(l.unit_price_cur) })),
  }
}

async function runPreview() {
  error.value = ''
  busy.value = true
  try {
    preview.value = await $fetch<any>(`${props.apiBase}/api/cost/purchase/invoice/preview`, { method: 'POST', headers: props.headers, body: payload() })
  } catch (e: any) {
    preview.value = null
    error.value = e?.data?.detail || e?.message || String(e)
  } finally { busy.value = false }
}

function loadForm(inv: any) {
  Object.assign(form, emptyForm(), inv, {
    invoice_date: inv.invoice_date ? String(inv.invoice_date).slice(0, 10) : '',
    arrival_date: inv.arrival_date ? String(inv.arrival_date).slice(0, 10) : '',
    rate_date: inv.rate_date ? String(inv.rate_date).slice(0, 10) : '',
    overheads: (inv.overheads || []).map((o: any) => ({ ...o })),
    lines: (inv.lines || []).map((l: any) => ({ ...l })),
  })
  preview.value = inv.lines?.length && inv.totals?.sum_byn != null ? { lines: inv.lines, totals: inv.totals } : null
}

async function save(): Promise<boolean> {
  error.value = ''
  busy.value = true
  try {
    const inv = await $fetch<any>(`${props.apiBase}/api/cost/purchase/invoice`, { method: 'POST', headers: props.headers, body: payload() })
    loadForm(inv)
    await loadList()
    return true
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || String(e)
    return false
  } finally { busy.value = false }
}

async function applyInvoice() {
  if (!(await save())) return
  const t = preview.value?.totals
  if (!confirm(
    `Применить инвойс № ${form.number}?\n\n` +
    `Строк: ${form.lines.length}, стоимость ${fmt(t?.sum_byn)} BYN, себестоимость партии ${fmt(t?.total_byn)} BYN` +
    (t?.total_usd ? ` (${fmt(t.total_usd)} $)` : '') + '.\n' +
    'По каждой строке будет создана калькуляция ПФКСС с итогом в «Основных материалах» и раскладкой ' +
    '(логистика, таможня, сертификация). После применения инвойс не редактируется.'
  )) return
  busy.value = true
  error.value = ''
  try {
    const inv = await $fetch<any>(`${props.apiBase}/api/cost/purchase/invoice/${form.id}/apply`, { method: 'POST', headers: props.headers })
    loadForm(inv)
    await loadList()
    emit('applied', inv)
    alert(`Создано калькуляций ПФКСС: ${inv.applied?.created ?? 0}, обновлено: ${inv.applied?.updated ?? 0}.\nНажмите «Загрузить данные», чтобы увидеть их в таблице.`)
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || String(e)
  } finally { busy.value = false }
}

async function removeDraft() {
  if (!form.id || !confirm(`Удалить черновик инвойса № ${form.number}?`)) return
  busy.value = true
  try {
    await $fetch(`${props.apiBase}/api/cost/purchase/invoice/${form.id}`, { method: 'DELETE', headers: props.headers })
    await loadList()
    view.value = 'list'
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || String(e)
  } finally { busy.value = false }
}

async function loadList() {
  listLoading.value = true
  listError.value = ''
  try {
    const res = await $fetch<any>(`${props.apiBase}/api/cost/purchase/invoices`, { headers: props.headers })
    invoices.value = res?.items || []
  } catch (e: any) {
    listError.value = e?.data?.detail || e?.message || String(e)
  } finally { listLoading.value = false }
}

async function openInvoice(id: number) {
  error.value = ''
  busy.value = true
  try {
    loadForm(await $fetch<any>(`${props.apiBase}/api/cost/purchase/invoice/${id}`, { headers: props.headers }))
    view.value = 'edit'
  } catch (e: any) {
    listError.value = e?.data?.detail || e?.message || String(e)
  } finally { busy.value = false }
}

function newInvoice(rows: any[] = []) {
  Object.assign(form, emptyForm())
  // Дата прихода по умолчанию — сегодня: по ней сразу тянутся курсы НБ РБ.
  const today = new Date().toISOString().slice(0, 10)
  form.arrival_date = today
  form.rate_date = today
  preview.value = null
  error.value = ''
  // Базовые статьи: пошлина добавится сама по кодам ТН ВЭД строк.
  for (const k of ['transport', 'svh', 'customs_fee', 'cert']) addOverhead(k)
  if (rows.length) addRows(rows)
  view.value = 'edit'
  loadRates(true)
}

function fmt(v: any): string {
  if (v == null || v === '') return '—'
  const n = Number(v)
  return Number.isNaN(n) ? String(v) : n.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}
function fmtDate(v: any): string { return v ? new Date(v).toLocaleDateString('ru-RU') : '—' }
function fmtDateTime(v: any): string { return v ? new Date(v).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' }) : '' }

onMounted(async () => {
  await loadList()
  if (props.preselected?.length) newInvoice(props.preselected)
})
</script>

<style scoped>
.pinv-modal { width: min(1380px, calc(100vw - 32px)); max-height: calc(100vh - 32px); display: flex; flex-direction: column; background: var(--bg-surface, #fff); color: var(--text, #1f2937); border-radius: 10px; }
/* Шапка — своя: стили .modal-header страницы scoped и сюда не доходят. */
.pinv-modal .modal-header { display: flex; align-items: baseline; gap: 12px; padding: 12px 18px; border-bottom: 1px solid var(--border-color, #e5e7eb); font-weight: 600; }
.pinv-modal .modal-subtitle { font-weight: 400; color: var(--text-muted, #6b7280); font-size: 13px; }
.pinv-modal .modal-close { margin-left: auto; background: none; border: none; font-size: 16px; cursor: pointer; color: var(--text-muted, #6b7280); }
.pinv-body { padding: 14px 18px 18px; overflow: auto; display: flex; flex-direction: column; gap: 12px; }
.pinv-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.pinv-hint { font-size: 12px; }
.pinv-block { border: 1px solid var(--border-color, #e5e7eb); border-radius: 8px; padding: 10px 14px 12px; margin: 0; min-width: 0; display: flex; flex-direction: column; gap: 8px; }
.pinv-block legend { font-size: 12px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted, #6b7280); padding: 0 6px; }
.pinv-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(190px, 1fr)); gap: 8px 14px; }
.pinv-grid label { display: flex; flex-direction: column; gap: 3px; font-size: 12px; color: var(--text-muted, #6b7280); }
.pinv-grid input, .pinv-grid select { padding: 5px 8px; border: 1px solid var(--border-color, #d1d5db); border-radius: 4px; font-size: 13px; background: var(--bg-surface, #fff); color: inherit; font-variant-numeric: tabular-nums; }
.pinv-rates { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; }
.pinv-rate { margin-right: 10px; }
.pinv-add { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.pinv-search { padding: 6px 10px; border: 1px solid var(--border-color, #d1d5db); border-radius: 4px; font-size: 13px; }
.pinv-search--sm { flex: 0 1 180px; }
.pinv-hits-wrap { border: 1px solid var(--border-color, #d1d5db); border-radius: 6px; padding: 6px 8px; background: var(--bg-tonal, #f9fafb); display: flex; flex-direction: column; gap: 6px; }
.pinv-hits-table { max-height: 240px; display: block; overflow-y: auto; }
.pinv-hits-table thead, .pinv-hits-table tbody, .pinv-hits-table tr { display: table; width: 100%; table-layout: fixed; }
.pinv-hits-table tbody tr { cursor: pointer; }
.pinv-hits-table tbody tr:hover { background: var(--bg-surface, #fff); }
.pinv-hits-table th:first-child, .pinv-hits-table td:first-child { width: 32px; }
.pinv-table-wrap { overflow-x: auto; }
.pinv-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.pinv-table th { text-align: left; font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted, #6b7280); padding: 6px 8px; border-bottom: 1px solid var(--border-color, #e5e7eb); white-space: nowrap; }
.pinv-table td { padding: 4px 6px; border-bottom: 1px solid var(--border-color, #eef0f3); white-space: nowrap; vertical-align: middle; }
.pinv-table tfoot td { border-bottom: 0; border-top: 2px solid var(--border-color, #d1d5db); font-size: 12px; }
.pinv-oh tr.oh-duty td:first-child { color: var(--text-muted, #6b7280); }
.pinv-table input.cell, .pinv-table select.cell { width: 130px; padding: 4px 6px; border: 1px solid var(--border-color, #d1d5db); border-radius: 4px; font-size: 13px; background: var(--bg-surface, #fff); color: inherit; }
.pinv-table input.cell.wide { width: 200px; }
.pinv-table input.cell.narrow { width: 80px; }
.pinv-table input.cell.num { text-align: right; font-variant-numeric: tabular-nums; }
.pinv-table input.cell[readonly] { background: var(--bg-tonal, #f3f4f6); }
.col-num { text-align: right; }
.num { font-variant-numeric: tabular-nums; font-family: 'JetBrains Mono', ui-monospace, monospace; }
.mono { font-family: 'JetBrains Mono', ui-monospace, monospace; font-variant-numeric: tabular-nums; }
.strong { font-weight: 600; }
.ellipsis { max-width: 220px; overflow: hidden; text-overflow: ellipsis; }
.center { text-align: center; padding: 16px !important; }
.muted { color: var(--text-muted, #6b7280); }
.btn-x { background: none; border: none; cursor: pointer; color: var(--text-muted, #6b7280); font-size: 13px; }
.btn-x:hover { color: var(--neg, #dc2626); }
.pill { display: inline-block; padding: 1px 8px; border-radius: 999px; font-size: 12px; }
.pill-ok { background: color-mix(in srgb, var(--pos, #16a34a) 14%, transparent); color: var(--pos, #15803d); }
.pill-draft { background: color-mix(in srgb, #2563eb 12%, transparent); color: #1d4ed8; }
.pinv-totals { display: flex; flex-direction: column; gap: 4px; font-size: 13px; padding: 10px 14px; border-radius: 8px; background: var(--bg-tonal, #f3f4f6); }
.pinv-share { margin-right: 12px; }
.pinv-warn { color: var(--warn, #b76e00); }
.pinv-error { color: var(--neg, #dc2626); font-size: 13px; }
.pinv-actions { display: flex; flex-wrap: wrap; gap: 8px; }
</style>
