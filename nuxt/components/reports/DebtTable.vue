<template>
  <div class="table-wrap report-table-wrap debt-table-wrap">
    <table class="data-table report-table debt-table">
      <thead>
        <!--
          Level 1 MVP (см. docs/reports/debt/open-questions.md §E): скрыты колонки
          «Отсрочка, дн.», «Дата оплаты по договору», «Выручка», «Просрочка, дн.» —
          для них пока нет источника данных / подтверждённой формулы.
          После ответов автора ТЗ (B3, C3) — вернуть колонки и пересчитать colspan.
        -->
        <tr>
          <th class="col-sticky col-article" rowspan="2">Группировка</th>
          <th rowspan="2">Счёт</th>
          <th rowspan="2">Субсчёт</th>
          <th rowspan="2">Договор</th>
          <th rowspan="2">Валюта</th>
          <th class="col-num group-th" colspan="2">На начало</th>
          <th class="col-num group-th" colspan="2">Обороты</th>
          <th class="col-num group-th" colspan="2">На конец</th>
        </tr>
        <tr>
          <th class="col-num">ДЗ</th>
          <th class="col-num">КЗ</th>
          <th class="col-num">ДЗ</th>
          <th class="col-num">КЗ</th>
          <th class="col-num">ДЗ</th>
          <th class="col-num">КЗ</th>
        </tr>
      </thead>
      <tbody>
        <template v-for="node in flat" :key="node.key">
          <!-- Группы Уровень 1..4 -->
          <tr
            v-if="node.kind === 'group'"
            class="row-group"
            :class="`lvl-${node.level}`"
            @click="toggleNode(node.key, node.expandable)"
          >
            <td class="col-sticky col-article">
              <span class="lvl-indent" :style="{ paddingLeft: `${node.level * 14}px` }">
                <Icon
                  v-if="node.expandable"
                  :name="isOpen(node.key) ? 'lucide:chevron-down' : 'lucide:chevron-right'"
                  class="row-chevron"
                />
                <span v-else class="row-chevron-spacer" />
                <span class="row-group-label" :title="node.label">{{ node.label }}</span>
              </span>
            </td>
            <td>{{ node.aggCols.account }}</td>
            <td>{{ node.aggCols.subaccount }}</td>
            <td>{{ node.aggCols.contract }}</td>
            <td>{{ node.aggCols.currency }}</td>
            <td class="col-num">{{ moneyAuto(node.sums.opening_dz, node.currency) }}</td>
            <td class="col-num">{{ moneyAuto(node.sums.opening_kz, node.currency) }}</td>
            <td class="col-num">{{ moneyAuto(node.sums.turnover_dz, node.currency) }}</td>
            <td class="col-num">{{ moneyAuto(node.sums.turnover_kz, node.currency) }}</td>
            <td class="col-num">{{ moneyAuto(node.sums.closing_dz, node.currency) }}</td>
            <td class="col-num">{{ moneyAuto(node.sums.closing_kz, node.currency) }}</td>
          </tr>

          <!-- Лист — строка-валюта -->
          <tr v-else-if="node.kind === 'leaf'" class="row-leaf">
            <td class="col-sticky col-article">
              <span class="lvl-indent" :style="{ paddingLeft: `${node.level * 14}px` }">
                <span class="row-chevron-spacer" />
                <span class="row-group-label" :title="node.label">{{ node.label }}</span>
              </span>
            </td>
            <td>{{ node.row.account }}</td>
            <td>{{ node.row.subaccount }}</td>
            <td :title="node.row.contract">{{ node.row.contract }}</td>
            <td>{{ node.row.currency }}</td>
            <td class="col-num">{{ moneyFmt(node.row.opening_dz, node.row.currency) }}</td>
            <td class="col-num">{{ moneyFmt(node.row.opening_kz, node.row.currency) }}</td>
            <td class="col-num">{{ moneyFmt(node.row.turnover_dz, node.row.currency) }}</td>
            <td class="col-num">{{ moneyFmt(node.row.turnover_kz, node.row.currency) }}</td>
            <td class="col-num">{{ moneyFmt(node.row.closing_dz, node.row.currency) }}</td>
            <td class="col-num">{{ moneyFmt(node.row.closing_kz, node.row.currency) }}</td>
          </tr>

          <!-- Подгруппа документов по trans_description (M5) -->
          <tr v-else-if="node.kind === 'trans-group'" class="row-leaf row-trans-group">
            <td class="col-sticky col-article">
              <span class="lvl-indent" :style="{ paddingLeft: `${node.level * 14}px` }">
                <span class="row-chevron-spacer" />
                <span class="row-group-label">{{ node.label }}</span>
                <span class="trans-count">×{{ node.count }}</span>
              </span>
            </td>
            <td>—</td>
            <td>—</td>
            <td>—</td>
            <td>{{ node.currency }}</td>
            <td class="col-num">{{ moneyFmt(node.sumAmount, node.currency) }}</td>
            <td class="col-num">—</td>
            <td class="col-num">—</td>
            <td class="col-num">—</td>
            <td class="col-num">—</td>
            <td class="col-num">—</td>
          </tr>

          <!-- Документы drilldown -->
          <tr v-else-if="node.kind === 'doc-loading'" class="row-leaf">
            <td colspan="11" class="docs-loading">Загружаем документы…</td>
          </tr>
          <tr v-else-if="node.kind === 'doc-error'" class="row-leaf">
            <td colspan="11" class="docs-error">{{ node.message }}</td>
          </tr>
          <tr v-else-if="node.kind === 'doc'" class="row-leaf row-doc">
            <td class="col-sticky col-article">
              <span class="lvl-indent" :style="{ paddingLeft: `${node.level * 14}px` }">
                <span class="row-chevron-spacer" />
                <span class="row-group-label">{{ node.doc.doc_kind }} № {{ node.doc.doc_number }}</span>
              </span>
            </td>
            <!-- 2 пустые группировочные колонки + дата + валюта + сумма -->
            <td>—</td>
            <td>—</td>
            <td>{{ formatDate(node.doc.doc_date) }}</td>
            <td>{{ node.currency }}</td>
            <!-- Сумма по документу (модуль проводок) — основной "вес" -->
            <td class="col-num">{{ node.doc.amount ? moneyFmt(node.doc.amount, node.currency) : "—" }}</td>
            <!-- Дельты ДЗ/КЗ — заполнены только для DZ/KZ-счетов, для счёта 90 (выручка) и др. — нули -->
            <td class="col-num">{{ node.doc.dz_change ? moneyFmt(node.doc.dz_change, node.currency) : "—" }}</td>
            <td class="col-num">{{ node.doc.kz_change ? moneyFmt(node.doc.kz_change, node.currency) : "—" }}</td>
            <!-- Описание операции — длинный текстовый слот через colspan на оставшиеся 3 числовые колонки -->
            <td colspan="3" class="col-desc" :title="node.doc.description || ''">
              {{ node.doc.description || "—" }}
            </td>
          </tr>
        </template>

        <tr v-if="!rows.length" class="row-leaf">
          <td colspan="11" class="empty-row">Нет данных по выбранным фильтрам.</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { money } from "~/utils/format";
