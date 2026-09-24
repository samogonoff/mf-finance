<template>
  <div class="modal-overlay" @click.self="$emit('close')">
    <!-- Без @click.stop: закрытие по фону делает @click.self на оверлее, а
         остановленный клик не доходил до document — и CostMultiSelect не
         закрывался по клику вне себя (его слушатель висит на document). -->
    <div class="modal-content gp-editor">
      <div class="modal-header">
        <h2>{{ isNew ? 'Новая цепочка поставки' : `Цепочка «${props.chain?.name}»` }}</h2>
        <button class="modal-close" aria-label="Закрыть" @click="$emit('close')">×</button>
      </div>

      <div class="gp-editor-body">
        <div v-if="error" class="cost-error">
          <Icon name="lucide:alert-triangle" />
          <div>
            <strong>Ошибка</strong>
            <p>{{ error }}</p>
          </div>
          <button class="cost-error-x" aria-label="Закрыть" @click="error = ''">×</button>
        </div>

        <!-- ── Общее ─────────────────────────────────────────────────────── -->
        <section class="gp-section">
          <div class="gp-grid-2">
            <label class="gp-field">
              <span>Название *</span>
              <input v-model="form.name" type="text" class="form-input" placeholder="Например, Носки → КЗ" />
            </label>
            <label class="gp-field">
              <span>Дата курса НБ РБ</span>
              <input v-model="form.rate_date" type="date" class="form-input" />
              <small class="muted">пусто — курс на сегодня; нужен только звеньям с валютой не BYN</small>
            </label>
          </div>

          <div class="gp-grid-2">
            <div class="gp-field">
              <span>Режим расчёта</span>
              <div class="gp-seg">
                <button type="button" :class="{ active: form.mode === 'cascade' }" @click="form.mode = 'cascade'">Каскад</button>
                <button type="button" :class="{ active: form.mode === 'base' }" @click="form.mode = 'base'">От базовой</button>
              </div>
              <small class="muted">{{ form.mode === 'cascade' ? '% каждого звена — от цены предыдущего звена' : 'все % — от базовой отпускной цены' }}</small>
            </div>
            <div class="gp-field">
              <span>Вес строк</span>
              <div class="gp-seg">
                <button type="button" :class="{ active: form.weight === 'unit' }" @click="form.weight = 'unit'">По калькуляциям</button>
                <button type="button" :class="{ active: form.weight === 'volume' }" @click="form.weight = 'volume'">По выпуску</button>
              </div>
              <small class="muted">{{ form.weight === 'unit' ? '1 строка = 1 единица' : '«выпуск шт» строки; строки без объёма не участвуют (есть в основном у ФКСС)' }}</small>
            </div>
          </div>

          <label class="gp-field">
            <span>Комментарий</span>
            <textarea v-model="form.comment" class="form-input gp-textarea" rows="2"></textarea>
          </label>
        </section>

        <!-- ── Ассортимент ───────────────────────────────────────────────── -->
        <section class="gp-section">
          <div class="gp-section-title">
            Ассортимент
            <span class="muted">— фильтры главной таблицы; пусто = весь ассортимент</span>
          </div>

          <div class="gp-filters" :class="{ 'is-busy': cascadeBusy }">
            <div v-for="f in FILTERS" :key="f.key" class="gp-filter" :class="{ locked: isLocked(f.key) }">
              <label>{{ f.label }}</label>
              <CostMultiSelect
                v-model="scopeSel[f.key]"
                :options="enrichOptions(f.key, filterOptions[f.key] || [])"
                :placeholder="isLocked(f.key) ? '—' : `Все · ${f.label.toLowerCase()}`"
                :disabled="isLocked(f.key)"
                @change="onFilterChange(f.key)"
              />
            </div>
          </div>

          <div class="gp-grid-2">
            <label class="gp-field">
              <span>Модели <small class="muted" v-if="modelList.length">({{ modelList.length }})</small></span>
              <textarea v-model="modelsText" class="form-input gp-textarea" rows="3" placeholder="107K-1916 430A-3839 …"></textarea>
              <small class="muted">через пробел, запятую или с новой строки</small>
            </label>
            <label class="gp-field">
              <span>Артикулы <small class="muted" v-if="articulList.length">({{ articulList.length }})</small></span>
              <textarea v-model="articulsText" class="form-input gp-textarea" rows="3" placeholder="B3-263430A …"></textarea>
              <small class="muted">через пробел, запятую или с новой строки</small>
            </label>
          </div>

          <div class="gp-row">
            <label class="gp-inline">Дата расчёта с
              <input v-model="dateFrom" type="date" class="form-input" />
            </label>
            <label class="gp-inline">по
              <input v-model="dateTo" type="date" class="form-input" />
            </label>
            <label class="gp-check" title="На ключ цены (модель, артикул, признак, план) брать строки только последней даты расчёта">
              <input v-model="latestOnly" type="checkbox" />
              <span>только последний расчёт</span>
            </label>
            <button class="btn btn-ghost btn-sm" :disabled="countBusy" @click="checkScope">
              <Icon :name="countBusy ? 'lucide:loader' : 'lucide:search'" :class="{ spinning: countBusy }" />
              Проверить ассортимент
            </button>
            <span v-if="scopeCount" class="gp-count">{{ countText }}</span>
          </div>
        </section>

        <!-- ── Звенья ────────────────────────────────────────────────────── -->
        <section class="gp-section">
          <div class="gp-section-title">
            Звенья цепочки
            <span class="muted">— каждая компания продаёт следующей, последняя — внешнему рынку</span>
          </div>

          <div v-if="!links.length" class="muted gp-empty">Звеньев нет — добавьте хотя бы одно.</div>

          <div v-for="(l, i) in links" :key="l.key" class="gp-link-row">
            <span class="gp-link-seq">{{ i + 1 }}</span>
            <select v-model="l.company_id" class="form-input gp-company" @change="onCompanyChange(l)">
              <option :value="null" disabled>— компания —</option>
              <option v-for="c in companyOptions" :key="c.id" :value="c.id">
                {{ c.name }}{{ c.is_active ? '' : ' (неактивна)' }}
              </option>
            </select>
            <div class="gp-pct" title="+ наценка / − скидка к цене продажи этого звена">
              <input v-model.number="l.adjust_pct" type="number" step="0.1" class="form-input" />
              <span>%</span>
            </div>
            <select v-model="l.currency" class="form-input gp-cur" title="Валюта отгрузки звена — суммы дополнительно покажутся в ней по курсу НБ РБ">
              <option v-for="c in currencyOptions" :key="c.code" :value="c.code">{{ c.code }}</option>
            </select>
            <span class="gp-eff" title="Эффективный % цены продажи звена к базовой отпускной — хранится для выгрузки в 1С">
              к базовой <b>{{ fmtSignedPct(effPct(i)) }}</b>
            </span>
            <span class="gp-arrow muted">{{ i === links.length - 1 ? '→ внешний рынок' : '→ звено ' + (i + 2) }}</span>
            <div class="gp-link-btns">
              <button type="button" class="btn btn-ghost btn-xs" :disabled="i === 0" title="Выше" @click="moveLink(i, -1)">↑</button>
              <button type="button" class="btn btn-ghost btn-xs" :disabled="i === links.length - 1" title="Ниже" @click="moveLink(i, 1)">↓</button>
              <button type="button" class="btn btn-ghost btn-xs gp-danger" title="Убрать звено" @click="removeLink(i)">✕</button>
            </div>
          </div>

          <div class="gp-row">
            <button class="btn btn-ghost btn-sm" @click="addLink"><Icon name="lucide:plus" /> звено</button>
            <span class="muted gp-hint">+ наценка / − скидка. Без расходов звеньев и НДС — только цены.</span>
          </div>
        </section>

        <!-- ── Предпросмотр ──────────────────────────────────────────────── -->
        <section class="gp-section">
          <div class="gp-row">
            <button class="btn btn-ghost btn-sm" :disabled="previewBusy || !canPreview" :title="canPreview ? '' : 'Заполните звенья'" @click="runPreview">
              <Icon :name="previewBusy ? 'lucide:loader' : 'lucide:calculator'" :class="{ spinning: previewBusy }" />
              Предпросмотр
            </button>
            <span v-if="preview" class="muted gp-hint">
              {{ previewNote }}<template v-if="previewStale"> · параметры изменились — пересчитайте</template>
            </span>
          </div>

          <div v-if="preview" class="gp-tiles" :class="{ stale: previewStale }">
            <div class="gp-tile">
              <div class="gp-tile-label">Себестоимость Σ</div>
              <div class="gp-tile-value">{{ fmtMoney(preview.total?.cost_sum) }}</div>
            </div>
            <div class="gp-tile">
              <div class="gp-tile-label">Базовая отпускная Σ</div>
              <div class="gp-tile-value">{{ fmtMoney(preview.base?.base_sum) }}</div>
            </div>
            <div class="gp-tile">
              <div class="gp-tile-label">Конечная цена Σ</div>
              <div class="gp-tile-value">{{ fmtMoney(preview.total?.final_sum) }}</div>
            </div>
            <div class="gp-tile gp-tile-accent">
              <div class="gp-tile-label">Доход группы Σ</div>
              <div class="gp-tile-value" :class="signClass(preview.total?.income_sum)">{{ fmtMoney(preview.total?.income_sum) }}</div>
              <div class="gp-tile-meta muted">маржа {{ fmtPct1(preview.total?.margin_pct) }} · рентабельность {{ fmtPct1(preview.total?.markup_pct) }}</div>
            </div>
          </div>

          <table v-if="preview?.links?.length" class="gp-preview-links" :class="{ stale: previewStale }">
            <thead>
              <tr>
                <th>№</th><th>Компания</th><th class="num">% корр.</th><th class="num">% к базовой</th>
                <th class="num">Вход Σ</th><th class="num">Выход Σ</th><th class="num">Доход Σ</th><th class="num">Доход % к входу</th><th class="num">Доля</th>
                <th v-if="previewHasCurrency" class="num">В валюте</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="pl in preview.links" :key="pl.seq">
                <td>{{ pl.seq }}</td>
                <td>{{ pl.company_name }}</td>
                <td class="num">{{ fmtSignedPct(pl.adjust_pct) }}</td>
                <td class="num">{{ fmtSignedPct(pl.eff_pct) }}</td>
                <td class="num">{{ fmtMoney(pl.in_sum) }}</td>
                <td class="num">{{ fmtMoney(pl.out_sum) }}</td>
                <td class="num" :class="signClass(pl.income_sum)">{{ fmtMoney(pl.income_sum) }}</td>
                <td class="num">{{ fmtPct1(pl.income_pct_in) }}</td>
                <td class="num">{{ fmtPct1(pl.income_share) }}</td>
                <td v-if="previewHasCurrency" class="num">
                  <template v-if="pl.currency && pl.currency !== 'BYN'">
                    {{ fmtMoney(pl.out_sum_cur) }} {{ pl.currency }}
                    <small class="muted" :title="`курс ${pl.currency} на ${preview.rate?.date || ''}`">(курс {{ fmtRate(pl.rate) }})</small>
                  </template>
                  <template v-else>—</template>
                </td>
              </tr>
            </tbody>
          </table>
        </section>
      </div>

      <div class="gp-editor-footer">
        <span v-if="validationError" class="muted gp-hint">{{ validationError }}</span>
        <button class="btn btn-ghost btn-sm" @click="$emit('close')">Отмена</button>
        <button class="btn btn-primary btn-sm" :disabled="saving || !!validationError" @click="save">
          {{ saving ? 'Сохранение…' : (isNew ? 'Создать' : 'Сохранить') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted } from 'vue'

// Редактор цепочки поставки группы (постановка заказчика 23.09.2026).
// Цепочка = упорядоченные компании группы, каждая продаёт следующей со своей
// корректировкой в %; последняя — внешнему рынку. Ассортимент цепочки — те же
// фильтры, что у главной таблицы, поэтому каскад фильтров повторён отсюда
// (index.vue: loadFilters/onFilterChange), а не вынесен: у главной таблицы
// каскад завязан на её состояние, общий composable дал бы больше связей, чем
// экономии. В scope кладём ТЕКСТ уровней, как принимает POST /aggregated;
// id нужны только самому каскаду GET /filter-options.
//
// eff_pct в строке звена считается на клиенте только для показа — хранимое
// значение считает сервер при сохранении (§4.4 контракта), чтобы 1С читала
// одну и ту же цифру независимо от того, кто её посчитал.

type FilterKey = 'brand_manager' | 'level01' | 'level02' | 'level03' | 'level04' | 'level05' | 'calc_sign' | 'plan_id'
interface LinkRow { key: number; company_id: number | null; adjust_pct: number | string; currency: string }

const props = defineProps<{
  apiBase: string
  headers: Record<string, string>
  companies: any[]
  currencies: { code: string; name: string }[]
  chain: any | null // null — новая цепочка
}>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'saved', item: any): void }>()

