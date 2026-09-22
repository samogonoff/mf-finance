-- Откат 0046: вернуть провизорное ЮЛ Uzmarket.
UPDATE plans_directory_row r
   SET payload_json = jsonb_set(r.payload_json, '{legal_entity}', '"MF Kazakhstan"'::jsonb)
  FROM plans_directory d
 WHERE d.id = r.directory_id
   AND d.code IN ('dir_marketplace', 'dir_cfo')
   AND r.payload_json->>'code_cfo' = '339';
