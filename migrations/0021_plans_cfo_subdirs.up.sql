-- 0021 — под-справочники ЦФО (Группа/Подгруппа/Тип) как независимые
-- редактируемые списки + глобальный справочник ЮЛ с директором/ТОПом
-- (ссылки на users). ТЗ §7.1.1 (разрезы ЦФО) + Приложение B (ЮЛ).

INSERT INTO plans_directory (code, source, sync_status) VALUES
    ('dir_cfo_group',    'manual', 'seed'),   -- Группа (group_cfo1)
    ('dir_cfo_subgroup', 'manual', 'seed'),   -- Подгруппа (group_cfo2)
    ('dir_cfo_type',     'manual', 'seed'),   -- Тип (entity_type)
    ('dir_legal_entity', 'manual', 'seed')    -- ЮЛ + директор/ТОП
ON CONFLICT (code) DO NOTHING;

-- Засев ЮЛ по ТЗ Приложение B + согласующие этапа 3 (§5.4) как директор-подсказка.
-- alias_raw — сырые варианты из xlsx (гипотеза, правится в UI). director_user_id/
-- top_user_id заполняются после импорта пользователей (B24). uncertain=true —
-- спорный маппинг, требует сверки.
INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, le.code, le.payload
FROM plans_directory d
CROSS JOIN (VALUES
    ('mark_formelle', jsonb_build_object('code','mark_formelle','name','Mark Formelle','alias_raw','МФ, фМФ, «МФ, Ф», «МФ, ГП»','country','BY','director','Захарченко Н.М.','top','','director_user_id',null,'top_user_id',null,'uncertain',false)),
    ('formelle',      jsonb_build_object('code','formelle','name','Formelle','alias_raw','Ф, МФЦ(?)','country','BY','director','Дегтерева Е.В.','top','','director_user_id',null,'top_user_id',null,'uncertain',true)),
    ('mf_it',         jsonb_build_object('code','mf_it','name','MF IT','alias_raw','МФ IT','country','BY','director','Мавлянов Д.А.','top','','director_user_id',null,'top_user_id',null,'uncertain',false)),
    ('mf_kazakhstan', jsonb_build_object('code','mf_kazakhstan','name','MF Kazakhstan','alias_raw','МФ КЗ','country','KZ','director','Командиров М.В.','top','','director_user_id',null,'top_user_id',null,'uncertain',false)),
    ('mf_tex',        jsonb_build_object('code','mf_tex','name','MF Tex','alias_raw','МФ ТЕКС','country','BY','director','Сметанин П.С.','top','','director_user_id',null,'top_user_id',null,'uncertain',false)),
    ('td_mark_formelle', jsonb_build_object('code','td_mark_formelle','name','TD Mark Formelle','alias_raw','ТД, Mark Formelle Trade','country','RU','director','Левин','top','','director_user_id',null,'top_user_id',null,'uncertain',false)),
    ('mf_shanghai',   jsonb_build_object('code','mf_shanghai','name','MF Shanghai','alias_raw','','country','','director','Акаева','top','','director_user_id',null,'top_user_id',null,'uncertain',false)),
    ('ptir',          jsonb_build_object('code','ptir','name','ПТИР (?)','alias_raw','ПТИР','country','','director','','top','','director_user_id',null,'top_user_id',null,'uncertain',true)),
    ('dd',            jsonb_build_object('code','dd','name','ДД / Дримдом (?)','alias_raw','ДД','country','','director','','top','','director_user_id',null,'top_user_id',null,'uncertain',true)),
    ('barreiros',     jsonb_build_object('code','barreiros','name','BARREIROS SOFT OOO (?)','alias_raw','BARREIROS SOFT OOO','country','','director','','top','','director_user_id',null,'top_user_id',null,'uncertain',true)),
    ('mf_fashion',    jsonb_build_object('code','mf_fashion','name','MF FASHION (?)','alias_raw','MF FASHION','country','','director','','top','','director_user_id',null,'top_user_id',null,'uncertain',true))
) AS le(code, payload)
WHERE d.code = 'dir_legal_entity'
ON CONFLICT DO NOTHING;
