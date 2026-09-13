<?php

namespace App\Livewire\Explorer;

use App\Exceptions\AgentException;
use App\Jobs\RelayTransferJob;
use App\Models\Machine;
use App\Models\RelayTransfer;
use Illuminate\Http\Client\ConnectionException;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Str;
use Livewire\Attributes\Layout;
use Livewire\Attributes\On;
use Livewire\Component;
use Livewire\WithFileUploads;

#[Layout('layouts.app')]
class Index extends Component
{
    use WithFileUploads;

    /** @var array<int, array{id: string, machine_id: int, path: ?string, entries: array, sort: string, direction: string, showHidden: bool, view: string, error: ?string}> */
    public array $panes = [];

    public array $uploadFiles = [];

    public ?string $uploadTargetPaneId = null;

    public ?string $renamingPaneId = null;

    public ?string $renamingPath = null;

    public string $renameValue = '';

    /** @var array<string, array> search results keyed by pane id — absent means "not searching" */
    public array $searchResults = [];

    public array $searchQueries = [];

    public string $globalQuery = '';

    /** @var array<int, array{machine_id: int, machine_name: string, machine_color: string, path: string, name: string, type: string, size: ?int, modified: ?string}> */
    public array $globalResults = [];

    public ?string $globalError = null;

    public bool $globalSearched = false;

    /** @var array<int, array{machine_name: string, machine_color: string, reason: string}> */
    public array $globalFailures = [];

    public function mount(): void
    {
        $machines = Machine::accessibleTo(Auth::user())->orderBy('sort_order')->orderBy('name')->get();

        $requestedId = (int) request()->query('machine');

        if ($requestedId && $machines->contains('id', $requestedId)) {
            $machines = $machines->sortByDesc(fn ($m) => $m->id === $requestedId);
        }

        foreach ($machines as $machine) {
            $this->addPane($machine->id);
        }
    }

    public function addPane(int $machineId): void
    {
        $machine = Machine::findOrFail($machineId);
        $this->requireAccess($machine);

        $this->panes[] = [
            'id' => (string) Str::uuid(),
            'machine_id' => $machine->id,
            'path' => null,
            'entries' => [],
            'sort' => 'name',
            'direction' => 'asc',
            'showHidden' => false,
            'view' => 'list',
            'error' => null,
        ];

        $this->loadPane(array_key_last($this->panes));
    }

    public function closePane(string $paneId): void
    {
        $this->panes = array_values(array_filter($this->panes, fn ($p) => $p['id'] !== $paneId));
    }

    public function navigate(string $paneId, ?string $path): void
    {
        $index = $this->paneIndex($paneId);
        $this->panes[$index]['path'] = $path;
        $this->loadPane($index);
    }

    public function navigateUp(string $paneId): void
    {
        $index = $this->paneIndex($paneId);
        $current = $this->panes[$index]['path'];

        if (! $current) {
            return;
        }

        $machine = Machine::findOrFail($this->panes[$index]['machine_id']);
        $separator = $machine->os === 'windows' ? '\\' : '/';
        $trimmed = rtrim($current, $separator);
        $parent = strrpos($trimmed, $separator);

        $this->panes[$index]['path'] = $parent !== false && $parent > 0
            ? substr($trimmed, 0, $parent + 1)
            : null;

        $this->loadPane($index);
    }

    public function refreshPane(string $paneId): void
    {
        $this->loadPane($this->paneIndex($paneId));
    }

    public function toggleHidden(string $paneId): void
    {
        $index = $this->paneIndex($paneId);
        $this->panes[$index]['showHidden'] = ! $this->panes[$index]['showHidden'];
    }

    public function toggleView(string $paneId): void
    {
        $index = $this->paneIndex($paneId);
        $this->panes[$index]['view'] = $this->panes[$index]['view'] === 'list' ? 'grid' : 'list';
    }

    public function sortBy(string $paneId, string $column): void
    {
        $index = $this->paneIndex($paneId);

        if ($this->panes[$index]['sort'] === $column) {
            $this->panes[$index]['direction'] = $this->panes[$index]['direction'] === 'asc' ? 'desc' : 'asc';
        } else {
            $this->panes[$index]['sort'] = $column;
            $this->panes[$index]['direction'] = 'asc';
        }
    }

