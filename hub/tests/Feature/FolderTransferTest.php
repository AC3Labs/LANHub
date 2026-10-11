<?php

namespace Tests\Feature;

use App\Livewire\Explorer\Index;
use App\Models\Machine;
use App\Models\RelayTransfer;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Http\Client\Request;
use Illuminate\Support\Facades\Http;
use Livewire\Livewire;
use Tests\TestCase;

/**
 * Dragging whole folders, not just files: between machines (relayed file by
 * file through the hub) and on the same machine (including moves across
 * drives, which a plain rename can't do).
 *
 * Each fake agent keeps an in-memory file tree, so these tests check what
 * actually ends up where - including that a failed folder move never
 * deletes the source.
 */
class FolderTransferTest extends TestCase
{
    use RefreshDatabase;

    /** @var array<string, array<string, ?string>> host => [path => contents, or null for a folder] */
    private array $fs = [];

    /** @var array<string, string> host => error message for any path containing "unreadable" */
    private array $failListingsOn = [];

    private bool $crossDeviceMoves = false;

    private function machine(string $host, string $os, User $user): Machine
    {
        $machine = Machine::factory()->create(['host' => $host, 'os' => $os]);
        $machine->users()->attach($user, ['role' => 'full']);

        return $machine;
    }

    private function sep(string $host): string
    {
        return str_starts_with($host, 'win') ? '\\' : '/';
    }

    private function fakeAgents(): void
    {
        Http::fake(function (Request $request) {
            $host = parse_url($request->url(), PHP_URL_HOST);
            $path = parse_url($request->url(), PHP_URL_PATH);
            parse_str((string) parse_url($request->url(), PHP_URL_QUERY), $query);
            $data = $request->data();
            $sep = $this->sep($host);
            $fs = &$this->fs[$host];

            switch ($path) {
                case '/api/drives':
                    return Http::response(['drives' => []]);

                case '/api/list':
                    $dir = rtrim($query['path'], $sep);
                    if (str_contains($dir, 'unreadable')) {
                        return Http::response(['message' => "Permission denied reading {$dir}."], 403);
                    }
                    if (! array_key_exists($dir, $fs ?? []) || $fs[$dir] !== null) {
                        return Http::response(['message' => "{$dir} is not a directory."], 404);
                    }
                    $entries = [];
                    foreach ($fs as $p => $contents) {
                        if ($p !== $dir && dirname(str_replace('\\', '/', $p)) === str_replace('\\', '/', $dir)) {
                            $entries[] = ['name' => basename(str_replace('\\', '/', $p)), 'path' => $p, 'type' => $contents === null ? 'dir' : 'file', 'size' => $contents === null ? null : strlen($contents), 'hidden' => false];
                        }
                    }

                    return Http::response(['path' => $dir, 'entries' => $entries]);

                case '/api/download':
                    $file = $query['path'];
                    if (($fs[$file] ?? null) === null) {
                        return Http::response(['message' => "{$file} is not a file."], 404);
                    }

                    return Http::response($fs[$file], 200, ['Content-Type' => 'application/octet-stream']);

                case '/api/upload':
                    $dir = rtrim($query['path'], $sep);
                    $name = $request->data()[0]['filename'] ?? null;
                    $contents = $request->data()[0]['contents'] ?? '';
                    $contents = is_resource($contents) ? stream_get_contents($contents) : (string) $contents;
                    $this->mkdirP($fs, $dir, $sep);
                    $fs[$dir.$sep.$name] = $contents;

                    return Http::response(['name' => $name, 'path' => $dir.$sep.$name, 'type' => 'file']);

                case '/api/mkdir':
                    $this->mkdirP($fs, rtrim($data['path'], $sep), $sep);

                    return Http::response(['path' => $data['path'], 'type' => 'dir']);

                case '/api/delete':
                    foreach (array_keys($fs) as $p) {
                        if ($p === $data['path'] || str_starts_with($p, $data['path'].$sep)) {
                            unset($fs[$p]);
                        }
                    }

                    return Http::response(['deleted' => $data['path']]);

                case '/api/move':
                    if ($this->crossDeviceMoves) {
                        return Http::response(['message' => "rename {$data['source']} {$data['destination']}: invalid cross-device link"], 500);
                    }
                    // fall through to copy, then remove the source
                case '/api/copy':
                    if (array_key_exists($data['destination'], $fs)) {
                        return Http::response(['message' => "{$data['destination']} already exists."], 409);
                    }
                    foreach ($fs as $p => $contents) {
                        if ($p === $data['source'] || str_starts_with($p, $data['source'].$sep)) {
                            $fs[$data['destination'].substr($p, strlen($data['source']))] = $contents;
                            if ($path === '/api/move') {
                                unset($fs[$p]);
                            }
                        }
                    }

                    return Http::response(['path' => $data['destination']]);
            }

            return Http::response(['message' => "unexpected {$path}"], 500);
        });
    }

