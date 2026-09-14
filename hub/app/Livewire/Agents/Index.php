<?php

namespace App\Livewire\Agents;

use App\Exceptions\AgentException;
use App\Models\Machine;
use App\Models\RelayTransfer;
use Illuminate\Http\Client\ConnectionException;
use Illuminate\Support\Collection;
use Illuminate\Support\Facades\Auth;
use Livewire\Attributes\Layout;
use Livewire\Component;

#[Layout('layouts.app')]
class Index extends Component
{
    public function mount(): void
    {
        $this->refresh();
    }

    /**
     * Pings every accessible machine's agent live, same as the
     * Dashboard — the "Agents" page is specifically about health, so
     * showing a status that's only as fresh as the last unrelated page
     * visit or the once-a-minute background check would be misleading.
     */
    public function refresh(): void
    {
        foreach (Machine::accessibleTo(Auth::user())->get() as $machine) {
            try {
                $machine->agent()->health();
                $machine->update(['status' => 'online', 'last_seen_at' => now()]);
            } catch (AgentException|ConnectionException) {
                $machine->update(['status' => 'offline']);
            }
        }
    }

    public function render()
    {
        $machines = Machine::accessibleTo(Auth::user())->orderBy('sort_order')->orderBy('name')->get();

        return view('livewire.agents.index', [
            'machines' => $machines,
            'stats' => $this->computeStats($machines->pluck('id')),
        ]);
    }

    /**
     * One pass over every completed relay transfer touching an accessible
     * machine, aggregated here in PHP rather than SQL — average speed
     * needs bytes_transferred paired with (completed_at - started_at),
     * and doing that in raw SQL means dialect-specific date-diff
     * functions that don't behave the same on sqlite vs anything else
     * this ever runs on. Transfer volume on a LAN tool is small enough
     * that this is cheap.
     *
     * @param  Collection<int, int>  $machineIds
     * @return array<int, array{sent_bytes: int, received_bytes: int, avg_up_speed: float, avg_down_speed: float, active: int}>
     */
    private function computeStats(Collection $machineIds): array
    {
        $stats = [];
        foreach ($machineIds as $id) {
            $stats[$id] = [
                'sent_bytes' => 0,
                'received_bytes' => 0,
                'sent_seconds' => 0.0,
                'received_seconds' => 0.0,
                'active' => 0,
            ];
        }

        $transfers = RelayTransfer::query()
            ->where(fn ($q) => $q->whereIn('source_machine_id', $machineIds)->orWhereIn('destination_machine_id', $machineIds))
            ->get(['source_machine_id', 'destination_machine_id', 'bytes_transferred', 'status', 'started_at', 'completed_at']);

        foreach ($transfers as $transfer) {
            if (in_array($transfer->status, ['queued', 'downloading', 'uploading'], true)) {
                if (isset($stats[$transfer->source_machine_id])) {
                    $stats[$transfer->source_machine_id]['active']++;
                }
                if (isset($stats[$transfer->destination_machine_id])) {
                    $stats[$transfer->destination_machine_id]['active']++;
                }

                continue;
            }

            if ($transfer->status !== 'done' || ! $transfer->bytes_transferred || ! $transfer->started_at || ! $transfer->completed_at) {
                continue;
            }

            $seconds = max($transfer->started_at->diffInMilliseconds($transfer->completed_at) / 1000, 0.001);

            if (isset($stats[$transfer->source_machine_id])) {
                $stats[$transfer->source_machine_id]['sent_bytes'] += $transfer->bytes_transferred;
                $stats[$transfer->source_machine_id]['sent_seconds'] += $seconds;
            }

            if (isset($stats[$transfer->destination_machine_id])) {
                $stats[$transfer->destination_machine_id]['received_bytes'] += $transfer->bytes_transferred;
                $stats[$transfer->destination_machine_id]['received_seconds'] += $seconds;
            }
        }

        foreach ($stats as &$s) {
            $s['avg_up_speed'] = $s['sent_seconds'] > 0 ? $s['sent_bytes'] / $s['sent_seconds'] : 0.0;
            $s['avg_down_speed'] = $s['received_seconds'] > 0 ? $s['received_bytes'] / $s['received_seconds'] : 0.0;
        }

        return $stats;
    }
}
