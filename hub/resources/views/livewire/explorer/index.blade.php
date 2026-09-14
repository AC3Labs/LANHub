<div
    x-data="{
        dragging: null,
        dropTarget: null,
        startDrag(event, paneId, path, name, type) {
            if (type === 'drive') return;
            this.dragging = { paneId, path, name };
            event.dataTransfer.effectAllowed = 'copyMove';
            event.dataTransfer.setData('text/plain', JSON.stringify(this.dragging));
            this.setDragImage(event, name);
        },
        setDragImage(event, name) {
            // A small custom drag ghost (filename on a rounded pill) reads
            // much better than the browser's default full-row screenshot,
            // especially in the dense list view.
            const canvas = document.createElement('canvas');
            const ctx = canvas.getContext('2d');
            const label = name.length > 28 ? name.slice(0, 25) + '…' : name;
            ctx.font = '600 13px system-ui, sans-serif';
            const textWidth = ctx.measureText(label).width;
            canvas.width = textWidth + 28;
            canvas.height = 32;
            ctx.font = '600 13px system-ui, sans-serif';
            ctx.fillStyle = '#ac8544';
            ctx.beginPath();
            ctx.roundRect(0, 0, canvas.width, canvas.height, 16);
            ctx.fill();
            ctx.fillStyle = '#ffffff';
            ctx.textBaseline = 'middle';
            ctx.fillText(label, 14, canvas.height / 2 + 1);
            event.dataTransfer.setDragImage(canvas, 12, 16);
        },
        endDrag() {
            this.dragging = null;
            this.dropTarget = null;
        },
        async handleDrop(event, targetPaneId, targetPath) {
            this.dropTarget = null;

            if (event.dataTransfer.files && event.dataTransfer.files.length > 0) {
                await $wire.setUploadTarget(targetPaneId);
                $wire.uploadMultiple('uploadFiles', event.dataTransfer.files, () => {}, () => {});
                return;
            }

            const raw = event.dataTransfer.getData('text/plain');
            if (!raw) return;

            const data = JSON.parse(raw);
            if (data.paneId === targetPaneId && data.path === targetPath) return;

            $wire.handleInternalDrop(data.paneId, data.path, data.name, targetPaneId, targetPath, !event.ctrlKey);
        },
        previewUrl: null,
        previewName: null,
        openPreview(machineId, path, name) {
            this.previewUrl = '{{ url('machines') }}/' + machineId + '/preview?path=' + encodeURIComponent(path);
            this.previewName = name;
        },
        closePreview() {
            this.previewUrl = null;
            this.previewName = null;
        },
        currentlySearching: null,
        async runGlobalSearch() {
            await $wire.startGlobalSearch();
            while ($wire.globalQueue.length > 0) {
                this.currentlySearching = $wire.globalQueue[0].name;
                await $wire.searchNextMachine();
            }
            this.currentlySearching = null;
        },
    }"