    private function mkdirP(?array &$fs, string $dir, string $sep): void
    {
        $parts = explode($sep, $dir);
        $current = '';
        foreach ($parts as $i => $part) {
            $current = $i === 0 ? $part : $current.$sep.$part;
            if ($current !== '' && ! array_key_exists($current, $fs ?? [])) {
                $fs[$current] = null;
            }
        }
    }

    private function drop(User $user, Machine $from, string $path, string $name, Machine $to, string $targetDir, bool $copy, bool $isDirectory)
    {
        $component = Livewire::actingAs($user)->test(Index::class);
        $panes = collect($component->instance()->panes);
        $fromPane = $panes->firstWhere('machine_id', $from->id)['id'];
        $toPane = $from->is($to)
            ? $fromPane
            : $panes->firstWhere('machine_id', $to->id)['id'];

        return $component->call('handleInternalDrop', $fromPane, $path, $name, $toPane, $targetDir, $copy, $isDirectory);
    }

    private function photosFolderOnLinux(): void
    {
        $this->fs['linux-box'] = [
            '/home' => null,
            '/home/me' => null,
            '/home/me/Photos' => null,
            '/home/me/Photos/a.jpg' => 'AAA',
            '/home/me/Photos/2024' => null,
            '/home/me/Photos/2024/b.jpg' => 'BBBB',
            '/home/me/Photos/empty' => null,
        ];
    }

    public function test_copying_a_folder_to_another_machine_copies_everything_inside_it(): void
    {
        $this->fakeAgents();
        $user = User::factory()->create();
        $linux = $this->machine('linux-box', 'linux', $user);
        $windows = $this->machine('win-box', 'windows', $user);
        $this->photosFolderOnLinux();
        $this->fs['win-box'] = ['C:' => null, 'C:\\Backup' => null];

        $this->drop($user, $linux, '/home/me/Photos', 'Photos', $windows, 'C:\\Backup', true, true);

        $relay = RelayTransfer::firstOrFail();
        $this->assertSame('done', $relay->status, (string) $relay->error);
        $this->assertTrue($relay->is_directory);
        $this->assertSame(7, $relay->bytes_transferred);

        $this->assertSame('AAA', $this->fs['win-box']['C:\\Backup\\Photos\\a.jpg']);
        $this->assertSame('BBBB', $this->fs['win-box']['C:\\Backup\\Photos\\2024\\b.jpg']);
        $this->assertArrayHasKey('C:\\Backup\\Photos\\empty', $this->fs['win-box'], 'empty subfolders come along too');

        // a copy leaves the original alone
        $this->assertSame('AAA', $this->fs['linux-box']['/home/me/Photos/a.jpg']);
    }

    public function test_moving_a_folder_to_another_machine_removes_the_original_afterwards(): void
    {
        $this->fakeAgents();
        $user = User::factory()->create();
        $linux = $this->machine('linux-box', 'linux', $user);
        $windows = $this->machine('win-box', 'windows', $user);
        $this->photosFolderOnLinux();
        $this->fs['win-box'] = ['C:' => null, 'C:\\Backup' => null];

        $this->drop($user, $linux, '/home/me/Photos', 'Photos', $windows, 'C:\\Backup', false, true);

        $this->assertSame('done', RelayTransfer::firstOrFail()->status);
        $this->assertSame('BBBB', $this->fs['win-box']['C:\\Backup\\Photos\\2024\\b.jpg']);
        $this->assertArrayNotHasKey('/home/me/Photos', $this->fs['linux-box']);
        $this->assertArrayNotHasKey('/home/me/Photos/2024/b.jpg', $this->fs['linux-box']);
    }