    public function createFolder(string $paneId): void
    {
        $index = $this->paneIndex($paneId);
        $pane = $this->panes[$index];
        $machine = Machine::findOrFail($pane['machine_id']);
        $this->requireAccess($machine, 'full');
        $separator = $machine->os === 'windows' ? '\\' : '/';
        $base = $pane['path'] ?? ($machine->os === 'windows' ? 'C:\\' : '/');

        $name = 'New Folder';
        $attempt = 0;
        $target = rtrim($base, $separator).$separator.$name;

        try {
            $machine->agent()->mkdir($target);
            $this->loadPane($index);
            $this->startRename($paneId, $target);
        } catch (AgentException $e) {
            $this->panes[$index]['error'] = $e->getMessage();
        }
    }

    public function startRename(string $paneId, string $path): void
    {
        $this->renamingPaneId = $paneId;
        $this->renamingPath = $path;
        $this->renameValue = basename(str_replace('\\', '/', $path));
    }

    public function cancelRename(): void
    {
        $this->renamingPaneId = null;
        $this->renamingPath = null;
        $this->renameValue = '';
    }

    public function confirmRename(): void
    {
        if (! $this->renamingPaneId || ! $this->renamingPath || $this->renameValue === '') {
            $this->cancelRename();

            return;
        }

        $index = $this->paneIndex($this->renamingPaneId);
        $machine = Machine::findOrFail($this->panes[$index]['machine_id']);
        $this->requireAccess($machine, 'full');

        try {
            $machine->agent()->rename($this->renamingPath, $this->renameValue);
        } catch (AgentException $e) {
            $this->panes[$index]['error'] = $e->getMessage();
        }

        $this->loadPane($index);
        $this->cancelRename();
    }

    public function deleteEntry(string $paneId, string $path): void
    {
        $index = $this->paneIndex($paneId);
        $machine = Machine::findOrFail($this->panes[$index]['machine_id']);
        $this->requireAccess($machine, 'full');

        try {
            $machine->agent()->delete($path);
        } catch (AgentException $e) {
            $this->panes[$index]['error'] = $e->getMessage();
        }

        $this->loadPane($index);
    }

    public function downloadEntry(string $paneId, string $path)
    {
        $index = $this->paneIndex($paneId);
        $machine = Machine::findOrFail($this->panes[$index]['machine_id']);
        $this->requireAccess($machine);

        $response = $machine->agent()->download($path);
        $filename = basename(str_replace('\\', '/', $path));

        return response()->streamDownload(
            fn () => print ($response->body()),
            $filename
        );
    }

    /**
     * Drop a row (drag source) onto a directory (drop target). Default drag copies;
     * holding Ctrl moves (deletes the source after a successful transfer). Cross-machine
     * transfers relay the file through the hub either way.
     */
    public function handleInternalDrop(string $sourcePaneId, string $sourcePath, string $sourceName, string $targetPaneId, ?string $targetPath, bool $copy): void
    {
        $sourceIndex = $this->paneIndex($sourcePaneId);
        $targetIndex = $this->paneIndex($targetPaneId);

        $sourceMachine = Machine::findOrFail($this->panes[$sourceIndex]['machine_id']);
        $targetMachine = Machine::findOrFail($this->panes[$targetIndex]['machine_id']);

        $this->requireAccess($sourceMachine, $copy ? 'read_only' : 'full');
        $this->requireAccess($targetMachine, 'full');

        $targetSeparator = $targetMachine->os === 'windows' ? '\\' : '/';
        $targetDir = $targetPath ?? ($this->panes[$targetIndex]['path'] ?? ($targetMachine->os === 'windows' ? 'C:\\' : '/'));
        $destination = rtrim($targetDir, $targetSeparator).$targetSeparator.$sourceName;

        if ($sourceMachine->id === $targetMachine->id && $destination === $sourcePath) {
            return;
        }

        try {
            if ($sourceMachine->id === $targetMachine->id) {
                $copy
                    ? $sourceMachine->agent()->copy($sourcePath, $destination)
                    : $sourceMachine->agent()->move($sourcePath, $destination);
            } else {
                $this->relayTransfer($sourceMachine, $sourcePath, $targetMachine, $destination, $copy);
            }
        } catch (AgentException $e) {
            $this->panes[$targetIndex]['error'] = $e->getMessage();
        }

        $this->loadPane($sourceIndex);
        $this->loadPane($this->paneIndex($targetPaneId));
    }

    /**
     * Cross-machine transfers relay through the hub (agents never talk to
     * each other directly), which can take a while for anything sizeable —
     * this runs on the queue via RelayTransferJob instead of blocking the
     * request, and the Transfers panel polls RelayTransfer for progress.
     */
    private function relayTransfer(Machine $source, string $sourcePath, Machine $target, string $destination, bool $copy): void
    {
        $relay = RelayTransfer::create([
            'source_machine_id' => $source->id,
            'destination_machine_id' => $target->id,
            'source_path' => $sourcePath,
            'destination_path' => $destination,
            'kind' => $copy ? 'copy' : 'move',
            'status' => 'queued',
            'user_id' => Auth::id(),
        ]);

        RelayTransferJob::dispatch($relay->id);
    }

