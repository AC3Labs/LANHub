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

# DB_DATABASE (see docker/.env.docker) points here, deliberately outside
# database/ — see docker-compose.yml for why this exact path should be
# the one persistent volume mount. Created before the APP_KEY step below
# since that also persists a file into this same directory.
mkdir -p storage/db
touch storage/db/database.sqlite

# The image ships a real .env (see Dockerfile) with everything except
# APP_KEY — no file needs to exist or be writable for a default install;
# anything the host sets via docker-compose `environment:`/`env_file:`
# already wins over it automatically (real process env vars always take
# precedence over .env, and clear_env=no in docker/www.conf passes them
# through to PHP). APP_KEY is the one value that can't just default to
# empty: it encrypts stored data (agent tokens), so once one is in use it
# has to stay stable across restarts and recreations, not be regenerated
# every boot.
#
# Two ways a real key can already be in place, neither visible as a
# shell env var here: a real process env var (checked via $APP_KEY,
# which *is* visible to this script) or a value already baked into or
# bind-mounted over .env (e.g. someone using the old-style full-.env
# setup) — that one has to be checked by reading the file itself, since
# nothing sources .env into this shell. Only fall back to the
# volume-persisted/auto-generated key when neither is present.
if [ -z "$APP_KEY" ] && ! grep -q '^APP_KEY=base64:' .env; then
    key_file="storage/db/.app_key"
    if [ -f "$key_file" ]; then
        APP_KEY="$(cat "$key_file")"
    else
        APP_KEY="base64:$(openssl rand -base64 32)"
        printf '%s' "$APP_KEY" > "$key_file"
        echo "No APP_KEY was set — generated one and saved it to the persistent volume (storage/db/.app_key)."
    fi
    export APP_KEY
fi

chown -R www-data:www-data storage bootstrap/cache

if ! php artisan migrate --force; then
    serve_setup_incomplete "php artisan migrate failed — check the logs above for the real error."
fi

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