import type { DebtRow, DebtDocumentRow } from "~/composables/useDebtReport";

const props = defineProps<{
  rows: DebtRow[];
  reportDate: string;
  dateFrom: string;
  dateTo: string;
  drilldown: (q: {
    company_inn: string;
    partner_inn: string;
    account: string;
    contract: string;
    currency: string;
    date_from: string;
    date_to: string;
  }) => Promise<DebtDocumentRow[]>;
}>();

// 5 — trans-group, 6 — документ внутри trans-group (M5).
type Level = 0 | 1 | 2 | 3 | 4 | 5 | 6;

interface RowSums {
  opening_dz: number;
  opening_kz: number;
  turnover_dz: number;
  turnover_kz: number;
  closing_dz: number;
  closing_kz: number;
  revenue_period: number;
  revenue_last_month: number;
}

interface GroupNode {
  kind: "group";
  key: string;
  level: Level;
  label: string;
  expandable: boolean;
  currency: string | "";     // если внутри одна валюта — её код, иначе ""
  sums: RowSums;
  aggCols: {
    account: string;
    subaccount: string;
    contract: string;
    paymentTerm: string;
    currency: string;
  };
}

interface LeafNode {
  kind: "leaf";
  key: string;
  level: Level;
  label: string;
  row: DebtRow;
}

