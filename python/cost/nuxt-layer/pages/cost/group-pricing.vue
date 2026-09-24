<template>
  <div class="page-cost gp-page">
    <header class="page-header">
      <div>
        <h1 class="page-title">Ценообразование группы</h1>
        <p class="page-subtitle muted">Компании группы, цепочки поставки с наценками и скидками, финрез по звеньям</p>
      </div>
      <div class="page-header-actions">
        <button class="btn btn-ghost btn-sm" @click="navigateTo('/cost')">
          <Icon name="lucide:arrow-left" /> Себестоимость
        </button>
      </div>
    </header>

    <!-- Гейт по праву раздела -->
    <div v-if="permLoading" class="muted" style="text-align:center;padding:24px">
      <Icon name="lucide:loader" class="spinning" /> Проверка прав доступа…
    </div>
    <div v-else-if="!can(PERM)" class="cost-error">
      <Icon name="lucide:alert-triangle" />
      <div>
        <strong>Нет доступа</strong>
        <p>Для ценообразования группы требуется право <code>{{ PERM }}</code>.</p>
      </div>
    </div>

    <template v-else>
      <div v-if="lastError" class="cost-error">
        <Icon name="lucide:alert-triangle" />
        <div>
          <strong>Ошибка</strong>
          <p>{{ lastError }}</p>
        </div>
        <button class="cost-error-x" aria-label="Закрыть" @click="lastError = ''">×</button>
      </div>

      <!-- Вкладки -->
      <div class="gp-tabs" role="tablist">
        <button class="gp-tab" role="tab" :class="{ active: tab === 'chains' }" :aria-selected="tab === 'chains'" @click="tab = 'chains'">
          <Icon name="lucide:network" /> Цепочки <span class="gp-tab-count">{{ chains.length }}</span>
        </button>
        <button class="gp-tab" role="tab" :class="{ active: tab === 'companies' }" :aria-selected="tab === 'companies'" @click="tab = 'companies'">
          <Icon name="lucide:building-2" /> Компании <span class="gp-tab-count">{{ companies.length }}</span>
        </button>
      </div>

      <!-- ── Цепочки ─────────────────────────────────────────────────────── -->
      <template v-if="tab === 'chains'">
        <div class="cost-actions">
          <button class="btn btn-primary btn-sm" @click="openChainEditor(null)">
            <Icon name="lucide:plus" /> Цепочка
          </button>
          <label class="gp-check">
            <input v-model="showArchived" type="checkbox" />
            <span>показывать архивные <template v-if="archivedCount">({{ archivedCount }})</template></span>
          </label>
          <button class="btn btn-ghost btn-sm" :disabled="loading" title="Перечитать цепочки, компании и пересечения" @click="loadAll">
            <Icon name="lucide:refresh-cw" :class="{ spinning: loading }" /> Обновить
          </button>
          <span class="muted gp-note">Пересечение ассортимента разных цепочек допускается и только подсвечивается.</span>
        </div>

        <div v-if="loading && !chains.length" class="muted" style="text-align:center;padding:24px">
          <Icon name="lucide:loader" class="spinning" /> Загрузка…
        </div>

        <div v-else-if="!visibleChains.length" class="card">
          <div class="card-body" style="text-align:center;padding:var(--sp-5)">
            <p class="muted">
              {{ chains.length ? 'Все цепочки в архиве — включите «показывать архивные».' : 'Цепочек пока нет. Нажмите «+ Цепочка», чтобы описать первую.' }}
            </p>
          </div>
        </div>

        <div v-else class="gp-list">
          <div v-for="c in visibleChains" :key="c.id" class="card gp-card" :class="{ archived: c.status === 'archived', selected: selectedChainId === c.id }">
            <div class="card-header gp-card-header">
              <div class="gp-card-main">
                <div class="card-title gp-title">
                  {{ c.name }}
                  <span class="gp-badge" :title="modeTitle(c.mode)">{{ modeLabel(c.mode) }}</span>
                  <span class="gp-badge gp-badge-muted" :title="weightTitle(c.weight)">{{ weightLabel(c.weight) }}</span>
                  <span v-if="c.status === 'archived'" class="gp-badge gp-badge-muted">в архиве</span>
                </div>

                <div class="gp-scope">
                  <span v-for="(chip, i) in scopeChips(c.scope)" :key="i" class="gp-chip" :title="chip.title || ''">
                    <span class="gp-chip-k">{{ chip.label }}:</span> {{ chip.value }}
                  </span>
                </div>

                <div class="gp-links-line">
                  <template v-for="(l, i) in sortedLinks(c)" :key="l.id || i">
                    <span class="gp-link-item" :title="`к базовой ${fmtSignedPct(l.eff_pct)}`">
                      <span class="gp-link-company">{{ l.company_name || companyName(l.company_id) }}</span>
                      <span class="gp-link-pct" :class="signClass(l.adjust_pct)">{{ fmtSignedPct(l.adjust_pct) }}</span>
                      <span v-if="l.currency && l.currency !== 'BYN'" class="gp-link-cur">({{ l.currency }})</span>
                    </span>
                    <span class="gp-link-arrow muted">→</span>
                  </template>
                  <span class="gp-link-market muted">рынок</span>
                </div>

                <div v-for="o in overlapsFor(c.id)" :key="o.other_chain_id" class="gp-overlap">
                  <Icon name="lucide:git-merge" /> пересекается с «{{ o.other_chain_name }}» — {{ fmtInt(o.shared_pairs) }} арт.
                </div>

                <div class="gp-meta muted">
                  курс на {{ c.rate_date || 'сегодня' }}
                  <template v-if="c.comment"> · {{ c.comment }}</template>
                  <template v-if="c.updated_at"> · изменено {{ fmtDateTime(c.updated_at) }}<template v-if="c.updated_by"> ({{ c.updated_by }})</template></template>
                </div>
              </div>

              <div class="card-actions gp-card-actions">
                <button class="btn btn-primary btn-xs" :disabled="busyChainId === c.id" @click="openResult(c)">
                  <Icon name="lucide:bar-chart-3" /> Финрез
                </button>
                <button class="btn btn-ghost btn-xs" :disabled="busyChainId === c.id" @click="openChainEditor(c)">
                  <Icon name="lucide:pencil" /> Изменить
                </button>
                <button v-if="c.status !== 'archived'" class="btn btn-ghost btn-xs" :disabled="busyChainId === c.id" @click="setChainStatus(c, 'archived')">
                  <Icon name="lucide:archive" /> В архив
                </button>
                <button v-else class="btn btn-ghost btn-xs" :disabled="busyChainId === c.id" @click="setChainStatus(c, 'active')">
                  <Icon name="lucide:archive-restore" /> Вернуть
                </button>
                <button class="btn btn-ghost btn-xs btn-danger" :disabled="busyChainId === c.id" @click="deleteChain(c)">
                  <Icon name="lucide:trash-2" /> {{ busyChainId === c.id ? '…' : 'Удалить' }}
                </button>
              </div>
            </div>
          </div>
        </div>

      </template>

      <!-- ── Компании ────────────────────────────────────────────────────── -->
      <template v-else>
        <div class="cost-actions">
          <button class="btn btn-primary btn-sm" @click="openCompanyModal(null)">
            <Icon name="lucide:plus" /> Компания
          </button>
          <span class="muted gp-note">Компанию, стоящую в цепочках, удалить нельзя — снимите флаг «активна» или уберите её из звеньев.</span>
        </div>

        <div v-if="loading && !companies.length" class="muted" style="text-align:center;padding:24px">
          <Icon name="lucide:loader" class="spinning" /> Загрузка…
        </div>

        <div v-else-if="!companies.length" class="card">
          <div class="card-body" style="text-align:center;padding:var(--sp-5)">
            <p class="muted">Компаний нет. Нажмите «+ Компания», чтобы добавить первую.</p>
          </div>
        </div>

        <div v-else class="gp-list">
          <div v-for="k in companies" :key="k.id" class="card gp-card" :class="{ archived: !k.is_active }">
            <div class="card-header gp-card-header">
              <div class="gp-card-main">
                <div class="card-title gp-title">
                  {{ k.name }}
                  <span class="gp-badge gp-badge-muted" title="Код страны">{{ k.country || '—' }}</span>
                  <span class="gp-badge gp-badge-muted" title="Валюта отгрузки по умолчанию для новых звеньев">{{ k.currency || 'BYN' }}</span>
                  <span v-if="!k.is_active" class="gp-badge gp-badge-neg">неактивна</span>
                  <span v-if="k.used_in_chains" class="gp-badge">в {{ k.used_in_chains }} {{ pluralChains(k.used_in_chains) }}</span>
                </div>
                <div v-if="k.comment" class="gp-meta">{{ k.comment }}</div>
                <div class="gp-meta muted">
                  порядок {{ k.sort_order }}
                  <template v-if="k.updated_at"> · изменено {{ fmtDateTime(k.updated_at) }}<template v-if="k.updated_by"> ({{ k.updated_by }})</template></template>
                </div>
              </div>
              <div class="card-actions gp-card-actions">
                <button class="btn btn-ghost btn-xs" @click="openCompanyModal(k)">
                  <Icon name="lucide:pencil" /> Изменить
                </button>
                <button class="btn btn-ghost btn-xs btn-danger" :disabled="deletingCompanyId === k.id" @click="deleteCompany(k)">
                  <Icon name="lucide:trash-2" /> {{ deletingCompanyId === k.id ? 'Удаление…' : 'Удалить' }}
                </button>
              </div>
            </div>
          </div>
        </div>
      </template>

      <!-- Финрез выбранной цепочки — компонент другого исполнителя (§6.2).
           Стоит вне вкладки и прячется v-show: при переходе на «Компании» и
           обратно он не размонтируется, путь проваливания и данные остаются.
           :key с resultKey пересоздаёт его после сохранения цепочки или смены
           статуса — финрез считался по прежним звеньям и ассортименту. -->
      <div v-if="selectedChainId" v-show="tab === 'chains'" ref="resultBox" class="gp-result">
        <CostGroupChainResult :key="`${selectedChainId}-${resultKey}`" :chain-id="selectedChainId" @close="selectedChainId = null" />
      </div>
    </template>

    <!-- Модалка компании -->
    <Teleport to="body">
      <div v-if="showCompanyModal" class="modal-overlay" @click.self="closeCompanyModal">
        <!-- Без @click.stop: закрытие по фону делает @click.self на оверлее,
             остановленный клик мешал бы document-слушателям внутри модалки. -->
        <div class="modal-content gp-modal">
          <div class="modal-header">
            <h2>{{ editingCompany ? 'Изменить компанию' : 'Новая компания' }}</h2>
            <button class="modal-close" aria-label="Закрыть" @click="closeCompanyModal">×</button>
          </div>
          <div class="gp-modal-body">
            <!-- Ошибка сохранения (400/409 — дубль имени, неизвестная валюта) — здесь,
                 а не в баннере страницы: баннер под модалкой не видно, а форма
                 остаётся открытой, чтобы исправить и повторить. -->
            <div v-if="companyError" class="cost-error">
              <Icon name="lucide:alert-triangle" />
              <div>
                <strong>Ошибка</strong>
                <p>{{ companyError }}</p>
              </div>
              <button class="cost-error-x" aria-label="Закрыть" @click="companyError = ''">×</button>
            </div>
            <label class="gp-field">
              <span>Название *</span>
              <input v-model="companyForm.name" type="text" class="form-input" placeholder="Марк Формэль КЗ" @keydown.enter="saveCompany" />
            </label>
            <div class="gp-grid-2">
              <label class="gp-field">
                <span>Страна</span>
                <input v-model="companyForm.country" type="text" class="form-input" placeholder="BY / RU / KZ / UZ" maxlength="8" />
              </label>
              <label class="gp-field">
                <span>Валюта отгрузки по умолчанию</span>
                <select v-model="companyForm.currency" class="form-input">
                  <option v-for="cur in currencyOptions" :key="cur.code" :value="cur.code">{{ cur.code }} — {{ cur.name }}</option>
                </select>
              </label>
            </div>
            <label class="gp-field">
              <span>Комментарий</span>
              <textarea v-model="companyForm.comment" class="form-input gp-textarea" rows="2"></textarea>
            </label>
            <div class="gp-grid-2">
              <label class="gp-check">
                <input v-model="companyForm.is_active" type="checkbox" />
                <span>активна — доступна для новых звеньев</span>
              </label>
              <label class="gp-field">
                <span>Порядок в списке</span>
                <input v-model.number="companyForm.sort_order" type="number" step="10" class="form-input" />
              </label>
            </div>
          </div>
          <div class="gp-modal-footer">
            <button class="btn btn-ghost btn-sm" @click="closeCompanyModal">Отмена</button>
            <button class="btn btn-primary btn-sm" :disabled="!companyForm.name.trim() || companySaving" @click="saveCompany">
              {{ companySaving ? 'Сохранение…' : 'Сохранить' }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- Редактор цепочки -->
    <Teleport to="body">
      <CostGroupChainEditor
        v-if="showChainEditor"
        :api-base="apiBase"
        :headers="fetchHeaders"
        :companies="companies"
        :currencies="currencies"
        :chain="editingChain"
        @close="showChainEditor = false"
        @saved="onChainSaved"
      />
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, nextTick } from 'vue'
import { useCostPermission } from '~/composables/useCostPermission'

