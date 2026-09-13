#!/bin/sh
set -e

cd /var/www/html

if [ ! -f .env ]; then
    echo "No .env found at /var/www/html/.env — copy hub/.env.example to hub/.env (and mount it) before starting this container." >&2
    exit 1
fi

# APP_KEY must already be set before the container ever starts — this
# entrypoint deliberately never tries to write it into .env itself.
# .env is usually a single-file bind mount from the host, and on at
# least one real host, even a --privileged root container was denied
# write access to it (host-level restriction, not a LANHub bug) — rather
# than depend on that working everywhere, generate the key up front with
# `docker compose run --rm hub php artisan key:generate --show` (which
# only prints, never writes) and paste it into .env yourself. See README.
if ! grep -q '^APP_KEY=base64:' .env; then
    echo "APP_KEY is missing from .env. Generate one without starting the app:" >&2
    echo "    docker compose run --rm hub php artisan key:generate --show" >&2
    echo "...and paste the result into hub/.env as APP_KEY=, then start the container again." >&2
    exit 1
fi

# DB_DATABASE (see .env.example) points here, deliberately outside
# database/ — see docker-compose.yml for why this exact path should be
# the one persistent volume mount.
mkdir -p storage/db
touch storage/db/database.sqlite
chown -R www-data:www-data storage bootstrap/cache

php artisan migrate --force

# Drives Schedule::command('machines:check-health') in routes/console.php
# (and anything scheduled later) — nothing else runs cron/supervisord in
# this container, so without this loop scheduled commands would simply
# never fire.
(while true; do php artisan schedule:run >> /dev/null 2>&1; sleep 60; done) &

# QUEUE_CONNECTION=database — RelayTransferJob (cross-machine drag/drop)
# and anything queued later needs a worker actually running, and same
# story as the scheduler: nothing else in this container runs one.
# --stop-when-empty + the outer loop keeps a crashed/finished worker from
# staying dead instead of a long-lived `queue:work` process.
(while true; do php artisan queue:work --stop-when-empty --tries=1 >> /dev/null 2>&1; sleep 2; done) &

php-fpm -D
exec nginx -g "daemon off;"
