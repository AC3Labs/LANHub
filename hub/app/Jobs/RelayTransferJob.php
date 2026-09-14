<?php

namespace App\Jobs;

use App\Models\RelayTransfer;
use Illuminate\Contracts\Queue\ShouldQueue;
use Illuminate\Foundation\Queue\Queueable;
use Illuminate\Http\UploadedFile;
use Illuminate\Support\Facades\Log;

/**
 * Performs a cross-machine drag/drop relay in the background instead of
 * blocking the Livewire request for however long the transfer takes.
 * Agents never talk to each other directly (see docs/AGENT_API.md), so
 * this is still hub-mediated: download the whole file from the source
 * agent, then upload it to the destination agent.
 */
class RelayTransferJob implements ShouldQueue
{
    use Queueable;

    public int $tries = 1;

    public function __construct(public readonly int $relayTransferId)
    {
        //
    }

    public function handle(): void
    {
        $relay = RelayTransfer::findOrFail($this->relayTransferId);
        $source = $relay->sourceMachine;
        $destination = $relay->destinationMachine;

        try {
            $relay->update(['status' => 'downloading', 'started_at' => now()]);

            $temp = tmpfile();
            $meta = stream_get_meta_data($temp);

            $response = $source->agent()->download($relay->source_path);
            $body = $response->body();
            fwrite($temp, $body);
            rewind($temp);

            $relay->update(['status' => 'uploading', 'bytes_transferred' => strlen($body)]);

            $uploaded = new UploadedFile(
                $meta['uri'],
                basename(str_replace('\\', '/', $relay->destination_path)),
                $response->header('Content-Type'),
                null,
                true
            );

            $destinationDir = dirname(str_replace('\\', '/', $relay->destination_path));
            $destinationDir = $destination->os === 'windows' ? str_replace('/', '\\', $destinationDir) : $destinationDir;

            $destination->agent()->upload($destinationDir, $uploaded);

            if ($relay->kind === 'move') {
                $source->agent()->delete($relay->source_path);
            }

            fclose($temp);

            $relay->update(['status' => 'done', 'completed_at' => now()]);
        } catch (\Throwable $e) {
            Log::warning("Relay transfer #{$relay->id} failed: ".$e->getMessage());
            $relay->update(['status' => 'error', 'error' => $e->getMessage(), 'completed_at' => now()]);
        }
    }
}
