DELETE FROM plans_directory WHERE code = 'dir_fx_per_usd';
DELETE FROM plans_directory_row r
 USING plans_directory d
 WHERE d.id = r.directory_id AND d.code = 'dir_fx_rate'
   AND r.payload_json->>'note' = 'курсовая таблица эталона 2026';
