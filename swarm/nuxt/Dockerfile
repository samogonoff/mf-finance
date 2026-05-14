ARG tag
FROM registry.markformelle.ru/services/docker-registry/finance-node-${tag} AS base

# ---------- build stage ----------
FROM base AS builder
WORKDIR /var/www/nuxt_app


COPY nuxt/package*.json ./
RUN npm install

COPY nuxt/. .
# Слой раздела «Себестоимость» — в nuxt.config.ts: extends ["../python/cost/nuxt-layer"].
# Без него /cost не попадает в build → на проде Nuxt отдаёт 404.
COPY python/cost/nuxt-layer /var/www/python/cost/nuxt-layer

ENV NODE_ENV=production
RUN npm run build

# ---------- runtime stage ----------
    
FROM base AS runner
WORKDIR /var/www/nuxt_app

ENV NODE_ENV=production \
    NUXT_HOST=0.0.0.0 \
    NUXT_PORT=3000

COPY --from=builder /var/www/nuxt_app/.output ./.output
COPY --from=builder /var/www/nuxt_app/package*.json ./
COPY --from=builder /var/www/nuxt_app/node_modules ./node_modules
COPY swarm/nuxt/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 3000

ENTRYPOINT ["/entrypoint.sh"]