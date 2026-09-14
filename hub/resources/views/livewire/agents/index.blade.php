<div>
    <div class="bg-paper border-b border-stone-200">
        <div class="max-w-[1600px] mx-auto py-6 px-4 sm:px-6 lg:px-8 flex items-center justify-between">
            <div>
                <h2 class="font-semibold text-xl text-stone-800">Agents</h2>
                <p class="text-sm text-stone-500 mt-1">Live health and transfer throughput for every machine's agent.</p>
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
                    No machines registered yet. <a href="{{ route('machines') }}" wire:navigate class="text-tan-600 underline">Add one</a> to get started.
                </p>
            </div>
        @else
            <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-5">
                @foreach ($machines as $machine)
                    @php $s = $stats[$machine->id]; @endphp
                    <div class="rounded-xl bg-paper shadow-soft border border-stone-200 p-5 flex flex-col gap-4">
                        <div class="flex items-start justify-between">
                            <div class="flex items-center gap-3">
                                <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-white font-semibold" style="background-color: {{ $machine->color }}">
                                    {{ strtoupper(substr($machine->name, 0, 1)) }}
                                </span>
                                <div>
                                    <p class="font-medium text-stone-800">{{ $machine->name }}</p>
                                    <p class="text-xs text-stone-500">{{ $machine->host }}:{{ $machine->port }}</p>
                                </div>
                            </div>
                            <span @class([
                                'inline-flex items-center gap-1.5 text-xs font-medium px-2 py-1 rounded-full',
                                'bg-green-100 text-green-700' => $machine->isOnline(),
                                'bg-red-100 text-red-700' => ! $machine->isOnline(),
                            ])>
                                <span @class([
                                    'w-1.5 h-1.5 rounded-full',
                                    'bg-green-500' => $machine->isOnline(),
                                    'bg-red-500' => ! $machine->isOnline(),
                                ])></span>
                                {{ $machine->isOnline() ? 'Online' : 'Offline' }}
                            </span>
                        </div>

                        @if ($machine->last_seen_at)
                            <p class="text-xs text-stone-400 -mt-2">Last seen {{ $machine->last_seen_at->diffForHumans() }}</p>
                        @endif

                        <div class="grid grid-cols-2 gap-3 pt-3 border-t border-stone-100">
                            <div>
                                <p class="text-[10px] uppercase tracking-wide text-stone-400 flex items-center gap-1">
                                    <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 19V5m0 0l-6 6m6-6l6 6"/></svg>
                                    Sent
                                </p>
                                <p class="text-sm font-medium text-stone-800">{{ human_filesize($s['sent_bytes']) }}</p>
                                <p class="text-[11px] text-stone-400">avg {{ human_speed($s['avg_up_speed']) }}</p>
                            </div>
                            <div>
                                <p class="text-[10px] uppercase tracking-wide text-stone-400 flex items-center gap-1">
                                    <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 5v14m0 0l-6-6m6 6l6-6"/></svg>
                                    Received
                                </p>
                                <p class="text-sm font-medium text-stone-800">{{ human_filesize($s['received_bytes']) }}</p>
                                <p class="text-[11px] text-stone-400">avg {{ human_speed($s['avg_down_speed']) }}</p>
                            </div>
                        </div>

                        @if ($s['active'] > 0)
                            <p class="text-xs text-tan-600 flex items-center gap-1.5">
                                <span class="w-1.5 h-1.5 rounded-full bg-tan-500 animate-pulse"></span>
                                {{ $s['active'] }} transfer{{ $s['active'] === 1 ? '' : 's' }} in progress
                            </p>
                        @endif

                        <a href="{{ route('agents.log', $machine) }}" wire:navigate class="mt-auto text-center text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-stone-600 hover:bg-stone-50 transition">
                            View transfer log
                        </a>
                    </div>
                @endforeach
            </div>
        @endif
    </div>
</div>
