// Навигация по сетке ввода «как в Excel»: стрелки, Tab/Enter, вставка диапазона
// из буфера обмена (ТЗ Розница §4.3 «ввод как в Excel»).
//
// Вынесено из страницы формы намеренно: то же самое требование стоит и в форме
// МП, а поведение клавиатуры — ровно тот код, который расходится между двумя
// копиями быстрее всего (в одной форме Enter уходит вниз, в другой вправо — и
// пользователь, работающий с обеими, перестаёт доверять обеим).
//
// Композабл НИЧЕГО не знает о данных: он оперирует координатами (строка,
// колонка) и находит DOM-узел по атрибуту data-grid-cell="строка:колонка".
// Это позволяет использовать его и с виртуализованным списком — строки, которой
// нет в DOM, композабл дожидается через ensureVisible + nextTick.

export interface GridPos {
  row: number;
  col: number;
}

export interface GridNavOptions {
  /** Число строк сетки (реактивно вычисляется вызывающим). */
  rows: () => number;
  /** Число колонок ввода. */
  cols: () => number;
  /**
   * Вставка диапазона из буфера: matrix[i][j] — сырой текст ячейки.
   * Вызывающий сам решает, как разобрать число и куда положить (он знает
   * маппинг координат на code_cfo/месяц).
   */
  onPaste?: (anchor: GridPos, matrix: string[][]) => void;
  /**
   * Показать строку перед фокусом. Нужно при виртуализации: строка вне окна
   * рендера отсутствует в DOM, и focus() было бы некуда ставить.
   */
  ensureVisible?: (row: number) => void;
}

export function useGridNav(opts: GridNavOptions) {
  /** Контейнер сетки — область поиска ячеек и приёмник события paste. */
  const container = ref<HTMLElement | null>(null);
  /** Активная ячейка. null — фокус вне сетки. */
  const active = ref<GridPos | null>(null);

  const clamp = (v: number, max: number) => Math.max(0, Math.min(v, max - 1));

  const cellEl = (p: GridPos): HTMLInputElement | null => {
    const root = container.value;
    if (!root) return null;
    return root.querySelector<HTMLInputElement>(`[data-grid-cell="${p.row}:${p.col}"]`);
  };

  /**
   * Поставить фокус в ячейку. Асинхронно: между запросом и фокусом может
   * потребоваться прокрутка и перерисовка виртуального окна.
   */
  const focusCell = async (row: number, col: number) => {
    const r = clamp(row, opts.rows());
    const c = clamp(col, opts.cols());
    if (opts.rows() === 0 || opts.cols() === 0) return;
    active.value = { row: r, col: c };
    opts.ensureVisible?.(r);
    await nextTick();
    let el = cellEl({ row: r, col: c });
    if (!el) {
      // Виртуализатор мог отрисовать окно только после собственного тика —
      // даём ему ещё один кадр, прежде чем сдаться.
      await new Promise((res) => requestAnimationFrame(() => res(null)));
      el = cellEl({ row: r, col: c });
    }
    if (!el) return;
    el.focus();
    el.select?.();
  };

  const move = (dRow: number, dCol: number, from: GridPos) => {
    const rows = opts.rows();
    const cols = opts.cols();
    let r = from.row + dRow;
    let c = from.col + dCol;
    // Перенос по краю колонок — как Tab в Excel: с конца строки на начало следующей.
    if (c >= cols) {
      c = 0;
      r += 1;
    } else if (c < 0) {
      c = cols - 1;
      r -= 1;
    }
    if (r < 0 || r >= rows) return;
    void focusCell(r, c);
  };

  /**
   * Обработчик клавиш на ячейке. Вызывается из шаблона: @keydown="nav.onKeydown($event, ri, ci)".
   *
   * Влево/вправо перехватываем ТОЛЬКО на границе текста — внутри значения
   * стрелки должны двигать каретку, иначе число не отредактировать посимвольно.
   */
  const onKeydown = (e: KeyboardEvent, row: number, col: number) => {
    const here: GridPos = { row, col };
    const el = e.target as HTMLInputElement | null;
    const atStart = !el || (el.selectionStart === 0 && el.selectionEnd === 0);
    const atEnd =
      !el || (el.selectionStart === el.value.length && el.selectionEnd === el.value.length);

    switch (e.key) {
      case "ArrowUp":
        e.preventDefault();
        move(-1, 0, here);
        return;
      case "ArrowDown":
        e.preventDefault();
        move(1, 0, here);
        return;
      case "Enter":
        e.preventDefault();
        move(e.shiftKey ? -1 : 1, 0, here);
        return;
      case "Tab":
        e.preventDefault();
        move(0, e.shiftKey ? -1 : 1, here);
        return;
      case "ArrowLeft":
        if (!atStart) return;
        e.preventDefault();
        move(0, -1, here);
        return;
      case "ArrowRight":
        if (!atEnd) return;
        e.preventDefault();
        move(0, 1, here);
        return;
      case "Home":
        if (!e.ctrlKey) return;
        e.preventDefault();
        void focusCell(0, 0);
        return;
      case "End":
        if (!e.ctrlKey) return;
        e.preventDefault();
        void focusCell(opts.rows() - 1, opts.cols() - 1);
        return;
      case "Escape":
        el?.blur();
        active.value = null;
    }
  };

  /** Запомнить активную ячейку при клике/фокусе мышью — от неё считается вставка. */
  const onFocusCell = (row: number, col: number) => {
    active.value = { row, col };
  };

  /**
   * Вставка диапазона. Многострочный текст из Excel приходит как строки,
   * разделённые \n, и колонки, разделённые \t. Одиночное значение НЕ
   * перехватываем — пусть работает обычная вставка в поле.
   */
  const onPaste = (e: ClipboardEvent) => {
    if (!opts.onPaste || !active.value) return;
    const text = e.clipboardData?.getData("text/plain");
    if (!text) return;
    const lines = text.replace(/\r\n?/g, "\n").split("\n");
    // Excel добавляет завершающий перевод строки — иначе получили бы пустую строку.
    while (lines.length && lines[lines.length - 1] === "") lines.pop();
    if (lines.length === 0) return;
    const matrix = lines.map((l) => l.split("\t"));
    if (matrix.length === 1 && matrix[0].length === 1) return;
    e.preventDefault();
    opts.onPaste(active.value, matrix);
  };

  /**
   * Разбор числа «как из Excel»: пробелы-разделители тысяч (в т.ч. NBSP),
   * запятая как десятичный разделитель, пустая ячейка = null (пусто ≠ 0 — этого
   * различия требует V-01).
   */
  const parseCellNumber = (raw: string): number | null => {
    const s = String(raw).replace(/\s/g, "").replace(/ /g, "").replace(",", ".");
    if (s === "" || s === "-") return null;
    const n = Number(s);
    return Number.isFinite(n) ? n : null;
  };

  return { container, active, focusCell, onKeydown, onFocusCell, onPaste, parseCellNumber };
}
