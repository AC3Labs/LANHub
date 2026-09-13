<?php

namespace App\Services;

use Illuminate\Support\Facades\Http;
use Illuminate\Support\Facades\Log;

/**
 * Push notifications via ntfy.sh — a free, no-signup service. Pick a
 * random/hard-to-guess topic name (ntfy topics are public-by-obscurity)
 * and subscribe to it in the ntfy mobile app or at https://ntfy.sh/<topic>.
 *
 * Extracted from App\Console\Commands\CheckMachineHealth (the original,
 * only caller) so every other alert-worthy event (low disk space, a
 * failed sync rule, a failed transfer) can reuse the same call instead of
 * duplicating the HTTP POST.
 */
class NtfyClient
{
    public function notify(string $message, string $title = 'LANHub'): void
    {
        $topic = config('services.ntfy.topic');

        if (! $topic) {
            return;
        }

        try {
            Http::timeout(5)
                ->withHeaders(['Title' => $title])
                ->withBody($message, 'text/plain')
                ->post('https://ntfy.sh/'.$topic);
        } catch (\Throwable $e) {
            Log::warning('ntfy.sh notification failed: '.$e->getMessage());
        }
    }
}
