<?php

namespace Tests\Feature;

use App\Livewire\Agents\Index;
use App\Livewire\Agents\Log;
use App\Models\Machine;
use App\Models\RelayTransfer;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Http;
use Livewire\Livewire;
use Tests\TestCase;

class AgentsTest extends TestCase
{
    use RefreshDatabase;

    public function test_agents_index_computes_sent_received_and_average_speed(): void
    {
        Http::fake(['*/api/health' => Http::response(['hostname' => 'x', 'os' => 'linux', 'version' => '1.0.0'])]);

        $user = User::factory()->create();
        $a = Machine::factory()->create(['name' => 'Alpha']);
        $b = Machine::factory()->create(['name' => 'Beta']);
        $a->users()->attach($user, ['role' => 'read_only']);
        $b->users()->attach($user, ['role' => 'read_only']);

        // 10,000,000 bytes moved from A to B over 5 seconds => 2,000,000 B/s average.
        RelayTransfer::create([
            'source_machine_id' => $a->id,
            'destination_machine_id' => $b->id,
            'source_path' => '/a/video.mp4',
            'destination_path' => 'C:\\video.mp4',
            'bytes_transferred' => 10_000_000,
            'kind' => 'copy',
            'status' => 'done',
            'started_at' => now()->subSeconds(5),
            'completed_at' => now(),
        ]);

        $component = Livewire::actingAs($user)->test(Index::class);
        $stats = $component->instance()->render()->getData()['stats'];

        $this->assertSame(10_000_000, $stats[$a->id]['sent_bytes']);
        $this->assertSame(0, $stats[$a->id]['received_bytes']);
        $this->assertEqualsWithDelta(2_000_000, $stats[$a->id]['avg_up_speed'], 1);

        $this->assertSame(10_000_000, $stats[$b->id]['received_bytes']);
        $this->assertSame(0, $stats[$b->id]['sent_bytes']);
        $this->assertEqualsWithDelta(2_000_000, $stats[$b->id]['avg_down_speed'], 1);
    }

    public function test_agents_index_counts_in_progress_transfers(): void
    {
        Http::fake(['*/api/health' => Http::response(['hostname' => 'x', 'os' => 'linux', 'version' => '1.0.0'])]);

        $user = User::factory()->create();
        $a = Machine::factory()->create();
        $b = Machine::factory()->create();
        $a->users()->attach($user, ['role' => 'read_only']);
        $b->users()->attach($user, ['role' => 'read_only']);

        RelayTransfer::create([
            'source_machine_id' => $a->id,
            'destination_machine_id' => $b->id,
            'source_path' => '/a/file.zip',
            'destination_path' => 'C:\\file.zip',
            'kind' => 'copy',
            'status' => 'uploading',
        ]);

        $component = Livewire::actingAs($user)->test(Index::class);
        $stats = $component->instance()->render()->getData()['stats'];

        $this->assertSame(1, $stats[$a->id]['active']);
        $this->assertSame(1, $stats[$b->id]['active']);
    }

    public function test_transfer_log_shows_sent_and_received_rows(): void
    {
        $user = User::factory()->create();
        $a = Machine::factory()->create(['name' => 'Alpha']);
        $b = Machine::factory()->create(['name' => 'Beta']);
        $a->users()->attach($user, ['role' => 'read_only']);
        $b->users()->attach($user, ['role' => 'read_only']);

        RelayTransfer::create([
            'source_machine_id' => $a->id,
            'destination_machine_id' => $b->id,
            'source_path' => '/a/report.pdf',
            'destination_path' => 'C:\\report.pdf',
            'bytes_transferred' => 5000,
            'kind' => 'copy',
            'status' => 'done',
            'started_at' => now()->subSecond(),
            'completed_at' => now(),
        ]);

        RelayTransfer::create([
            'source_machine_id' => $b->id,
            'destination_machine_id' => $a->id,
            'source_path' => 'C:\\photo.jpg',
            'destination_path' => '/a/photo.jpg',
            'bytes_transferred' => 2000,
            'kind' => 'move',
            'status' => 'done',
            'started_at' => now()->subSecond(),
            'completed_at' => now(),
        ]);

        Livewire::actingAs($user)->test(Log::class, ['machine' => $a])
            ->assertSee('report.pdf')
            ->assertSee('Sent')
            ->assertSee('Beta')
            ->assertSee('photo.jpg')
            ->assertSee('Received');
    }

    public function test_agents_index_pings_every_machine_live_on_load(): void
    {
        $user = User::factory()->create();
        $online = Machine::factory()->create(['status' => 'offline']);
        $offline = Machine::factory()->create(['status' => 'online']);
        $online->users()->attach($user, ['role' => 'read_only']);
        $offline->users()->attach($user, ['role' => 'read_only']);

        Http::fake([
            $online->base_url.'/api/health' => Http::response(['hostname' => 'x', 'os' => 'linux', 'version' => '1.0.0']),
            $offline->base_url.'/api/health' => Http::response([], 500),
        ]);

        Livewire::actingAs($user)->test(Index::class);

        $this->assertSame('online', $online->fresh()->status);
        $this->assertSame('offline', $offline->fresh()->status);
    }

    public function test_transfer_log_denies_a_user_without_access(): void
    {
        $user = User::factory()->create();
        $machine = Machine::factory()->create();
        // Deliberately not attached — no access.

        Livewire::actingAs($user)->test(Log::class, ['machine' => $machine])
            ->assertStatus(403);
    }
}
