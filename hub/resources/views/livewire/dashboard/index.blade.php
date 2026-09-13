<div>
    <div class="bg-paper border-b border-stone-200">
        <div class="max-w-[1600px] mx-auto py-6 px-4 sm:px-6 lg:px-8 flex items-center justify-between">
            <div>
                <h2 class="font-semibold text-xl text-stone-800">Dashboard</h2>
                <p class="text-sm text-stone-500 mt-1">Live status and storage across every registered machine.</p>
            </div>
            <button wire:click="refresh" wire:loading.attr="disabled" wire:target="refresh" class="inline-flex items-center gap-2 px-4 py-2 bg-tan-500 hover:bg-tan-600 text-white text-sm font-medium rounded-lg shadow-soft transition disabled:opacity-60">
                <svg wire:loading.class="animate-spin" wire:target="refresh" class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/></svg>
                <span wire:loading.remove wire:target="refresh">Refresh</span>
                <span wire:loading wire:target="refresh">Refreshing…</span>
            </button>
        </div>
    </div>

    <div class="max-w-[1600px] mx-auto py-8 px-4 sm:px-6 lg:px-8">
        @if ($machines->isEmpty())
            <div class="rounded-xl border border-dashed border-stone-300 bg-paper p-12 text-center">
                <p class="text-stone-500">
                    No machines registered yet. <a href="{{ route('machines') }}" wire:navigate class="text-tan-600 underline">Add one</a> to see it here.
                </p>
            </div>
        @else
            <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-5">
                @foreach ($machines as $machine)
                    @php $info = $status[$machine->id] ?? ['status' => 'unknown', 'hostname' => null, 'drives' => [], 'error' => null]; @endphp
                    <div class="rounded-xl bg-paper shadow-soft border border-stone-200 overflow-hidden flex flex-col">
                        <div class="flex items-center justify-between px-5 py-4 border-b border-stone-100" style="background: linear-gradient(to right, {{ $machine->color }}14, transparent)">
                            <div class="flex items-center gap-3 min-w-0">
                                <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-white font-semibold text-sm" style="background-color: {{ $machine->color }}">
                                    {{ strtoupper(substr($machine->name, 0, 1)) }}
                                </span>
                                <div class="min-w-0">
                                    <p class="font-medium text-stone-800 truncate">{{ $machine->name }}</p>
                                    <p class="text-xs text-stone-400 capitalize">{{ $machine->os }}</p>
                                </div>
                            </div>
                            <span @class([
                                'inline-flex items-center gap-1.5 text-xs font-medium px-2 py-1 rounded-full shrink-0',
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

                        <div class="px-5 py-4 grid grid-cols-2 gap-4 border-b border-stone-100">
                            <div>
                                <p class="text-xs text-stone-400 uppercase tracking-wide">Computer Name</p>
                                <p class="text-sm text-stone-700 font-medium truncate">{{ $info['hostname'] ?? '—' }}</p>
                            </div>
                            <div>
                                <p class="text-xs text-stone-400 uppercase tracking-wide">LAN IP</p>
                                <p class="text-sm text-stone-700 font-medium truncate">{{ $machine->host }}:{{ $machine->port }}</p>
                            </div>
                        </div>

                        <div class="px-5 py-4 flex-1">
                            @if ($info['status'] !== 'online')
                                <p class="text-sm text-stone-400 text-center py-4">
                                    {{ $info['error'] ?? 'Drive information unavailable.' }}
                                </p>
                            @elseif (empty($info['drives']))
                                <p class="text-sm text-stone-400 text-center py-4">No drives reported.</p>
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
                    </div>
                @endforeach
            </div>
        @endif
    </div>
</div>
