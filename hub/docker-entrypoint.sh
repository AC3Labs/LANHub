#!/bin/sh
set -e

cd /var/www/html

# Lets the port nginx actually listens on be set via a PORT env var
# (defaults to 8000) instead of being baked into the image — needed for
# platforms (Dokploy, etc.) that route to a specific container-internal
# port rather than just remapping the host side of a `ports:` mapping.
export PORT="${PORT:-8000}"

# Serves a static "here's what to fix" page and keeps nginx running in
# the foreground instead of exiting — with restart: unless-stopped (the
# documented compose setup), exiting on a config problem just crash-loops
# the container forever regardless of how clear the log message is
# (confirmed for real: reproduced this exact loop on a fresh `docker
# compose up` after the "print clearer instructions, then exit" version
# of this script shipped — a clearer message doesn't stop a restart
# loop). Staying up with a visible in-browser explanation is strictly
# better: `docker ps` shows the real state (Up, not Restarting), and
# whoever deployed it sees the actual problem by just loading the page,
# which is how most people notice something's wrong in the first place.
serve_setup_incomplete() {
    echo "$1" >&2
    envsubst '${PORT}' < /etc/nginx/setup-incomplete.conf.template > /etc/nginx/sites-enabled/default
    exec nginx -g "daemon off;"
}

if [ ! -f .env ]; then
    serve_setup_incomplete "No .env found at /var/www/html/.env — copy .env.example to .env (and mount it), then restart this container."
fi

# If APP_KEY is missing, try to write one via Laravel's own key:generate
# (which rewrites .env in place through PHP's normal file I/O, not a
# shell sed -i that could swap out the file's inode) — this is what most
# hosts need, and fixes a real first-deploy problem where a user never
# ran the manual key-generation step. On at least one real host, though,
# even a --privileged root container was denied write access to a
# bind-mounted .env (host-level restriction, never fully explained, and
# not unique to that one host — reproduced it again on a second machine)
# — if that happens here too, serve setup instructions instead of
# crash-looping.
if ! grep -q '^APP_KEY=base64:' .env; then
    if php artisan key:generate --force >/dev/null 2>&1 && grep -q '^APP_KEY=base64:' .env; then
        echo "No APP_KEY was set — generated one and wrote it to .env."
    else
        serve_setup_incomplete "APP_KEY is missing from .env, and this container could not write one automatically (bind-mounted .env isn't writable from inside the container on this host). Run: docker compose run --rm hub php artisan key:generate --show — then paste the result into .env as APP_KEY=, and restart this container."
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

envsubst '${PORT}' < /etc/nginx/nginx.conf.template > /etc/nginx/sites-enabled/default

php-fpm -D
exec nginx -g "daemon off;"
