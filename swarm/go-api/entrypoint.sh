#!/bin/sh
set -e

set -a
[ -f /etc/api.env ] && . /etc/api.env
set +a

exec /server