const isNew = computed(() => !props.chain)

const FILTERS: { key: FilterKey; label: string }[] = [
  { key: 'brand_manager', label: 'Бренд-менеджер' },
  { key: 'level01', label: 'Level 01' },
  { key: 'level02', label: 'Level 02' },
  { key: 'level03', label: 'Level 03' },
  { key: 'level04', label: 'Level 04' },
  { key: 'level05', label: 'Level 05' },
  { key: 'calc_sign', label: 'Признак калькуляции' },
  { key: 'plan_id', label: 'План' },
]
const LEVEL_KEYS = ['level01', 'level02', 'level03', 'level04', 'level05']
// Каскад сервера считает только по бренд-менеджеру и уровням (CASCADE_KEYS в routes.py).
const CASCADE_KEYS = ['brand_manager', ...LEVEL_KEYS]
// Ключи scope, которых в этом редакторе нет (country, season): при правке
// существующей цепочки переносим как есть, чтобы сохранение их не стирало.
const PASSTHROUGH_KEYS = ['country', 'season']

const CALC_SIGN_DESCRIPTIONS: Record<string, string> = {
  'ПКПСС': 'новая разработка',
  'КПСС': 'плановая калькуляция',
  'ПФКСС': 'фактическая расценка ассортимента',
  'ФКСС': 'история себестоимости',
}

