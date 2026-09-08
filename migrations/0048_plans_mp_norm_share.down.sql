-- Откат 0048: убрать норматив удержаний из справочника площадок.
UPDATE plans_directory_row r
   SET payload_json = r.payload_json - 'norm_share'
  FROM plans_directory d
 WHERE d.id = r.directory_id
   AND d.code = 'dir_marketplace';
