<?php

namespace App\Livewire\Machines;

use App\Exceptions\AgentException;
use App\Models\Machine;
use App\Models\User;
use Illuminate\Http\Client\ConnectionException;
use Illuminate\Support\Collection;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Str;
use Livewire\Attributes\Layout;
use Livewire\Attributes\Validate;
use Livewire\Component;

#[Layout('layouts.app')]
class Index extends Component
{
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

    public array $statuses = [];

    public function mount(): void
    {
        $this->refreshStatuses();
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
        $this->refreshStatuses();
    }

    public function delete(int $machineId): void
    {
        $machine = Machine::findOrFail($machineId);
        abort_unless(Auth::user()->canAccess($machine, 'full'), 403);
        $machine->delete();
        $this->refreshStatuses();
    }

    public function checkStatus(int $machineId): void
    {
        $machine = Machine::findOrFail($machineId);
        abort_unless(Auth::user()->canAccess($machine), 403);

        try {
            $health = $machine->agent()->health();
            $machine->update(['status' => 'online', 'last_seen_at' => now()]);
            $this->statuses[$machineId] = ['status' => 'online', 'health' => $health];
        } catch (AgentException|ConnectionException $e) {
            $machine->update(['status' => 'offline']);
            $this->statuses[$machineId] = ['status' => 'offline', 'error' => $e->getMessage()];
        }
    }

    public function suggestToken(): void
    {
        $this->agentToken = Str::random(48);
    }

    private function refreshStatuses(): void
    {
        foreach (Machine::accessibleTo(Auth::user())->get() as $machine) {
            $this->statuses[$machine->id] = ['status' => $machine->status];
        }
    }

    public function render()
    {
        return view('livewire.machines.index', [
            'machines' => Machine::accessibleTo(Auth::user())->orderBy('sort_order')->orderBy('name')->get(),
        ]);
    }
}
