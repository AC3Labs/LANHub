<?php

namespace App\Models;

// use Illuminate\Contracts\Auth\MustVerifyEmail;
use App\Notifications\ResetPasswordNotification;
use Database\Factories\UserFactory;
use Illuminate\Database\Eloquent\Attributes\Fillable;
use Illuminate\Database\Eloquent\Attributes\Hidden;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Relations\BelongsToMany;
use Illuminate\Foundation\Auth\User as Authenticatable;
use Illuminate\Notifications\Notifiable;
use Illuminate\Support\Facades\Hash;

#[Fillable(['name', 'email', 'password'])]
#[Hidden(['password', 'remember_token'])]
class User extends Authenticatable
{
    /** @use HasFactory<UserFactory> */
    use HasFactory, Notifiable;

    /**
     * Get the attributes that should be cast.
     *
     * @return array<string, string>
     */
    protected function casts(): array
    {
        return [
            'email_verified_at' => 'datetime',
            'password' => 'hashed',
            'is_admin' => 'boolean',
            'invite_token_expires_at' => 'datetime',
            'two_factor_expires_at' => 'datetime',
        ];
    }

    /**
     * An invited user has no usable password until they follow their
     * set-password link — this is the only reliable signal for "has this
     * person actually activated their account yet."
     */
    public function isPending(): bool
    {
        return $this->invite_token !== null;
    }

    /**
     * Uppercase letters, digits, and a handful of special characters picked
     * for being unambiguous to read and easy to type on any keyboard —
     * deliberately excludes lookalikes (0/O, 1/I/L) and characters that are
     * awkward to type or read in an email (backtick, quotes, backslash).
     */
    private const TWO_FACTOR_CODE_ALPHABET = 'ABCDEFGHJKMNPQRSTUVWXYZ23456789!@#$%&*-_+?';

    /**
     * Generates and stores a hashed 10-character login code — letters,
     * digits, and special characters (never stored in plain text, same as
     * the password) — and returns the plain code for the caller to email.
     * Overwrites any previous code.
     */
    public function generateTwoFactorCode(): string
    {
        $alphabet = self::TWO_FACTOR_CODE_ALPHABET;
        $code = '';

        for ($i = 0; $i < 10; $i++) {
            $code .= $alphabet[random_int(0, strlen($alphabet) - 1)];
        }

        $this->forceFill([
            'two_factor_code' => bcrypt($code),
            'two_factor_expires_at' => now()->addMinutes(10),
        ])->save();

        return $code;
    }

    public function verifyTwoFactorCode(string $code): bool
    {
        if (! $this->two_factor_code || ! $this->two_factor_expires_at || $this->two_factor_expires_at->isPast()) {
            return false;
        }

        return Hash::check(strtoupper($code), $this->two_factor_code);
    }

    public function clearTwoFactorCode(): void
    {
        $this->forceFill(['two_factor_code' => null, 'two_factor_expires_at' => null])->save();
    }

    public function sendPasswordResetNotification($token): void
    {
        $this->notify(new ResetPasswordNotification($token));
    }

    public function machines(): BelongsToMany
    {
        return $this->belongsToMany(Machine::class)->withPivot('role')->withTimestamps();
    }

    /**
     * @param  'read_only'|'full'  $atLeast
     */
    public function canAccess(Machine $machine, string $atLeast = 'read_only'): bool
    {
        $role = $this->machines()->where('machine_id', $machine->id)->value('role');

        if ($role === null) {
            return false;
        }

        return $atLeast === 'read_only' || $role === 'full';
    }
}
