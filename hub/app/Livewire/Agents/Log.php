<?php

namespace App\Livewire\Agents;

use App\Models\Machine;
use App\Models\RelayTransfer;
use Illuminate\Support\Facades\Auth;
use Livewire\Attributes\Layout;
use Livewire\Component;
use Livewire\WithPagination;

#[Layout('layouts.app')]
class Log extends Component
{
    use WithPagination;

    public Machine $machine;

    public function mount(Machine $machine): void
    {
        abort_unless(Auth::user()->canAccess($machine), 403);

        $this->machine = $machine;
    }

    public function render()
    {
        $transfers = RelayTransfer::with(['sourceMachine', 'destinationMachine'])
            ->where('source_machine_id', $this->machine->id)
            ->orWhere('destination_machine_id', $this->machine->id)
            ->orderByDesc('created_at')
            ->paginate(25);

        return view('livewire.agents.log', ['transfers' => $transfers]);
    }
}