// Страница «Ценообразование группы» (постановка заказчика 23.09.2026):
// справочник компаний группы и цепочки поставки между ними. Каждая компания
// продаёт следующей со своей корректировкой в %, последняя — внешнему рынку;
// база цены — отпускная «как в главной таблице». Право отдельное,
// cost:group_pricing, миграцией выдано только Full Admin — раздел пока
// смотрит один человек, остальным роли раздадут после приёмки.
//
// Расчёт и хранение — на сервере (/api/cost/group/*); здесь только списки,
// формы и вызов компонентов редактора и финреза. Все запросы — с X-Cost-User.

const PERM = 'cost:group_pricing'

const { can, loading: permLoading } = useCostPermission()
const config = useRuntimeConfig()
const apiBase = computed(() => (config.public.costOnly ? '' : ((config.public.apiBase as string) || '')))
const user = useState<any>('auth-user')
const fetchHeaders = computed<Record<string, string>>(() => (user.value?.email ? { 'X-Cost-User': user.value.email } : {}))

function api<T = any>(path: string, opts: Record<string, any> = {}): Promise<T> {
  return $fetch<T>(`${apiBase.value}/api/cost${path}`, { ...opts, headers: fetchHeaders.value })
}
function errText(e: any): string {
  return e?.data?.detail || e?.message || String(e)
}