    public function test_a_failed_folder_move_never_deletes_the_original(): void
    {
        $this->fakeAgents();
        $user = User::factory()->create();
        $linux = $this->machine('linux-box', 'linux', $user);
        $windows = $this->machine('win-box', 'windows', $user);
        $this->photosFolderOnLinux();
        $this->fs['linux-box']['/home/me/Photos/unreadable'] = null;
        $this->fs['win-box'] = ['C:' => null, 'C:\\Backup' => null];

        $this->drop($user, $linux, '/home/me/Photos', 'Photos', $windows, 'C:\\Backup', false, true);

        $relay = RelayTransfer::firstOrFail();
        $this->assertSame('error', $relay->status);
        $this->assertStringContainsString('Permission denied', $relay->error);
        $this->assertSame('AAA', $this->fs['linux-box']['/home/me/Photos/a.jpg'], 'source must survive');
        $this->assertSame('BBBB', $this->fs['linux-box']['/home/me/Photos/2024/b.jpg'], 'source must survive');
    }

    public function test_a_folder_is_not_merged_into_one_that_already_exists(): void
    {
        $this->fakeAgents();
        $user = User::factory()->create();
        $linux = $this->machine('linux-box', 'linux', $user);
        $windows = $this->machine('win-box', 'windows', $user);
        $this->photosFolderOnLinux();
        $this->fs['win-box'] = ['C:' => null, 'C:\\Backup' => null, 'C:\\Backup\\photos' => null, 'C:\\Backup\\photos\\keep.txt' => 'mine'];

        $this->drop($user, $linux, '/home/me/Photos', 'Photos', $windows, 'C:\\Backup', false, true);

        $relay = RelayTransfer::firstOrFail();
        $this->assertSame('error', $relay->status);
        $this->assertStringContainsString('already exists', $relay->error);
        $this->assertSame('mine', $this->fs['win-box']['C:\\Backup\\photos\\keep.txt']);
        $this->assertArrayHasKey('/home/me/Photos/a.jpg', $this->fs['linux-box']);
    }

    public function test_single_files_still_relay_between_machines(): void
    {
        $this->fakeAgents();
        $user = User::factory()->create();
        $linux = $this->machine('linux-box', 'linux', $user);
        $windows = $this->machine('win-box', 'windows', $user);
        $this->photosFolderOnLinux();
        $this->fs['win-box'] = ['C:' => null, 'C:\\Backup' => null];

        $this->drop($user, $linux, '/home/me/Photos/a.jpg', 'a.jpg', $windows, 'C:\\Backup', false, false);

        $this->assertSame('done', RelayTransfer::firstOrFail()->status);
        $this->assertSame('AAA', $this->fs['win-box']['C:\\Backup\\a.jpg']);
        $this->assertArrayNotHasKey('/home/me/Photos/a.jpg', $this->fs['linux-box']);
    }

    public function test_moving_a_folder_across_drives_on_the_same_machine_falls_back_to_copy_then_delete(): void
    {
        $this->fakeAgents();
        $this->crossDeviceMoves = true;
        $user = User::factory()->create();
        $linux = $this->machine('linux-box', 'linux', $user);
        $this->photosFolderOnLinux();
        $this->fs['linux-box']['/mnt'] = null;
        $this->fs['linux-box']['/mnt/usb'] = null;

        $this->drop($user, $linux, '/home/me/Photos', 'Photos', $linux, '/mnt/usb', false, true)
            ->assertSet('panes.0.error', null);

        $this->assertSame('BBBB', $this->fs['linux-box']['/mnt/usb/Photos/2024/b.jpg']);
        $this->assertArrayNotHasKey('/home/me/Photos', $this->fs['linux-box']);
    }

    public function test_a_folder_cannot_be_dropped_into_itself(): void
    {
        $this->fakeAgents();
        $user = User::factory()->create();
        $linux = $this->machine('linux-box', 'linux', $user);
        $this->photosFolderOnLinux();
        $before = $this->fs['linux-box'];

        $this->drop($user, $linux, '/home/me/Photos', 'Photos', $linux, '/home/me/Photos/2024', false, true)
            ->assertSet('panes.0.error', "Can't put a folder inside itself.");

        $this->assertSame($before, $this->fs['linux-box']);
    }

    public function test_a_similarly_named_sibling_folder_is_not_mistaken_for_itself(): void
    {
        $this->fakeAgents();
        $user = User::factory()->create();
        $linux = $this->machine('linux-box', 'linux', $user);
        $this->photosFolderOnLinux();
        $this->fs['linux-box']['/home/me/Photos-old'] = null;

        $this->drop($user, $linux, '/home/me/Photos', 'Photos', $linux, '/home/me/Photos-old', false, true)
            ->assertSet('panes.0.error', null);

        $this->assertSame('AAA', $this->fs['linux-box']['/home/me/Photos-old/Photos/a.jpg']);
    }
}
