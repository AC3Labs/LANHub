<?php

namespace App\Console\Commands;

use App\Models\DiskReading;
use App\Models\Machine;
use App\Services\NtfyClient;
use Illuminate\Console\Command;
use Illuminate\Support\Facades\Log;

/**
 * Records a free/total disk-space reading per machine (summed across every
 * drive GET /api/drives reports) and alerts via ntfy.sh the moment a
 * machine's free space drops under the threshold — a transition, like
 * App\Console\Commands\CheckMachineHealth's online/offline alerting, so it
 * fires once rather than every minute while a machine stays low.
 * Readings older than 30 days are pruned on each run so this table never
 * grows unbounded.
 */
class CheckDiskSpace extends Command
{
    protected $signature = 'machines:check-disk-space';

    protected $description = 'Record a disk-space reading for every registered machine and alert on low-space transitions';

    private const LOW_SPACE_THRESHOLD_PERCENT = 10.0;

    public function __construct(private readonly NtfyClient $ntfy)
    {
        parent::__construct();
    }

    public function handle(): int
    {
        foreach (Machine::all() as $machine) {
            try {
                $drives = $machine->agent()->drives();
            } catch (\Throwable $e) {
                Log::info("Skipping disk-space check for offline machine {$machine->name}: ".$e->getMessage());

                continue;
            }

            $free = array_sum(array_column($drives, 'free'));
            $total = array_sum(array_column($drives, 'total'));

            if ($total <= 0) {
                continue;
            }

            DiskReading::create([
                'machine_id' => $machine->id,
                'free_bytes' => $free,
                'total_bytes' => $total,
                'recorded_at' => now(),
            ]);

            $freePercent = ($free / $total) * 100;

            if ($freePercent < self::LOW_SPACE_THRESHOLD_PERCENT) {
                if (! $machine->low_space_alerted_at) {
                    $machine->update(['low_space_alerted_at' => now()]);
                    $this->ntfy->notify(sprintf(
                        '%s is low on disk space: %.1f%% free.',
                        $machine->name,
                        $freePercent,
                    ));
                }
            } elseif ($machine->low_space_alerted_at) {
                $machine->update(['low_space_alerted_at' => null]);
            }
        }

        DiskReading::where('recorded_at', '<', now()->subDays(30))->delete();

        return self::SUCCESS;
    }
}