interface DocLoadingNode {
  kind: "doc-loading";
  key: string;
  level: Level;
}

interface DocErrorNode {
  kind: "doc-error";
  key: string;
  level: Level;
  message: string;
}

interface DocNode {
  kind: "doc";
  key: string;
  level: Level;
  currency: string;
  doc: DebtDocumentRow;
}

// TransGroupNode — заголовок подгруппы документов внутри drill-down (M5).
// Документы разделены по trans_description (тип операции из 1С).
interface TransGroupNode {
  kind: "trans-group";
  key: string;
  level: Level;
  label: string;
  count: number;
  sumAmount: number;
  currency: string;
}

type Node = GroupNode | LeafNode | DocLoadingNode | DocErrorNode | DocNode | TransGroupNode;

// ── управляющее состояние раскрытия ───────────────────────────────────────
const openKeys = ref<Set<string>>(new Set());
const isOpen = (k: string) => openKeys.value.has(k);

// Deep-link раскрытия дерева: DebtTable — единственный владелец query-параметра
// `exp` (фильтры пишет DebtReport). Сериализуем открытые ключи JSON+URI; при
// заходе по ссылке восстанавливаем и лениво дотягиваем документы.
const route = useRoute();
const router = useRouter();
const EXP_MAX_LEN = 1800; // предел длины exp в URL; глубже — усекаем с предупреждением

const encodeOpen = (keys: string[]): string => encodeURIComponent(JSON.stringify(keys));
const decodeOpen = (raw: unknown): string[] => {
  if (typeof raw !== "string" || !raw) return [];
  try {
    const a = JSON.parse(decodeURIComponent(raw));
    return Array.isArray(a) ? a.filter((x): x is string => typeof x === "string") : [];
  } catch {
    return [];
  }
};

// Запись текущего раскрытия в URL (merge поверх фильтров DebtReport).
const writeExp = () => {
  const keys = [...openKeys.value];
  let encoded = encodeOpen(keys);
  if (encoded.length > EXP_MAX_LEN) {
    // не влезаем в URL — оставляем менее глубокие (короткие) ключи, остальное
    // не теряем молча, а сообщаем в консоль (см. plan Task 3.2).
    const sorted = [...keys].sort((a, b) => a.length - b.length);
    const kept: string[] = [];
    for (const k of sorted) {
      if (encodeOpen([...kept, k]).length > EXP_MAX_LEN) break;
      kept.push(k);
    }
    console.warn(`[DebtTable] раскрытие усечено в URL: ${kept.length}/${keys.length} узлов (лимит длины)`);
    encoded = encodeOpen(kept);
  }
  const q = { ...route.query };
  if (openKeys.value.size) q.exp = encoded;
  else delete q.exp;
  router.replace({ query: q }).catch(() => {});
};

// drilldown-cache: key (contract+currency) → DocumentRow[] | "loading" | {error: string}
const docCache = reactive<Record<string, DebtDocumentRow[] | "loading" | { error: string }>>({});

const toggleNode = async (key: string, expandable: boolean) => {
  if (!expandable) return;
  const next = new Set(openKeys.value);
  if (next.has(key)) {
    next.delete(key);
  } else {
    next.add(key);
    // Если это договор — лениво подгружаем документы
    if (key.startsWith("contract:")) {
      await ensureDocs(key);
    }
  }
  openKeys.value = next;
  writeExp();
};

// Восстановление раскрытия из URL при заходе по ссылке/перезагрузке. DebtTable
// перемонтируется на каждый новый отчёт, поэтому restore идёт один раз на mount
// с уже готовыми props.rows; ручное «Сформировать» сбрасывает exp в DebtReport.
onMounted(async () => {
  const keys = decodeOpen(route.query.exp);
  if (!keys.length) return;
  openKeys.value = new Set(keys);
  // дотягиваем документы для открытых договоров (docKey начинается с "contract:")
  for (const k of keys) {
    if (k.startsWith("contract:")) await ensureDocs(k);
  }
});

