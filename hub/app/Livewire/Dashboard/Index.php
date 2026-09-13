<?php

namespace App\Livewire\Dashboard;

use App\Exceptions\AgentException;
use App\Models\Machine;
use Illuminate\Http\Client\ConnectionException;
use Illuminate\Support\Facades\Auth;
use Livewire\Attributes\Layout;
use Livewire\Component;

#[Layout('layouts.app')]
class Index extends Component
{
    /** @var array<int, array{hostname: ?string, status: string, drives: array, error: ?string}> */
    public array $status = [];

    public function mount(): void
    {
        $this->refresh();
    }

    public function refresh(): void
    {
        foreach (Machine::accessibleTo(Auth::user())->get() as $machine) {
            $this->status[$machine->id] = $this->fetchStatus($machine);
        }
    }

    private function fetchStatus(Machine $machine): array
    {
        try {
            $health = $machine->agent()->health();
            $drives = $machine->agent()->drives();

            $machine->update(['status' => 'online', 'last_seen_at' => now()]);

            return [
                'hostname' => $health['hostname'] ?? null,
                'status' => 'online',
                'drives' => $drives,
                'error' => null,
            ];
        } catch (AgentException|ConnectionException $e) {
            $machine->update(['status' => 'offline']);

            return [
                'hostname' => null,
                'status' => 'offline',
                'drives' => [],
                'error' => $e->getMessage(),
            ];
        }
    }

    public function render()
    {
        return view('livewire.dashboard.index', [
            'machines' => Machine::accessibleTo(Auth::user())->orderBy('sort_order')->orderBy('name')->get(),
        ]);
    }
}
