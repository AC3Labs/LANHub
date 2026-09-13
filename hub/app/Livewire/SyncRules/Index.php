<?php

namespace App\Livewire\SyncRules;

use App\Models\Machine;
use App\Models\SyncRule;
use Illuminate\Support\Facades\Auth;
use Livewire\Attributes\Layout;
use Livewire\Attributes\Validate;
use Livewire\Component;

#[Layout('layouts.app')]
class Index extends Component
{
    public ?int $editingId = null;

    #[Validate('required|string|max:255')]
    public string $name = '';

    #[Validate('required|exists:machines,id')]
    public ?int $sourceMachineId = null;

    #[Validate('required|string|max:1000')]
    public string $sourcePath = '';

    #[Validate('required|exists:machines,id')]
    public ?int $destinationMachineId = null;

    #[Validate('required|string|max:1000')]
    public string $destinationPath = '';

    #[Validate('required|in:one_way,mirror')]
    public string $direction = 'one_way';

    #[Validate('required|integer|min:5|max:10080')]
    public int $intervalMinutes = 60;

    #[Validate('boolean')]
    public bool $enabled = true;

    public function openCreate(): void
    {
        $this->reset(['editingId', 'name', 'sourceMachineId', 'sourcePath', 'destinationMachineId', 'destinationPath', 'direction', 'intervalMinutes', 'enabled']);
        $this->direction = 'one_way';
        $this->intervalMinutes = 60;
        $this->enabled = true;
        $this->resetErrorBag();
        $this->dispatch('open-modal', 'sync-rule-form');
    }

    public function openEdit(int $ruleId): void
    {
        $rule = SyncRule::findOrFail($ruleId);
        $this->authorizeRule($rule);

        $this->editingId = $rule->id;
        $this->name = $rule->name;
        $this->sourceMachineId = $rule->source_machine_id;
        $this->sourcePath = $rule->source_path;
        $this->destinationMachineId = $rule->destination_machine_id;
        $this->destinationPath = $rule->destination_path;
        $this->direction = $rule->direction;
        $this->intervalMinutes = $rule->interval_minutes;
        $this->enabled = $rule->enabled;
        $this->resetErrorBag();
        $this->dispatch('open-modal', 'sync-rule-form');
    }

    public function save(): void
    {
        $this->validate();

        $sourceMachine = Machine::findOrFail($this->sourceMachineId);
        $destinationMachine = Machine::findOrFail($this->destinationMachineId);
        abort_unless(Auth::user()->canAccess($sourceMachine), 403);
        abort_unless(Auth::user()->canAccess($destinationMachine, 'full'), 403);

        $data = [
            'name' => $this->name,
            'source_machine_id' => $this->sourceMachineId,
            'source_path' => $this->sourcePath,
            'destination_machine_id' => $this->destinationMachineId,
            'destination_path' => $this->destinationPath,
            'direction' => $this->direction,
            'interval_minutes' => $this->intervalMinutes,
            'enabled' => $this->enabled,
        ];

        if ($this->editingId) {
            $rule = SyncRule::findOrFail($this->editingId);
            $this->authorizeRule($rule);
            $rule->update($data);
        } else {
            SyncRule::create($data + ['created_by' => Auth::id(), 'has_conflict' => false]);
        }

        $this->dispatch('close-modal', 'sync-rule-form');
    }

    public function toggle(int $ruleId): void
    {
        $rule = SyncRule::findOrFail($ruleId);
        $this->authorizeRule($rule);
        $rule->update(['enabled' => ! $rule->enabled]);
    }

    public function delete(int $ruleId): void
    {
        $rule = SyncRule::findOrFail($ruleId);
        $this->authorizeRule($rule);
        $rule->delete();
    }

    private function authorizeRule(SyncRule $rule): void
    {
        abort_unless(Auth::user()->canAccess($rule->destinationMachine, 'full'), 403);
    }

    public function render()
    {
        $machines = Machine::accessibleTo(Auth::user())->orderBy('name')->get();

        return view('livewire.sync-rules.index', [
            'machines' => $machines,
            'rules' => SyncRule::whereIn('destination_machine_id', $machines->pluck('id'))
                ->orWhereIn('source_machine_id', $machines->pluck('id'))
                ->with(['sourceMachine', 'destinationMachine'])
                ->orderBy('name')
                ->get(),
        ]);
    }
}
