<?php

namespace Tests\Feature;

use App\Livewire\SyncRules\Index;
use App\Models\Machine;
use App\Models\SyncRule;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Livewire\Livewire;
use Tests\TestCase;

class SyncRulesManagementTest extends TestCase
{
    use RefreshDatabase;

    public function test_creating_a_rule_persists_every_field(): void
    {
        $user = User::factory()->create();
        $source = Machine::factory()->create();
        $destination = Machine::factory()->create();
        $source->users()->attach($user, ['role' => 'read_only']);
        $destination->users()->attach($user, ['role' => 'full']);

        Livewire::actingAs($user)
            ->test(Index::class)
            ->set('name', 'Backup Photos')
            ->set('sourceMachineId', $source->id)
            ->set('sourcePath', '/src')
            ->set('destinationMachineId', $destination->id)
            ->set('destinationPath', '/dst')
            ->set('direction', 'mirror')
            ->set('intervalMinutes', 30)
            ->call('save')
            ->assertHasNoErrors();

        $rule = SyncRule::where('name', 'Backup Photos')->firstOrFail();
        $this->assertSame($source->id, $rule->source_machine_id);
        $this->assertSame('/src', $rule->source_path);
        $this->assertSame($destination->id, $rule->destination_machine_id);
        $this->assertSame('/dst', $rule->destination_path);
        $this->assertSame('mirror', $rule->direction);
        $this->assertSame(30, $rule->interval_minutes);
        $this->assertTrue($rule->enabled);
    }

    public function test_toggle_flips_the_enabled_flag(): void
    {
        $user = User::factory()->create();
        $destination = Machine::factory()->create();
        $destination->users()->attach($user, ['role' => 'full']);

        $rule = SyncRule::create([
            'name' => 'Test rule',
            'source_machine_id' => Machine::factory()->create()->id,
            'source_path' => '/a',
            'destination_machine_id' => $destination->id,
            'destination_path' => '/b',
            'direction' => 'one_way',
            'interval_minutes' => 60,
            'enabled' => true,
        ]);

        Livewire::actingAs($user)->test(Index::class)->call('toggle', $rule->id);

        $this->assertFalse($rule->fresh()->enabled);
    }

    public function test_a_user_without_full_access_to_the_destination_cannot_manage_the_rule(): void
    {
        $user = User::factory()->create();
        $destination = Machine::factory()->create();
        $destination->users()->attach($user, ['role' => 'read_only']);

        $rule = SyncRule::create([
            'name' => 'Test rule',
            'source_machine_id' => Machine::factory()->create()->id,
            'source_path' => '/a',
            'destination_machine_id' => $destination->id,
            'destination_path' => '/b',
            'direction' => 'one_way',
            'interval_minutes' => 60,
            'enabled' => true,
        ]);

        Livewire::actingAs($user)
            ->test(Index::class)
            ->call('toggle', $rule->id)
            ->assertStatus(403);
    }
}