const tab = ref<'chains' | 'companies'>('chains')
const loading = ref(false)
const lastError = ref('')

const companies = ref<any[]>([])
const chains = ref<any[]>([])
const overlaps = ref<any[]>([])
const currencies = ref<{ code: string; name: string }[]>([])

// ── Загрузка ─────────────────────────────────────────────────────────────────
async function loadCompanies() {
  const res = await api<{ items: any[] }>('/group/companies')
  companies.value = res?.items || []
}
async function loadChains() {
  const res = await api<{ items: any[] }>('/group/chains')
  chains.value = res?.items || []
}
/** Пересечения и валюты — вспомогательные данные: их сбой не должен прятать список. */
async function loadOverlaps() {
  try {
    const res = await api<{ items: any[] }>('/group/chains/overlaps')
    overlaps.value = res?.items || []
  } catch (e) {
    overlaps.value = []
    console.error('[cost] group-pricing overlaps failed', e)
  }
}
async function loadCurrencies() {
  try {
    const res = await api<{ items: any[] }>('/purchase/currencies')
    currencies.value = res?.items || []
  } catch (e) {
    currencies.value = []
    console.error('[cost] group-pricing currencies failed', e)
  }
}
async function loadAll() {
  loading.value = true
  lastError.value = ''
  try {
    await Promise.all([loadCompanies(), loadChains()])
    await Promise.all([loadOverlaps(), loadCurrencies()])
  } catch (e: any) {
    lastError.value = errText(e)
    console.error('[cost] group-pricing load failed', e)
  } finally {
    loading.value = false
  }
}

