<?php

namespace Tests\Feature;

use App\Models\Machine;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class DashboardTest extends TestCase
{
    use RefreshDatabase;

    public function test_it_shows_only_accessible_machines(): void
    {
        Http::fake([
            '*/api/health' => Http::response(['hostname' => 'box', 'os' => 'linux', 'version' => '0.2.0']),
            '*/api/drives' => Http::response(['drives' => []]),
        ]);

        $user = User::factory()->create();

        $shared = Machine::factory()->create(['name' => 'Visible Machine']);
        $shared->users()->attach($user, ['role' => 'read_only']);

        Machine::factory()->create(['name' => 'Hidden Machine']);

        $this->actingAs($user)
            ->get('/dashboard')
            ->assertOk()
            ->assertSee('Visible Machine')
            ->assertDontSee('Hidden Machine');
    }

    public function test_it_marks_a_reachable_machine_online_and_records_last_seen(): void
    {
        Http::fake([
            '*/api/health' => Http::response(['hostname' => 'box', 'os' => 'linux', 'version' => '0.2.0']),
            '*/api/drives' => Http::response(['drives' => []]),
        ]);

        $user = User::factory()->create();
        $machine = Machine::factory()->create(['status' => 'unknown']);
        $machine->users()->attach($user, ['role' => 'read_only']);

        $this->actingAs($user)->get('/dashboard')->assertOk();

        $machine->refresh();
        $this->assertSame('online', $machine->status);
        $this->assertNotNull($machine->last_seen_at);
    }

    public function test_it_marks_an_unreachable_machine_offline(): void
    {
        Http::fake(['*/api/health' => Http::response([], 500)]);

        $user = User::factory()->create();
        $machine = Machine::factory()->create(['status' => 'online']);
        $machine->users()->attach($user, ['role' => 'read_only']);

        $this->actingAs($user)->get('/dashboard')->assertOk();

        $this->assertSame('offline', $machine->fresh()->status);
    }
}
