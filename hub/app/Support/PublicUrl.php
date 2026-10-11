<?php

namespace App\Support;

use App\Models\Setting;
use Illuminate\Http\Request;

/**
 * Where the hub is reachable, for the two places that need to know.
 *
 * Pages: the hub works out its own address from each request (IP, hostname,
 * port, or a reverse proxy's domain/HTTPS), so a fresh install works with
 * no APP_URL at all. Only an APP_URL the deployer set on purpose overrides
 * that — the compose file's old `http://localhost:8000` placeholder (and
 * Laravel's own `http://localhost` default) is ignored, since left
 * unedited it sent every remote browser to localhost.
 *
 * Emails: invite and password-reset links can't be built from the request,
 * because anyone can ask for a password reset with a forged Host header and
 * get a link to their own server mailed to the victim. They use a stored,
 * trusted address instead — recorded from the admin's own browser during
 * setup (or their next login on an existing install), editable in Settings.
 */
class PublicUrl
{
    private const SETTING = 'public_url';

    private const PLACEHOLDER_HOSTS = ['localhost', '127.0.0.1', '::1', '[::1]'];

    /** APP_URL, only if the deployer actually set a real address. */
    public static function configured(): ?string
    {
        $url = rtrim((string) config('app.url'), '/');
        $host = strtolower((string) parse_url($url, PHP_URL_HOST));

        if ($host === '' || in_array($host, self::PLACEHOLDER_HOSTS, true)) {
            return null;
        }

        return $url;
    }

    /** The address saved from setup/admin login, or from the Settings page. */
    public static function stored(): ?string
    {
        try {
            $url = Setting::get(self::SETTING);
        } catch (\Throwable) {
            return null; // settings table not migrated yet
        }

        return $url ? rtrim($url, '/') : null;
    }

    public static function store(?string $url): void
    {
        Setting::set(self::SETTING, $url ? rtrim($url, '/') : null);
    }

    /** Record the address an admin is using, unless one is already known. */
    public static function rememberFrom(Request $request): void
    {
        if (self::configured() || self::stored()) {
            return;
        }

        self::store($request->getSchemeAndHttpHost());
    }

    /** Base address for links inside emails. */
    public static function forEmails(): string
    {
        return self::configured() ?? self::stored() ?? rtrim((string) config('app.url'), '/');
    }

    /** Absolute link for an email, e.g. PublicUrl::to('/set-password/abc'). */
    public static function to(string $path): string
    {
        return self::forEmails().'/'.ltrim($path, '/');
    }
}