// Ждём загрузки прав и грузим данные только при наличии права — иначе первый
// же запрос ответил бы 403 и баннер ошибки перекрыл бы понятное «Нет доступа».
watch(permLoading, (l) => {
  if (!l && can(PERM)) loadAll()
}, { immediate: true })

// ── Цепочки: список ──────────────────────────────────────────────────────────
const showArchived = ref(false)
const archivedCount = computed(() => chains.value.filter((c) => c.status === 'archived').length)
const visibleChains = computed(() => (showArchived.value ? chains.value : chains.value.filter((c) => c.status !== 'archived')))

const companyById = computed(() => new Map<number, any>(companies.value.map((c) => [c.id, c])))
const companyName = (id: number) => companyById.value.get(id)?.name || `#${id}`
const sortedLinks = (c: any) => [...(c.links || [])].sort((a: any, b: any) => (a.seq || 0) - (b.seq || 0))
const overlapsFor = (chainId: number) => overlaps.value.filter((o) => o.chain_id === chainId)

const modeLabel = (m: string) => (m === 'base' ? 'от базовой' : 'каскад')
const modeTitle = (m: string) => (m === 'base' ? 'Все % считаются от базовой отпускной цены' : '% каждого звена — от цены предыдущего звена')
const weightLabel = (w: string) => (w === 'volume' ? 'по выпуску' : 'по калькуляциям')
const weightTitle = (w: string) => (w === 'volume' ? 'Вес строки — «выпуск шт»' : 'Вес строки — 1 (одна калькуляция = одна единица)')

