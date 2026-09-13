<?php

namespace App\Console\Commands;

use App\Exceptions\AgentException;
use App\Models\Machine;
use App\Services\NtfyClient;
use Illuminate\Console\Command;
use Illuminate\Http\Client\ConnectionException;
use Illuminate\Support\Facades\Log;

/**
 * Background health check for every registered Machine — the only thing
 * that previously updated Machine.status was a Livewire component
 * actually being open in someone's browser, so a machine going offline
 * was silent until someone happened to load the Dashboard/Machines page.
 * Runs on the scheduler (see routes/console.php) so status/last_seen_at
 * stay current, and fires a push notification via ntfy.sh the moment a
 * machine transitions online -> offline, rather than only on discovery.
 */
class CheckMachineHealth extends Command
{
    protected $signature = 'machines:check-health';

    protected $description = 'Ping every registered machine, update its status, and alert on offline transitions';

    public function __construct(private readonly NtfyClient $ntfy)
    {
        parent::__construct();
    }

    public function handle(): int
    {
        foreach (Machine::all() as $machine) {
            $wasOnline = $machine->status === 'online';

            try {
                $machine->agent()->health();
                $machine->update(['status' => 'online', 'last_seen_at' => now()]);

                if (! $wasOnline) {
                    $this->notify("{$machine->name} is back online.");
                }
            } catch (AgentException|ConnectionException $e) {
                $machine->update(['status' => 'offline']);

                if ($wasOnline) {
                    Log::warning("Machine {$machine->name} went offline: ".$e->getMessage());
                    $this->notify("{$machine->name} just went offline: ".$e->getMessage());
                }
            }
        }

        return self::SUCCESS;
    }

    private function notify(string $message): void
    {
        $this->ntfy->notify($message);
    }
}
