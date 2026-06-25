-- Откат 0017.
DELETE FROM plans_directory WHERE code IN
  ('dir_lisa_prod_units','dir_lisa_norms','dir_lisa_shipments','dir_lisa_routes',
   'dir_lisa_tariffs','dir_lease','dir_payroll_rate','dir_headcount');
DROP TABLE IF EXISTS plans_route_config;