// ── Состояние формы ──────────────────────────────────────────────────────────
const form = reactive({ name: '', mode: 'cascade', weight: 'unit', rate_date: '', comment: '' })
const scopeSel = reactive<Record<string, string[]>>(Object.fromEntries(FILTERS.map((f) => [f.key, [] as string[]])))
const modelsText = ref('')
const articulsText = ref('')
const dateFrom = ref('')
const dateTo = ref('')
const latestOnly = ref(true)
const passthrough = reactive<Record<string, any>>({})
const links = ref<LinkRow[]>([])
let linkKeySeq = 1

const error = ref('')
const saving = ref(false)

function errText(e: any): string {
  return e?.data?.detail || e?.message || String(e)
}

function newLink(): LinkRow {
  return { key: linkKeySeq++, company_id: null, adjust_pct: 0, currency: 'BYN' }
}

function initForm() {
  const c = props.chain
  if (!c) {
    // Заказчик считает цены по факту выпуска — новой цепочке сразу ставим ФКСС.
    scopeSel.calc_sign = ['ФКСС']
    links.value = [newLink()]
    return
  }
  form.name = c.name || ''
  form.mode = c.mode === 'base' ? 'base' : 'cascade'
  form.weight = c.weight === 'volume' ? 'volume' : 'unit'
  form.rate_date = c.rate_date || ''
  form.comment = c.comment || ''
  const s = c.scope || {}
  for (const f of FILTERS) scopeSel[f.key] = Array.isArray(s[f.key]) ? s[f.key].map(String) : []
  modelsText.value = Array.isArray(s.model) ? s.model.join('\n') : ''
  articulsText.value = Array.isArray(s.articul) ? s.articul.join('\n') : ''
  dateFrom.value = s.date_from || ''
  dateTo.value = s.date_to || ''
  latestOnly.value = s.latest_only !== false
  for (const k of PASSTHROUGH_KEYS) if (Array.isArray(s[k]) && s[k].length) passthrough[k] = s[k]
  links.value = [...(c.links || [])]
    .sort((a: any, b: any) => (a.seq || 0) - (b.seq || 0))
    .map((l: any) => ({ key: linkKeySeq++, company_id: l.company_id, adjust_pct: Number(l.adjust_pct) || 0, currency: l.currency || 'BYN' }))
}
initForm()

