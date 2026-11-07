"""Движок выгрузки «1:1 как в деке»: актив + данные → pptx.

Один запрос = один `Presentation`: шаблон читается из кеша bytes и каждый раз
раскрывается в новый объект через BytesIO (объекты python-pptx между запросами
не переиспользуются — иначе клоны прототипов потащили бы правки прошлого
запроса).

Порядок работы:
  * актив разбирается один раз: имена прототипов, ёмкости таблиц, цвета УФ —
    всё из manifest.json, ни одного литерала деки в коде;
  * по registry.SHEETS для каждой вкладки строится payload (collect) и слайды:
    на каждый прототип — клон на вариант данных, затем binder заполняет клон;
  * прототипы из актива удаляются (в выгрузку они попадать не должны);
  * сквозная нумерация страниц 1..N подставляется в {{page}};
  * финальная проверка: в текстах не осталось «{{» — иначе исключение с именем
    шейпа (лучше упасть, чем отдать пользователю шаблон с плейсхолдерами).

Согласованность актива и манифеста (B7/R2): manifest.json хранит sha256 файла
актива, а кеш байтов перепроверяется по mtime обоих файлов на каждом запросе
(os.stat — дёшево). Пересобрали актив, не перезапустив контейнер, — кеш
перечитается сам; если же актив и манифест разошлись (правили файл руками или
собрали только один из них), выгрузка падает с внятной причиной, а не отдаёт
данные по чужой вёрстке.
"""

from __future__ import annotations

import hashlib
import io
import logging
import re
from pathlib import Path

from app.fot_deck_export import collect as collect_mod
from app.fot_deck_export import format as format_mod
from app.fot_deck_export import registry, shapes
from app.fot_deck_export.errors import AssetMismatch, EmptySlice
from app.fot_deck_export.registry import SHEETS, sheet_for
from app.logship import log

try:
    from pptx import Presentation
except ImportError as exc:  # python-pptx — опциональная зависимость образа
    Presentation = None
    PPTX_IMPORT_ERROR = str(exc)
else:
    PPTX_IMPORT_ERROR = None

_P_NS = "{http://schemas.openxmlformats.org/presentationml/2006/main}"

#: Кеш байтов актива: файл читается один раз на процесс (сам Presentation
#: создаётся заново на каждый запрос).
_TEMPLATE_BYTES: bytes | None = None
_MANIFEST: dict | None = None
#: Отпечаток файлов актива на момент кеширования: (mtime_ns, size) шаблона и
#: манифеста. Изменился — кеш перечитывается без рестарта контейнера.
_STAMP: tuple | None = None


def _stamp(template: Path, manifest: Path) -> tuple:
    def one(path: Path) -> tuple:
        try:
            st = path.stat()
        except OSError:
            return (0, 0)
        return (st.st_mtime_ns, st.st_size)
    return one(template), one(manifest)


def _sha256(blob: bytes) -> str:
    return hashlib.sha256(blob).hexdigest()


def _read_asset() -> tuple[bytes, dict]:
    """Читает актив и манифест с диска и сверяет их отпечатки."""
    template, manifest_path = shapes.TEMPLATE_PATH, shapes.MANIFEST_PATH
    if not template.is_file():
        raise AssetMismatch(
            "актив деки не найден: %s — соберите его скриптом "
            "scripts/build_fot_deck_template.py" % template)
    if not manifest_path.is_file():
        raise AssetMismatch(
            "манифест актива не найден: %s — соберите его скриптом "
            "scripts/build_fot_deck_template.py" % manifest_path)
    blob = template.read_bytes()
    manifest = shapes.load_manifest(manifest_path)
    want = manifest.get("asset_sha256")
    if not want:
        raise AssetMismatch(
            "в манифесте нет asset_sha256 — актив собран старой версией "
            "scripts/build_fot_deck_template.py; пересоберите актив")
    got = _sha256(blob)
    if got != want:
        raise AssetMismatch(
            "актив не соответствует manifest — пересоберите актив скриптом "
            "scripts/build_fot_deck_template.py (sha256 актива %s, в манифесте %s)"
            % (got[:12], want[:12]))
    return blob, manifest


