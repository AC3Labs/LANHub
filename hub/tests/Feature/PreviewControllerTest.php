<?php

namespace Tests\Feature;

use App\Models\Machine;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Http\Client\ConnectionException;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class PreviewControllerTest extends TestCase
{
    use RefreshDatabase;

    public function test_it_streams_the_agent_response_through_unchanged_for_an_accessible_machine(): void
    {
        Http::fake([
            '*/api/preview*' => Http::response(
                '<html><script>alert(1)</script></html>',
                200,
                ['Content-Type' => 'text/plain; charset=utf-8', 'X-Content-Type-Options' => 'nosniff']
            ),
        ]);

        $user = User::factory()->create();
        $machine = Machine::factory()->create();
        $machine->users()->attach($user, ['role' => 'read_only']);

        $response = $this->actingAs($user)->get("/machines/{$machine->id}/preview?path=".urlencode('/notes/evil.html'));

        $response->assertOk();
        $response->assertHeader('Content-Type', 'text/plain; charset=utf-8');
        $response->assertHeader('X-Content-Type-Options', 'nosniff');
        $response->assertDontSee('text/html', false);
        $this->assertSame('<html><script>alert(1)</script></html>', $response->streamedContent());
        $response->assertHeader('X-Accel-Buffering', 'no');
    }

    public function test_it_denies_a_user_with_no_access_to_the_machine(): void
    {
        $user = User::factory()->create();
        $machine = Machine::factory()->create();

        $this->actingAs($user)
            ->get("/machines/{$machine->id}/preview?path=/notes.txt")
            ->assertForbidden();
    }

    public function test_it_maps_an_unsupported_file_type_to_a_415(): void
    {
        Http::fake([
            '*/api/preview*' => Http::response(['message' => 'No preview available for archive.zip.'], 415),
        ]);

        $user = User::factory()->create();
        $machine = Machine::factory()->create();
        $machine->users()->attach($user, ['role' => 'read_only']);

        $this->actingAs($user)
            ->get("/machines/{$machine->id}/preview?path=/archive.zip")
            ->assertStatus(415);
    }

    public function test_an_unreachable_agent_is_a_502(): void
    {
        Http::fake(fn () => throw new ConnectionException('Connection refused'));

        $user = User::factory()->create();
        $machine = Machine::factory()->create();
        $machine->users()->attach($user, ['role' => 'read_only']);

        $this->actingAs($user)
            ->get("/machines/{$machine->id}/preview?path=/photo.jpg")
            ->assertStatus(502);
    }

    public function test_a_missing_file_is_a_404(): void
    {
        Http::fake(['*/api/preview*' => Http::response(['message' => 'not a file'], 404)]);

        $user = User::factory()->create();
        $machine = Machine::factory()->create();
        $machine->users()->attach($user, ['role' => 'read_only']);

        $this->actingAs($user)
            ->get("/machines/{$machine->id}/preview?path=/gone.jpg")
            ->assertNotFound();
    }
}