// ── Списки моделей / артикулов ───────────────────────────────────────────────
function splitList(text: string): string[] {
  const seen = new Set<string>()
  for (const raw of String(text || '').split(/[\s,;]+/)) {
    const v = raw.trim()
    if (v) seen.add(v)
  }
  return [...seen]
}
const modelList = computed(() => splitList(modelsText.value))
const articulList = computed(() => splitList(articulsText.value))

function buildScope(): Record<string, any> {
  const scope: Record<string, any> = { ...passthrough }
  for (const f of FILTERS) {
    const vals = (scopeSel[f.key] || []).map((v) => String(v).trim()).filter(Boolean)
    if (vals.length) scope[f.key] = vals
  }
  if (modelList.value.length) scope.model = modelList.value
  if (articulList.value.length) scope.articul = articulList.value
  if (dateFrom.value) scope.date_from = dateFrom.value
  if (dateTo.value) scope.date_to = dateTo.value
  scope.latest_only = latestOnly.value
  return scope
}

// ── Каскад фильтров (повтор index.vue) ───────────────────────────────────────
const filterOptions = ref<Record<string, string[]>>({})
const levelTextToId = reactive<Record<string, Record<string, string>>>(Object.fromEntries(LEVEL_KEYS.map((k) => [k, {}])))
const cascadeBusy = ref(false)

function normalizeFilterOptions(raw: Record<string, any>): Record<string, string[]> {
  const result: Record<string, string[]> = {}
  for (const key of Object.keys(raw || {})) {
    const vals = raw[key]
    if (LEVEL_KEYS.includes(key) && Array.isArray(vals) && vals.length > 0 && typeof vals[0] === 'object') {
      const map: Record<string, string> = {}
      result[key] = vals.map((v: any) => { map[v.id] = v.text; return v.text })
      levelTextToId[key] = map
    } else {
      result[key] = Array.isArray(vals) ? vals.map(String) : []
    }
  }
  return result
}

function enrichOptions(key: string, raw: string[]): any[] {
  if (key !== 'calc_sign') return raw
  return raw.map((v) => ({ value: v, label: v, description: CALC_SIGN_DESCRIPTIONS[v] }))
}

function isLocked(key: FilterKey): boolean {
  if (key === 'calc_sign' || key === 'plan_id') return false
  let lowestIdx = -1
  for (let i = LEVEL_KEYS.length - 1; i >= 0; i--) {
    if (scopeSel[LEVEL_KEYS[i]]?.length > 0) { lowestIdx = i; break }
  }
  if (lowestIdx === -1) return false
  if (key === 'brand_manager') return true
  const keyIdx = LEVEL_KEYS.indexOf(key)
  return keyIdx !== -1 && keyIdx < lowestIdx
}

