<?php

namespace Tests\Feature;

use App\Livewire\Explorer\Index;
use App\Models\Machine;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Http;
use Livewire\Livewire;
use Tests\TestCase;

class MachineAccessControlTest extends TestCase
{
    use RefreshDatabase;

    public function test_user_with_no_pivot_row_cannot_access_a_machine(): void
    {
        $user = User::factory()->create();
        $machine = Machine::factory()->create();

        $this->assertFalse($user->canAccess($machine));
    }

    public function test_read_only_role_does_not_satisfy_a_full_access_check(): void
    {
        $user = User::factory()->create();
        $machine = Machine::factory()->create();
        $machine->users()->attach($user, ['role' => 'read_only']);

        $this->assertTrue($user->canAccess($machine));
        $this->assertTrue($user->canAccess($machine, 'read_only'));
        $this->assertFalse($user->canAccess($machine, 'full'));
    }

    public function test_full_role_satisfies_both_access_levels(): void
    {
        $user = User::factory()->create();
        $machine = Machine::factory()->create();
        $machine->users()->attach($user, ['role' => 'full']);

        $this->assertTrue($user->canAccess($machine, 'read_only'));
        $this->assertTrue($user->canAccess($machine, 'full'));
    }

    public function test_read_only_user_cannot_delete_a_file_in_the_explorer(): void
    {
        Http::fake([
            '*/api/drives' => Http::response(['drives' => []]),
            '*/api/delete' => Http::response(['deleted' => '/tmp/foo.txt']),
        ]);

        $user = User::factory()->create();
        $machine = Machine::factory()->create(['os' => 'linux']);
        $machine->users()->attach($user, ['role' => 'read_only']);

        $component = Livewire::actingAs($user)->test(Index::class);
        $paneId = $component->instance()->panes[0]['id'];

        $component->call('deleteEntry', $paneId, '/tmp/foo.txt')
            ->assertForbidden();
    }
}
