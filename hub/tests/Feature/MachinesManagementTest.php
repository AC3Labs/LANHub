<?php

namespace Tests\Feature;

use App\Livewire\Dashboard\Index;
use App\Models\Machine;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Livewire\Livewire;
use Tests\TestCase;

class MachinesManagementTest extends TestCase
{
    use RefreshDatabase;

    public function test_creating_a_machine_persists_every_field(): void
    {
        $user = User::factory()->create();

        Livewire::actingAs($user)
            ->test(Index::class)
            ->set('name', 'New Box')
            ->set('host', '10.0.0.5')
            ->set('port', 8765)
            ->set('os', 'windows')
            ->set('agentToken', 'a-real-token')
            ->set('color', '#123456')
            ->set('useTls', true)
            ->set('notes', 'some notes')
            ->call('save')
            ->assertHasNoErrors();

        $machine = Machine::where('name', 'New Box')->firstOrFail();
        $this->assertSame('10.0.0.5', $machine->host);
        $this->assertSame(8765, $machine->port);
        $this->assertSame('windows', $machine->os);
        $this->assertSame('a-real-token', $machine->agent_token);
        $this->assertSame('#123456', $machine->color);
        $this->assertTrue($machine->use_tls);
        $this->assertSame('some notes', $machine->notes);

        // The creating user should automatically have full access.
        $this->assertTrue($user->fresh()->canAccess($machine, 'full'));
    }

    public function test_editing_a_machine_updates_every_field_and_can_keep_the_existing_token(): void
    {
        $user = User::factory()->create();
        $machine = Machine::factory()->create(['agent_token' => 'original-token']);
        $machine->users()->attach($user, ['role' => 'full']);

        Livewire::actingAs($user)
            ->test(Index::class)
            ->call('openEdit', $machine->id)
            ->set('name', 'Renamed Box')
            ->set('host', '10.0.0.9')
            ->set('color', '#abcdef')
            ->call('save')
            ->assertHasNoErrors();

        $machine->refresh();
        $this->assertSame('Renamed Box', $machine->name);
        $this->assertSame('10.0.0.9', $machine->host);
        $this->assertSame('#abcdef', $machine->color);
        $this->assertSame('original-token', $machine->agent_token);
    }
}
