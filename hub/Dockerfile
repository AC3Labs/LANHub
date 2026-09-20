FROM node:20-slim AS assets
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY resources resources
COPY vite.config.js tailwind.config.js postcss.config.js ./
RUN npm run build

FROM php:8.4-fpm
RUN apt-get update && apt-get install -y --no-install-recommends \
        unzip git libsqlite3-dev nginx gettext-base \
    && docker-php-ext-install pdo pdo_sqlite bcmath \
    && rm -rf /var/lib/apt/lists/*
COPY --from=composer:2 /usr/bin/composer /usr/bin/composer

WORKDIR /var/www/html
COPY . .
COPY --from=assets /app/public/build public/build
# A real .env baked into the image (not bind-mounted) — Laravel picks up
# whatever the host actually sets via docker-compose `environment:` /
# `env_file:` (clear_env=no in docker/www.conf passes those straight
# through to PHP, and Laravel's own Dotenv loader never overrides an
# already-set real environment variable), so this is just the fallback
# for anything the user doesn't override. It also means there's nothing
# to bind-mount and nothing that needs to be writable at runtime for a
# default install — see docker-entrypoint.sh for the one exception
# (APP_KEY, which still needs to persist across container recreations).
COPY docker/.env.docker .env
RUN mkdir -p storage/framework/cache storage/framework/sessions storage/framework/views storage/logs bootstrap/cache \
    && composer install --no-dev --optimize-autoloader --no-interaction \
    && chown -R www-data:www-data storage bootstrap/cache

COPY docker/nginx.conf.template /etc/nginx/nginx.conf.template
COPY docker/setup-incomplete.conf.template /etc/nginx/setup-incomplete.conf.template
COPY docker/setup-incomplete.html /etc/lanhub/setup-incomplete.html
COPY docker/www.conf /usr/local/etc/php-fpm.d/www.conf
COPY docker/opcache.ini /usr/local/etc/php/conf.d/opcache.ini

COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

EXPOSE 8000
ENTRYPOINT ["docker-entrypoint.sh"]