def load_template(path: Path | None = None) -> bytes:
    """Байты актива из кеша (None-путь — штатный файл репозитория)."""
    global _TEMPLATE_BYTES
    if path is not None:
        return Path(path).read_bytes()
    _refresh_cache()
    return _TEMPLATE_BYTES


def load_manifest(path: Path | None = None) -> dict:
    """manifest.json из кеша (ёмкости таблиц, цвета УФ, текстовые константы)."""
    global _MANIFEST
    if path is not None:
        return shapes.load_manifest(path)
    _refresh_cache()
    return _MANIFEST


def _refresh_cache() -> None:
    """Перечитывает актив и манифест, если файлы изменились (или кеша нет).

    Проверка — два os.stat на запрос: пересборка актива подхватывается без
    рестарта контейнера, а рассинхрон файлов ловится тем же путём (B7).
    """
    global _TEMPLATE_BYTES, _MANIFEST, _STAMP
    stamp = _stamp(shapes.TEMPLATE_PATH, shapes.MANIFEST_PATH)
    if _STAMP == stamp and _TEMPLATE_BYTES is not None and _MANIFEST is not None:
        return
    _TEMPLATE_BYTES, _MANIFEST = _read_asset()
    _STAMP = stamp
    log(logging.INFO, "fot_deck: актив перечитан",
        sha256=_sha256(_TEMPLATE_BYTES)[:12], bytes=len(_TEMPLATE_BYTES))


def reset_cache() -> None:
    """Сбрасывает кеш актива (после пересборки шаблона в тестах/на стенде)."""
    global _TEMPLATE_BYTES, _MANIFEST, _STAMP
    _TEMPLATE_BYTES = None
    _MANIFEST = None
    _STAMP = None