function cascadeParams(): URLSearchParams {
  const params = new URLSearchParams()
  for (const key of CASCADE_KEYS) {
    const vals = scopeSel[key] || []
    if (!vals.length) continue
    if (LEVEL_KEYS.includes(key)) {
      const idMap: Record<string, string> = {}
      for (const [id, text] of Object.entries(levelTextToId[key] || {})) idMap[text] = id
      for (const v of vals) params.append(key, idMap[v] || v)
    } else {
      for (const v of vals) params.append(key, v)
    }
  }
  return params
}

/** useParams — сузить варианты под текущий выбор; changedKey — фильтр, который
 *  менял пользователь (его варианты не трогаем, а выбор в остальных обрезаем
 *  под новые варианты, как в главной таблице). */
async function fetchOptions(useParams: boolean, changedKey: FilterKey | null) {
  cascadeBusy.value = true
  try {
    const qs = useParams ? cascadeParams().toString() : ''
    const raw = await $fetch<Record<string, any>>(`${props.apiBase}/api/cost/filter-options${qs ? '?' + qs : ''}`, { headers: props.headers })
    const normalized = normalizeFilterOptions(raw)
    for (const f of FILTERS) {
      const k = f.key
      if (k === changedKey) continue
      filterOptions.value[k] = normalized[k] || []
      if (changedKey) scopeSel[k] = (scopeSel[k] || []).filter((v) => filterOptions.value[k].includes(v))
    }
  } catch (e: any) {
    error.value = errText(e)
    console.error('[cost] group-pricing filter-options failed', e)
  } finally {
    cascadeBusy.value = false
  }
}

async function onFilterChange(changedKey: FilterKey) {
  if (changedKey === 'brand_manager') {
    for (const k of LEVEL_KEYS) scopeSel[k] = []
  } else if (LEVEL_KEYS.includes(changedKey)) {
    const idx = LEVEL_KEYS.indexOf(changedKey)
    for (let i = idx + 1; i < LEVEL_KEYS.length; i++) scopeSel[LEVEL_KEYS[i]] = []
  }
  if (!CASCADE_KEYS.includes(changedKey)) return
  await fetchOptions(true, changedKey)
}

onMounted(async () => {
  // Сначала полные списки (они же наполняют levelTextToId — без него текст
  // уровня не перевести в id каскада), затем сужение под сохранённый выбор.
  await fetchOptions(false, null)
  if (CASCADE_KEYS.some((k) => scopeSel[k]?.length)) await fetchOptions(true, null)
})

// ── Размер ассортимента ──────────────────────────────────────────────────────
const countBusy = ref(false)
const scopeCount = ref<{ rows: number; pairs: number; models: number } | null>(null)
const countText = computed(() => {
  const c = scopeCount.value
  if (!c) return ''
  return `${fmtInt(c.rows)} калькуляций, ${fmtInt(c.pairs)} артикулов, ${fmtInt(c.models)} моделей`
})

async function checkScope() {
  countBusy.value = true
  error.value = ''
  try {
    scopeCount.value = await $fetch<any>(`${props.apiBase}/api/cost/group/scope/count`, {
      method: 'POST', body: { scope: buildScope() }, headers: props.headers,
    })
  } catch (e: any) {
    scopeCount.value = null
    error.value = errText(e)
  } finally {
    countBusy.value = false
  }
}

// ── Звенья ───────────────────────────────────────────────────────────────────
const companyById = computed(() => new Map<number, any>((props.companies || []).map((c: any) => [c.id, c])))
/** В выбор идут активные компании плюс те, что уже стоят в звеньях (иначе
 *  звено с деактивированной компанией показалось бы пустым). */
const companyOptions = computed(() => {
  const used = new Set(links.value.map((l) => l.company_id))
  return (props.companies || []).filter((c: any) => c.is_active !== false || used.has(c.id))
})
const currencyOptions = computed(() => (props.currencies?.length ? props.currencies : [{ code: 'BYN', name: 'бел. рубль' }]))

function addLink() {
  links.value.push(newLink())
}
function removeLink(i: number) {
  links.value.splice(i, 1)
}
function moveLink(i: number, dir: -1 | 1) {
  const j = i + dir
  if (j < 0 || j >= links.value.length) return
  const arr = links.value
  ;[arr[i], arr[j]] = [arr[j], arr[i]]
}
/** Валюта отгрузки по умолчанию берётся у выбранной компании (cost_group_company.currency). */
function onCompanyChange(l: LinkRow) {
  const c = l.company_id != null ? companyById.value.get(l.company_id) : null
  if (c?.currency) l.currency = c.currency
}

/** Эффективный % к базовой отпускной (§4.4): каскад — произведение (1 + a/100)
 *  по звеньям до текущего, режим «от базовой» — сам % звена. */
