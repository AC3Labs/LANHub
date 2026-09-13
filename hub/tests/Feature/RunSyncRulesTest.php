<?php

namespace Tests\Feature;

use App\Models\Machine;
use App\Models\SyncRule;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class RunSyncRulesTest extends TestCase
{
    use RefreshDatabase;

    public function test_it_copies_a_new_source_file_to_the_destination_on_the_same_machine(): void
    {
        Http::fake([
            '*/api/list*' => Http::sequence()
                ->push(['path' => '/src', 'entries' => [
                    ['name' => 'a.txt', 'path' => '/src/a.txt', 'type' => 'file', 'size' => 10, 'modified' => '2026-01-01T00:00:00Z', 'hidden' => false],
                ]])
                ->push(['path' => '/dst', 'entries' => []]),
            '*/api/mkdir' => Http::response(['name' => 'dst', 'path' => '/dst', 'type' => 'dir']),
            '*/api/copy' => Http::response(['name' => 'a.txt', 'path' => '/dst/a.txt', 'type' => 'file']),
        ]);

        $machine = Machine::factory()->create(['os' => 'linux']);

        $rule = SyncRule::create([
            'name' => 'Test rule',
            'source_machine_id' => $machine->id,
            'source_path' => '/src',
            'destination_machine_id' => $machine->id,
            'destination_path' => '/dst',
            'direction' => 'one_way',
            'interval_minutes' => 60,
            'enabled' => true,
        ]);

        $this->artisan('sync:run-rules')->assertSuccessful();

        Http::assertSent(fn ($request) => str_contains($request->url(), '/api/copy')
            && $request['source'] === '/src/a.txt'
            && $request['destination'] === '/dst/a.txt');

        $rule->refresh();
        $this->assertNotNull($rule->last_run_at);
        $this->assertFalse($rule->has_conflict);
        $this->assertNull($rule->last_error);
    }

    public function test_it_skips_and_flags_a_conflict_when_the_destination_changed_since_the_scan(): void
    {
        $source = Machine::factory()->create(['host' => 'source.local', 'os' => 'linux']);
        $destination = Machine::factory()->create(['host' => 'dest.local', 'os' => 'linux']);

        Http::fake([
            'http://source.local:*/api/list*' => Http::response(['path' => '/src', 'entries' => [
                ['name' => 'a.txt', 'path' => '/src/a.txt', 'type' => 'file', 'size' => 20, 'modified' => '2026-02-01T00:00:00Z', 'hidden' => false],
            ]]),
            'http://dest.local:*/api/list*' => Http::sequence()
                // Initial scan: an older version of the same file.
                ->push(['path' => '/dst', 'entries' => [
                    ['name' => 'a.txt', 'path' => '/dst/a.txt', 'type' => 'file', 'size' => 10, 'modified' => '2026-01-01T00:00:00Z', 'hidden' => false],
                ]])
                // Fresh re-check immediately before writing: it changed
                // again in the meantime — must NOT be overwritten.
                ->push(['path' => '/dst', 'entries' => [
                    ['name' => 'a.txt', 'path' => '/dst/a.txt', 'type' => 'file', 'size' => 15, 'modified' => '2026-01-02T00:00:00Z', 'hidden' => false],
                ]]),
        ]);

        $rule = SyncRule::create([
            'name' => 'Conflict rule',
            'source_machine_id' => $source->id,
            'source_path' => '/src',
            'destination_machine_id' => $destination->id,
            'destination_path' => '/dst',
            'direction' => 'one_way',
            'interval_minutes' => 60,
            'enabled' => true,
        ]);

        $this->artisan('sync:run-rules')->assertSuccessful();

        Http::assertNotSent(fn ($request) => str_contains($request->url(), '/api/upload') || str_contains($request->url(), '/api/copy'));

        $rule->refresh();
        $this->assertTrue($rule->has_conflict);
    }

    public function test_disabled_rules_are_not_run(): void
    {
        Http::fake();

        $machine = Machine::factory()->create();

        SyncRule::create([
            'name' => 'Disabled rule',
            'source_machine_id' => $machine->id,
            'source_path' => '/src',
            'destination_machine_id' => $machine->id,
            'destination_path' => '/dst',
            'direction' => 'one_way',
            'interval_minutes' => 60,
            'enabled' => false,
        ]);

        $this->artisan('sync:run-rules')->assertSuccessful();

        Http::assertNothingSent();
    }
}
