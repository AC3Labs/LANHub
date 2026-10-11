<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;

class Setting extends Model
{
    protected $fillable = ['key', 'value'];

    protected function casts(): array
    {
        return [
            'value' => 'encrypted',
        ];
    }

    public static function get(string $key, ?string $default = null): ?string
    {
        return static::where('key', $key)->value('value') ?? $default;
    }

    /**
     * Whether login codes/invites can actually reach anyone - SMTP set on
     * the Settings page, or a real mailer configured in .env. With neither,
     * mail only goes to the server log.
     */
    public static function mailConfigured(): bool
    {
        try {
            if (static::get('smtp_host')) {
                return true;
            }
        } catch (\Throwable) {
            // settings table not migrated yet
        }

        return ! in_array(config('mail.default'), ['log', 'array'], true);
    }

    public static function set(string $key, ?string $value): void
    {
        static::updateOrCreate(['key' => $key], ['value' => $value]);
    }
}
