<?php

namespace Tests\Feature;

use App\Models\Machine;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class ExplorerSmokeTest extends TestCase
{
    use RefreshDatabase;

    public function test_machines_page_redirects_to_the_merged_dashboard(): void
    {
        $user = User::factory()->create();

        $this->actingAs($user)
            ->get('/machines')
            ->assertRedirect('/dashboard');
    }

    public function test_dashboard_page_renders_for_an_authenticated_user(): void
    {
        $user = User::factory()->create();

        $this->actingAs($user)
            ->get('/dashboard')
            ->assertOk()
            ->assertSee('Dashboard');
    }

    public function test_explorer_page_renders_with_a_registered_machine(): void
    {
        Http::fake([
            '*/api/drives' => Http::response(['drives' => [
                ['path' => '/', 'label' => '/', 'free' => 1000, 'total' => 2000],
            ]]),
        ]);

        $user = User::factory()->create();

        $machine = Machine::factory()->create([
            'name' => 'Test Box',
            'host' => '127.0.0.1',
            'port' => 8765,
            'os' => 'linux',
            'agent_token' => 'dev-test-token-123',
        ]);
        $machine->users()->attach($user, ['role' => 'full']);

        $this->actingAs($user)
            ->get('/explorer')
            ->assertOk()
            ->assertSee('Explorer')
            ->assertSee('Test Box');
    }

    public function test_explorer_hides_machines_the_user_has_no_access_to(): void
    {
        $user = User::factory()->create();

        Machine::factory()->create([
            'name' => 'Not Shared Box',
            'host' => '127.0.0.1',
            'port' => 8765,
            'os' => 'linux',
            'agent_token' => 'dev-test-token-123',
        ]);

        $this->actingAs($user)
            ->get('/explorer')
            ->assertOk()
            ->assertDontSee('Not Shared Box');
    }
}
