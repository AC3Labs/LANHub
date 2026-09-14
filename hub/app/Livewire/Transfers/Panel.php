<?php

namespace App\Livewire\Transfers;

use App\Models\Machine;
use App\Models\RelayTransfer;
use Illuminate\Support\Facades\Auth;
use Livewire\Component;

/**
 * A small floating panel (mounted globally in layouts/app.blade.php)
 * showing every in-flight transfer the user can see: same-machine jobs
 * live only on the agent (polled via GET /api/transfers), cross-machine
 * relays live in the hub's own relay_transfers table (see
 * App\Jobs\RelayTransferJob) since a relay job needs to be tracked
 * somewhere both agents don't share.
 */
class Panel extends Component
{
    public function render()
    {
        $machines = Machine::accessibleTo(Auth::user())->get();

        $agentJobs = $machines
            ->flatMap(function (Machine $machine) {
                try {
                    return collect($machine->agent()->transfers())
                        ->map(fn (array $job) => [
                            'kind' => $job['kind'],
                            'label' => basename(str_replace('\\', '/', $job['destination'])),
                            'status' => $job['status'],
                            'percent' => $job['bytes_total'] > 0
                                ? min(100, (int) round($job['bytes_done'] / $job['bytes_total'] * 100))
                                : ($job['status'] === 'done' ? 100 : 0),
                            'error' => $job['error'],
                            'color' => $machine->color,
                            'created_at' => $job['created_at'],
                        ]);
                } catch (\Throwable) {
                    return collect();
                }
            })
            ->filter(fn (array $job) => in_array($job['status'], ['pending', 'running'], true)
                || abs(now()->diffInSeconds($job['created_at'])) < 30);

        $relayPhasePercent = ['queued' => 10, 'downloading' => 40, 'uploading' => 75, 'done' => 100, 'error' => 100];

        $relayJobs = RelayTransfer::query()
            ->whereIn('destination_machine_id', $machines->pluck('id'))
            ->where('created_at', '>=', now()->subMinutes(10))
            ->latest()
            ->limit(50)
            ->get()
            ->map(fn (RelayTransfer $relay) => [
                'kind' => $relay->kind,
                'label' => basename(str_replace('\\', '/', $relay->destination_path)),
                'status' => in_array($relay->status, ['queued', 'downloading', 'uploading'], true) ? 'running' : $relay->status,
                'percent' => $relayPhasePercent[$relay->status],
                'error' => $relay->error,
                'color' => $relay->destinationMachine->color,
                'created_at' => $relay->created_at->toIso8601String(),
                'finished_at' => $relay->completed_at,
            ])
            // Same 30-second grace period as same-machine jobs, instead of
            // sitting in the panel for the full 10-minute query window
            // regardless of status — a relay that finished 8 minutes ago
            // has no more business on screen than one that finished 8
            // seconds ago just because the same-machine version doesn't.
            ->filter(fn (array $job) => $job['status'] === 'running'
                || ! $job['finished_at']
                || abs(now()->diffInSeconds($job['finished_at'])) < 30);

        $jobs = $agentJobs->concat($relayJobs)->sortByDesc('created_at')->values();

        return view('livewire.transfers.panel', ['jobs' => $jobs]);
    }
}