const SCOPE_LABELS: Record<string, string> = {
  brand_manager: 'БМ',
  level01: 'Level 01', level02: 'Level 02', level03: 'Level 03', level04: 'Level 04', level05: 'Level 05',
  calc_sign: 'Признак', plan_id: 'План', country: 'Страна', season: 'Сезон',
}
const SCOPE_ORDER = ['level01', 'level02', 'level03', 'level04', 'level05', 'brand_manager', 'calc_sign', 'plan_id', 'country', 'season']

/** Сводка ассортимента чипами. Пустой scope — «весь ассортимент». Длинные
 *  списки моделей/артикулов сворачиваем до количества (полный список — в title). */
function scopeChips(scope: any): { label: string; value: string; title?: string }[] {
  const s = scope || {}
  const chips: { label: string; value: string; title?: string }[] = []
  for (const k of SCOPE_ORDER) {
    const v = s[k]
    if (Array.isArray(v) && v.length) chips.push({ label: SCOPE_LABELS[k] || k, value: v.join(', ') })
  }
  for (const [k, label, unit] of [['model', 'Модели', 'моделей'], ['articul', 'Артикулы', 'артикулов']] as const) {
    const v = s[k]
    if (!Array.isArray(v) || !v.length) continue
    chips.push(v.length > 3 ? { label, value: `${v.length} ${unit}`, title: v.join(', ') } : { label, value: v.join(', ') })
  }
  if (s.date_from || s.date_to) {
    chips.push({ label: 'Дата расчёта', value: `${s.date_from ? 'с ' + s.date_from : ''}${s.date_from && s.date_to ? ' ' : ''}${s.date_to ? 'по ' + s.date_to : ''}` })
  }
  if (s.latest_only === false) chips.push({ label: 'Расчёты', value: 'все, не только последний' })
  if (!chips.length) chips.push({ label: 'Ассортимент', value: 'весь' })
  return chips
}

// ── Цепочки: действия ────────────────────────────────────────────────────────
const busyChainId = ref<number | null>(null)
const selectedChainId = ref<number | null>(null)
/** Инкремент пересоздаёт <CostGroupChainResult> (через :key) для той же цепочки —
 *  после сохранения или смены статуса открытый финрез иначе показывал бы старые цифры. */
const resultKey = ref(0)
const resultBox = ref<HTMLElement | null>(null)

const showChainEditor = ref(false)
const editingChain = ref<any | null>(null)

function openChainEditor(c: any | null) {
  editingChain.value = c
  showChainEditor.value = true
}
async function onChainSaved(item: any) {
  showChainEditor.value = false
  editingChain.value = null
  if (item?.id && selectedChainId.value === item.id) resultKey.value++
  await loadAll()
}

function openResult(c: any) {
  selectedChainId.value = c.id
}
watch(selectedChainId, (v) => {
  if (v) nextTick(() => resultBox.value?.scrollIntoView({ behavior: 'smooth', block: 'start' }))
})

