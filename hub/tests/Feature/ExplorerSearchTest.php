<?php

namespace Tests\Feature;

use App\Livewire\Explorer\Index;
use App\Models\Machine;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Http;
use Livewire\Livewire;
use Tests\TestCase;

class ExplorerSearchTest extends TestCase
{
    use RefreshDatabase;

    /**
     * Drains the per-machine search queue exactly like the browser's
     * runGlobalSearch() Alpine loop does — one searchNextMachine() call
     * per machine until none are left.
     */
    private function runFullGlobalSearch($component): void
    {
        $component->call('startGlobalSearch');

        while ($component->instance()->globalQueue !== []) {
            $component->call('searchNextMachine');
        }
    }

    public function test_searching_a_pane_populates_results_from_the_agent(): void
    {
        Http::fake([
            '*/api/drives' => Http::response(['drives' => []]),
            '*/api/search*' => Http::response(['path' => '/', 'query' => 'invoice', 'entries' => [
                ['name' => 'invoice-1.pdf', 'path' => '/docs/invoice-1.pdf', 'type' => 'file', 'size' => 100, 'modified' => null, 'hidden' => false],
            ], 'truncated' => false]),
        ]);

        $user = User::factory()->create();
        $machine = Machine::factory()->create(['os' => 'linux']);
        $machine->users()->attach($user, ['role' => 'read_only']);

        $component = Livewire::actingAs($user)->test(Index::class);
        $paneId = $component->instance()->panes[0]['id'];

        $component->set("searchQueries.{$paneId}", 'invoice')
            ->call('search', $paneId)
            ->assertSet("searchResults.{$paneId}.0.name", 'invoice-1.pdf');
    }

    public function test_clearing_the_query_clears_the_results(): void
    {
        Http::fake(['*/api/drives' => Http::response(['drives' => []])]);

        $user = User::factory()->create();
        $machine = Machine::factory()->create(['os' => 'linux']);
        $machine->users()->attach($user, ['role' => 'read_only']);

        $component = Livewire::actingAs($user)->test(Index::class);
        $paneId = $component->instance()->panes[0]['id'];

        $component->set("searchQueries.{$paneId}", '')
            ->call('search', $paneId);

        $this->assertArrayNotHasKey($paneId, $component->instance()->searchResults);
    }

    public function test_search_everywhere_aggregates_results_across_every_accessible_machine(): void
    {
        $user = User::factory()->create();
        $one = Machine::factory()->create(['os' => 'linux', 'name' => 'Alpha']);
        $two = Machine::factory()->create(['os' => 'linux', 'name' => 'Beta']);
        $one->users()->attach($user, ['role' => 'read_only']);
        $two->users()->attach($user, ['role' => 'read_only']);

        Http::fake([
            $one->base_url.'/api/drives' => Http::response(['drives' => [['label' => '/', 'path' => '/']]]),
            $one->base_url.'/api/search*' => Http::response(['entries' => [
                ['name' => 'invoice.pdf', 'path' => '/docs/invoice.pdf', 'type' => 'file', 'size' => 1, 'modified' => null, 'hidden' => false],
            ], 'truncated' => false]),
            $two->base_url.'/api/drives' => Http::response(['drives' => [['label' => '/', 'path' => '/']]]),
            $two->base_url.'/api/search*' => Http::response(['entries' => [], 'truncated' => false]),
        ]);

        $component = Livewire::actingAs($user)->test(Index::class)
            ->set('globalQuery', 'invoice');

        $this->runFullGlobalSearch($component);

        $component
            ->assertSet('globalResults.0.machine_name', 'Alpha')
            ->assertSet('globalResults.0.name', 'invoice.pdf')
            ->assertSet('globalSearching', false);
    }

    public function test_search_everywhere_reports_a_machine_whose_agent_is_unreachable_as_a_failure(): void
    {
        $user = User::factory()->create();
        $offline = Machine::factory()->create(['os' => 'linux', 'name' => 'Offline Box']);
        $offline->users()->attach($user, ['role' => 'read_only']);

        Http::fake([
            $offline->base_url.'/api/drives' => Http::response([], 500),
        ]);

        $component = Livewire::actingAs($user)->test(Index::class)
            ->set('globalQuery', 'invoice');

        $this->runFullGlobalSearch($component);

        $component
            ->assertSet('globalResults', [])
            ->assertSet('globalFailures.0.machine_name', 'Offline Box');
    }

    public function test_search_next_machine_processes_exactly_one_machine_per_call(): void
    {
        $user = User::factory()->create();
        $one = Machine::factory()->create(['os' => 'linux', 'name' => 'Alpha']);
        $two = Machine::factory()->create(['os' => 'linux', 'name' => 'Beta']);
        $one->users()->attach($user, ['role' => 'read_only']);
        $two->users()->attach($user, ['role' => 'read_only']);

        Http::fake([
            $one->base_url.'/api/drives' => Http::response(['drives' => [['label' => '/', 'path' => '/']]]),
            $one->base_url.'/api/search*' => Http::response(['entries' => [
                ['name' => 'invoice.pdf', 'path' => '/docs/invoice.pdf', 'type' => 'file', 'size' => 1, 'modified' => null, 'hidden' => false],
            ], 'truncated' => false]),
            $two->base_url.'/api/drives' => Http::response(['drives' => [['label' => '/', 'path' => '/']]]),
            $two->base_url.'/api/search*' => Http::response(['entries' => [], 'truncated' => false]),
        ]);

        $component = Livewire::actingAs($user)->test(Index::class)
            ->set('globalQuery', 'invoice')
            ->call('startGlobalSearch')
            ->assertSet('globalSearching', true)
            ->assertSet('globalQueue', [
                ['id' => $one->id, 'name' => 'Alpha'],
                ['id' => $two->id, 'name' => 'Beta'],
            ]);

        // After exactly one searchNextMachine() call, only Alpha has been
        // hit — its result is already in, Beta hasn't been touched, and
        // the queue/searching flag reflect that real, partial progress.
        $component->call('searchNextMachine')
            ->assertSet('globalQueue', [['id' => $two->id, 'name' => 'Beta']])
            ->assertSet('globalSearching', true)
            ->assertSet('globalResults.0.machine_name', 'Alpha');

        $component->call('searchNextMachine')
            ->assertSet('globalQueue', [])
            ->assertSet('globalSearching', false);
    }

    public function test_clearing_the_global_query_clears_global_results(): void
    {
        $user = User::factory()->create();

        $component = Livewire::actingAs($user)->test(Index::class)
            ->set('globalQuery', '');

        $this->runFullGlobalSearch($component);

        $component->assertSet('globalResults', [])
            ->assertSet('globalSearched', false);
    }
}
