/**
 * Состояние формы TPL-MP (этап 1.1): загрузка матрицы (факт read-only + тактика),
 * редактирование тактики и сохранение. См. docs/reports/plans/SPEC.md §9, VS3.
 */
import { usePlans, type MpFormData, type SaveMpRow } from "~/composables/usePlans";

export const usePlanForm = (
  segment: Ref<"large" | "small">,
  year: Ref<number>,
  month: Ref<number>,
  currency: Ref<string>
) => {
  const { mpForm, saveMpForm } = usePlans();

  const form = ref<MpFormData | null>(null);
  const loading = ref(false);
  const saving = ref(false);
  const error = ref("");
  const savedAt = ref<string>("");
  // Причина корректировки (ADJ-02) — обязательна, если есть изменения тактики.
  const reason = ref("");
  // Снимок исходных значений тактики для детекции ручных правок.
  const baseline = ref<Record<string, number | null>>({});

  const cellKey = (codePl: number, codeCfo: number) => `${codePl}:${codeCfo}`;

  const snapshot = () => {
    const snap: Record<string, number | null> = {};
    for (const b of form.value?.blocks ?? []) {
      for (const r of b.rows) snap[cellKey(b.code_pl, r.code_cfo)] = r.tactic ?? null;
    }
    baseline.value = snap;
  };

  const load = async () => {
    loading.value = true;
    error.value = "";
    try {
      form.value = await mpForm({
        year: year.value,
        month: month.value,
        segment: segment.value,
        currency: currency.value
      });
      snapshot();
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : "Ошибка загрузки формы";
      form.value = null;
    } finally {
      loading.value = false;
    }
  };

  const save = async () => {
    if (!form.value) return;
    saving.value = true;
    error.value = "";
    try {
      const rows: SaveMpRow[] = [];
      let hasChanges = false;
      for (const b of form.value.blocks) {
        if (!b.editable) continue;
        for (const r of b.rows) {
          if (r.tactic === null || r.tactic === undefined) continue;
          const changed = Number(r.tactic) !== (baseline.value[cellKey(b.code_pl, r.code_cfo)] ?? null);
          if (changed) hasChanges = true;
          rows.push({
            code_cfo: r.code_cfo,
            code_pl: b.code_pl,
            block_type: b.block_type,
            amount: Number(r.tactic),
            is_manual: changed,
            comment: changed ? reason.value : undefined
          });
        }
      }
      if (hasChanges && !reason.value.trim()) {
        error.value = "Укажите причину корректировки (ADJ-02)";
        return;
      }
      await saveMpForm({
        template_code: "TPL-MP",
        segment: segment.value,
        period: { year: year.value, month: month.value },
        header: { currency: form.value.header.currency || "RUB", scenario: form.value.header.scenario },
        rows
      });
      savedAt.value = new Date().toLocaleTimeString("ru-RU");
      reason.value = "";
      await load();
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : "Ошибка сохранения";
    } finally {
      saving.value = false;
    }
  };

  return { form, loading, saving, error, savedAt, reason, load, save };
};