/** PUT ждёт полное тело — собираем его из карточки, звенья в формате запроса. */
function chainBody(c: any, overrides: Record<string, any> = {}) {
  return {
    name: c.name,
    mode: c.mode,
    weight: c.weight,
    scope: c.scope || {},
    rate_date: c.rate_date || null,
    status: c.status,
    comment: c.comment || null,
    links: sortedLinks(c).map((l: any) => ({ company_id: l.company_id, adjust_pct: l.adjust_pct, currency: l.currency || 'BYN' })),
    ...overrides,
  }
}

async function setChainStatus(c: any, status: 'active' | 'archived') {
  busyChainId.value = c.id
  lastError.value = ''
  try {
    await api(`/group/chains/${c.id}`, { method: 'PUT', body: chainBody(c, { status }) })
    await loadChains()
    if (selectedChainId.value === c.id) {
      // Архивная цепочка при выключенных «архивных» из списка исчезает — финрез
      // без карточки над ним висел бы сиротой; в остальных случаях перечитываем.
      // До loadOverlaps: иначе карточка пропадает сразу, а финрез — только после
      // ответа по пересечениям.
      if (status === 'archived' && !showArchived.value) selectedChainId.value = null
      else resultKey.value++
    }
    await loadOverlaps()
  } catch (e: any) {
    lastError.value = errText(e)
    console.error('[cost] group-pricing chain status failed', e)
  } finally {
    busyChainId.value = null
  }
}

async function deleteChain(c: any) {
  if (!confirm(`Удалить цепочку «${c.name}»? Звенья удалятся вместе с ней, компании останутся.`)) return
  busyChainId.value = c.id
  lastError.value = ''
  try {
    await api(`/group/chains/${c.id}`, { method: 'DELETE' })
    if (selectedChainId.value === c.id) selectedChainId.value = null
    await loadChains()
    await Promise.all([loadOverlaps(), loadCompanies()])
  } catch (e: any) {
    lastError.value = errText(e)
    console.error('[cost] group-pricing delete chain failed', e)
  } finally {
    busyChainId.value = null
  }
}

// ── Компании ─────────────────────────────────────────────────────────────────
const currencyOptions = computed(() => (currencies.value.length ? currencies.value : [{ code: 'BYN', name: 'бел. рубль' }]))
const showCompanyModal = ref(false)
const editingCompany = ref<any | null>(null)
const companySaving = ref(false)
/** Ошибка сохранения компании живёт в модалке (сбрасывается при открытии/закрытии). */
const companyError = ref('')
const deletingCompanyId = ref<number | null>(null)
const companyForm = reactive({ name: '', country: 'BY', currency: 'BYN', comment: '', is_active: true, sort_order: 0 })

function openCompanyModal(k: any | null) {
  editingCompany.value = k
  companyError.value = ''
  if (k) {
    companyForm.name = k.name || ''
    companyForm.country = k.country || 'BY'
    companyForm.currency = k.currency || 'BYN'
    companyForm.comment = k.comment || ''
    companyForm.is_active = k.is_active !== false
    companyForm.sort_order = Number(k.sort_order) || 0
  } else {
    // Новая компания встаёт в конец списка: seed миграции идёт с шагом 10.
    const maxOrder = companies.value.reduce((m, c) => Math.max(m, Number(c.sort_order) || 0), 0)
    companyForm.name = ''
    companyForm.country = 'BY'
    companyForm.currency = 'BYN'
    companyForm.comment = ''
    companyForm.is_active = true
    companyForm.sort_order = maxOrder + 10
  }
  showCompanyModal.value = true
}
function closeCompanyModal() {
  showCompanyModal.value = false
  editingCompany.value = null
  companyError.value = ''
}

