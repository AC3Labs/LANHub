<?php

namespace Tests\Feature;

use App\Models\DiskReading;
use App\Models\Machine;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class CheckDiskSpaceTest extends TestCase
{
    use RefreshDatabase;

    public function test_it_records_a_reading_and_alerts_once_on_the_low_space_transition(): void
    {
        config(['services.ntfy.topic' => 'test-topic']);

        Http::fake([
            '*/api/drives' => Http::response(['drives' => [
                ['path' => '/', 'label' => '/', 'free' => 50, 'total' => 1000],
            ]]),
            'https://ntfy.sh/*' => Http::response('ok'),
        ]);

        $machine = Machine::factory()->create();

        $this->artisan('machines:check-disk-space')->assertSuccessful();

        $this->assertDatabaseCount('disk_readings', 1);
        $reading = DiskReading::first();
        $this->assertSame(50, $reading->free_bytes);
        $this->assertSame(1000, $reading->total_bytes);

        $machine->refresh();
        $this->assertNotNull($machine->low_space_alerted_at);

        Http::assertSentCount(2); // drives + ntfy alert

        // A second run while still low should NOT alert again.
        $this->artisan('machines:check-disk-space')->assertSuccessful();
        Http::assertSentCount(3); // one more drives call, no second ntfy call
    }

    public function test_it_clears_the_alert_flag_once_space_recovers(): void
    {
        Http::fake(['*/api/drives' => Http::response(['drives' => [
            ['path' => '/', 'label' => '/', 'free' => 900, 'total' => 1000],
        ]])]);

        $machine = Machine::factory()->create(['low_space_alerted_at' => now()]);

        $this->artisan('machines:check-disk-space')->assertSuccessful();

        $this->assertNull($machine->refresh()->low_space_alerted_at);
    }
}
