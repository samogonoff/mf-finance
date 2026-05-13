#!/bin/sh
set -eu

if [ -f /var/www/nuxt_app/.env ]; then
  set -a
  . /var/www/nuxt_app/.env
  set +a
fi

exec node .output/server/index.mjs