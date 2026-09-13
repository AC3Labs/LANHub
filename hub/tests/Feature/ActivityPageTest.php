<?php

namespace Tests\Feature;

use App\Models\Machine;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class ActivityPageTest extends TestCase
{
    use RefreshDatabase;

    public function test_it_shows_entries_only_from_accessible_machines(): void
    {
        Http::fake([
            '*/api/activity*' => Http::response(['entries' => [
                ['time' => now()->toIso8601String(), 'method' => 'GET', 'path' => '/api/list?path=/', 'status' => 200, 'client' => '10.0.0.5'],
            ]]),
        ]);

        $user = User::factory()->create();

        $shared = Machine::factory()->create(['name' => 'Shared Box']);
        $shared->users()->attach($user, ['role' => 'read_only']);

        Machine::factory()->create(['name' => 'Hidden Box']);

        $this->actingAs($user)
            ->get('/activity')
            ->assertOk()
            ->assertSee('Shared Box')
            ->assertDontSee('Hidden Box');
    }
}
