<div wire:poll.2s x-data="{ dismissed: false }">
    @if ($jobs->isNotEmpty())
        <div x-show="!dismissed" x-cloak class="fixed bottom-4 right-4 z-40 w-80 max-h-96 overflow-y-auto rounded-xl bg-paper shadow-panel ring-1 ring-stone-200 divide-y divide-stone-100">
            <div class="px-4 py-2 flex items-center justify-between text-xs font-semibold text-stone-500 uppercase tracking-wide bg-stone-50 rounded-t-xl">
                Transfers
                <button type="button" x-on:click="dismissed = true" aria-label="Close" class="text-stone-400 hover:text-stone-600 normal-case">
                    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/></svg>
                </button>
            </div>
            @foreach ($jobs as $i => $job)
                <div wire:key="transfer-{{ $i }}" class="px-4 py-2.5 text-xs">
                    <div class="flex items-center justify-between gap-2 mb-1">
                        <span class="flex items-center gap-1.5 min-w-0">
                            <span class="w-1.5 h-1.5 rounded-full shrink-0" style="background-color: {{ $job['color'] }}"></span>
                            <span class="truncate text-stone-700">{{ $job['label'] }}</span>
                        </span>
                        <span @class([
                            'shrink-0 font-medium',
                            'text-stone-400' => $job['status'] === 'pending',
                            'text-tan-600' => $job['status'] === 'running',
                            'text-green-600' => $job['status'] === 'done',
                            'text-red-600' => $job['status'] === 'error',
                        ])>{{ ucfirst($job['status']) }}</span>
                    </div>
                    <div class="h-1.5 rounded-full bg-stone-100 overflow-hidden">
                        <div class="h-full bg-tan-400 transition-all" style="width: {{ $job['percent'] }}%"></div>
                    </div>
                    @if ($job['error'])
                        <p class="text-red-500 mt-1 truncate" title="{{ $job['error'] }}">{{ $job['error'] }}</p>
                    @endif
                </div>
            @endforeach
        </div>
    @endif
</div>
