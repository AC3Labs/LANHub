<?php

namespace App\Support;

use BaconQrCode\Renderer\Image\SvgImageBackEnd;
use BaconQrCode\Renderer\ImageRenderer;
use BaconQrCode\Renderer\RendererStyle\RendererStyle;
use BaconQrCode\Writer;
use PragmaRX\Google2FA\Google2FA;

/**
 * Authenticator-app (TOTP, RFC 6238) codes — the same 6-digit, 30-second
 * codes Google Authenticator, Microsoft Authenticator, Authy, 1Password
 * etc. all generate. Lets users log in without an emailed code, so a hub
 * with no SMTP configured can't lock its own admin out.
 */
class Authenticator
{
    // Recovery codes avoid lookalike characters, same idea as the emailed code.
    private const RECOVERY_ALPHABET = 'ABCDEFGHJKMNPQRSTUVWXYZ23456789';

    public static function newSecret(): string
    {
        return (new Google2FA)->generateSecretKey(32);
    }

    /** otpauth:// link an authenticator app reads from the QR code. */
    public static function setupUri(string $email, string $secret): string
    {
        return (new Google2FA)->getQRCodeUrl(config('app.name', 'LANHub'), $email, $secret);
    }

    public static function qrCodeSvg(string $email, string $secret): string
    {
        $writer = new Writer(new ImageRenderer(new RendererStyle(192, 1), new SvgImageBackEnd));

        return $writer->writeString(self::setupUri($email, $secret));
    }

    /**
     * Checks a 6-digit code, allowing one 30-second step of clock drift
     * either way. Returns the time step it matched (store it, and pass it
     * back as $lastUsedStep next time, so the same code can't be used
     * twice), or null if it didn't match.
     */
    public static function verify(string $secret, string $code, ?int $lastUsedStep = null): ?int
    {
        $code = preg_replace('/\s+/', '', $code);

        if (! preg_match('/^\d{6}$/', $code)) {
            return null;
        }

        // A previous step of 0 makes the library return the matched step
        // (an int) instead of plain true.
        $step = (new Google2FA)->verifyKeyNewer($secret, $code, $lastUsedStep ?? 0, 1);

        return is_int($step) ? $step : null;
    }

    /** @return list<string> e.g. ["K7MPQ-3HXWA", ...] */
    public static function newRecoveryCodes(int $count = 8): array
    {
        $codes = [];

        for ($i = 0; $i < $count; $i++) {
            $code = '';
            for ($c = 0; $c < 10; $c++) {
                $code .= self::RECOVERY_ALPHABET[random_int(0, strlen(self::RECOVERY_ALPHABET) - 1)];
            }
            $codes[] = substr($code, 0, 5).'-'.substr($code, 5);
        }

        return $codes;
    }

    /** Uppercase, no spaces, dash optional — so "k7mpq 3hxwa" still works. */
    public static function normalizeRecoveryCode(string $code): string
    {
        $code = strtoupper(preg_replace('/[\s-]+/', '', $code));

        return strlen($code) === 10 ? substr($code, 0, 5).'-'.substr($code, 5) : $code;
    }
}