    #[On('upload-target-set')]
    public function setUploadTarget(string $paneId): void
    {
        $this->uploadTargetPaneId = $paneId;
    }

    public function updatedUploadFiles(): void
    {
        if (! $this->uploadTargetPaneId) {
            return;
        }

        $index = $this->paneIndex($this->uploadTargetPaneId);
        $machine = Machine::findOrFail($this->panes[$index]['machine_id']);
        $this->requireAccess($machine, 'full');
        $destination = $this->panes[$index]['path'] ?? ($machine->os === 'windows' ? 'C:\\' : '/');

        foreach ($this->uploadFiles as $file) {
            try {
                $machine->agent()->upload($destination, $file);
            } catch (AgentException $e) {
                $this->panes[$index]['error'] = $e->getMessage();
            }
        }

        $this->uploadFiles = [];
        $this->loadPane($index);
    }

    /**
     * A bounded recursive search under the pane's current directory (or
     * machine root) via the agent's own GET /api/search — no index exists
     * anywhere in this system, so this can take a moment on a large tree,
     * which is why the agent enforces its own depth/result/time caps.
     */
    public function search(string $paneId): void
    {
        $query = trim($this->searchQueries[$paneId] ?? '');
        $index = $this->paneIndex($paneId);

        if ($query === '') {
            $this->clearSearch($paneId);

            return;
        }

        $machine = Machine::findOrFail($this->panes[$index]['machine_id']);
        $this->requireAccess($machine);
        $path = $this->panes[$index]['path'] ?? ($machine->os === 'windows' ? 'C:\\' : '/');

        try {
            $result = $machine->agent()->search($path, $query);
            $this->searchResults[$paneId] = $result['entries'];
            $this->panes[$index]['error'] = $result['truncated']
                ? 'Showing partial results — the search hit its limit.'
                : null;
        } catch (AgentException $e) {
            $this->searchResults[$paneId] = [];
            $this->panes[$index]['error'] = $e->getMessage();
        }
    }

    public function clearSearch(string $paneId): void
    {
        unset($this->searchResults[$paneId], $this->searchQueries[$paneId]);
    }

    /**
     * Searches every machine the user has access to, one drive/root at a
     * time via the same agent search() used per-pane above — there's still
     * no index, so this is just that same bounded walk run once per drive
     * per machine. A machine that's offline or errors is skipped rather
     * than failing the whole search, since the point is "where is this
     * file", not "prove every machine is reachable."
     */
    public function searchEverywhere(): void
    {
        $query = trim($this->globalQuery);
        $this->globalSearched = true;

        if ($query === '') {
            $this->clearGlobalSearch();

            return;
        }

        $this->globalResults = [];
        $this->globalFailures = [];
        $truncated = false;

        foreach (Machine::accessibleTo(Auth::user())->orderBy('name')->get() as $machine) {
            try {
                $drives = $machine->agent()->drives();
            } catch (AgentException|ConnectionException $e) {
                $this->globalFailures[] = [
                    'machine_name' => $machine->name,
                    'machine_color' => $machine->color,
                    'reason' => $e->getMessage(),
                ];

                continue;
            }

            $machineFailed = false;

            foreach ($drives as $drive) {
                try {
                    $result = $machine->agent()->search($drive['path'], $query);
                } catch (AgentException|ConnectionException $e) {
                    if (! $machineFailed) {
                        $this->globalFailures[] = [
                            'machine_name' => $machine->name,
                            'machine_color' => $machine->color,
                            'reason' => $e->getMessage(),
                        ];
                        $machineFailed = true;
                    }

                    continue;
                }

                $truncated = $truncated || $result['truncated'];

                foreach ($result['entries'] as $entry) {
                    $this->globalResults[] = [
                        'machine_id' => $machine->id,
                        'machine_name' => $machine->name,
                        'machine_color' => $machine->color,
                        'path' => $entry['path'],
                        'name' => $entry['name'],
                        'type' => $entry['type'],
                        'size' => $entry['size'] ?? null,
                        'modified' => $entry['modified'] ?? null,
                    ];
                }
            }
        }

        $this->globalError = $truncated
            ? 'Showing partial results — the search hit its limit on at least one machine.'
            : null;
    }

    public function clearGlobalSearch(): void
    {
        $this->globalQuery = '';
        $this->globalResults = [];
        $this->globalError = null;
        $this->globalSearched = false;
        $this->globalFailures = [];
    }