>
    <div x-show="previewUrl" x-cloak class="fixed inset-0 z-50 bg-black/50 flex items-center justify-center p-6" x-on:click.self="closePreview()">
        <div class="bg-paper rounded-xl shadow-panel w-full max-w-3xl h-[80vh] flex flex-col overflow-hidden">
            <div class="flex items-center justify-between px-4 py-2.5 border-b border-stone-100">
                <span class="text-sm font-medium text-stone-700 truncate" x-text="previewName"></span>
                <button x-on:click="closePreview()" class="p-1 rounded hover:bg-stone-100 text-stone-400 hover:text-stone-600">
                    <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/></svg>
                </button>
            </div>
            <iframe :src="previewUrl" class="flex-1 w-full bg-white" sandbox=""></iframe>
        </div>
    </div>

    <div class="bg-paper border-b border-stone-200">
        <div class="max-w-[1900px] mx-auto px-4 sm:px-6 lg:px-8 py-6 flex items-center justify-between flex-wrap gap-3">
            <div>
                <h2 class="font-semibold text-xl text-stone-800">Explorer</h2>
                <p class="text-sm text-stone-500 mt-1">Browse, move, and manage files across every machine you've registered.</p>
            </div>

            @if ($machines->isNotEmpty())
                <form x-on:submit.prevent="runGlobalSearch()" class="flex items-center gap-1.5 px-3 py-2 bg-stone-50 border border-stone-200 rounded-lg w-full sm:w-[26rem]">
                    <svg x-show="!currentlySearching" class="w-3.5 h-3.5 text-stone-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"/></svg>
                    <svg x-show="currentlySearching" x-cloak class="w-3.5 h-3.5 text-tan-500 shrink-0 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/></svg>
                    <input
                        type="text"
                        wire:model="globalQuery"
                        x-bind:disabled="currentlySearching !== null"
                        placeholder="Search for a file on any machine connected to LANHub"
                        class="flex-1 text-sm border-0 focus:ring-0 p-0 bg-transparent placeholder:text-stone-400 disabled:text-stone-400"
                    >
                    <span x-show="currentlySearching" x-cloak x-text="'Searching ' + currentlySearching + '…'" class="text-xs text-tan-600 shrink-0 truncate max-w-[9rem]"></span>
                    @if ($globalSearched)
                        <button type="button" wire:click="clearGlobalSearch" x-show="!currentlySearching" class="text-xs text-stone-400 hover:text-stone-600 shrink-0">Clear</button>
                    @endif
                </form>
            @endif

            @if ($machines->isNotEmpty())
                <div x-data="{ open: false }" class="relative">
                    <button @click="open = !open" class="inline-flex items-center gap-2 px-4 py-2 bg-tan-500 hover:bg-tan-600 text-white text-sm font-medium rounded-lg shadow-soft transition">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4"/></svg>
                        Open Machine
                    </button>
                    <div x-show="open" @click.outside="open = false" x-transition style="display:none" class="absolute right-0 mt-2 w-56 bg-paper rounded-lg shadow-panel ring-1 ring-stone-200 py-1 z-20">
                        @foreach ($machines as $machine)
                            <button wire:click="addPane({{ $machine->id }})" @click="open = false" class="w-full flex items-center gap-2 px-4 py-2 text-sm text-stone-700 hover:bg-stone-50">
                                <span class="w-2.5 h-2.5 rounded-full shrink-0" style="background-color: {{ $machine->color }}"></span>
                                {{ $machine->name }}
                            </button>
                        @endforeach
                    </div>
                </div>
            @endif
        </div>

        @if ($globalSearched)
            <div class="max-w-[1900px] mx-auto px-4 sm:px-6 lg:px-8 pb-6">
                <div class="bg-paper rounded-xl shadow-soft border border-stone-200 overflow-hidden">
                    @if ($globalError)
                        <p class="px-4 pt-3 text-xs text-stone-400">{{ $globalError }}</p>
                    @endif

                    @if (empty($globalResults))
                        <div class="p-8 text-center text-sm text-stone-400">No matches found on any machine.</div>
                    @else
                        <table class="w-full text-sm">
                            <tbody>
                                @foreach ($globalResults as $result)
                                    <tr wire:key="global-search-{{ $result['machine_id'] }}-{{ $result['path'] }}" class="border-b border-stone-50 hover:bg-stone-50/80">
                                        <td class="px-4 py-2.5">
                                            <button wire:click="openGlobalResult({{ $result['machine_id'] }}, '{{ addslashes($result['path']) }}')" class="flex items-center gap-3 min-w-0 text-left w-full">
                                                <x-explorer.file-icon :type="$result['type']" :name="$result['name']" />
                                                <span class="min-w-0 flex-1">
                                                    <span class="block truncate text-stone-700">{{ $result['name'] }}</span>
                                                    <span class="block truncate text-[10px] text-stone-400">{{ $result['path'] }}</span>
                                                </span>
                                                <span class="flex items-center gap-1.5 shrink-0 text-xs text-stone-500">
                                                    <span class="w-2 h-2 rounded-full" style="background-color: {{ $result['machine_color'] }}"></span>
                                                    {{ $result['machine_name'] }}
                                                </span>
                                            </button>
                                        </td>
                                    </tr>
                                @endforeach
                            </tbody>
                        </table>
                    @endif

                    @if (! empty($globalFailures))
                        <div class="px-4 py-3 border-t border-stone-100 bg-stone-50/60">
                            <p class="text-xs font-medium text-stone-500 mb-1.5">Couldn't be searched:</p>
                            <ul class="space-y-1">
                                @foreach ($globalFailures as $failure)
                                    <li class="flex items-center gap-1.5 text-xs text-stone-500">
                                        <span class="w-2 h-2 rounded-full shrink-0" style="background-color: {{ $failure['machine_color'] }}"></span>
                                        <span class="font-medium">{{ $failure['machine_name'] }}</span> — {{ $failure['reason'] }}
                                    </li>
                                @endforeach
                            </ul>
                        </div>
                    @endif
                </div>
            </div>
        @endif
    </div>

    <div class="max-w-[1900px] mx-auto py-6 px-4 sm:px-6 lg:px-8">
        @if (empty($panes))
            <div class="rounded-xl border border-dashed border-stone-300 bg-paper p-12 text-center">
                <p class="text-stone-500">
                    @if ($machines->isEmpty())
                        No machines registered yet. <a href="{{ route('dashboard') }}" wire:navigate class="text-tan-600 underline">Add one</a> to start browsing.
                    @else
                        Open a machine to start browsing its files.
                    @endif
                </p>
            </div>
        @else
            <div class="flex gap-4 overflow-x-auto pb-4" style="scrollbar-gutter: stable;">
                @foreach ($panes as $pane)
                    @php
                        $machine = $machines->firstWhere('id', $pane['machine_id']);
                        $entries = $this->visibleEntries($pane);
                        $crumbs = $this->breadcrumbs($pane);
                        $canWrite = $this->canWrite($pane['machine_id']);
                    @endphp
                    <div wire:key="pane-{{ $pane['id'] }}" class="flex flex-col w-[420px] shrink-0 bg-paper rounded-xl shadow-soft border border-stone-200 overflow-hidden">
                        <!-- Pane header -->
                        <div class="flex items-center justify-between px-4 py-3 border-b border-stone-100" style="background: linear-gradient(to right, {{ $machine?->color }}14, transparent)">
                            <div class="flex items-center gap-2 min-w-0">
                                <span class="w-2.5 h-2.5 rounded-full shrink-0" style="background-color: {{ $machine?->color }}"></span>
                                <span class="font-medium text-stone-800 truncate">{{ $machine?->name ?? 'Unknown machine' }}</span>
                                <span class="text-xs text-stone-400 capitalize shrink-0">{{ $machine?->os }}</span>
                            </div>
                            <div class="flex items-center gap-1 shrink-0">
                                <button wire:click="toggleView('{{ $pane['id'] }}')" title="Toggle view" class="p-1.5 rounded hover:bg-stone-100 text-stone-500">
                                    @if ($pane['view'] === 'list')
                                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16"/></svg>
                                    @else
                                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h4v4H4V6zm6 0h4v4h-4V6zm6 0h4v4h-4V6zM4 14h4v4H4v-4zm6 0h4v4h-4v-4zm6 0h4v4h-4v-4z"/></svg>
                                    @endif
                                </button>
                                <button wire:click="toggleHidden('{{ $pane['id'] }}')" title="Toggle hidden files" @class(['p-1.5 rounded hover:bg-stone-100', 'text-tan-600' => $pane['showHidden'], 'text-stone-500' => ! $pane['showHidden']])>
                                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/></svg>
                                </button>
                                <button wire:click="refreshPane('{{ $pane['id'] }}')" title="Refresh" class="p-1.5 rounded hover:bg-stone-100 text-stone-500">
                                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/></svg>
                                </button>
                                <button wire:click="closePane('{{ $pane['id'] }}')" title="Close" class="p-1.5 rounded hover:bg-red-50 text-stone-400 hover:text-red-500">
                                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/></svg>
                                </button>
                            </div>
                        </div>

                        <!-- Breadcrumb -->
                        <div class="flex items-center gap-1 px-3 py-2 border-b border-stone-100 text-xs overflow-x-auto whitespace-nowrap">
                            <button wire:click="navigateUp('{{ $pane['id'] }}')" class="p-1 rounded hover:bg-stone-100 text-stone-500 shrink-0" title="Up">
                                <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 17l-5-5m0 0l5-5m-5 5h12"/></svg>
                            </button>
                            <button wire:click="navigate('{{ $pane['id'] }}', null)" class="px-2 py-0.5 rounded hover:bg-stone-100 text-stone-500 shrink-0">
                                <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6"/></svg>
                            </button>
                            @foreach ($crumbs as $crumb)
                                <span class="text-stone-300">/</span>
                                <button wire:click="navigate('{{ $pane['id'] }}', '{{ $crumb['path'] }}')" class="px-2 py-0.5 rounded hover:bg-stone-100 text-stone-600 shrink-0">
                                    {{ $crumb['label'] }}
                                </button>
                            @endforeach
                        </div>

                        <!-- Toolbar -->
                        <div class="flex items-center gap-2 px-3 py-2 border-b border-stone-100">
                            @if ($canWrite)
                                <button wire:click="createFolder('{{ $pane['id'] }}')" @disabled($pane['path'] === null) class="text-xs font-medium px-2.5 py-1.5 rounded-md border border-stone-200 text-stone-600 hover:bg-stone-50 disabled:opacity-40 disabled:cursor-not-allowed transition">
                                    + Folder
                                </button>
                                <label @class(['text-xs font-medium px-2.5 py-1.5 rounded-md border border-stone-200 text-stone-600 hover:bg-stone-50 transition cursor-pointer', 'opacity-40 pointer-events-none' => $pane['path'] === null]) x-on:click="$wire.setUploadTarget('{{ $pane['id'] }}')">
                                    Upload
                                    <input type="file" multiple class="hidden" wire:model="uploadFiles">
                                </label>
                            @else
                                <span class="text-xs text-stone-400 italic">Read-only</span>
                            @endif
                            @if ($pane['error'])
                                <span class="text-xs text-red-500 truncate" title="{{ $pane['error'] }}">{{ $pane['error'] }}</span>
                            @endif
                        </div>

                        <!-- Search -->
                        <form wire:submit="search('{{ $pane['id'] }}')" class="flex items-center gap-1.5 px-3 py-2 border-b border-stone-100">
                            <svg class="w-3.5 h-3.5 text-stone-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"/></svg>
                            <input
                                type="text"
                                wire:model="searchQueries.{{ $pane['id'] }}"
                                placeholder="Search this folder…"
                                class="flex-1 text-xs border-0 focus:ring-0 p-0 bg-transparent placeholder:text-stone-400"
                            >
                            @if (isset($searchResults[$pane['id']]))
                                <button type="button" wire:click="clearSearch('{{ $pane['id'] }}')" class="text-xs text-stone-400 hover:text-stone-600">Clear</button>
                            @endif
                        </form>

                        <!-- Listing -->
                        <div
                            class="flex-1 overflow-y-auto min-h-[320px] max-h-[520px]"
                            x-on:dragover.prevent="dropTarget = '{{ $pane['id'] }}::__self'"
                            x-on:dragleave="dropTarget === '{{ $pane['id'] }}::__self' && (dropTarget = null)"
                            x-on:drop.prevent="handleDrop($event, '{{ $pane['id'] }}', {{ $pane['path'] === null ? 'null' : "'".addslashes($pane['path'])."'" }})"
                            :style="dropTarget === '{{ $pane['id'] }}::__self' ? 'background-color: {{ $machine?->color }}14' : ''"
                        >
                            @if (isset($searchResults[$pane['id']]))
                                @if (empty($searchResults[$pane['id']]))
                                    <div class="p-8 text-center text-sm text-stone-400">No matches found.</div>
                                @else
                                    <table class="w-full text-sm">
                                        <tbody>
                                            @foreach ($searchResults[$pane['id']] as $result)
                                                @php
                                                    $parent = $result['type'] === 'dir' ? $result['path'] : dirname(str_replace('\\', '/', $result['path']));
                                                @endphp
                                                <tr wire:key="search-{{ $pane['id'] }}-{{ $result['path'] }}" class="border-b border-stone-50 hover:bg-stone-50/80">
                                                    <td class="px-3 py-2">
                                                        <button wire:click="clearSearch('{{ $pane['id'] }}'); navigate('{{ $pane['id'] }}', '{{ addslashes($parent) }}')" class="flex items-center gap-2 min-w-0 text-left w-full">
                                                            <x-explorer.file-icon :type="$result['type']" :name="$result['name']" />
                                                            <span class="min-w-0">
                                                                <span class="block truncate text-stone-700">{{ $result['name'] }}</span>
                                                                <span class="block truncate text-[10px] text-stone-400">{{ $result['path'] }}</span>
                                                            </span>
                                                        </button>
                                                    </td>
                                                </tr>
                                            @endforeach
                                        </tbody>
                                    </table>
                                @endif
                            @elseif (empty($entries))
                                <div class="p-8 text-center text-sm text-stone-400">
                                    {{ $pane['error'] ? 'Could not load this folder.' : 'This folder is empty.' }}
                                </div>
                            @elseif ($pane['view'] === 'list')
                                <table class="w-full text-sm">
                                    <thead class="sticky top-0 bg-stone-50 text-stone-400 text-xs uppercase tracking-wide">
                                        <tr>
                                            <th class="text-left font-medium px-3 py-2 cursor-pointer" wire:click="sortBy('{{ $pane['id'] }}', 'name')">Name</th>
                                            <th class="text-right font-medium px-3 py-2 cursor-pointer w-20" wire:click="sortBy('{{ $pane['id'] }}', 'size')">Size</th>
                                            <th class="text-right font-medium px-3 py-2 cursor-pointer w-16"></th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        @foreach ($entries as $entry)
                                            <tr
                                                wire:key="entry-{{ $pane['id'] }}-{{ $entry['path'] }}"
                                                x-data="{ menu: false, menuX: 0, menuY: 0 }"
                                                draggable="{{ $entry['type'] === 'drive' ? 'false' : 'true' }}"
                                                x-on:dragstart="startDrag($event, '{{ $pane['id'] }}', '{{ addslashes($entry['path']) }}', '{{ addslashes($entry['name']) }}', '{{ $entry['type'] }}')"
                                                x-on:dragend="endDrag()"
                                                @if (in_array($entry['type'], ['dir', 'drive']))
                                                    x-on:dragover.prevent.stop="dropTarget = '{{ addslashes($entry['path']) }}'"
                                                    x-on:dragleave.stop="dropTarget === '{{ addslashes($entry['path']) }}' && (dropTarget = null)"
                                                    x-on:drop.prevent.stop="handleDrop($event, '{{ $pane['id'] }}', '{{ addslashes($entry['path']) }}')"
                                                @endif
                                                x-on:contextmenu.prevent="menu = true; menuX = Math.min($event.clientX, window.innerWidth - 170); menuY = Math.min($event.clientY, window.innerHeight - 150)"
                                                :style="dropTarget === '{{ addslashes($entry['path']) }}' ? 'background-color: {{ $machine?->color }}1f' : ''"
                                                class="relative border-b border-stone-50 hover:bg-stone-50/80 group"
                                            >
                                                <td class="px-3 py-2">
                                                    <div class="flex items-center gap-2 min-w-0">
                                                        <x-explorer.file-icon :type="$entry['type']" :name="$entry['name']" />

                                                        @if ($renamingPaneId === $pane['id'] && $renamingPath === $entry['path'])
                                                            <input
                                                                type="text"
                                                                wire:model="renameValue"
                                                                wire:keydown.enter="confirmRename"
                                                                wire:keydown.escape="cancelRename"
                                                                x-init="$nextTick(() => $el.select())"
                                                                class="text-sm border-tan-400 rounded px-1 py-0.5 w-full focus:ring-tan-400"
                                                            >
                                                            <button wire:click="confirmRename" class="text-tan-600 shrink-0">
                                                                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/></svg>
                                                            </button>
                                                        @elseif ($entry['type'] === 'dir' || $entry['type'] === 'drive')
                                                            <button wire:click="navigate('{{ $pane['id'] }}', '{{ addslashes($entry['path']) }}')" class="truncate text-stone-700 hover:text-tan-700 text-left">
                                                                {{ $entry['name'] }}
                                                            </button>
                                                        @else
                                                            <span class="truncate text-stone-700">{{ $entry['name'] }}</span>
                                                        @endif
                                                    </div>
                                                </td>
                                                <td class="px-3 py-2 text-right text-stone-400 text-xs whitespace-nowrap">
                                                    {{ $entry['size'] !== null ? human_filesize($entry['size']) : '—' }}
                                                </td>
                                                <td class="px-3 py-2 text-right relative">
                                                    <button x-on:click="menu = !menu; menuX = Math.min($event.clientX, window.innerWidth - 170); menuY = Math.min($event.clientY, window.innerHeight - 150)" class="p-1 rounded hover:bg-stone-200 text-stone-400 opacity-0 group-hover:opacity-100 transition">
                                                        <svg class="w-4 h-4" fill="currentColor" viewBox="0 0 20 20"><path d="M10 6a2 2 0 110-4 2 2 0 010 4zm0 6a2 2 0 110-4 2 2 0 010 4zm0 6a2 2 0 110-4 2 2 0 010 4z"/></svg>
                                                    </button>
                                                    <div x-show="menu" @click.outside="menu = false" x-transition x-cloak :style="'top: ' + menuY + 'px; left: ' + menuX + 'px;'" class="fixed w-40 bg-paper rounded-lg shadow-panel ring-1 ring-stone-200 py-1 z-40 text-left">
                                                        @if ($entry['type'] === 'file')
                                                            <button x-on:click="openPreview({{ $pane['machine_id'] }}, '{{ addslashes($entry['path']) }}', '{{ addslashes($entry['name']) }}'); menu = false" class="w-full text-left px-3 py-1.5 text-sm text-stone-700 hover:bg-stone-50">Preview</button>
                                                            <button wire:click="downloadEntry('{{ $pane['id'] }}', '{{ addslashes($entry['path']) }}')" x-on:click="menu = false" class="w-full text-left px-3 py-1.5 text-sm text-stone-700 hover:bg-stone-50">Download</button>
                                                        @endif
                                                        @if ($entry['type'] !== 'drive' && $canWrite)
                                                            <button wire:click="startRename('{{ $pane['id'] }}', '{{ addslashes($entry['path']) }}')" x-on:click="menu = false" class="w-full text-left px-3 py-1.5 text-sm text-stone-700 hover:bg-stone-50">Rename</button>
                                                            <button wire:click="deleteEntry('{{ $pane['id'] }}', '{{ addslashes($entry['path']) }}')" wire:confirm="Delete {{ addslashes($entry['name']) }}?" x-on:click="menu = false" class="w-full text-left px-3 py-1.5 text-sm text-red-600 hover:bg-red-50">Delete</button>
                                                        @endif
                                                    </div>
                                                </td>
                                            </tr>
                                        @endforeach
                                    </tbody>
                                </table>
                            @else
                                <div class="grid grid-cols-3 gap-3 p-3">
                                    @foreach ($entries as $entry)
                                        <div
                                            wire:key="grid-{{ $pane['id'] }}-{{ $entry['path'] }}"
                                            draggable="{{ $entry['type'] === 'drive' ? 'false' : 'true' }}"
                                            x-on:dragstart="startDrag($event, '{{ $pane['id'] }}', '{{ addslashes($entry['path']) }}', '{{ addslashes($entry['name']) }}', '{{ $entry['type'] }}')"
                                            x-on:dragend="endDrag()"
                                            @if (in_array($entry['type'], ['dir', 'drive']))
                                                x-on:dragover.prevent.stop="dropTarget = '{{ addslashes($entry['path']) }}'"
                                                x-on:dragleave.stop="dropTarget === '{{ addslashes($entry['path']) }}' && (dropTarget = null)"
                                                x-on:drop.prevent.stop="handleDrop($event, '{{ $pane['id'] }}', '{{ addslashes($entry['path']) }}')"
                                            @endif
                                            :style="dropTarget === '{{ addslashes($entry['path']) }}' ? 'background-color: {{ $machine?->color }}1f; box-shadow: inset 0 0 0 1px {{ $machine?->color }}' : ''"
                                            @if ($entry['type'] === 'dir' || $entry['type'] === 'drive')
                                                wire:click="navigate('{{ $pane['id'] }}', '{{ addslashes($entry['path']) }}')"
                                            @endif
                                            class="flex flex-col items-center gap-1.5 p-3 rounded-lg hover:bg-stone-50 cursor-pointer text-center"
                                        >
                                            @if ($entry['type'] === 'file' && is_previewable_image($entry['name']))
                                                <img
                                                    src="{{ route('preview', $pane['machine_id']) }}?path={{ urlencode($entry['path']) }}"
                                                    alt=""
                                                    loading="lazy"
                                                    class="w-12 h-12 object-cover rounded-md border border-stone-200 bg-stone-50"
                                                >
                                            @else
                                                <x-explorer.file-icon :type="$entry['type']" :name="$entry['name']" size="lg" />
                                            @endif
                                            <span class="text-xs text-stone-600 truncate w-full">{{ $entry['name'] }}</span>
                                        </div>
                                    @endforeach
                                </div>
                            @endif
                        </div>
                    </div>
                @endforeach
            </div>
        @endif
    </div>
</div>
