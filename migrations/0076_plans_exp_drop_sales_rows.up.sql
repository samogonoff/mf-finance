-- Строки продаж, себестоимости, маржи и отгрузок (GroupPL «ПРОДАЖИ», «СЕБЕСТОИМОСТЬ…», «…МАРЖА», «ОТГРУЗКИ…») не
-- расходы: процедура затрат отдаёт их вместе с расходами по тем же ЦФО, а листы Excel берут в блок расходов только
-- расходные коды. Новая загрузка их больше не приносит (expIsSalesGroupPL); здесь чистим то, что уже попало в формы.
-- Удаляем только то, что восстанавливается из источника и не тронуто человеком: строки открытых карточек
-- (черновик, возвращена), по которым нет плановых значений. Утверждённые карточки остаются как согласованы.
-- Значения строки (факт, стратегия) удаляются вместе с ней (ON DELETE CASCADE).
DELETE FROM exp_row r
 USING exp_instance i, form_card c
 WHERE r.instance_id = i.id AND i.card_id = c.id
   AND i.form_kind IN ('retail', 'mp', 'im', 'opt')
   AND c.status IN ('draft', 'returned') AND NOT c.locked
   AND (UPPER(r.group_pl) LIKE '%ПРОДАЖИ%' OR UPPER(r.group_pl) LIKE '%СЕБЕСТОИМОСТЬ%'
        OR UPPER(r.group_pl) LIKE '%МАРЖА%' OR UPPER(r.group_pl) LIKE '%ОТГРУЗК%')
   AND NOT EXISTS (SELECT 1 FROM exp_value v WHERE v.row_id = r.id AND v.scenario = 'plan' AND v.source <> 'budget');