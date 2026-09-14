@php
    $osNames = ['windows' => 'Windows', 'linux' => 'Linux', 'macos' => 'macOS', 'other' => 'Other'];
@endphp

<div wire:poll.5s="pollNetwork">
    <div class="bg-paper border-b border-stone-200">
        <div class="max-w-[1600px] mx-auto py-6 px-4 sm:px-6 lg:px-8 flex items-center justify-between">
            <div>
                <h2 class="font-semibold text-xl text-stone-800">Dashboard</h2>
                <p class="text-sm text-stone-500 mt-1">Live status, storage, and throughput across every registered machine.</p>
            </div>
            <div class="flex items-center gap-2">
                <button wire:click="refresh" wire:loading.attr="disabled" wire:target="refresh" class="inline-flex items-center gap-2 px-4 py-2 bg-white hover:bg-stone-50 border border-stone-200 text-stone-600 text-sm font-medium rounded-lg shadow-soft transition disabled:opacity-60">
                    <svg wire:loading.class="animate-spin" wire:target="refresh" class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/></svg>
                    <span wire:loading.remove wire:target="refresh">Refresh</span>
                    <span wire:loading wire:target="refresh">Refreshing…</span>
                </button>
                <button wire:click="openCreate" class="inline-flex items-center gap-2 px-4 py-2 bg-tan-500 hover:bg-tan-600 text-white text-sm font-medium rounded-lg shadow-soft transition">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4"/></svg>
                    Add Machine
                </button>
            </div>
        </div>
    </div>

    <div class="max-w-[1600px] mx-auto py-8 px-4 sm:px-6 lg:px-8">
        @if ($machines->isEmpty())
            <div class="rounded-xl border border-dashed border-stone-300 bg-paper p-12 text-center">
                <p class="text-stone-500">No machines registered yet.</p>
                <button wire:click="openCreate" class="mt-4 inline-flex items-center gap-2 px-4 py-2 bg-tan-500 hover:bg-tan-600 text-white text-sm font-medium rounded-lg transition">
                    Add your first machine
                </button>
            </div>
        @else
            <div class="space-y-3">
                @foreach ($machines as $machine)
                    @php
                        $info = $status[$machine->id] ?? ['status' => $machine->status, 'hostname' => null, 'drives' => [], 'error' => null];
                        $speed = $speeds[$machine->id] ?? ['up' => 0.0, 'down' => 0.0, 'avg' => 0.0];
                        $scaleFloor = 1024 * 1024; // 1 MB/s — keeps idle machines' gauges resting near zero rather than jittering.
                        $scaleMax = max($speed['up'], $speed['down'], $speed['avg'], $scaleFloor) * 1.15;
                    @endphp
                    <div
                        x-data="{ open: false }"
                        class="rounded-xl bg-paper shadow-soft border border-stone-200 overflow-hidden"
                    >
                        {{-- Header: gradient wash stays identical whether the item is open or closed. --}}
                        <button
                            type="button"
                            x-on:click="open = ! open"
                            class="w-full flex items-center gap-4 px-5 py-4 text-left"
                            style="background: linear-gradient(to right, {{ $machine->color }}14, transparent)"
                        >
                            <div class="flex items-center gap-3 shrink-0">
                                <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-white font-semibold text-sm" style="background-color: {{ $machine->color }}">
                                    {{ strtoupper(substr($machine->name, 0, 1)) }}
                                </span>
                                <p class="font-medium text-stone-800 truncate max-w-[12rem]">{{ $machine->name }}</p>
                            </div>

                            {{-- Collapsed-only summary strip — stretches to fill the row, fades out as the item opens. --}}
                            <div
                                x-show="! open"
                                x-transition:enter="transition ease-out duration-200"
                                x-transition:enter-start="opacity-0"
                                x-transition:enter-end="opacity-100"
                                x-transition:leave="transition ease-in duration-150"
                                x-transition:leave-start="opacity-100"
                                x-transition:leave-end="opacity-0"
                                class="hidden sm:grid flex-1 grid-cols-4 gap-4 min-w-0 pl-4 ml-1 border-l border-stone-200"
                            >
                                <div class="min-w-0">
                                    <p class="text-[0.65rem] text-stone-400 uppercase tracking-wide">Computer Name</p>
                                    <p class="text-sm text-stone-700 font-medium truncate mt-0.5">{{ $info['hostname'] ?? '—' }}</p>
                                </div>
                                <div class="min-w-0">
                                    <p class="text-[0.65rem] text-stone-400 uppercase tracking-wide">Operating System</p>
                                    <p class="flex items-center gap-1.5 text-sm text-stone-700 font-medium truncate mt-0.5">
                                        <x-os-icon :os="$machine->os" class="w-3.5 h-3.5 shrink-0" />
                                        {{ $osNames[$machine->os] ?? ucfirst($machine->os) }}
                                    </p>
                                </div>
                                <div class="min-w-0">
                                    <p class="text-[0.65rem] text-stone-400 uppercase tracking-wide">LAN IP</p>
                                    <p class="text-sm text-stone-700 font-medium font-mono truncate mt-0.5">{{ $machine->host }}:{{ $machine->port }}</p>
                                </div>
                                <div class="min-w-0">
                                    <p class="text-[0.65rem] text-stone-400 uppercase tracking-wide">Status</p>
                                    <span @class([
                                        'inline-flex items-center gap-1.5 text-xs font-medium px-2 py-0.5 rounded-full mt-1',
                                        'bg-green-100 text-green-700' => $info['status'] === 'online',
                                        'bg-red-100 text-red-700' => $info['status'] === 'offline',
                                        'bg-stone-100 text-stone-500' => ! in_array($info['status'], ['online', 'offline']),
                                    ])>
                                        <span @class([
                                            'w-1.5 h-1.5 rounded-full',
                                            'bg-green-500' => $info['status'] === 'online',
                                            'bg-red-500' => $info['status'] === 'offline',
                                            'bg-stone-400' => ! in_array($info['status'], ['online', 'offline']),
                                        ])></span>
                                        {{ ucfirst($info['status']) }}
                                    </span>
                                </div>
                            </div>

                            <svg class="w-4 h-4 text-stone-400 shrink-0 transition-transform duration-200" :class="{ 'rotate-180': open }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
                            </svg>
                        </button>

                        {{-- Expanded body: everything the collapsed strip hid, plus drives and gauges. --}}
                        <div
                            x-show="open"
                            x-transition:enter="transition ease-out duration-200"
                            x-transition:enter-start="opacity-0"
                            x-transition:enter-end="opacity-100"
                            x-transition:leave="transition ease-in duration-150"
                            x-transition:leave-start="opacity-100"
                            x-transition:leave-end="opacity-0"
                            class="border-t border-stone-100"
                            style="display: none;"
                        >
                            <div class="px-5 py-5 flex flex-col sm:flex-row sm:items-stretch border-b border-stone-100 divide-y sm:divide-y-0 sm:divide-x divide-stone-100">
                                <div class="flex-1 py-2 sm:py-0 sm:px-4 first:sm:pl-0 last:sm:pr-0">
                                    <p class="text-xs text-stone-400 uppercase tracking-wide">Computer Name</p>
                                    <p class="text-lg text-stone-800 font-semibold truncate mt-1">{{ $info['hostname'] ?? '—' }}</p>
                                </div>
                                <div class="flex-1 py-2 sm:py-0 sm:px-4 first:sm:pl-0 last:sm:pr-0">
                                    <p class="text-xs text-stone-400 uppercase tracking-wide">Operating System</p>
                                    <p class="flex items-center gap-2 text-lg text-stone-800 font-semibold mt-1">
                                        <x-os-icon :os="$machine->os" class="w-5 h-5 shrink-0" />
                                        {{ $osNames[$machine->os] ?? ucfirst($machine->os) }}
                                    </p>
                                </div>
                                <div class="flex-1 py-2 sm:py-0 sm:px-4 first:sm:pl-0 last:sm:pr-0">
                                    <p class="text-xs text-stone-400 uppercase tracking-wide">LAN IP</p>
                                    <p class="text-lg text-stone-800 font-semibold font-mono truncate mt-1">{{ $machine->host }}:{{ $machine->port }}</p>
                                </div>
                                <div class="flex-1 py-2 sm:py-0 sm:px-4 first:sm:pl-0 last:sm:pr-0">
                                    <p class="text-xs text-stone-400 uppercase tracking-wide">Status</p>
                                    <span @class([
                                        'inline-flex items-center gap-1.5 text-sm font-medium px-2.5 py-1 rounded-full mt-1.5',
                                        'bg-green-100 text-green-700' => $info['status'] === 'online',
                                        'bg-red-100 text-red-700' => $info['status'] === 'offline',
                                        'bg-stone-100 text-stone-500' => ! in_array($info['status'], ['online', 'offline']),
                                    ])>
                                        <span @class([
                                            'w-1.5 h-1.5 rounded-full',
                                            'bg-green-500' => $info['status'] === 'online',
                                            'bg-red-500' => $info['status'] === 'offline',
                                            'bg-stone-400' => ! in_array($info['status'], ['online', 'offline']),
                                        ])></span>
                                        {{ ucfirst($info['status']) }}
                                    </span>
                                </div>
                            </div>

                            <div class="px-5 py-5 border-b border-stone-100">
                                <p class="text-xs text-stone-400 uppercase tracking-wide mb-3">Storage</p>
                                @if ($info['status'] !== 'online')
                                    <p class="text-sm text-stone-400 py-2">{{ $info['error'] ?? 'Drive information unavailable.' }}</p>
                                @elseif (empty($info['drives']))
                                    <p class="text-sm text-stone-400 py-2">No drives reported.</p>
                                @else
                                    <div class="space-y-3">
                                        @foreach ($info['drives'] as $drive)
                                            @php
                                                $total = $drive['total'] ?? 0;
                                                $free = $drive['free'] ?? 0;
                                                $used = max($total - $free, 0);
                                                $pct = $total > 0 ? round(($used / $total) * 100) : 0;
                                            @endphp
                                            <div>
                                                <div class="flex items-center justify-between text-sm mb-1">
                                                    <span class="font-medium text-stone-700">{{ $drive['label'] ?? $drive['path'] }}</span>
                                                    <span class="text-xs text-stone-400">{{ human_filesize($free) }} free of {{ human_filesize($total) }}</span>
                                                </div>
                                                <div class="h-1.5 rounded-full bg-stone-100 overflow-hidden">
                                                    <div @class([
                                                        'h-full rounded-full',
                                                        'bg-tan-500' => $pct < 80,
                                                        'bg-red-400' => $pct >= 80,
                                                    ]) style="width: {{ min($pct, 100) }}%"></div>
                                                </div>
                                            </div>
                                        @endforeach
                                    </div>
                                @endif
                            </div>

                            <div class="px-5 py-5 border-b border-stone-100">
                                <p class="text-xs text-stone-400 uppercase tracking-wide mb-3">Current Speed</p>
                                <div class="flex items-start gap-4">
                                    <x-gauge label="Upstream" :value-label="human_speed($speed['up'])" :percent="$scaleMax > 0 ? ($speed['up'] / $scaleMax) * 100 : 0" :color="$machine->color" />
                                    <x-gauge label="Downstream" :value-label="human_speed($speed['down'])" :percent="$scaleMax > 0 ? ($speed['down'] / $scaleMax) * 100 : 0" :color="$machine->color" />
                                    <x-gauge label="Average" :value-label="human_speed($speed['avg'])" :percent="$scaleMax > 0 ? ($speed['avg'] / $scaleMax) * 100 : 0" :color="$machine->color" />
                                </div>
                            </div>

                            @if ($machine->notes)
                                <p class="px-5 pt-4 text-sm text-stone-500">{{ $machine->notes }}</p>
                            @endif

                            <div class="px-5 py-4 flex items-center gap-2">
                                <button wire:click="refreshOne({{ $machine->id }})" wire:loading.attr="disabled" wire:target="refreshOne({{ $machine->id }})" class="text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-stone-600 hover:bg-stone-50 transition">
                                    <span wire:loading.remove wire:target="refreshOne({{ $machine->id }})">Ping</span>
                                    <span wire:loading wire:target="refreshOne({{ $machine->id }})">Checking…</span>
                                </button>
                                <a href="{{ route('explorer') }}?machine={{ $machine->id }}" wire:navigate class="text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-stone-600 hover:bg-stone-50 transition">
                                    Browse
                                </a>
                                <button wire:click="openEdit({{ $machine->id }})" class="text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-stone-600 hover:bg-stone-50 transition">
                                    Edit
                                </button>
                                <button wire:click="delete({{ $machine->id }})" wire:confirm="Remove {{ $machine->name }}? This cannot be undone." class="ml-auto text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-red-600 hover:bg-red-50 transition">
                                    Delete
                                </button>
                            </div>
                        </div>
                    </div>
                @endforeach
            </div>
        @endif
    </div>

    <x-modal name="machine-form" :show="$errors->isNotEmpty()" maxWidth="lg">
        <form wire:submit="save" class="p-6">
            <h3 class="text-lg font-semibold text-stone-800 mb-4">
                {{ $editingId ? 'Edit machine' : 'Add a machine' }}
            </h3>

            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div class="sm:col-span-2">
                    <x-input-label for="name" value="Name" />
                    <x-text-input id="name" wire:model="name" class="mt-1 block w-full" placeholder="Desktop — Office" />
                    <x-input-error :messages="$errors->get('name')" class="mt-1" />
                </div>

                <div>
                    <x-input-label for="host" value="Host / IP" />
                    <x-text-input id="host" wire:model="host" class="mt-1 block w-full" placeholder="192.168.1.42" />
                    <x-input-error :messages="$errors->get('host')" class="mt-1" />
                </div>

                <div>
                    <x-input-label for="port" value="Port" />
                    <x-text-input id="port" type="number" wire:model="port" class="mt-1 block w-full" />
                    <x-input-error :messages="$errors->get('port')" class="mt-1" />
                </div>

                <div>
                    <x-input-label for="os" value="Operating system" />
                    <select id="os" wire:model="os" class="mt-1 block w-full border-stone-300 rounded-md shadow-sm focus:border-tan-400 focus:ring-tan-400">
                        <option value="windows">Windows</option>
                        <option value="linux">Linux</option>
                        <option value="macos">macOS</option>
                        <option value="other">Other</option>
                    </select>
                </div>

                <div>
                    <x-input-label for="color" value="Color tag" />
                    <input id="color" type="color" wire:model="color" class="mt-1 block w-full h-10 border-stone-300 rounded-md shadow-sm" />
                </div>

                <div class="sm:col-span-2">
                    <div class="flex items-center justify-between">
                        <x-input-label for="agentToken" :value="$editingId ? 'Agent token (leave blank to keep current)' : 'Agent token'" />
                        <button type="button" wire:click="suggestToken" class="text-xs text-tan-600 hover:text-tan-700">Generate</button>
                    </div>
                    <x-text-input id="agentToken" wire:model="agentToken" class="mt-1 block w-full font-mono text-sm" placeholder="Paste into the agent's config.json" />
                    <x-input-error :messages="$errors->get('agentToken')" class="mt-1" />
                </div>

                <div class="sm:col-span-2 flex items-center gap-2">
                    <input id="useTls" type="checkbox" wire:model="useTls" class="rounded border-stone-300 text-tan-500 focus:ring-tan-400">
                    <x-input-label for="useTls" value="Use HTTPS to reach the agent" class="!mb-0" />
                </div>

                <div class="sm:col-span-2">
                    <x-input-label for="notes" value="Notes" />
                    <textarea id="notes" wire:model="notes" rows="2" class="mt-1 block w-full border-stone-300 rounded-md shadow-sm focus:border-tan-400 focus:ring-tan-400"></textarea>
                </div>

                @php $shareableUsers = $this->shareableUsers(); @endphp
                @if ($shareableUsers->isNotEmpty())
                    <div class="sm:col-span-2">
                        <x-input-label value="Shared with" />
                        <p class="text-xs text-stone-500 mt-0.5 mb-2">You always have full access. Grant other users read-only or full access to this machine.</p>
                        <div class="space-y-2">
                            @foreach ($shareableUsers as $user)
                                <div class="flex items-center justify-between gap-3 text-sm">
                                    <span class="text-stone-700">{{ $user->name }}</span>
                                    <select wire:model="shareRoles.{{ $user->id }}" class="border-stone-300 rounded-md shadow-sm text-xs focus:border-tan-400 focus:ring-tan-400">
                                        <option value="none">No access</option>
                                        <option value="read_only">Read-only</option>
                                        <option value="full">Full access</option>
                                    </select>
                                </div>
                            @endforeach
                        </div>
                    </div>
                @endif
            </div>

            <div class="mt-6 flex justify-end gap-3">
                <button type="button" x-on:click="$dispatch('close-modal', 'machine-form')" class="px-4 py-2 text-sm font-medium text-stone-600 hover:text-stone-800">Cancel</button>
                <x-primary-button>{{ $editingId ? 'Save changes' : 'Add machine' }}</x-primary-button>
            </div>
        </form>
    </x-modal>
</div>
