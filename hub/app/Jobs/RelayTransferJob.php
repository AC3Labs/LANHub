<?php

namespace App\Jobs;

use App\Exceptions\AgentException;
use App\Models\Machine;
use App\Models\RelayTransfer;
use Illuminate\Contracts\Queue\ShouldQueue;
use Illuminate\Foundation\Queue\Queueable;
use Illuminate\Http\UploadedFile;
use Illuminate\Support\Facades\Log;
use RuntimeException;

/**
 * Performs a cross-machine drag/drop relay in the background instead of
 * blocking the Livewire request for however long the transfer takes.
 * Agents never talk to each other directly (see docs/AGENT_API.md), so
 * this is still hub-mediated: download each file from the source agent,
 * then upload it to the destination agent. A folder is walked with the
 * agent's own list/mkdir endpoints and relayed file by file.
 */
class RelayTransferJob implements ShouldQueue
{
    use Queueable;

    public int $tries = 1;

    // Guards against symlink loops, which the agent's listing follows.
    private const MAX_FOLDER_DEPTH = 64;

    private int $bytes = 0;

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

            if ($relay->is_directory) {
                $this->relayFolder($relay, $source, $destination);
            } else {
                $this->relayFile($relay, $source, $relay->source_path, $destination, $relay->destination_path);
            }

            // Only reached if every file made it across - a failure anywhere
            // above throws first, so a move never deletes a partial copy's source.
            if ($relay->kind === 'move') {
                $source->agent()->delete($relay->source_path);
            }

            $relay->update(['status' => 'done', 'bytes_transferred' => $this->bytes, 'completed_at' => now()]);
        } catch (\Throwable $e) {
            Log::warning("Relay transfer #{$relay->id} failed: ".$e->getMessage());
            $relay->update(['status' => 'error', 'error' => $e->getMessage(), 'bytes_transferred' => $this->bytes, 'completed_at' => now()]);
        }
    }

    /**
     * Recreates the source folder (and everything in it, empty subfolders
     * included) at the destination. Refuses to merge into an existing
     * folder, same as the agent's own move/copy.
     */
    private function relayFolder(RelayTransfer $relay, Machine $source, Machine $destination): void
    {
        $separator = $destination->os === 'windows' ? '\\' : '/';
        $root = $relay->destination_path;

        if ($this->exists($destination, $root, $separator)) {
            throw new RuntimeException("{$root} already exists on {$destination->name}.");
        }

        $destination->agent()->mkdir($root);

        $queue = [[$relay->source_path, $root, 0]];

        while ($queue !== []) {
            [$sourceDir, $destinationDir, $depth] = array_shift($queue);

            if ($depth > self::MAX_FOLDER_DEPTH) {
                throw new RuntimeException("{$sourceDir} is nested too deeply (possible folder link loop).");
            }

            // Not caught: an unreadable subfolder must fail the whole transfer,
            // not be skipped, or a move would delete files that never arrived.
            $listing = $source->agent()->listDirectory($sourceDir);

            foreach ($listing['entries'] ?? [] as $entry) {
                $target = rtrim($destinationDir, $separator).$separator.$entry['name'];

                if ($entry['type'] === 'dir') {
                    $destination->agent()->mkdir($target);
                    $queue[] = [$entry['path'], $target, $depth + 1];
                } else {
                    $this->relayFile($relay, $source, $entry['path'], $destination, $target);
                }
            }
        }
    }

    /** Download one file from the source agent and upload it to the destination. */
    private function relayFile(RelayTransfer $relay, Machine $source, string $sourcePath, Machine $destination, string $destinationPath): void
    {
        $temp = tmpfile();
        $meta = stream_get_meta_data($temp);

        try {
            $response = $source->agent()->download($sourcePath);
            $body = $response->body();
            fwrite($temp, $body);
            rewind($temp);

            $this->bytes += strlen($body);
            $relay->update(['status' => 'uploading', 'bytes_transferred' => $this->bytes]);

            $uploaded = new UploadedFile(
                $meta['uri'],
                basename(str_replace('\\', '/', $destinationPath)),
                $response->header('Content-Type'),
                null,
                true
            );

            $destinationDir = dirname(str_replace('\\', '/', $destinationPath));
            $destinationDir = $destination->os === 'windows' ? str_replace('/', '\\', $destinationDir) : $destinationDir;

            $destination->agent()->upload($destinationDir, $uploaded);
        } finally {
            fclose($temp);
        }
    }

    private function exists(Machine $machine, string $path, string $separator): bool
    {
        $normalized = str_replace('\\', '/', rtrim($path, $separator));
        $parent = dirname($normalized);
        $parent = $separator === '\\' ? str_replace('/', '\\', $parent).(str_ends_with($parent, ':') ? '\\' : '') : $parent;
        $name = basename($normalized);

        try {
            $entries = $machine->agent()->listDirectory($parent)['entries'] ?? [];
        } catch (AgentException) {
            return false; // parent doesn't exist yet - mkdir creates it
        }

        foreach ($entries as $entry) {
            if (strcasecmp($entry['name'], $name) === 0 && ($separator === '\\' || $entry['name'] === $name)) {
                return true;
            }
        }

        return false;
    }
}