function effPct(i: number): number | null {
  const rows = links.value
  if (form.mode === 'base') {
    const a = Number(rows[i]?.adjust_pct)
    return Number.isFinite(a) ? a : null
  }
  let f = 1
  for (let k = 0; k <= i; k++) {
    const a = Number(rows[k]?.adjust_pct)
    if (!Number.isFinite(a) || rows[k].adjust_pct === '') return null
    f *= 1 + a / 100
  }
  return (f - 1) * 100
}

function linksPayload() {
  return links.value.map((l) => ({
    company_id: Number(l.company_id),
    adjust_pct: Number(l.adjust_pct) || 0,
    currency: l.currency || 'BYN',
  }))
}

const validationError = computed(() => {
  if (!form.name.trim()) return 'Укажите название цепочки'
  if (!links.value.length) return 'Добавьте хотя бы одно звено'
  for (let i = 0; i < links.value.length; i++) {
    const l = links.value[i]
    if (l.company_id == null) return `Звено ${i + 1}: выберите компанию`
    const a = Number(l.adjust_pct)
    if (l.adjust_pct === '' || !Number.isFinite(a)) return `Звено ${i + 1}: корректировка должна быть числом`
    if (a <= -100) return `Звено ${i + 1}: скидка не может быть 100 % и больше`
    // Верхняя граница — та же, что у сервера: колонка NUMERIC(9,4) вмещает
    // меньше 100 000 %, дальше INSERT падает переполнением, а не осмысленной ошибкой.
    if (a >= 100000) return `Звено ${i + 1}: корректировка звена вне допустимого диапазона`
  }
  if (dateFrom.value && dateTo.value && dateFrom.value > dateTo.value) return 'Дата «с» позже даты «по»'
  return ''
})

// ── Предпросмотр ─────────────────────────────────────────────────────────────
const preview = ref<any | null>(null)
const previewBusy = ref(false)
const previewStale = ref(false)
const canPreview = computed(() => links.value.length > 0 && links.value.every((l) => l.company_id != null && l.adjust_pct !== '' && Number.isFinite(Number(l.adjust_pct))))
const previewHasCurrency = computed(() => !!preview.value?.links?.some((l: any) => l.currency && l.currency !== 'BYN'))
const previewNote = computed(() => {
  const b = preview.value?.base
  if (!b) return ''
  let s = `в расчёте ${fmtInt(b.rows)} калькуляций, с ценой ${fmtInt(b.rows_priced)}, без цены ${fmtInt(b.rows_without_price)}`
  if (b.weight === 'volume') s += `, без объёма ${fmtInt(b.rows_without_volume)}`
  const r = preview.value?.rate
  if (r?.rates) {
    const parts = Object.entries(r.rates).filter(([k]) => k !== 'BYN').map(([k, v]) => `${k} ${fmtRate(v)}`)
    if (parts.length) s += ` · курс НБ РБ на ${r.date}: ${parts.join(', ')}`
  }
  return s
})

async function runPreview() {
  previewBusy.value = true
  error.value = ''
  try {
    preview.value = await $fetch<any>(`${props.apiBase}/api/cost/group/simulate`, {
      method: 'POST',
      headers: props.headers,
      body: {
        chain: { mode: form.mode, weight: form.weight, scope: buildScope(), rate_date: form.rate_date || null, links: linksPayload() },
        summary: true,
      },
    })
    previewStale.value = false
  } catch (e: any) {
    error.value = errText(e)
  } finally {
    previewBusy.value = false
  }
}

// Любая правка после предпросмотра помечает его устаревшим — цифры остаются на
// экране для сравнения, но подпись честно говорит, что они не про текущие параметры.
watch([form, scopeSel, links, modelsText, articulsText, dateFrom, dateTo, latestOnly], () => {
  if (preview.value) previewStale.value = true
}, { deep: true })

// Результат «Проверить ассортимент» относится к тем фильтрам, по которым его
// считали: сменился ассортимент — цифра снимается, а не висит рядом с новыми
// фильтрами. Название, режим и звенья на размер ассортимента не влияют.
watch([scopeSel, modelsText, articulsText, dateFrom, dateTo, latestOnly], () => {
  scopeCount.value = null
}, { deep: true })

// ── Сохранение ───────────────────────────────────────────────────────────────
async function save() {
  if (validationError.value) return
  saving.value = true
  error.value = ''
  try {
    const body: Record<string, any> = {
      name: form.name.trim(),
      mode: form.mode,
      weight: form.weight,
      scope: buildScope(),
      rate_date: form.rate_date || null,
      comment: form.comment.trim() || null,
      links: linksPayload(),
    }
    let res: any
    if (isNew.value) {
      res = await $fetch<any>(`${props.apiBase}/api/cost/group/chains`, { method: 'POST', body, headers: props.headers })
    } else {
      // Статус редактор не меняет — архив/возврат делаются кнопками карточки.
      body.status = props.chain.status
      res = await $fetch<any>(`${props.apiBase}/api/cost/group/chains/${props.chain.id}`, { method: 'PUT', body, headers: props.headers })
    }
    emit('saved', res?.item || res)
  } catch (e: any) {
    error.value = errText(e)
    console.error('[cost] group-pricing save chain failed', e)
  } finally {
    saving.value = false
  }
}

