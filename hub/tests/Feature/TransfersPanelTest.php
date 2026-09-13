<?php

namespace Tests\Feature;

use App\Livewire\Transfers\Panel;
use App\Models\Machine;
use App\Models\RelayTransfer;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Http;
use Livewire\Livewire;
use Tests\TestCase;

class TransfersPanelTest extends TestCase
{
    use RefreshDatabase;

    public function test_it_shows_a_running_agent_side_transfer_for_an_accessible_machine(): void
    {
        Http::fake(['*/api/transfers' => Http::response(['transfers' => [
            ['id' => 'abc', 'kind' => 'copy', 'source' => '/a', 'destination' => '/b/file.txt', 'status' => 'running', 'bytes_done' => 50, 'bytes_total' => 100, 'error' => null, 'created_at' => now()->toIso8601String()],
        ]])]);

        $user = User::factory()->create();
        $machine = Machine::factory()->create();
        $machine->users()->attach($user, ['role' => 'read_only']);

        Livewire::actingAs($user)
            ->test(Panel::class)
            ->assertSee('file.txt')
            ->assertSee('Running');
    }

    public function test_it_does_not_show_transfers_for_an_inaccessible_machine(): void
    {
        Http::fake(['*/api/transfers' => Http::response(['transfers' => [
            ['id' => 'abc', 'kind' => 'copy', 'source' => '/a', 'destination' => '/b/secret-file.txt', 'status' => 'running', 'bytes_done' => 0, 'bytes_total' => 0, 'error' => null, 'created_at' => now()->toIso8601String()],
        ]])]);

        $user = User::factory()->create();
        Machine::factory()->create(); // not shared with $user

        Livewire::actingAs($user)
            ->test(Panel::class)
            ->assertDontSee('secret-file.txt');
    }

    public function test_it_shows_a_cross_machine_relay_in_progress(): void
    {
        $user = User::factory()->create();
        $source = Machine::factory()->create();
        $destination = Machine::factory()->create();
        $destination->users()->attach($user, ['role' => 'read_only']);

        RelayTransfer::create([
            'source_machine_id' => $source->id,
            'destination_machine_id' => $destination->id,
            'source_path' => '/a/report.pdf',
            'destination_path' => '/b/report.pdf',
            'kind' => 'copy',
            'status' => 'downloading',
        ]);

        Livewire::actingAs($user)
            ->test(Panel::class)
            ->assertSee('report.pdf')
            ->assertSee('Running');
    }

    public function test_it_hides_an_old_relay_transfer(): void
    {
        $user = User::factory()->create();
        $source = Machine::factory()->create();
        $destination = Machine::factory()->create();
        $destination->users()->attach($user, ['role' => 'read_only']);

        $relay = RelayTransfer::create([
            'source_machine_id' => $source->id,
            'destination_machine_id' => $destination->id,
            'source_path' => '/a/old-file.pdf',
            'destination_path' => '/b/old-file.pdf',
            'kind' => 'copy',
            'status' => 'done',
        ]);
        $relay->forceFill(['created_at' => now()->subHours(2)])->save();

        Livewire::actingAs($user)
            ->test(Panel::class)
            ->assertDontSee('old-file.pdf');
    }
}
