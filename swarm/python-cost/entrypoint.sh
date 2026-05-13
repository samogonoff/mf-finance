#!/bin/sh
set -eu

if [ -f /var/www/cost/.env ]; then
  set -a
  . /var/www/cost/.env
  set +a
fi

exec "$@"