#!/bin/sh
set -e

cd /var/www/html

if [ ! -f .env ]; then
    echo "No .env found at /var/www/html/.env — copy hub/.env.example to hub/.env (and mount it) before starting this container." >&2
    exit 1
fi

# If APP_KEY is missing, try to write one via Laravel's own key:generate
# (which rewrites .env in place through PHP's normal file I/O, not a
# shell sed -i that could swap out the file's inode) — this is what most
# hosts need, and fixes a real first-deploy crash-loop where a user
# never ran the manual key-generation step. On at least one real host,
# though, even a --privileged root container was denied write access to
# a bind-mounted .env (host-level restriction, never fully explained) —
# if that happens here too, fall back to the old fail-fast instructions
# instead of crash-looping with no explanation. See README.
if ! grep -q '^APP_KEY=base64:' .env; then
    if php artisan key:generate --force >/dev/null 2>&1 && grep -q '^APP_KEY=base64:' .env; then
        echo "No APP_KEY was set — generated one and wrote it to .env."
    else
        echo "APP_KEY is missing from .env, and this container could not write one" >&2
        echo "automatically (this happens on some hosts where a bind-mounted .env" >&2
        echo "isn't writable from inside the container). Generate one yourself:" >&2
        echo "    docker compose run --rm hub php artisan key:generate --show" >&2
        echo "...and paste the result into .env as APP_KEY=, then start the container again." >&2
        exit 1
    fi
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

# Lets the port nginx actually listens on be set via a PORT env var
# (defaults to 8000) instead of being baked into the image — needed for
# platforms (Dokploy, etc.) that route to a specific container-internal
# port rather than just remapping the host side of a `ports:` mapping.
export PORT="${PORT:-8000}"
envsubst '${PORT}' < /etc/nginx/nginx.conf.template > /etc/nginx/sites-enabled/default

php-fpm -D
exec nginx -g "daemon off;"