async function saveCompany() {
  if (!companyForm.name.trim() || companySaving.value) return
  companySaving.value = true
  companyError.value = ''
  try {
    const body = {
      name: companyForm.name.trim(),
      country: (companyForm.country || '').trim().toUpperCase() || 'BY',
      currency: companyForm.currency || 'BYN',
      comment: companyForm.comment.trim() || null,
      is_active: !!companyForm.is_active,
      sort_order: Number(companyForm.sort_order) || 0,
    }
    if (editingCompany.value) {
      await api(`/group/companies/${editingCompany.value.id}`, { method: 'PUT', body })
    } else {
      await api('/group/companies', { method: 'POST', body })
    }
    closeCompanyModal()
    await loadCompanies()
    // Имя компании показывается в звеньях — перечитываем и цепочки.
    await loadChains()
  } catch (e: any) {
    companyError.value = errText(e)
    console.error('[cost] group-pricing save company failed', e)
  } finally {
    companySaving.value = false
  }
}

async function deleteCompany(k: any) {
  if (!confirm(`Удалить компанию «${k.name}»?`)) return
  deletingCompanyId.value = k.id
  lastError.value = ''
  try {
    await api(`/group/companies/${k.id}`, { method: 'DELETE' })
    await loadCompanies()
  } catch (e: any) {
    // 409 «участвует в цепочках…» приходит русским detail — показываем как есть.
    lastError.value = errText(e)
    console.error('[cost] group-pricing delete company failed', e)
  } finally {
    deletingCompanyId.value = null
  }
}

const pluralChains = (n: number) => {
  const m10 = n % 10, m100 = n % 100
  if (m10 === 1 && m100 !== 11) return 'цепочке'
  return 'цепочках'
}

// ── Форматирование (ru-RU) ───────────────────────────────────────────────────
const nf0 = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 0 })
const nf2 = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 2 })
const isNum = (v: any) => v !== null && v !== undefined && v !== '' && Number.isFinite(Number(v))
const fmtInt = (v: any) => (isNum(v) ? nf0.format(Number(v)) : '—')
const fmtSignedPct = (v: any) => {
  if (!isNum(v)) return '—'
  const n = Number(v)
  const sign = n > 0 ? '+' : n < 0 ? '−' : ''
  return `${sign}${nf2.format(Math.abs(n))} %`
}
const signClass = (v: any) => (isNum(v) ? (Number(v) < 0 ? 'neg' : Number(v) > 0 ? 'pos' : '') : '')
const fmtDateTime = (iso: string) => {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? String(iso) : d.toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })
}
</script>

<style scoped>
.page-cost { padding-top: var(--sp-2); }

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: var(--sp-4);
}
.page-header-actions { display: flex; gap: var(--sp-3); }

/* Actions bar (как в admin/roles.vue) */
.cost-actions {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  padding: var(--sp-2) var(--sp-4);
  background: var(--bg-surface-2, var(--bg-surface));
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  margin-bottom: var(--sp-3);
  flex-wrap: wrap;
}
.gp-note { font-size: var(--fs-xs); margin-left: auto; }

/* Error banner */
.cost-error {
  display: flex;
  align-items: flex-start;
  gap: var(--sp-3);
  padding: var(--sp-4) var(--sp-5);
  border-radius: var(--rd-3);
  background: color-mix(in srgb, var(--neg) 8%, var(--bg-surface));
  border: 1px solid color-mix(in srgb, var(--neg) 30%, transparent);
  color: var(--text-strong);
  margin-bottom: var(--sp-5);
  position: relative;
}
.cost-error :deep(svg) { color: var(--neg); flex-shrink: 0; margin-top: 2px; }
.cost-error p { margin-top: 4px; font-size: var(--fs-sm); }
.cost-error code {
  background: var(--bg-surface-3);
  padding: 1px 5px;
  border-radius: var(--rd-1);
  font-family: var(--font-mono);
  font-size: 90%;
}
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

/* Вкладки */
.gp-tabs {
  display: flex;
  gap: var(--sp-2);
  border-bottom: 1px solid var(--border);
  margin-bottom: var(--sp-4);
}
.gp-tab {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  border: none;
  background: transparent;
  color: var(--text-muted);
  font-size: var(--fs-sm);
  padding: var(--sp-3) var(--sp-5);
  cursor: pointer;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
  transition: color var(--t-fast), border-color var(--t-fast);
}
.gp-tab:hover { color: var(--text-strong); }
.gp-tab.active {
  color: var(--accent);
  border-bottom-color: var(--accent);
  font-weight: var(--fw-semibold, 600);
}
.gp-tab-count {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--fs-2xs);
  padding: 0 6px;
  border-radius: var(--rd-pill);
  background: var(--bg-surface-3);
  color: var(--text-muted);
}

