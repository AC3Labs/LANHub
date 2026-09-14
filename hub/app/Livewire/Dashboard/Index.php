<?php

namespace App\Livewire\Dashboard;

use App\Exceptions\AgentException;
use App\Models\Machine;
use App\Models\User;
use Illuminate\Http\Client\ConnectionException;
use Illuminate\Support\Collection;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\Cache;
use Illuminate\Support\Str;
use Livewire\Attributes\Layout;
use Livewire\Attributes\Validate;
use Livewire\Component;

#[Layout('layouts.app')]
class Index extends Component
{
    /** @var array<int, array{hostname: ?string, status: string, drives: array, error: ?string}> */
    public array $status = [];

    public ?int $editingId = null;

    #[Validate('required|string|max:255')]
    public string $name = '';

    #[Validate('required|string|max:255')]
    public string $host = '';

    #[Validate('required|integer|min:1|max:65535')]
    public int $port = 8765;

    #[Validate('required|in:windows,linux,macos,other')]
    public string $os = 'linux';

    #[Validate('nullable|string|max:255')]
    public ?string $agentToken = null;

    #[Validate('required|string|max:7')]
    public string $color = '#ac8544';

    #[Validate('boolean')]
    public bool $useTls = false;

    #[Validate('nullable|string|max:1000')]
    public ?string $notes = null;

    /** @var array<int, 'none'|'read_only'|'full'> keyed by user id */
    public array $shareRoles = [];

    /** @var array<int, array{up: float, down: float, avg: float}> */
    public array $speeds = [];

    public function mount(): void
    {
        $this->refresh();
        $this->pollNetwork();
    }

    /**
     * Called every few seconds via wire:poll (see the view) — samples
     * each accessible machine's live network-interface byte counters and
     * turns the delta since the last sample into a real "right now"
     * speed, not a transfer-history average. First call after a fresh
     * page load always reads 0 for every gauge (there's no previous
     * sample yet to diff against) and fills in on the next tick.
     */
    public function pollNetwork(): void
    {
        foreach (Machine::accessibleTo(Auth::user())->get() as $machine) {
            $this->speeds[$machine->id] = $this->sampleCurrentSpeed($machine);
        }
    }

    /**
     * @return array{up: float, down: float, avg: float}
     */
    private function sampleCurrentSpeed(Machine $machine): array
    {
        $zero = ['up' => 0.0, 'down' => 0.0, 'avg' => 0.0];

        try {
            $stats = $machine->agent()->netstats();
        } catch (AgentException|ConnectionException) {
            return $zero;
        }

        $now = microtime(true);
        $cacheKey = "dashboard:netstats:{$machine->id}";
        $previous = Cache::get($cacheKey);
        Cache::put($cacheKey, ['sent' => $stats['bytes_sent'], 'recv' => $stats['bytes_recv'], 'at' => $now], now()->addMinutes(2));

        if (! $previous) {
            return $zero;
        }

        $elapsed = $now - $previous['at'];
        $sentDelta = $stats['bytes_sent'] - $previous['sent'];
        $recvDelta = $stats['bytes_recv'] - $previous['recv'];

        // A negative delta means the agent (or its host) restarted and
        // its counters reset to near-zero — showing that as a "speed"
        // would be a nonsense spike, not a real reading.
        if ($elapsed < 0.5 || $sentDelta < 0 || $recvDelta < 0) {
            return $zero;
        }

        $up = $sentDelta / $elapsed;
        $down = $recvDelta / $elapsed;

        return ['up' => $up, 'down' => $down, 'avg' => ($up + $down) / 2];
    }

    public function refresh(): void
    {
        foreach (Machine::accessibleTo(Auth::user())->get() as $machine) {
            $this->status[$machine->id] = $this->fetchStatus($machine);
        }
    }

    public function refreshOne(int $machineId): void
    {
        $machine = Machine::findOrFail($machineId);
        abort_unless(Auth::user()->canAccess($machine), 403);
        $this->status[$machineId] = $this->fetchStatus($machine);
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

    public function openCreate(): void
    {
        $this->reset(['editingId', 'name', 'host', 'port', 'os', 'agentToken', 'color', 'useTls', 'notes']);
        $this->port = 8765;
        $this->os = 'linux';
        $this->color = '#ac8544';
        $this->shareRoles = User::where('id', '!=', Auth::id())->pluck('id')->mapWithKeys(fn ($id) => [$id => 'none'])->all();
        $this->resetErrorBag();
        $this->dispatch('open-modal', 'machine-form');
    }

    public function openEdit(int $machineId): void
    {
        $machine = Machine::findOrFail($machineId);
        abort_unless(Auth::user()->canAccess($machine, 'full'), 403);

        $this->editingId = $machine->id;
        $this->name = $machine->name;
        $this->host = $machine->host;
        $this->port = $machine->port;
        $this->os = $machine->os;
        $this->agentToken = null;
        $this->color = $machine->color;
        $this->useTls = $machine->use_tls;
        $this->notes = $machine->notes;

        $existingRoles = $machine->users()->pluck('machine_user.role', 'users.id');
        $this->shareRoles = User::where('id', '!=', Auth::id())
            ->pluck('id')
            ->mapWithKeys(fn ($id) => [$id => $existingRoles[$id] ?? 'none'])
            ->all();

        $this->resetErrorBag();
        $this->dispatch('open-modal', 'machine-form');
    }

    /** @return Collection<int, User> */
    public function shareableUsers()
    {
        return User::where('id', '!=', Auth::id())->orderBy('name')->get();
    }

    public function save(): void
    {
        $this->validate();

        if (! $this->editingId && ! $this->agentToken) {
            $this->addError('agentToken', 'A token is required when registering a new machine.');

            return;
        }

        $data = [
            'name' => $this->name,
            'host' => $this->host,
            'port' => $this->port,
            'os' => $this->os,
            'color' => $this->color,
            'use_tls' => $this->useTls,
            'notes' => $this->notes,
        ];

        if ($this->agentToken) {
            $data['agent_token'] = $this->agentToken;
        }

        if ($this->editingId) {
            $machine = Machine::findOrFail($this->editingId);
            abort_unless(Auth::user()->canAccess($machine, 'full'), 403);
            $machine->update($data);
        } else {
            $machine = Machine::create($data + ['agent_token' => $this->agentToken]);
        }

        // The creating/editing user always keeps full access to a machine
        // they can see in this list — sharing only ever adds *other*
        // people, it can't be used to lock yourself out.
        $sync = [Auth::id() => ['role' => 'full']];
        foreach ($this->shareRoles as $userId => $role) {
            if ($role !== 'none') {
                $sync[$userId] = ['role' => $role];
            }
        }
        $machine->users()->sync($sync);

        $this->dispatch('close-modal', 'machine-form');
        $this->refresh();
    }

    public function delete(int $machineId): void
    {
        $machine = Machine::findOrFail($machineId);
        abort_unless(Auth::user()->canAccess($machine, 'full'), 403);
        $machine->delete();
        unset($this->status[$machineId]);
    }

    public function suggestToken(): void
    {
        $this->agentToken = Str::random(48);
    }

    public function render()
    {
        return view('livewire.dashboard.index', [
            'machines' => Machine::accessibleTo(Auth::user())->orderBy('sort_order')->orderBy('name')->get(),
        ]);
    }
}
