<?php

namespace Tests\Feature;

use App\Livewire\Explorer\Index;
use App\Models\Machine;
use App\Models\RelayTransfer;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Http;
use Livewire\Livewire;
use Tests\TestCase;

class RelayTransferTest extends TestCase
{
    use RefreshDatabase;

    public function test_dropping_a_file_on_a_different_machine_queues_a_relay_that_completes(): void
    {
        Http::fake([
            '*/api/drives' => Http::response(['drives' => []]),
            '*/api/download*' => Http::response('file contents', 200, ['Content-Type' => 'text/plain']),
            '*/api/upload*' => Http::response(['name' => 'notes.txt', 'path' => 'C:\\notes.txt', 'type' => 'file']),
        ]);

        $user = User::factory()->create();

        $sourceMachine = Machine::factory()->create(['os' => 'linux']);
        $destinationMachine = Machine::factory()->create(['os' => 'windows']);
        $sourceMachine->users()->attach($user, ['role' => 'full']);
        $destinationMachine->users()->attach($user, ['role' => 'full']);

        $component = Livewire::actingAs($user)->test(Index::class);
        $panes = $component->instance()->panes;
        $sourcePaneId = collect($panes)->firstWhere('machine_id', $sourceMachine->id)['id'];
        $destPaneId = collect($panes)->firstWhere('machine_id', $destinationMachine->id)['id'];

        $component->call(
            'handleInternalDrop',
            $sourcePaneId,
            '/home/andrew/notes.txt',
            'notes.txt',
            $destPaneId,
            'C:\\Users\\andrew',
            true,
        );

        $this->assertDatabaseCount('relay_transfers', 1);
        $relay = RelayTransfer::first();
        $this->assertSame('done', $relay->status);
        $this->assertSame($sourceMachine->id, $relay->source_machine_id);
        $this->assertSame($destinationMachine->id, $relay->destination_machine_id);
    }
}
