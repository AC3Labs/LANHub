<?php

namespace App\Console\Commands;

use App\Exceptions\AgentException;
use App\Jobs\RelayTransferJob;
use App\Models\Machine;
use App\Models\RelayTransfer;
use App\Models\SyncRule;
use Illuminate\Console\Command;
use Illuminate\Support\Facades\Log;

/**
 * Runs every due SyncRule: diffs the source and destination trees (via
 * AgentClient::listFilesRecursive) and copies anything new or changed.
 * "mirror" rules also delete destination files with no matching source
 * file. Never overwrites/deletes a destination file that changed since it
 * was scanned this run — that's flagged as a conflict on the rule instead
 * (see SyncRule::has_conflict), not clobbered.
 */
class RunSyncRules extends Command
{
    protected $signature = 'sync:run-rules';

    protected $description = 'Run every sync rule that is due, copying (and, for mirror rules, deleting) files to keep two machines in sync';

    public function handle(): int
    {
        foreach (SyncRule::where('enabled', true)->get() as $rule) {
            if (! $rule->isDue()) {
                continue;
            }

            $this->runRule($rule);
        }

        return self::SUCCESS;
    }

    private function runRule(SyncRule $rule): void
    {
        $source = $rule->sourceMachine;
        $destination = $rule->destinationMachine;
        $destSeparator = $destination->os === 'windows' ? '\\' : '/';
        $hasConflict = false;

        try {
            $sourceFiles = collect($source->agent()->listFilesRecursive(
                $rule->source_path,
                $source->os === 'windows' ? '\\' : '/'
            ))->keyBy('relative');

            $destFiles = collect($destination->agent()->listFilesRecursive($rule->destination_path, $destSeparator))
                ->keyBy('relative');

            // Cache one fresh re-listing per destination directory so a
            // folder with many changed files only costs one extra
            // GET /api/list call, not one per file.
            $freshDestDirs = [];

            foreach ($sourceFiles as $relative => $sourceFile) {
                $destPath = rtrim($rule->destination_path, $destSeparator).$destSeparator.str_replace('/', $destSeparator, $relative);
                $existing = $destFiles->get($relative);

                if ($existing && $existing['size'] === $sourceFile['size'] && $existing['modified'] === $sourceFile['modified']) {
                    continue; // already in sync
                }

                if ($existing) {
                    $freshEntry = $this->freshEntry($destination, $destPath, $destSeparator, $freshDestDirs);

                    if ($freshEntry && ($freshEntry['size'] !== $existing['size'] || $freshEntry['modified'] !== $existing['modified'])) {
                        $hasConflict = true;

                        continue;
                    }
                }

                $this->copyFile($source, $sourceFile['path'], $destination, $destPath, $destSeparator, overwrite: (bool) $existing);
            }

            if ($rule->direction === 'mirror') {
                foreach ($destFiles as $relative => $destFile) {
                    if ($sourceFiles->has($relative)) {
                        continue;
                    }

                    $freshEntry = $this->freshEntry($destination, $destFile['path'], $destSeparator, $freshDestDirs);

                    if ($freshEntry && ($freshEntry['size'] !== $destFile['size'] || $freshEntry['modified'] !== $destFile['modified'])) {
                        $hasConflict = true;

                        continue;
                    }

                    $destination->agent()->delete($destFile['path']);
                }
            }

            $rule->update(['last_run_at' => now(), 'has_conflict' => $hasConflict, 'last_error' => null]);
        } catch (\Throwable $e) {
            Log::warning("Sync rule #{$rule->id} ({$rule->name}) failed: ".$e->getMessage());
            $rule->update(['last_run_at' => now(), 'last_error' => $e->getMessage()]);
        }
    }

    /**
     * @param  array<string, array>  $cache  keyed by directory path, mutated in place
     */
    private function freshEntry(Machine $machine, string $path, string $separator, array &$cache): ?array
    {
        $dir = rtrim(substr($path, 0, strrpos($path, $separator) ?: 0), $separator) ?: $separator;

        if (! array_key_exists($dir, $cache)) {
            try {
                $cache[$dir] = collect($machine->agent()->listDirectory($dir)['entries'])->keyBy('path')->all();
            } catch (AgentException) {
                $cache[$dir] = [];
            }
        }

        return $cache[$dir][$path] ?? null;
    }

    /**
     * The agent's /api/copy and /api/move both refuse to overwrite an
     * existing destination (409) and don't create missing parent
     * directories — fine for the Explorer's single-file drag/drop, but a
     * sync rule needs both: an existing-but-stale file is meant to be
     * overwritten, and a whole new source subdirectory has nothing on the
     * destination side yet to create it. Cross-machine uploads already
     * handle both (POST /api/upload always overwrites and mkdir()s the
     * destination directory), so only the same-machine path needs help.
     */
    private function copyFile(Machine $source, string $sourcePath, Machine $destination, string $destPath, string $destSeparator, bool $overwrite): void
    {
        if ($source->id === $destination->id) {
            $destDir = rtrim(substr($destPath, 0, strrpos($destPath, $destSeparator) ?: 0), $destSeparator) ?: $destSeparator;
            $destination->agent()->mkdir($destDir);

            if ($overwrite) {
                try {
                    $destination->agent()->delete($destPath);
                } catch (AgentException) {
                    // already gone — fine, the copy below still succeeds
                }
            }

            $source->agent()->copy($sourcePath, $destPath);

            return;
        }

        $relay = RelayTransfer::create([
            'source_machine_id' => $source->id,
            'destination_machine_id' => $destination->id,
            'source_path' => $sourcePath,
            'destination_path' => $destPath,
            'kind' => 'copy',
            'status' => 'queued',
        ]);

        RelayTransferJob::dispatchSync($relay->id);
    }
}