class SlideContext:
    """Контекст одного прототипа: клоны слайдов, манифест, данные.

    Биндер видит только этот объект — ни prs, ни путей к активу у него нет,
    поэтому «сколько слайдов получилось» и «куда писать» решает движок.
    """

    def __init__(self, engine: "DeckEngine", proto: str, slides: list) -> None:
        self._engine = engine
        self.proto_name = proto
        self.tab = engine.tab
        self._slides = slides
        self.sign = engine.manifest["sign_colors"]
        # границы светофора «% укомпл.» сняты с деки при сборке актива
        self.pct_bands = engine.manifest.get("pct_bands")
        self._proto_meta = engine.manifest["prototypes"][proto]
        # типографика прототипа: разряды и знаки процентов (тоже из деки)
        self.fmt = format_mod.fmt_of(self._proto_meta.get("format"))
        # цвета ранов полосы «Итого» — позиционные, снятые с деки
        self.total_bar_colors = engine.manifest.get("total_bar_colors")

    # слайды
    def slide(self, page_idx: int = 0):
        """Клон прототипа для страницы page_idx (создаётся по требованию).

        Пагинация рождается в биндере: если данных больше, чем помещается на
        один слайд, биндер просит следующую страницу — движок клонирует ещё
        один прототип и запоминает его в общем списке выгрузки.
        """
        if page_idx < len(self._slides):
            return self._slides[page_idx]
        while len(self._slides) <= page_idx:
            self._slides.append(self._engine.clone(proto=self.proto_name,
                                                   page=len(self._slides)))
        return self._slides[page_idx]

    # таблицы
    def table_names(self, proto: str | None = None) -> list[str]:
        """Имена таблиц прототипа в порядке документа (из манифеста)."""
        meta = self._engine.manifest["prototypes"][proto or self.proto_name]
        return list(meta["tables"].keys())

    def capacity(self, table: str) -> int:
        """Максимум строк тела в таблице прототипа (из манифеста)."""
        meta = self._engine.manifest["prototypes"][self.proto_name]["tables"]
        if table not in meta:
            raise RuntimeError("таблица %r не описана в манифесте прототипа %r"
                               % (table, self.proto_name))
        return int(meta[table]["capacity"])

    def proto_rows(self, table: str) -> list[str]:
        """Порядок строк-прототипов таблицы (header / tinted / plain / total)."""
        meta = self._engine.manifest["prototypes"][self.proto_name]["tables"]
        return list(meta[table]["row_protos"])

    def table_protos(self, frame, table: str) -> dict:
        """Строки-прототипы таблицы с проверкой их заливок против манифеста."""
        return shapes.table_protos(frame, self.proto_rows(table),
                                   self._engine.manifest.get("row_colors"))

    def deck_rows(self, table: str) -> int:
        """Сколько строк было в таблице деки (для высоты graphicFrame)."""
        meta = self._engine.manifest["prototypes"][self.proto_name]["tables"]
        return int(meta[table]["deck_rows"])

    def card_count(self, proto: str | None = None) -> int:
        """Сколько карточек в сетке прототипа (из манифеста).

        Сетки карточек листов 7/8 раскладывают отделы по местам, и биндер
        обязан покрыть КАЖДУЮ ячейку: данных меньше — лишние ячейки удаляются,
        больше — открывается страница-продолжение. Число ячеек — свойство
        прототипа (4×4 у листа 8, 6 у листа 7), поэтому оно в манифесте.
        """
        meta = self._engine.manifest["prototypes"][proto or self.proto_name]
        count = meta.get("card_count")
        if not count:
            raise RuntimeError("в манифесте прототипа %r нет card_count"
                               % (proto or self.proto_name))
        return int(count)

    def is_total_row(self, table: str) -> bool:
        """Есть ли у таблицы строка-прототип итога (у KZ — вверху таблицы)."""
        return any(name.startswith("total") for name in self.proto_rows(table))

    def const(self, proto: str, key: str) -> str:
        """Текстовая константа деки, снятая при сборке актива («•  » и т. п.)."""
        meta = self._engine.manifest["prototypes"][proto]
        consts = meta.get("consts") or {}
        if key not in consts:
            raise RuntimeError("в манифесте прототипа %r нет константы %r"
                               % (proto, key))
        return consts[key]

    def fmt_of(self, proto: str | None = None) -> dict:
        """Флаги типографики прототипа (разряды, знаки процентов) — из манифеста."""
        meta = self._engine.manifest["prototypes"][proto or self.proto_name]
        return format_mod.fmt_of(meta.get("format"))

    # подписи-шаблоны
    def label(self, name: str, *, year=None, prev=None, country=None,
              values=(), proto: str | None = None) -> list[list[str]]:
        """Подпись шейпа по шаблону деки: абзацы → раны, готовые к записи.

        В манифесте подписи лежат прорезями («Динамика, п.п. ({v1}ПГ{y2}/…): »):
        так формулировка остаётся декиной, а год и значения подставляет биндер.
        Возвращает [[текст рана, …], …] — по абзацам, чтобы биндер писал раны
        как есть и не собирал подпись сам (правка деки не потребует правки кода).

        Прорези: ``{year}``/``{prev}`` — полные годы отчётного/прошлого периода,
        ``{y2}``/``{p2}`` — их двузначные хвосты, ``{country}`` — страна листа,
        ``{vN}`` — значения из `values` по порядку (сквозная нумерация по шейпу).
        """
        meta = self._engine.manifest["prototypes"][proto or self.proto_name]
        slots = (meta.get("labels") or {}).get(name)
        if slots is None:
            raise RuntimeError("в манифесте прототипа %r нет подписи %r"
                               % (proto or self.proto_name, name))
        try:
            return format_mod.fill_label(slots, year=year, prev=prev,
                                         country=country, values=values)
        except RuntimeError as exc:
            raise RuntimeError("%s/%s: %s" % (proto or self.proto_name, name, exc))

    def has_label(self, name: str, proto: str | None = None) -> bool:
        """Есть ли у прототипа снятая подпись (для необязательных подписей)."""
        meta = self._engine.manifest["prototypes"][proto or self.proto_name]
        return name in (meta.get("labels") or {})

    def header_labels(self, table: str, *, year=None, prev=None, country=None,
                      values=(), proto: str | None = None) -> list[list[str]]:
        """Подписи шапки таблицы по шаблонам деки: [колонка][абзац].

        Дека ломает подписи на абзацы («Факт» / «1 ПГ 2025») — это её
        типографика, и биндер обязан писать по строке на абзац, иначе узкая
        колонка поедет. Годы подставляются те же, что и в подписях шейпов.
        """
        meta = self._engine.manifest["prototypes"][proto or self.proto_name]
        cells = (meta.get("header_labels") or {}).get(table)
        if cells is None:
            raise RuntimeError("в манифесте прототипа %r нет шапки таблицы %r"
                               % (proto or self.proto_name, table))
        return format_mod.fill_header_labels(cells, year=year, prev=prev,
                                             country=country, values=values)


