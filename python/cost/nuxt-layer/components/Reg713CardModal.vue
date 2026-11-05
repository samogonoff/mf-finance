<template>
  <!-- Окно карточки согласования по постановлению 713 — ОДНО на раздел.
       Живёт в компоненте, а не в странице: карточку открывают из двух мест
       (главная таблица и отчёт «Согласование 713»), а форма у неё одна и
       правки в ней должны совпадать до последней подсказки. Стили окна здесь
       свои: стили страниц scoped и сюда не доходят (как у CostPurchaseInvoice). -->
  <div class="modal-overlay" @click.self="requestClose()">
    <div class="modal reg713-modal">
      <div class="modal-header">
        <span>Согласование по постановлению 713</span>
        <!-- Карточка одна на модель+артикул: признак и план в подзаголовке
             только сбивали бы с толку — решение действует на все. -->
        <span class="modal-subtitle">{{ model || '—' }} / {{ articul || '—' }}</span>
        <button class="modal-close" aria-label="Закрыть" @click="requestClose()">✕</button>
      </div>
      <div class="reg713-body">
        <div class="reg713-name muted">{{ name || '' }}</div>
        <!-- Цена изделия (задача Б24 666861): снимок карточки — выбор
             «Розничной наценки», как в главной таблице, фиксирует розницу и
             опт изделия; после «согласовано» эта цена показывается ПФКСС-строкам
             в главной таблице. Пока снимка нет — цены как в строке /aggregated. -->
        <div class="reg713-block">
          <div class="reg713-block-title">Цена изделия</div>
          <div class="muted reg713-hint">
            <template v-if="form.item_retail != null || form.item_wholesale != null">
              Снимок карточки<template v-if="form.item_markup"> (наценка {{ Number(form.item_markup).toFixed(1) }}%)</template>:
              розница <b>{{ form.item_retail != null ? fmt(form.item_retail) : '—' }}</b>, опт
              <b>{{ form.item_wholesale != null ? fmt(form.item_wholesale) : '—' }}</b>
            </template>
            <template v-else>
              Как в главной таблице: розница
              <b>{{ retail != null ? fmt(retail) : '—' }}</b>, опт
              <b>{{ wholesale != null ? fmt(wholesale) : '—' }}</b><template v-if="priceLevel">, уровень цен {{ priceLevel }}</template>.
            </template>
          </div>
          <div v-if="markupOptions.length" class="reg713-prices">
            <span class="reg713-label">Розничная наценка:</span>
            <select
              class="reg713-search-input"
              :value="form.item_markup || ''"
              :disabled="!canEdit"
              @click.stop
              @change="onMarkupSelect(($event.target as HTMLSelectElement).value)"
            >
              <option value="">—</option>
              <option v-for="opt in markupOptions" :key="opt.value" :value="opt.value">
                {{ opt.label }}
              </option>
            </select>
            <button v-if="form.item_markup || form.item_retail != null || form.item_wholesale != null"
                    class="btn btn-ghost btn-sm" type="button" :disabled="!canEdit"
                    @click="form.item_markup = null; form.item_retail = null; form.item_wholesale = null">
              сбросить
            </button>
          </div>
          <div v-else class="muted reg713-hint">Наценку выбрать нельзя: среди уровней цен нет розницы этой строки.</div>
        </div>

        <label class="reg713-check">
          <input type="checkbox" v-model="form.required" :disabled="!canEdit" />
          <span>Требуется согласование с исполкомом</span>
        </label>

        <div class="reg713-row">
          <span class="reg713-label">Вид:</span>
          <label class="reg713-radio"><input type="radio" value="price_increase" v-model="form.kind" :disabled="!canEdit" /> Повышение цены</label>
          <label class="reg713-radio"><input type="radio" value="novelty" v-model="form.kind" :disabled="!canEdit" /> Новинка</label>
          <button v-if="form.kind" class="btn btn-ghost btn-sm" type="button" :disabled="!canEdit" @click="form.kind = null">сбросить</button>
        </div>
        <div v-if="form.kind" class="muted reg713-hint">{{ KIND_HINT[form.kind] }}</div>

        <!-- Обоснование по виду (задача Б24 666631): колонки расчётного
             файла «Комментарий в чём новизна», «Планируемый объём
             реализации», «Производитель», «Розничная цена аналога других
             производителей». Обязательны при сохранении решения исполкома. -->
        <div v-if="form.kind === 'novelty'" class="reg713-block">
          <div class="reg713-block-title">Обоснование новинки</div>
          <textarea class="reg713-comment" v-model="form.novelty_comment" :disabled="!canEdit" rows="2" maxlength="1000" placeholder="В чём именно заключается новизна: новый вид сырья, новый состав сырья, усложнённая конструкция и т.п."></textarea>
          <div class="reg713-prices">
            <div class="reg713-price">
              <span class="reg713-label">План. объём (внутр. рынок):</span>
              <input type="number" step="1" min="0" v-model="form.planned_volume" :disabled="!canEdit" />
            </div>
          </div>
        </div>
        <div v-if="form.kind === 'price_increase'" class="reg713-block">
          <div class="reg713-block-title">Конкурент</div>
          <input type="text" class="reg713-doc" v-model="form.competitor_name" :disabled="!canEdit" placeholder="Наименование производителя-конкурента" maxlength="200" />
          <div class="reg713-prices">
            <div class="reg713-price">
              <span class="reg713-label">Розн. цена конкурента:</span>
              <input type="number" step="0.01" min="0" v-model="form.competitor_retail" :disabled="!canEdit" />
            </div>
          </div>
        </div>

        <div class="reg713-block">
          <div class="reg713-block-title">Артикул аналога</div>
          <div v-if="form.analog_articul" class="reg713-analog">
            <div>
              <b>{{ form.analog_articul }}</b> / {{ form.analog_model || '—' }}
              <span class="muted">{{ form.analog_name || '' }}</span>
            </div>
            <button class="btn btn-ghost btn-sm" type="button" :disabled="!canEdit" @click="clearAnalog">Сменить</button>
          </div>
          <div v-else class="reg713-search">
            <input
              type="text"
              class="reg713-search-input"
              v-model="query"
              :disabled="!canEdit"
              placeholder="Артикул, модель или наименование — от 2 символов"
              autocomplete="off"
              @input="onQueryInput"
              @keydown.esc.stop.prevent="results = []"
            />
            <div v-if="searching" class="muted reg713-hint">Ищу в справочнике…</div>
            <div v-else-if="query.trim().length >= 2 && !results.length && searched" class="muted reg713-hint">Ничего не найдено</div>
            <ul v-if="results.length" class="reg713-results">
              <li v-for="it in results" :key="it.item_id ?? (it.model + '|' + it.articul)" @click="pickAnalog(it)">
                <b>{{ it.articul }}</b> / {{ it.model }}
                <span class="muted">{{ it.name }}</span>
                <span v-if="it.archived" class="reg713-arch">архив</span>
              </li>
            </ul>
          </div>

          <div class="reg713-prices">
            <div class="reg713-price">
              <span class="reg713-label">Розница:</span>
              <input type="number" step="0.01" min="0" v-model="form.analog_retail" :disabled="!canEdit" @change="form.analog_price_source = 'manual'" />
            </div>
            <div class="reg713-price">
              <span class="reg713-label">Опт:</span>
              <input type="number" step="0.01" min="0" v-model="form.analog_wholesale" :disabled="!canEdit" @change="form.analog_price_source = 'manual'" />
            </div>
            <button class="btn btn-ghost btn-sm" type="button" :disabled="!form.analog_articul || pricesLoading || !canEdit" @click="fetchAnalogPrices" title="Подтянуть цены аналога заново: последняя калькуляция → утверждённая цена → плановая">
              {{ pricesLoading ? 'Загрузка…' : 'Обновить цены' }}
            </button>
          </div>
          <div class="muted reg713-hint">{{ sourceText }}</div>
        </div>

        <div class="reg713-block">
          <div class="reg713-block-title">Решение исполкома</div>
          <div class="reg713-row">
            <label class="reg713-radio"><input type="radio" :value="null" v-model="form.decision" :disabled="!canEdit" /> Не рассмотрено</label>
            <label class="reg713-radio"><input type="radio" value="approved" v-model="form.decision" :disabled="!canEdit" /> ✅ Согласовано</label>
            <label class="reg713-radio"><input type="radio" value="rejected" v-model="form.decision" :disabled="!canEdit" /> ❌ Не согласовано</label>
          </div>
          <input type="text" class="reg713-doc" v-model="form.decision_doc" :disabled="!canEdit" placeholder="№ и дата письма исполкома (если есть)" maxlength="200" />
          <textarea class="reg713-comment" v-model="form.comment" :disabled="!canEdit" rows="2" placeholder="Комментарий" maxlength="1000"></textarea>
        </div>

        <div v-if="updatedBy" class="muted reg713-hint">
          Изменено: {{ updatedBy }}
          {{ updatedAt ? new Date(updatedAt).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' }) : '' }}
          <template v-if="history.length"> · записей в истории: {{ history.length }}</template>
        </div>
        <div v-if="error" class="reg713-error">{{ error }}</div>
      </div>

      <div class="reg713-footer">
        <!-- Листание по текущей сортировке отчёта: в главной таблице nav не
             передаётся, и блок не показывается. Все выходы (стрелки, ✕,
             «Закрыть», Esc, подложка) идут через requestClose — он спрашивает
             про несохранённые правки и не пускает наружу во время запроса. -->
        <div v-if="nav" class="reg713-nav">
          <button class="btn btn-ghost btn-sm" type="button" :disabled="!nav.hasPrev || saving" @click="requestStep(-1)">← Предыдущая</button>
          <span class="muted reg713-pos">{{ nav.position }} из {{ nav.total }}</span>
          <button class="btn btn-ghost btn-sm" type="button" :disabled="!nav.hasNext || saving" @click="requestStep(1)">Следующая →</button>
        </div>
        <div class="reg713-actions">
          <button class="btn btn-primary" type="button" :disabled="!canEdit || saving" @click="save">
            {{ saving ? 'Сохранение…' : 'Сохранить' }}
          </button>
          <button class="btn btn-ghost" type="button" :disabled="saving" @click="requestClose()">Закрыть</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useCostPermission } from '~/composables/useCostPermission'

// Карточка согласования по постановлению 713 (пожелание № 8) — общее окно
// главной таблицы и отчёта «Согласование 713» (задача Б24 666648). Вынесено из
// pages/cost/index.vue: отчёт открывает его прямо у себя, чтобы бренд мог
// пройти все строки подряд, не прыгая в главную таблицу по deep-link'у.
//
// Форма плоская: карточка одна на модель+артикул (решение заказчика
// 03.09.2026), поэтому строки «плана/этапа/задания» в окно не передаются.

interface PriceLevel { name: string; price_type1: number; price_type3: number; price_type4: number; price_type5: number; price_type6: number }

const props = defineProps<{
  model: string
  articul: string
  name?: string | null
  retail?: number | null
  wholesale?: number | null
  priceLevel?: string | null
  /** Ставка НДС изделия — из `vat` строки отчёта / `avg_Ставка НДС` главной. */
  vat?: number | null
  /** Начальные поля карточки: required, kind, item_*, novelty_*, competitor_*,
   *  analog_*, decision* — как их отдаёт /reg713/card. */
  card?: Record<string, any> | null
  /** Справочник уровней цен; не передан — компонент загрузит сам. */
  priceLevels?: PriceLevel[] | null
  /** Листание: в главной таблице не передаётся, и навигация не показывается. */
  nav?: { hasPrev: boolean; hasNext: boolean; position: number; total: number } | null
}>()

const emit = defineEmits<{
  (e: 'saved', card: Record<string, any>): void
  (e: 'next'): void
  (e: 'prev'): void
  (e: 'close'): void
}>()

const KIND_HINT: Record<string, string> = {
  price_increase: 'Для «повышения цены» укажите наименование конкурента и его розничную цену — они обязательны для решения исполкома.',
  novelty: 'Для «новинки» опишите, в чём именно заключается новизна (новый вид сырья, новый состав сырья, усложнённая конструкция и т.п.), и укажите планируемый объём реализации — оба поля обязательны для решения исполкома.',
}
const SOURCE_LABEL: Record<string, string> = {
  calc: 'последняя калькуляция аналога в CostHistory',
  dwh: 'утверждённая цена аналога в DWH',
  gpartner: 'плановая цена из справочника S_MODELI',
  manual: 'введено вручную',
}

const config = useRuntimeConfig()
const apiBase = computed(() => (config.public.costOnly ? '' : ((config.public.apiBase as string) || '')))
const user = useState<any>('auth-user')
const fetchHeaders = computed(() => {
  const email = user.value?.email || ''
  return email ? { 'X-Cost-User': email } : {}
})
const { can } = useCostPermission()
const canEdit = computed(() => can('cost:reg713'))

const form = reactive<any>({
  required: false, kind: null as string | null,
  item_retail: null as number | string | null, item_wholesale: null as number | string | null,
  item_markup: null as string | null,
  novelty_comment: '', planned_volume: null as number | string | null,
  competitor_name: '', competitor_retail: null as number | string | null,
  analog_model: null as string | null, analog_articul: null as string | null, analog_name: null as string | null,
  analog_retail: null as number | string | null, analog_wholesale: null as number | string | null,
  analog_price_source: null as string | null,
  decision: null as string | null, decision_doc: '', comment: '',
})

/** Заполнить форму из карточки (ответ /reg713/card или поля reg713_* строки).
 *  Ключи — как у строки `cost_reg713` (без префикса). */
function fillFromCard(card: Record<string, any> | null | undefined) {
  const c = card || {}
  Object.assign(form, {
    required: !!c.required, kind: c.kind || null,
    item_retail: c.item_retail ?? null, item_wholesale: c.item_wholesale ?? null,
    item_markup: c.item_markup || null,
    novelty_comment: c.novelty_comment || '', planned_volume: c.planned_volume ?? null,
    competitor_name: c.competitor_name || '', competitor_retail: c.competitor_retail ?? null,
    analog_model: c.analog_model || null, analog_articul: c.analog_articul || null,
    analog_name: c.analog_name || null,
    analog_retail: c.analog_retail ?? null, analog_wholesale: c.analog_wholesale ?? null,
    analog_price_source: c.analog_price_source || null,
    decision: c.decision || null, decision_doc: c.decision_doc || '', comment: c.comment || '',
  })
  updatedBy.value = c.updated_by || ''
  updatedAt.value = c.updated_at || ''
}

/** Снимок формы сразу после заполнения — по нему считаем «есть несохранённые
 *  правки». Без этого молча терялись заполненные обоснования: уход со строки
 *  (стрелка, Esc, ✕, подложка) выбрасывал форму без единого вопроса. */
const savedSnapshot = ref('')

const dirty = computed(() => savedSnapshot.value !== JSON.stringify(form))

const updatedBy = ref('')
const updatedAt = ref('')
const history = ref<any[]>([])
const query = ref('')
const results = ref<any[]>([])
const searching = ref(false)
const searched = ref(false)
const pricesLoading = ref(false)
const saving = ref(false)
const error = ref('')
let searchTimer: ReturnType<typeof setTimeout> | null = null
let searchSeq = 0
let pricesSeq = 0
/** Номер открытия: ответы истории и поиска от прошлой строки не должны
 *  перебивать состояние текущей (листание открывает форму заново). */
let openSeq = 0

/** Выбор «Розничной наценки» для изделия — те же варианты, что в главной
 *  таблице (getMarkupOptions): уровни, чья розница совпала с ценой строки;
 *  выбор фиксирует опт уровня (задача Б24 666861). Формула — как в главной:
 *  наценка считается к опту без НДС. */
const markupOptions = computed(() => {
  const retailPrice = Number(props.retail)
  const avgVat = Number(props.vat || 0)
  if (!retailPrice || isNaN(retailPrice) || retailPrice <= 0) return [] as { value: string; label: string; retail: number; wholesale: number }[]
  const map = new Map<string, { value: string; label: string; retail: number; wholesale: number }>()
  for (const level of levels.value || []) {
    if (String(level.price_type3) !== String(retailPrice)) continue
    const w = Number(level.price_type1)
    if (!w || isNaN(w) || w <= 0) continue
    const pct = ((retailPrice / (100 + avgVat) * 100) / w - 1) * 100
    const value = pct.toFixed(2)
    if (!map.has(value)) map.set(value, { value, label: `${pct.toFixed(1)}%`, retail: retailPrice, wholesale: w })
  }
  return Array.from(map.values()).sort((a, b) => Number(a.value) - Number(b.value))
})

function onMarkupSelect(value: string) {
  const opt = markupOptions.value.find((o) => o.value === value)
  if (!opt) {
    form.item_markup = null
    form.item_retail = null
    form.item_wholesale = null
    return
  }
  form.item_markup = opt.value
  form.item_retail = opt.retail
  form.item_wholesale = opt.wholesale
}

// Справочник уровней цен: страница может передать свой уже загруженный,
// иначе грузим сами — окно не должно зависеть от того, кто его открыл.
const selfLevels = ref<PriceLevel[]>([])
const levels = computed<PriceLevel[]>(() => (props.priceLevels?.length ? props.priceLevels : selfLevels.value))

async function loadLevels() {
  if (props.priceLevels?.length) return
  try {
    selfLevels.value = await $fetch<PriceLevel[]>(`${apiBase.value}/api/cost/price-levels`, { headers: fetchHeaders.value })
  } catch (e: any) {
    console.error('[cost] price-levels load failed', e)
  }
}

const sourceText = computed(() => {
  if (!form.analog_articul) return 'Выберите аналог — цены подтянутся по правилам главной таблицы: последняя калькуляция, затем утверждённая цена, затем плановая.'
  const src = form.analog_price_source
  if (!src) return 'Цены аналога не найдены ни в калькуляциях, ни в справочнике — введите вручную или нажмите «Обновить цены».'
  return 'Источник цен: ' + (SOURCE_LABEL[src] || src) + '.'
})

function onQueryInput() {
  if (searchTimer) clearTimeout(searchTimer)
  const q = query.value.trim()
  searched.value = false
  if (q.length < 2) { results.value = []; return }
  searchTimer = setTimeout(async () => {
    const seq = ++searchSeq
    searching.value = true
    try {
      const res = await $fetch<any>(`${apiBase.value}/api/cost/reg713/articuls?q=${encodeURIComponent(q)}&limit=30`, { headers: fetchHeaders.value })
      if (seq !== searchSeq) return // пришёл ответ на устаревший запрос
      results.value = res?.items || []
      searched.value = true
    } catch (e: any) {
      if (seq === searchSeq) { results.value = []; error.value = e?.data?.detail || e?.message || String(e) }
    } finally {
      if (seq === searchSeq) searching.value = false
    }
  }, 350)
}

async function pickAnalog(it: any) {
  form.analog_model = it.model
  form.analog_articul = it.articul
  form.analog_name = it.name
  form.analog_retail = null
  form.analog_wholesale = null
  form.analog_price_source = null
  results.value = []
  query.value = ''
  await fetchAnalogPrices()
}

function clearAnalog() {
  form.analog_model = null
  form.analog_articul = null
  form.analog_name = null
  form.analog_retail = null
  form.analog_wholesale = null
  form.analog_price_source = null
}

async function fetchAnalogPrices() {
  if (!form.analog_articul) return
  // seq-защита как у поиска: два быстрых клика по разным аналогам — ответ
  // первого не должен перезаписать цены второго (ревью 666648).
  const seq = ++pricesSeq
  pricesLoading.value = true
  error.value = ''
  try {
    const q = new URLSearchParams({ model: form.analog_model || '', articul: form.analog_articul }).toString()
    const res = await $fetch<any>(`${apiBase.value}/api/cost/reg713/analog-prices?${q}`, { headers: fetchHeaders.value })
    if (seq !== pricesSeq) return // пришёл ответ на устаревший аналог
    form.analog_retail = res?.retail ?? null
    form.analog_wholesale = res?.wholesale ?? null
    form.analog_price_source = res?.source || null
    if (res?.name && !form.analog_name) form.analog_name = res.name
  } catch (e: any) {
    if (seq === pricesSeq) error.value = 'Не удалось получить цены аналога: ' + (e?.data?.detail || e?.message || String(e))
  } finally {
    if (seq === pricesSeq) pricesLoading.value = false
  }
}

/** Сохранение карточки. Валидации и тело запроса — ровно как были на главной:
 *  сервер проверяет то же самое (KIND_REQUIRED в app/reg713.py), клиентские
 *  проверки нужны, чтобы не показывать 400 там, где можно подсказать сразу. */
async function save() {
  if (form.required && !form.kind) {
    error.value = 'Укажите вид: повышение цены или новинка.'
    return
  }
  // Обоснование по виду обязательно при сохранении решения исполкома.
  if (form.decision && form.kind === 'novelty'
      && (!String(form.novelty_comment || '').trim() || !Number(form.planned_volume))) {
    error.value = 'Для решения по «новинке» заполните, в чём именно новизна, и планируемый объём реализации.'
    return
  }
  if (form.decision && form.kind === 'price_increase'
      && (!String(form.competitor_name || '').trim() || !Number(form.competitor_retail))) {
    error.value = 'Для решения по «повышению цены» заполните наименование конкурента и его розничную цену.'
    return
  }
  saving.value = true
  error.value = ''
  try {
    const num = (v: any) => (v === '' || v == null ? null : Number(v))
    const res = await $fetch<any>(`${apiBase.value}/api/cost/reg713/card`, {
      method: 'POST', headers: fetchHeaders.value,
      body: {
        model: String(props.model || '').trim(), articul: String(props.articul || '').trim(),
        required: !!form.required, kind: form.kind || null,
        item_retail: num(form.item_retail), item_wholesale: num(form.item_wholesale),
        item_markup: form.item_markup || null,
        novelty_comment: form.novelty_comment || null, planned_volume: num(form.planned_volume),
        competitor_name: form.competitor_name || null, competitor_retail: num(form.competitor_retail),
        analog_model: form.analog_model, analog_articul: form.analog_articul, analog_name: form.analog_name,
        analog_retail: num(form.analog_retail), analog_wholesale: num(form.analog_wholesale),
        analog_price_source: form.analog_articul ? (form.analog_price_source || 'manual') : null,
        decision: form.decision || null, decision_doc: form.decision_doc || null, comment: form.comment || null,
      },
    })
    // Форма и база совпали — правок больше нет. Выход после сохранения идёт
    // через emit('saved'), подтверждение там не спрашивается (и не должно:
    // указание «после Сохранить — сразу к следующей строке»).
    savedSnapshot.value = JSON.stringify(form)
    emit('saved', res?.card || {})
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || String(e)
  } finally {
    saving.value = false
  }
}

function fmt(v: any): string {
  if (v == null || v === '') return '—'
  const n = Number(v)
  return Number.isNaN(n) ? String(v) : n.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

/** Заполнить форму начальной карточкой и подтянуть историю. Вызывается один
 *  раз при монтировании: другую строку открывает родитель новым `:key`, и
 *  компонент создаётся заново (см. Reg713CardModal в reg713.vue). */
function init() {
  openSeq++
  error.value = ''
  query.value = ''
  results.value = []
  searched.value = false
  history.value = []
  if (searchTimer) { clearTimeout(searchTimer); searchTimer = null }
  fillFromCard(props.card)
  savedSnapshot.value = JSON.stringify(form)
  loadHistory(openSeq)
}

async function loadHistory(seq: number) {
  // История — украшение окна, оно открывается сразу; ошибку не показываем.
  try {
    const q = new URLSearchParams({ model: String(props.model || '').trim(), articul: String(props.articul || '').trim() }).toString()
    const res = await $fetch<any>(`${apiBase.value}/api/cost/reg713/card?${q}`, { headers: fetchHeaders.value })
    if (seq === openSeq) history.value = res?.history || []
  } catch { /* история — украшение */ }
}

/** Единственная дверь наружу: «← / →», ✕, «Закрыть», Esc и клик по подложке.
 *
 *  Два запрета:
 *    • пока идёт сохранение — закрывать нельзя. Родитель хранит открытую строку
 *      (reg713Target в главной, cardRow в отчёте), и пришедший ответ `saved`
 *      применился бы к уже ДРУГОЙ строке.
 *    • при несохранённых правках — спрашиваем. Подтверждение не нужно после
 *      успешного save(): там выход идёт через emit('saved'), а не сюда. */
function requestClose(): boolean {
  if (saving.value) return false
  if (dirty.value && !confirm('Есть несохранённые изменения. Перейти без сохранения?')) return false
  emit('close')
  return true
}

/** Стрелки листания — тем же порядком, что и закрытие. */
function requestStep(delta: number) {
  if (saving.value) return
  if (dirty.value && !confirm('Есть несохранённые изменения. Перейти без сохранения?')) return
  emit(delta > 0 ? 'next' : 'prev')
}

/** Esc закрывает (с теми же запретами), Ctrl+Enter сохраняет — как в остальных
 *  окнах раздела. */
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.stopPropagation()
    if (requestClose()) e.preventDefault()
    return
  }
  if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') { e.preventDefault(); if (canEdit.value && !saving.value) save() }
}