// ── Форматирование ───────────────────────────────────────────────────────────
const nf0 = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 0 })
const nf1 = new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 1, maximumFractionDigits: 1 })
const nf2 = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 2 })
const nf4 = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 4 })
const isNum = (v: any) => v !== null && v !== undefined && v !== '' && Number.isFinite(Number(v))
const fmtInt = (v: any) => (isNum(v) ? nf0.format(Number(v)) : '—')
const fmtMoney = (v: any) => (isNum(v) ? nf2.format(Number(v)) : '—')
const fmtRate = (v: any) => (isNum(v) ? nf4.format(Number(v)) : '—')
const fmtPct1 = (v: any) => (isNum(v) ? `${nf1.format(Number(v))} %` : '—')
const fmtSignedPct = (v: any) => {
  if (!isNum(v)) return '—'
  const n = Number(v)
  const sign = n > 0 ? '+' : n < 0 ? '−' : ''
  return `${sign}${nf2.format(Math.abs(n))} %`
}
const signClass = (v: any) => (isNum(v) ? (Number(v) < 0 ? 'neg' : Number(v) > 0 ? 'pos' : '') : '')
</script>

<style scoped>
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  z-index: 10000;
  display: flex;
  justify-content: center;
  align-items: center;
}
.gp-editor {
  background: var(--bg-surface);
  border-radius: var(--rd-3);
  border: 1px solid var(--border);
  width: min(1040px, 96vw);
  max-height: 92vh;
  display: flex;
  flex-direction: column;
}
.modal-header {
  padding: var(--sp-4) var(--sp-5);
  border-bottom: 1px solid var(--border);
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-shrink: 0;
}
.modal-header h2 { margin: 0; font-size: var(--fs-lg); }
.modal-close {
  border: none;
  background: transparent;
  font-size: 24px;
  cursor: pointer;
  color: var(--text-muted);
  padding: 0 4px;
}
.modal-close:hover { color: var(--text-strong); }

.gp-editor-body {
  padding: var(--sp-4) var(--sp-5);
  overflow-y: auto;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: var(--sp-5);
}
.gp-editor-footer {
  padding: var(--sp-3) var(--sp-5);
  border-top: 1px solid var(--border);
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: var(--sp-3);
  flex-shrink: 0;
}

/* Error banner (как в admin/roles.vue) */
.cost-error {
  display: flex;
  align-items: flex-start;
  gap: var(--sp-3);
  padding: var(--sp-4) var(--sp-5);
  border-radius: var(--rd-3);
  background: color-mix(in srgb, var(--neg) 8%, var(--bg-surface));
  border: 1px solid color-mix(in srgb, var(--neg) 30%, transparent);
  color: var(--text-strong);
  position: relative;
}
.cost-error :deep(svg) { color: var(--neg); flex-shrink: 0; margin-top: 2px; }
.cost-error p { margin-top: 4px; font-size: var(--fs-sm); }
.cost-error-x {
  position: absolute;
  top: 6px;
  right: 6px;
  border: none;
  background: transparent;
  font-size: 18px;
  cursor: pointer;
  color: var(--text-muted);
  padding: 4px 8px;
  border-radius: var(--rd-1);
}
.cost-error-x:hover { color: var(--text-strong); background: var(--bg-surface-3); }

.gp-section {
  display: flex;
  flex-direction: column;
  gap: var(--sp-4);
  padding: var(--sp-4);
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  background: var(--bg-surface-2, var(--bg-surface));
}
.gp-section-title {
  font-size: var(--fs-sm);
  font-weight: var(--fw-semibold, 600);
  color: var(--text-strong);
}
.gp-section-title .muted { font-weight: normal; }

.gp-grid-2 {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--sp-4);
}
.gp-field {
  display: flex;
  flex-direction: column;
  gap: var(--sp-2);
  font-size: var(--fs-sm);
  color: var(--text-muted);
  min-width: 0;
}
.gp-field > span { font-size: var(--fs-xs); }
.gp-field small { font-size: var(--fs-2xs); }
.form-input {
  height: 32px;
  padding: 0 var(--sp-3);
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
  background: var(--bg-surface);
  color: var(--text-strong);
  font-size: var(--fs-sm);
  min-width: 0;
}
.gp-textarea {
  height: auto;
  padding: var(--sp-2) var(--sp-3);
  resize: vertical;
  font-family: inherit;
  line-height: 1.4;
}