class DeckEngine:
    """Сборка одной выгрузки: актив + payload'ы вкладок → pptx-байты."""

    def __init__(self, template_bytes: bytes, manifest: dict) -> None:
        if Presentation is None:
            raise RuntimeError(
                "python-pptx не установлен в образе python-cost: "
                "пересоберите образ. Причина: %s" % PPTX_IMPORT_ERROR)
        self.manifest = manifest
        self.prs = Presentation(io.BytesIO(template_bytes))
        self._proto_slides = {slide._element.find(_P_NS + "cSld").get("name"): slide
                              for slide in self.prs.slides}
        self._made: list = []          # выходные слайды в порядке добавления
        self._pages: list = []         # (слайд, прототип) — для нумерации страниц
        self.tab: str = ""             # вкладка, которая строится сейчас
        self.proto: str = ""           # прототип, на котором упали/строим

    def clone(self, proto: str, page: int):
        """Клон прототипа в конец презентации с говорящим именем."""
        if proto not in self._proto_slides:
            self.proto = proto
            raise RuntimeError("в активе нет прототипа %r (есть: %s)"
                               % (proto, ", ".join(self._proto_slides)))
        self.proto = proto
        name = "%s%s:%s:%d" % (shapes.SLIDE_NAME_PREFIX, self.tab, proto, page)
        slide = shapes.clone_slide(self.prs, self._proto_slides[proto], name)
        self._made.append(slide)
        self._pages.append((slide, proto))
        return slide

    def build_tab(self, tab: str, payload: dict) -> None:
        """Строит слайды одной вкладки по реестру (порядок записей = порядок)."""
        self.tab = tab
        for spec in sheet_for(tab).slides:
            self.proto = spec.proto
            proto_slide = self._proto_slides.get(spec.proto)
            if proto_slide is None:
                raise RuntimeError("в активе нет прототипа %r" % spec.proto)
            variants = spec.variants(payload) if spec.variants else [(None, payload)]
            if not variants:
                # данных под прототип нет (например, страна без строк) — пропуск
                log(logging.INFO, "fot_deck: прототип пропущен — нет данных",
                    tab=tab, proto=spec.proto)
                continue
            for variant, data in variants:
                ctx = SlideContext(self, spec.proto, [])
                self.proto = spec.proto
                spec.binder(ctx, data, variant)

    def finish(self) -> bytes:
        """Нумерация страниц, удаление прототипов, проверка на «{{», сохранение."""
        if not self._pages:
            # ни одного слайда: пустой pptx — это не «нет данных», а ошибка
            # сборки (молча отдать пустую презентацию нельзя)
            raise EmptySlice(self.tab)
        for num, (slide, proto) in enumerate(self._pages, 1):
            page = shapes.find_shape(slide, "page")
            if page is not None:
                shapes.set_run(page, 0, str(num))
        for proto_slide in self._proto_slides.values():
            shapes.delete_slide(self.prs, proto_slide)
        self._assert_no_placeholders()
        self._assert_all_placeholders_used()
        buf = io.BytesIO()
        self.prs.save(buf)
        blob = buf.getvalue()
        log(logging.INFO, "fot_deck: презентация собрана", pages=len(self._pages),
            bytes=len(blob), tab=self.tab)
        return blob

    def _assert_no_placeholders(self) -> None:
        """Плейсхолдер в выходном тексте = незаполненное поле → падаем сразу."""
        leftovers = []
        for slide in self._made:
            for sh in slide.shapes:
                for text in _shape_texts(sh):
                    if "{{" in text:
                        leftovers.append("%s/%s: %r"
                                         % (slide._element.find(_P_NS + "cSld")
                                            .get("name"), sh.name, text[:80]))
        if leftovers:
            raise RuntimeError("fot_deck: в выгрузке остались плейсхолдеры: "
                               + "; ".join(leftovers[:5]))

    def _assert_all_placeholders_used(self) -> None:
        """Каждый плейсхолдер актива обязан быть заполнен (B5, обратная сторона).

        Обратная проверка к _assert_no_placeholders: там ловится «в выгрузке
        остался {{…}}», а здесь — «в активе есть шейп, которого биндер не
        касается» (например, добавили поле в деку и забыли биндер). Такой шейп
        после удаления прототипов в выгрузку не попадает вообще, поэтому в
        тексте его не найти — сверяем множество токенов актива с манифестом.
        """
        declared = set()
        # манифест хранит ОБЩИЙ список токенов актива (строки «{{cardN_p1}}»):
        # номера в них свёрнуты, поэтому и найденные токены сворачиваются так
        # же — иначе card1…card16 не совпали бы ни с одной записью
        raw = self.manifest.get("placeholders") or []
        for name in (raw if isinstance(raw, list) else list(raw.values())):
            declared.add(re.sub(r"\d+", "*", str(name)).replace("N", "*"))
        found = set()
        for proto_slide in self._proto_slides.values():
            for sh in proto_slide.shapes:
                for text in _shape_texts(sh):
                    for token in re.findall(r"\{\{([^}]+)\}\}", text):
                        base = re.sub(r"\d+", "*", token)
                        # манифест объявляет токены по ИМЕНАМ ШЕЙПОВ
                        # ({{banner_good}}, {{cardN_cc_dev}}), а обезличивание
                        # кладёт в текст токены ЧАСТЕЙ шейпа:
                        # {{banner_good_lead}}, {{cardN_cc_dev_r0}},
                        # {{cardN_pp1_text_p0_r0}}, {{total_part*}}. Отрезаем
                        # хвост до первого «_», если сам шейп объявлен: так
                        # остаётся настоящая проверка «в активе есть шейп, до
                        # которого биндер не дотянулся»
                        head = base.strip("{}").split("_")
                        while head:
                            cand = "{{%s}}" % "_".join(head)
                            if cand in declared:
                                base = cand
                                break
                            head.pop()
                        found.add(base)
        unknown = sorted(found - declared)
        if unknown:
            raise RuntimeError(
                "fot_deck: в активе есть плейсхолдеры, не описанные манифестом "
                "(%s) — пересоберите актив и проверьте биндеры"
                % ", ".join(unknown))


