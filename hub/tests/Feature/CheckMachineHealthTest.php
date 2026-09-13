<?php

namespace Tests\Feature;

use App\Models\Machine;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class CheckMachineHealthTest extends TestCase
{
    use RefreshDatabase;

    public function test_it_marks_a_reachable_machine_online(): void
    {
        Http::fake(['*/api/health' => Http::response(['hostname' => 'box', 'os' => 'linux', 'version' => '0.2.0'])]);

        $machine = Machine::factory()->create(['status' => 'unknown']);

        $this->artisan('machines:check-health')->assertSuccessful();

        $machine->refresh();
        $this->assertSame('online', $machine->status);
        $this->assertNotNull($machine->last_seen_at);
    }

    public function test_it_marks_an_unreachable_machine_offline_and_alerts_on_the_transition(): void
    {
        config(['services.ntfy.topic' => 'test-topic']);
        Http::fake([
            '*/api/health' => Http::response([], 500),
            'https://ntfy.sh/*' => Http::response('ok'),
        ]);

        $machine = Machine::factory()->create(['status' => 'online']);

        $this->artisan('machines:check-health')->assertSuccessful();

        $this->assertSame('offline', $machine->fresh()->status);
        Http::assertSent(fn ($request) => str_contains($request->url(), 'ntfy.sh'));
    }

    public function test_it_does_not_alert_again_while_a_machine_stays_offline(): void
    {
        config(['services.ntfy.topic' => 'test-topic']);
        Http::fake([
            '*/api/health' => Http::response([], 500),
            'https://ntfy.sh/*' => Http::response('ok'),
        ]);

        $machine = Machine::factory()->create(['status' => 'offline']);

        $this->artisan('machines:check-health')->assertSuccessful();

        Http::assertNotSent(fn ($request) => str_contains($request->url(), 'ntfy.sh'));
    }
}