onMounted(() => {
  init()
  loadLevels()
  document.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('keydown', onKeydown)
  if (searchTimer) clearTimeout(searchTimer)
})

// Родитель открывает другую строку через :key — компонент пересоздаётся, и
// форма заполняется заново. Отдельного метода «перерисоваться» не нужно.
</script>

<style scoped>
/* Окно повторяет вид прежней модалки главной таблицы (было .approval-modal +
   .reg713-modal): та же ширина, белая карточка, тело с прокруткой. Классы
   .modal-* определены на странице scoped и сюда не доходят — поэтому шапка,
   тело и подвал описаны здесь. Цвета — только токены дизайн-системы. */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  z-index: 10000;
  display: flex;
  justify-content: center;
  align-items: center;
}
.reg713-modal {
  width: 560px;
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 32px);
  display: flex;
  flex-direction: column;
  background: var(--bg-surface, #fff);
  color: var(--text, #1f2937);
  border-radius: var(--rd-3, 8px);
  border: 1px solid var(--border, #e5e7eb);
}
.reg713-modal .modal-header {
  display: flex;
  align-items: baseline;
  gap: 12px;
  padding: var(--sp-4, 12px) var(--sp-5, 16px);
  border-bottom: 1px solid var(--border, #e5e7eb);
  font-weight: 600;
  flex-shrink: 0;
}
.reg713-modal .modal-subtitle { font-weight: 400; color: var(--text-muted, #6b7280); font-size: 13px; }
.reg713-modal .modal-close { margin-left: auto; background: none; border: none; font-size: 16px; cursor: pointer; color: var(--text-muted, #6b7280); }
.reg713-modal .modal-close:hover { color: var(--text-strong, #111827); }
.reg713-body { padding: 16px; display: flex; flex-direction: column; gap: 10px; overflow: auto; flex: 1; }
.reg713-footer {
  padding: 12px 16px;
  border-top: 1px solid var(--border, #e5e7eb);
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  flex-shrink: 0;
}
.reg713-nav { display: flex; align-items: center; gap: 8px; }
.reg713-pos { font-size: 12px; white-space: nowrap; }
.reg713-actions { display: flex; gap: 8px; margin-left: auto; }
.reg713-name { font-size: 13px; margin-top: -4px; }
.reg713-check { display: flex; align-items: center; gap: 8px; font-weight: 500; cursor: pointer; }
.reg713-row { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; }
.reg713-radio { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; }
.reg713-label { font-weight: 500; color: var(--text, #374151); }
.reg713-block {
  border: 1px solid var(--border-color, #e5e7eb);
  border-radius: 8px;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.reg713-block-title { font-size: 12px; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted, #6b7280); }
.reg713-analog { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.reg713-search { position: relative; }
.reg713-search-input, .reg713-doc, .reg713-comment {
  width: 100%;
  box-sizing: border-box;
  padding: 6px 8px;
  border: 1px solid var(--border-color, #d1d5db);
  border-radius: 4px;
  font-size: 13px;
  font-family: inherit;
  background: var(--bg-surface, #fff);
  color: inherit;
}
.reg713-results {
  list-style: none;
  margin: 4px 0 0;
  padding: 0;
  max-height: 220px;
  overflow-y: auto;
  border: 1px solid var(--border-color, #d1d5db);
  border-radius: 4px;
  background: var(--bg-surface, #fff);
}
.reg713-results li { padding: 6px 8px; cursor: pointer; font-size: 13px; display: flex; gap: 6px; align-items: baseline; flex-wrap: wrap; }
.reg713-results li:hover { background: var(--bg-tonal, #f3f4f6); }
.reg713-arch { font-size: 11px; color: var(--text-muted, #6b7280); border: 1px solid var(--border-color, #d1d5db); border-radius: 999px; padding: 0 6px; }
.reg713-prices { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; }
.reg713-price { display: inline-flex; align-items: center; gap: 6px; }
.reg713-price input { width: 110px; padding: 4px 6px; border: 1px solid var(--border-color, #d1d5db); border-radius: 4px; font-variant-numeric: tabular-nums; background: var(--bg-surface, #fff); color: inherit; }
.reg713-hint { font-size: 12px; }
.reg713-error { color: var(--neg, #dc2626); font-size: 13px; }
.muted { color: var(--text-muted, #6b7280); }
</style>