/* Карточки */
.gp-list { display: flex; flex-direction: column; gap: var(--sp-4); }
.gp-card.archived { opacity: 0.7; }
.gp-card.selected { border-color: var(--accent); }
.gp-card-header { align-items: flex-start; gap: var(--sp-4); }
.gp-card-main { display: flex; flex-direction: column; gap: var(--sp-2); min-width: 0; flex: 1; }
.gp-card-actions { display: flex; gap: var(--sp-2); flex-wrap: wrap; justify-content: flex-end; flex-shrink: 0; }
.gp-title { display: inline-flex; align-items: center; gap: var(--sp-2); flex-wrap: wrap; }

.gp-badge {
  display: inline-flex;
  align-items: center;
  font-size: var(--fs-2xs);
  padding: 2px 8px;
  border-radius: var(--rd-pill);
  background: color-mix(in srgb, var(--accent) 14%, var(--bg-surface));
  color: var(--accent);
  font-weight: var(--fw-semibold, 600);
  letter-spacing: 0.04em;
  text-transform: uppercase;
  white-space: nowrap;
}
.gp-badge-muted { background: var(--bg-surface-3); color: var(--text-muted); }
.gp-badge-neg { background: color-mix(in srgb, var(--neg) 12%, var(--bg-surface)); color: var(--neg); }

.gp-scope { display: flex; flex-wrap: wrap; gap: var(--sp-1) var(--sp-2); }
.gp-chip {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: var(--fs-2xs);
  padding: 2px 6px;
  border-radius: var(--rd-1);
  background: var(--bg-surface-3);
  border: 1px solid var(--border);
  color: var(--text-strong);
  max-width: 100%;
}
.gp-chip-k { color: var(--text-muted); }

.gp-links-line {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--sp-2);
  font-size: var(--fs-sm);
}
.gp-link-item { display: inline-flex; align-items: baseline; gap: var(--sp-2); }
.gp-link-company { color: var(--text-strong); }
.gp-link-pct {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--fs-xs);
}
.gp-link-pct.pos { color: var(--pos); }
.gp-link-pct.neg { color: var(--neg); }
.gp-link-cur { font-family: var(--font-mono); font-size: var(--fs-xs); color: var(--text-muted); }
.gp-link-arrow, .gp-link-market { font-size: var(--fs-xs); }

.gp-overlap {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  font-size: var(--fs-xs);
  color: var(--warn);
}
.gp-overlap :deep(svg) { width: 14px; height: 14px; }
.gp-meta { font-size: var(--fs-xs); }

.gp-result { margin-top: var(--sp-5); }

.gp-check {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  font-size: var(--fs-sm);
  cursor: pointer;
  color: var(--text-strong);
}
.gp-check input { width: 15px; height: 15px; cursor: pointer; }

/* Модалка компании */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  z-index: 10000;
  display: flex;
  justify-content: center;
  align-items: center;
}
.gp-modal {
  background: var(--bg-surface);
  border-radius: var(--rd-3);
  border: 1px solid var(--border);
  max-width: 560px;
  width: 92vw;
  max-height: 90vh;
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
.gp-modal-body {
  padding: var(--sp-4) var(--sp-5);
  overflow-y: auto;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: var(--sp-4);
}
/* Внутри модалки отступ баннера даёт flex-gap, внешний margin удвоил бы его. */
.gp-modal-body .cost-error { margin-bottom: 0; }
.gp-modal-footer {
  padding: var(--sp-3) var(--sp-5);
  border-top: 1px solid var(--border);
  display: flex;
  justify-content: flex-end;
  gap: var(--sp-3);
  flex-shrink: 0;
}
.gp-grid-2 {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--sp-4);
  align-items: end;
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

.spinning { animation: gp-spin 0.9s linear infinite; }
@keyframes gp-spin { to { transform: rotate(360deg); } }

.btn-danger { color: var(--neg); }
.btn-danger:hover { background: color-mix(in srgb, var(--neg) 10%, transparent); }
</style>
