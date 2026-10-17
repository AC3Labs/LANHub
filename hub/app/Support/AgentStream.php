<?php

namespace App\Support;

use Illuminate\Http\Client\Response;
use Illuminate\Support\Facades\Log;
use Symfony\Component\HttpFoundation\StreamedResponse;

/**
 * Passes an agent's file response on to the browser a chunk at a time, so
 * the hub never holds the whole file. Downloads used to go through
 * $response->body() inside a Livewire action, which base64'd the file into
 * the JSON reply and blew the 128 MB memory_limit on anything over ~30 MB.
 *
 * The agent's Content-Length is passed through, so if the agent stops
 * mid-file the browser sees a short body and marks the download failed
 * rather than saving a truncated file as if it were complete.
 */
class AgentStream
{
    private const CHUNK_BYTES = 1024 * 1024;

    /** @param array<string, string> $headers */
    public static function respond(Response $agentResponse, array $headers): StreamedResponse
    {
        $body = $agentResponse->toPsrResponse()->getBody();
        $expected = $agentResponse->header('Content-Length');

        $headers = array_filter($headers + [
            'Content-Length' => $expected,
            // nginx would otherwise spool the whole response to a temp file
            // on the hub's disk before (or while) sending it.
            'X-Accel-Buffering' => 'no',
            'Cache-Control' => 'private, no-store',
        ], fn ($value) => $value !== null && $value !== '');

        return new StreamedResponse(function () use ($body, $expected) {
            // max_execution_time would otherwise cut off a long download.
            set_time_limit(0);
            $sent = 0;

            try {
                while (! $body->eof()) {
                    $chunk = $body->read(self::CHUNK_BYTES);
                    echo $chunk;
                    $sent += strlen($chunk);
                    flush();

                    if (connection_aborted()) {
                        return;
                    }
                }
            } catch (\Throwable $e) {
                Log::warning("Agent stream broke after {$sent} bytes: {$e->getMessage()}");

                return;
            } finally {
                $body->close();
            }

            if ($expected !== '' && $expected !== null && (int) $expected !== $sent) {
                Log::warning("Agent stream ended after {$sent} of {$expected} bytes.");
            }
        }, $agentResponse->status(), $headers);
    }
}