const ensureDocs = async (groupKey: string) => {
  // ключ группы договора имеет вид "contract:<companyINN>|<partnerINN>|<account>|<contract>|<currency>"
  if (docCache[groupKey] !== undefined) return;
  const payload = groupKey.slice("contract:".length).split("|");
  const [companyINN, partnerINN, account, contract, currency] = payload;
  docCache[groupKey] = "loading";
  try {
    const docs = await props.drilldown({
      company_inn: companyINN,
      partner_inn: partnerINN,
      account,
      contract,
      currency,
      date_from: props.dateFrom,
      date_to: props.dateTo
    });
    docCache[groupKey] = docs;
  } catch (e: any) {
    docCache[groupKey] = { error: e?.data?.error || e?.message || "Не удалось загрузить" };
  }
};

// ── построение группировки ────────────────────────────────────────────────
const emptySums = (): RowSums => ({
  opening_dz: 0, opening_kz: 0,
  turnover_dz: 0, turnover_kz: 0,
  closing_dz: 0, closing_kz: 0,
  revenue_period: 0, revenue_last_month: 0
});

const addSums = (a: RowSums, r: DebtRow): RowSums => ({
  opening_dz: a.opening_dz + r.opening_dz,
  opening_kz: a.opening_kz + r.opening_kz,
  turnover_dz: a.turnover_dz + r.turnover_dz,
  turnover_kz: a.turnover_kz + r.turnover_kz,
  closing_dz: a.closing_dz + r.closing_dz,
  closing_kz: a.closing_kz + r.closing_kz,
  revenue_period: a.revenue_period + r.revenue_period,
  revenue_last_month: a.revenue_last_month + r.revenue_last_month
});