/* Двухпозиционный переключатель режима / веса */
.gp-seg {
  display: inline-flex;
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
  overflow: hidden;
  width: fit-content;
}
.gp-seg button {
  border: none;
  background: var(--bg-surface);
  color: var(--text-muted);
  padding: 0 var(--sp-5);
  height: 30px;
  font-size: var(--fs-sm);
  cursor: pointer;
}
.gp-seg button + button { border-left: 1px solid var(--border); }
.gp-seg button.active {
  background: color-mix(in srgb, var(--accent) 14%, var(--bg-surface));
  color: var(--accent);
  font-weight: var(--fw-semibold, 600);
}

/* Фильтры ассортимента */
.gp-filters {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--sp-3) var(--sp-4);
  transition: opacity var(--t-base);
}
.gp-filters.is-busy { opacity: 0.6; pointer-events: none; }
.gp-filter { display: flex; flex-direction: column; gap: var(--sp-1); min-width: 0; }
.gp-filter label { font-size: var(--fs-xs); color: var(--text-muted); }
.gp-filter.locked label { opacity: 0.6; }
@media (max-width: 900px) {
  .gp-filters { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .gp-grid-2 { grid-template-columns: 1fr; }
}

.gp-row {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  flex-wrap: wrap;
  font-size: var(--fs-sm);
}
.gp-inline {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  color: var(--text-muted);
  font-size: var(--fs-sm);
}
.gp-check {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  cursor: pointer;
  color: var(--text-strong);
}
.gp-check input { width: 15px; height: 15px; cursor: pointer; }
.gp-count {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--fs-sm);
  color: var(--text-strong);
}
.gp-hint { font-size: var(--fs-xs); }
.gp-empty { font-size: var(--fs-sm); }

/* Звенья */
.gp-link-row {
  display: grid;
  grid-template-columns: 24px minmax(160px, 1.6fr) 120px 84px minmax(130px, 1fr) minmax(110px, 1fr) auto;
  gap: var(--sp-3);
  align-items: center;
}
.gp-link-seq {
  width: 22px; height: 22px;
  display: inline-flex; align-items: center; justify-content: center;
  border-radius: var(--rd-pill);
  background: color-mix(in srgb, var(--accent) 14%, var(--bg-surface));
  color: var(--accent);
  font-size: var(--fs-2xs);
  font-weight: var(--fw-semibold, 600);
}
.gp-pct { display: inline-flex; align-items: center; gap: var(--sp-2); min-width: 0; }
.gp-pct input {
  width: 100%;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  text-align: right;
}
.gp-pct span { color: var(--text-muted); font-size: var(--fs-sm); }
.gp-cur { font-family: var(--font-mono); }
.gp-eff {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  white-space: nowrap;
}
.gp-eff b {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  color: var(--text-strong);
  font-weight: var(--fw-medium, 500);
}
.gp-arrow { font-size: var(--fs-xs); white-space: nowrap; }
.gp-link-btns { display: inline-flex; gap: var(--sp-1); }
.gp-link-btns .btn { min-width: 26px; padding: 0 var(--sp-2); }
.gp-danger { color: var(--neg); }
.gp-danger:hover { background: color-mix(in srgb, var(--neg) 10%, transparent); }
@media (max-width: 900px) {
  .gp-link-row { grid-template-columns: 24px 1fr 100px 80px; }
  .gp-eff, .gp-arrow { grid-column: 2 / -1; }
}

/* Плитки предпросмотра */
.gp-tiles {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--sp-4);
  transition: opacity var(--t-base);
}
.gp-tiles.stale, .gp-preview-links.stale { opacity: 0.55; }
.gp-tile {
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  background: var(--bg-surface);
  padding: var(--sp-4) var(--sp-5);
  display: flex;
  flex-direction: column;
  gap: var(--sp-1);
  min-width: 0;
}
.gp-tile-accent { border-color: color-mix(in srgb, var(--accent) 40%, var(--border)); }
.gp-tile-label {
  font-size: var(--fs-2xs);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-muted);
}
.gp-tile-value {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--fs-lg);
  color: var(--text-strong);
}
.gp-tile-value.pos, td.pos { color: var(--pos); }
.gp-tile-value.neg, td.neg { color: var(--neg); }
.gp-tile-meta { font-size: var(--fs-2xs); }
@media (max-width: 900px) {
  .gp-tiles { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

.gp-preview-links {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--fs-xs);
}
.gp-preview-links th, .gp-preview-links td {
  padding: var(--sp-2) var(--sp-3);
  border-bottom: 1px solid var(--border);
  text-align: left;
  white-space: nowrap;
}
.gp-preview-links th { color: var(--text-muted); font-weight: var(--fw-medium, 500); }
.gp-preview-links .num {
  text-align: right;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}

.spinning { animation: gp-spin 0.9s linear infinite; }
@keyframes gp-spin { to { transform: rotate(360deg); } }
</style>
