-- 0046 — ЮЛ площадки Uzmarket (ЦФО 339): MFUz вместо казахского.
--
-- Найдено проходом по кабинету 07.09.2026: в своде при раскрытии строки продаж
-- Uzmarket (ЦФО 339, страна UZ) стоял рядом с Kaspi и Ozon KZ на «MF Kazakhstan».
-- В dir_cfo у той же строки лежало «MF IT» — след импорта из Лисы, где ЦФО
-- закреплён за подразделением, а не за юрлицом плана.
--
-- Следствие было не косметическим: колонка «Компания» приёмника тактики берёт
-- реальный УНП, и узбекские продажи легли бы на казахскую компанию.
-- Реальное ЮЛ страны — MFUz, УНП 305554644 (dir_legal_entity UZ-305554644,
-- миграция 0042).
--
-- Идемпотентно: правка по ключу code_cfo, повторный прогон ничего не меняет.

UPDATE plans_directory_row r
   SET payload_json = jsonb_set(r.payload_json, '{legal_entity}', '"MFUz"'::jsonb)
  FROM plans_directory d
 WHERE d.id = r.directory_id
   AND d.code IN ('dir_marketplace', 'dir_cfo')
   AND r.payload_json->>'code_cfo' = '339'
   AND COALESCE(r.payload_json->>'legal_entity', '') <> 'MFUz';
