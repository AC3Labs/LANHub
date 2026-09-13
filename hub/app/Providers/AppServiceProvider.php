<?php

namespace App\Providers;

use App\Models\Setting;
use App\Support\Diagnostics\Runtime\Http\RequestContextMiddleware;
use Illuminate\Support\Facades\Route;
use Illuminate\Support\Facades\URL;
use Illuminate\Support\ServiceProvider;

class AppServiceProvider extends ServiceProvider
{
    /**
     * Register any application services.
     */
    public function register(): void
    {
        //
    }

    /**
     * Bootstrap any application services.
     */
    public function boot(): void
    {
        // Behind the nginx -> PHP-FPM FastCGI handoff, Symfony's request-based
        // port detection doesn't reliably see the real port (falls back to
        // treating it as the default for the scheme), so every url()/route()
        // call would otherwise silently drop :8000. Force the configured
        // APP_URL as the root instead of trusting request-based detection.
        if (config('app.url')) {
            URL::forceRootUrl(config('app.url'));
        }

        $this->applySmtpSettingsFromDatabase();

        Route::pushMiddlewareToGroup('web', RequestContextMiddleware::class);
    }

    /**
     * SMTP config lives in the settings table (see App\Livewire\Settings\
     * Index), not .env — on the Docker deployment .env is a bind-mounted
     * single file, awkward to edit safely and requiring a container
     * recreation to take effect either way. Falls back to whatever .env
     * already has if the table doesn't exist yet (fresh install, before
     * migrations run) or nothing's been configured.
     */
    private function applySmtpSettingsFromDatabase(): void
    {
        try {
            $host = Setting::get('smtp_host');
        } catch (\Throwable) {
            return;
        }

        if (! $host) {
            return;
        }

        config([
            'mail.default' => 'smtp',
            'mail.mailers.smtp.host' => $host,
            'mail.mailers.smtp.port' => Setting::get('smtp_port', '587'),
            'mail.mailers.smtp.username' => Setting::get('smtp_username'),
            'mail.mailers.smtp.password' => Setting::get('smtp_password'),
            // Symfony Mailer (Laravel's transport since v9) uses a URI
            // "scheme" here, not the old Swift Mailer "encryption" key:
            // "smtps" = implicit TLS (typically port 465), "smtp" = plain
            // or opportunistic STARTTLS (typically port 587).
            'mail.mailers.smtp.scheme' => Setting::get('smtp_encryption') === 'ssl' ? 'smtps' : 'smtp',
            'mail.from.address' => Setting::get('smtp_from_address', config('mail.from.address')),
            'mail.from.name' => Setting::get('smtp_from_name', config('mail.from.name')),
        ]);
    }
}
