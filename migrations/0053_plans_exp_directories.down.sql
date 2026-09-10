DELETE FROM plans_directory WHERE code IN ('dir_pl_group', 'dir_per_diem', 'dir_flight_norm');
UPDATE plans_directory_row r
   SET payload_json = r.payload_json - 'pnl_group'
  FROM plans_directory d
 WHERE d.id = r.directory_id AND d.code = 'dir_pl_line';