def _shape_texts(shape) -> list[str]:
    """Все тексты шейпа, включая ячейки таблиц — БЕЗ обращения к text_frame.

    `shape.has_text_frame` / `shape.text_frame` заставляют python-pptx создать
    пустой <p:txBody> у фигуры, у которой его нет (линии-разделители деки);
    после этого фигура рендерится как текстовый блок и вёрстка съезжает.
    Поэтому текст читаем прямо из XML.

    Ветка одна: у graphicFrame внутри a:tbl текст живёт только в ячейках, и
    обход «txBody и потом tbl» выдал бы первую ячейку дважды (у таблицы есть
    свой контейнер, а .//txBody нашёл бы ещё и шапку).
    """
    el = shape._element
    out: list[str] = []
    tbl = el.find(".//" + shapes.A + "tbl")
    if tbl is not None:
        for tc in tbl.iter(shapes.A + "tc"):
            inner = tc.find(shapes.A + "txBody")
            if inner is None:
                continue
            for p in inner.findall(shapes.A + "p"):
                out.append("".join(t.text or "" for t in p.findall(
                    shapes.A + "r/" + shapes.A + "t")))
        return out
    txBody = el.find(shapes.A + "txBody")
    if txBody is None:
        # у фигур (p:sp) контейнер текста живёт в PresentationML, а не в
        # DrawingML: `find(A+"txBody")` его не видит, и проверка на «{{»
        # молча проходила по пустому списку — плейсхолдер мог уехать в файл
        txBody = el.find(shapes.P + "txBody")
    if txBody is not None:
        for p in txBody.findall(shapes.A + "p"):
            out.append("".join(t.text or "" for t in p.findall(
                shapes.A + "r/" + shapes.A + "t")))
    return out


