<?php

namespace App\Livewire\Activity;

use App\Models\Machine;
use Illuminate\Support\Facades\Auth;
use Livewire\Attributes\Layout;
use Livewire\Component;

/**
 * Reads every accessible machine's own request log (agent/agent.py's
 * GET /api/activity) and merges them into one feed. There is no hub-side
 * table for this — each agent's log is the single source of truth, so
 * there's nothing to keep in sync.
 */
#[Layout('layouts.app')]
class Index extends Component
{
    public ?int $machineId = null;

    public function render()
    {
        $machines = Machine::accessibleTo(Auth::user())->orderBy('name')->get();

        $entries = $machines
            ->when($this->machineId, fn ($collection) => $collection->where('id', $this->machineId))
            ->flatMap(function (Machine $machine) {
                try {
                    return collect($machine->agent()->activity())
                        ->map(fn (array $entry) => $entry + ['machine' => $machine]);
                } catch (\Throwable) {
                    return collect();
                }
            })
            ->sortByDesc('time')
            ->take(300)
            ->values();

        return view('livewire.activity.index', [
            'machines' => $machines,
            'entries' => $entries,
        ]);
    }
}