const flat = computed<Node[]>(() => {
  if (!props.rows.length) return [];
  const out: Node[] = [];

  // Группировка: Компания → Партнёр → Счёт → Договор → строки-валюты
  const byCompany = new Map<string, DebtRow[]>();
  for (const r of props.rows) {
    const k = `${r.company_inn}__${r.company}`;
    if (!byCompany.has(k)) byCompany.set(k, []);
    byCompany.get(k)!.push(r);
  }

  for (const [cKey, cRows] of byCompany) {
    const companyName = cRows[0].company;
    const companyINN = cRows[0].company_inn;
    const cGroupKey = `company:${cKey}`;
    out.push({
      kind: "group",
      key: cGroupKey,
      level: 0,
      label: companyName,
      expandable: true,
      currency: oneCurrency(cRows),
      sums: cRows.reduce((acc, r) => addSums(acc, r), emptySums()),
      aggCols: { account: "—", subaccount: "—", contract: "—", paymentTerm: "—", currency: oneCurrency(cRows) || "разные" }
    });
    if (!isOpen(cGroupKey)) continue;

    // партнёры внутри компании
    const byPartner = new Map<string, DebtRow[]>();
    for (const r of cRows) {
      const k = `${r.partner_inn || ""}__${r.partner}`;
      if (!byPartner.has(k)) byPartner.set(k, []);
      byPartner.get(k)!.push(r);
    }
    for (const [pKey, pRows] of byPartner) {
      const partnerName = pRows[0].partner;
      const partnerINN = pRows[0].partner_inn || "";
      const pGroupKey = `${cGroupKey}/partner:${pKey}`;
      out.push({
        kind: "group",
        key: pGroupKey,
        level: 1,
        label: partnerName,
        expandable: true,
        currency: oneCurrency(pRows),
        sums: pRows.reduce((acc, r) => addSums(acc, r), emptySums()),
        aggCols: { account: "—", subaccount: "—", contract: "—", paymentTerm: "—", currency: oneCurrency(pRows) || "разные" }
      });
      if (!isOpen(pGroupKey)) continue;

      // счета внутри партнёра
      const byAccount = new Map<string, DebtRow[]>();
      for (const r of pRows) {
        const k = r.account;
        if (!byAccount.has(k)) byAccount.set(k, []);
        byAccount.get(k)!.push(r);
      }
      for (const [accKey, accRows] of byAccount) {
        const accountName = accRows[0].account_name;
        const accGroupKey = `${pGroupKey}/account:${accKey}`;
        out.push({
          kind: "group",
          key: accGroupKey,
          level: 2,
          label: `Счёт ${accKey} — ${accountName}`,
          expandable: true,
          currency: oneCurrency(accRows),
          sums: accRows.reduce((acc, r) => addSums(acc, r), emptySums()),
          aggCols: { account: accKey, subaccount: "—", contract: "—", paymentTerm: "—", currency: oneCurrency(accRows) || "разные" }
        });
        if (!isOpen(accGroupKey)) continue;

        // договоры. Группируем по стабильной ссылке (contract_ref), а не по имени —
        // разные договоры с похожим именем не сольются, и drill-down фильтрует точно.
        const byContract = new Map<string, DebtRow[]>();
        for (const r of accRows) {
          const k = r.contract_ref || r.contract || "(без договора)";
          if (!byContract.has(k)) byContract.set(k, []);
          byContract.get(k)!.push(r);
        }
        for (const [ctrRefKey, ctrRows] of byContract) {
          const ctrLabel = ctrRows[0].contract || "(без договора)";
          const ctrRef = ctrRows[0].contract_ref || ""; // что уходит в drilldown для фильтра
          const ctrGroupKey = `${accGroupKey}/contract:${ctrRefKey}`;
          out.push({
            kind: "group",
            key: ctrGroupKey,
            level: 3,
            label: ctrLabel,
            expandable: true,
            currency: oneCurrency(ctrRows),
            sums: ctrRows.reduce((acc, r) => addSums(acc, r), emptySums()),
            aggCols: {
              account: accKey,
              subaccount: ctrRows[0].subaccount,
              contract: ctrLabel,
              paymentTerm: String(ctrRows[0].payment_term_days || 0),
              currency: oneCurrency(ctrRows) || "разные"
            }
          });
          if (!isOpen(ctrGroupKey)) continue;

          // строки-валюты + drilldown по документам
          for (const r of ctrRows) {
            const leafKey = `${ctrGroupKey}/cur:${r.currency}`;
            out.push({
              kind: "leaf",
              key: leafKey,
              level: 4,
              label: `Валюта: ${r.currency}`,
              row: r
            });

            // ключ drilldown — отдельный, чтобы переиспользовать для подгрузки.
            // В сегмент договора кладём сырую ссылку (ctrRef) — по ней бэк точно
            // фильтрует документы; пустая ссылка = группа «без договора».
            const docKey = `contract:${companyINN}|${partnerINN}|${accKey}|${ctrRef}|${r.currency}`;
            if (!isOpen(docKey)) {
              // не открыто — рисуем мини-кнопку «развернуть документы»
            }
            // Always show a small toggle row under leaf to drill into docs
            out.push({
              kind: "group",
              key: docKey,
              level: 4,
              label: "Документы",
              expandable: true,
              currency: r.currency,
              sums: emptySums(),
              aggCols: { account: "—", subaccount: "—", contract: "—", paymentTerm: "—", currency: r.currency }
            });

            if (isOpen(docKey)) {
              const docs = docCache[docKey];
              if (docs === "loading" || docs === undefined) {
                out.push({ kind: "doc-loading", key: `${docKey}/loading`, level: 5 as any });
              } else if (typeof docs === "object" && "error" in docs) {
                out.push({ kind: "doc-error", key: `${docKey}/error`, level: 5 as any, message: docs.error });
              } else {
                // M5: группируем документы по trans_group (тип операции из 1С).
                // Внутри каждой подгруппы — сортируем по дате.
                const byTrans = new Map<string, DebtDocumentRow[]>();
                for (const d of docs) {
                  const label = (d.trans_group || d.doc_kind || "Без типа").trim();
                  if (!byTrans.has(label)) byTrans.set(label, []);
                  byTrans.get(label)!.push(d);
                }
                // Стабильный порядок: подгруппы — по убыванию суммы.
                const entries = [...byTrans.entries()].map(([label, items]) => ({
                  label,
                  items,
                  sumAmount: items.reduce((s, x) => s + (x.amount || 0), 0)
                }));
                entries.sort((a, b) => Math.abs(b.sumAmount) - Math.abs(a.sumAmount));

                for (const { label, items, sumAmount } of entries) {
                  out.push({
                    kind: "trans-group",
                    key: `${docKey}/tg:${label}`,
                    level: 5 as any,
                    label,
                    count: items.length,
                    sumAmount,
                    currency: r.currency
                  });
                  for (const d of items) {
                    out.push({
                      kind: "doc",
                      key: `${docKey}/tg:${label}/doc:${d.doc_number}-${d.doc_date}`,
                      level: 6 as any,
                      currency: r.currency,
                      doc: d
                    });
                  }
                }
              }
            }
          }
        }
      }
    }
  }
  return out;
});