def _has_country_filter(filters: dict) -> bool:
    """Сузил ли пользователь срез одной страной (тогда лист один, не постранично)."""
    value = filters.get("country")
    if isinstance(value, str):
        return bool(value.strip())
    if isinstance(value, (list, tuple)):
        return len([v for v in value if str(v).strip()]) == 1
    return False


async def export_deck(tabs: list[str] | None, filters: dict | None) -> bytes:
    """Точка входа роута: собирает данные вкладок и рендерит pptx.

    `tabs` пустой/None — ВСЕ листы реестра в порядке деки (это и есть режим
    «1:1 — все листы» на фронте): пользователь просит выгрузку дашборда, а
    состав листов определяет дека. Явный список (`["p17"]`) остаётся для
    точечной выгрузки одной вкладки и работает как раньше.
    """
    wanted = ([str(t).strip().lower() for t in tabs if str(t).strip()]
              if tabs else list(registry.ALL_TABS))
    if not wanted:
        wanted = list(registry.ALL_TABS)
    unknown = [t for t in wanted if t not in collect_mod.SUPPORTED_TABS]
    if unknown:
        raise collect_mod.UnsupportedTab(unknown[0])

    engine = DeckEngine(load_template(), load_manifest())
    try:
        for tab in wanted:
            if tab in collect_mod.COUNTRY_SLICED and _has_country_filter(
                    filters or {}):
                # срез уже сужен пользователем: один лист на эту страну
                payload = await collect_mod.collect(tab, filters or {})
                engine.build_tab(tab, payload)
                continue
            if tab in collect_mod.COUNTRY_SLICED:
                # листы 7/8 дека печатает ПОСТРАНИЧНО по странам присутствия:
                # свод группы без `meta.slide.cc` не даёт шапки и подписей
                for cc in collect_mod.COUNTRY_ORDER:
                    payload = await collect_mod.collect_country(tab, filters or {},
                                                                cc)
                    if not (payload.get("cards_by_department") or []):
                        log(logging.INFO, "fot_deck: страна без отделов — лист "
                            "пропущен", tab=tab, cc=cc)
                        continue
                    engine.build_tab(tab, payload)
                continue
            payload = await collect_mod.collect(tab, filters or {})
            engine.build_tab(tab, payload)
        return engine.finish()
    except RuntimeError as exc:
        # tab/proto — из движка: по ним видно, на каком листе деки упало, даже
        # если текст ошибки пришёл из глубины shapes.py без этой привязки
        log(logging.ERROR, "fot_deck: сборка не удалась", tabs=wanted,
            tab=engine.tab, proto=engine.proto, error=str(exc)[:300])
        raise


__all__ = ["AssetMismatch", "DeckEngine", "EmptySlice", "SlideContext",
           "export_deck", "load_manifest", "load_template", "reset_cache"]