    /**
     * Opens (or reuses) a pane for the result's machine and navigates it to
     * the containing folder — mirrors navigateUp()'s parent-path logic
     * rather than duplicating a separate dirname() helper.
     */
    public function openGlobalResult(int $machineId, string $path): void
    {
        $machine = Machine::findOrFail($machineId);
        $this->requireAccess($machine);

        $existing = collect($this->panes)->search(fn ($p) => $p['machine_id'] === $machineId);

        if ($existing === false) {
            $this->addPane($machineId);
            $paneId = $this->panes[array_key_last($this->panes)]['id'];
        } else {
            $paneId = $this->panes[$existing]['id'];
        }

        $separator = $machine->os === 'windows' ? '\\' : '/';
        $trimmed = rtrim($path, $separator);
        $parentPos = strrpos($trimmed, $separator);

        $parent = $parentPos !== false && $parentPos > 0
            ? substr($trimmed, 0, $parentPos + 1)
            : null;

        $this->navigate($paneId, $parent);
    }

    private function loadPane(int $index): void
    {
        $pane = $this->panes[$index];
        $machine = Machine::findOrFail($pane['machine_id']);

        try {
            if ($pane['path'] === null) {
                $drives = $machine->agent()->drives();
                $this->panes[$index]['entries'] = array_map(fn ($d) => [
                    'name' => $d['label'],
                    'path' => $d['path'],
                    'type' => 'drive',
                    'size' => null,
                    'modified' => null,
                    'hidden' => false,
                ], $drives);
            } else {
                $listing = $machine->agent()->listDirectory($pane['path']);
                $this->panes[$index]['entries'] = $listing['entries'];
            }

            $this->panes[$index]['error'] = null;
            $machine->update(['status' => 'online', 'last_seen_at' => now()]);
        } catch (AgentException|ConnectionException $e) {
            $this->panes[$index]['entries'] = [];
            $this->panes[$index]['error'] = $e->getMessage();
            $machine->update(['status' => 'offline']);
        }
    }

    public function breadcrumbs(array $pane): array
    {
        if ($pane['path'] === null) {
            return [];
        }

        $machine = Machine::find($pane['machine_id']);
        $separator = $machine && $machine->os === 'windows' ? '\\' : '/';
        $normalized = rtrim($pane['path'], $separator);
        $segments = array_values(array_filter(explode($separator, $normalized)));

        $crumbs = [];
        $running = $separator === '\\' ? '' : '';

        foreach ($segments as $i => $segment) {
            $running = $i === 0 && $separator === '\\'
                ? $segment.$separator
                : $running.$segment.$separator;

            $crumbs[] = ['label' => $segment, 'path' => $running];
        }

        return $crumbs;
    }

    public function visibleEntries(array $pane): array
    {
        $entries = $pane['entries'];

        if (! $pane['showHidden']) {
            $entries = array_filter($entries, fn ($e) => ! ($e['hidden'] ?? false));
        }

        $column = $pane['sort'];
        $direction = $pane['direction'] === 'asc' ? 1 : -1;

        usort($entries, function ($a, $b) use ($column, $direction) {
            if (($a['type'] === 'dir' || $a['type'] === 'drive') !== ($b['type'] === 'dir' || $b['type'] === 'drive')) {
                return ($a['type'] === 'file') <=> ($b['type'] === 'file');
            }

            return match ($column) {
                'size' => (($a['size'] ?? -1) <=> ($b['size'] ?? -1)) * $direction,
                'modified' => (($a['modified'] ?? '') <=> ($b['modified'] ?? '')) * $direction,
                default => (strcasecmp($a['name'], $b['name'])) * $direction,
            };
        });

        return array_values($entries);
    }

    private function paneIndex(string $paneId): int
    {
        foreach ($this->panes as $i => $pane) {
            if ($pane['id'] === $paneId) {
                return $i;
            }
        }

        throw new \RuntimeException("Pane {$paneId} not found.");
    }

    private function requireAccess(Machine $machine, string $atLeast = 'read_only'): void
    {
        abort_unless(Auth::user()->canAccess($machine, $atLeast), 403);
    }

    public function canWrite(int $machineId): bool
    {
        return Auth::user()->canAccess(Machine::findOrFail($machineId), 'full');
    }

    public function render()
    {
        return view('livewire.explorer.index', [
            'machines' => Machine::accessibleTo(Auth::user())->orderBy('sort_order')->orderBy('name')->get(),
            'searchResults' => $this->searchResults,
        ]);
    }
}