const oneCurrency = (rows: DebtRow[]): string => {
  const s = new Set(rows.map((r) => r.currency));
  return s.size === 1 ? [...s][0] : "";
};

const moneyFmt = (v: number, ccy: string): string => {
  if (!v) return "—";
  return money(v, { currency: ccy as any });
};
const moneyAuto = (v: number, ccy: string | ""): string => {
  if (!v) return "—";
  if (ccy) return money(v, { currency: ccy as any });
  // multi-currency aggregate — без символа валюты
  return new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 2 }).format(v);
};
const formatDate = (s: string): string => {
  if (!s) return "—";
  const d = new Date(s);
  if (Number.isNaN(d.getTime())) return s;
  return new Intl.DateTimeFormat("ru-RU", { day: "2-digit", month: "2-digit", year: "numeric" }).format(d);
};
</script>

<style scoped>
.debt-table-wrap {
  max-height: calc(100vh - 320px);
  overflow: auto;
}
.debt-table {
  font-variant-numeric: tabular-nums;
}
.debt-table .col-article {
  min-width: 320px;
  font-weight: var(--fw-medium);
}
.debt-table th {
  white-space: nowrap;
}
.debt-table th.group-th {
  text-align: center;
  border-left: 1px solid var(--border);
  border-right: 1px solid var(--border);
  background: var(--bg-surface-2);
}
.row-group {
  cursor: pointer;
}
.row-group.lvl-0 { background: var(--bg-surface-2); }
.row-group.lvl-0 td { font-weight: var(--fw-semibold); }
.row-group.lvl-1 td { font-weight: var(--fw-medium); }
.row-group.lvl-2 td { font-weight: var(--fw-medium); }
.row-group.lvl-3 td { font-style: italic; }
.row-group:hover td { background: var(--bg-surface-3); }
.row-group:hover .col-sticky { background: var(--bg-surface-3); }
.row-chevron {
  width: 14px;
  height: 14px;
  margin-right: 6px;
  vertical-align: middle;
  color: var(--text-muted);
}
.row-chevron-spacer {
  display: inline-block;
  width: 20px;
}
.lvl-indent {
  display: inline-flex;
  align-items: center;
}
.row-group-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 360px;
  display: inline-block;
}
.row-leaf td {
  font-size: var(--fs-sm);
  color: var(--text-secondary);
}
.row-doc td {
  font-size: var(--fs-xs);
  background: var(--bg-surface);
}
.col-desc {
  max-width: 320px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--text-secondary);
  font-style: italic;
}
.row-trans-group td {
  background: var(--bg-surface-hover, var(--bg-surface));
  font-weight: 500;
  font-size: var(--fs-xs);
}
.trans-count {
  margin-left: 0.5em;
  padding: 0.05em 0.4em;
  background: var(--bg-surface);
  color: var(--text-muted);
  border-radius: 3px;
  font-size: 0.8em;
  font-weight: 400;
  font-variant-numeric: tabular-nums;
}
.docs-loading, .docs-error, .empty-row {
  text-align: center;
  padding: var(--sp-4);
  color: var(--text-muted);
  font-style: italic;
}
.docs-error { color: var(--neg); }
</style>
